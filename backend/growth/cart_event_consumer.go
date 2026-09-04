package growth

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/garinesaiajay/commerceos/commerce/catalog"
)

// CartEventConsumer is the real consumer promised by
// PLAN-03-PROACTIVE-GROWTH-AGENT.md §7 ("Once real downstream
// consumers exist for other reasons... a cart.item_added event could
// drive suggestion computation server-side/asynchronously instead of
// the client polling on mutation") -- item 42, P3,
// PLAN-06-ADDITIONAL-OPPORTUNITIES.md's own listing of this as a
// stretch goal, explicitly not required for §1-§6's fix.
//
// It reads "cart.item_added" events off the same Redis Stream
// events.StreamConsumer already persists to event_log, as a SEPARATE
// consumer group ("growth-suggestions-group" in main.go) -- Redis
// Streams delivers every message to every group independently, so this
// doesn't compete with, replace, or change events.StreamConsumer's own
// persist-and-ack behavior at all; both simply see the same stream.
//
// On each cart.item_added, it precomputes the same cart-based
// cross-sell recommendation POST /growth/suggest would compute on
// demand (reusing suggest.go's bestCandidate/buildSignals/
// heuristicEVInputs directly -- one scoring function, not a second
// implementation of it), so the recommendations row already exists
// (upserted under the same "rec_<cartID>_<productID>" ID
// GrowthAgent.EvaluateCandidate always uses) by the time a buyer's
// checkout UI actually triggers the synchronous endpoint. Given how
// cheap this scoring already is (an in-memory catalog scan, no LLM or
// network call), the honest value here is architectural, not a
// measured latency win: it's a genuine "proactive, not reactive"
// consumer, not a fake one that only logs.
//
// Deliberately does NOT run SuggestHandler.evaluate's frequency-cap
// check or call ImpressionStore.RecordImpression: nobody has been
// shown anything yet at this point, so counting this as an impression
// would corrupt the impressions-vs-acceptances metric PLAN-03 §8 /
// item 20 exists to make honest. It only ever writes the
// recommendations row itself via GrowthAgent.EvaluateCandidate -- the
// same unconditional Save the synchronous path already does even for
// a REJECT decision (impression recording is the thing that's
// conditional, not persistence).
type CartEventConsumer struct {
	client     *redis.Client
	stream     string
	group      string
	catalog    CatalogSearcher
	cart       CartReader
	agent      *GrowthAgent
	dismissals DismissalStore
}

func NewCartEventConsumer(
	client *redis.Client,
	stream string,
	group string,
	catalogSearcher CatalogSearcher,
	cartReader CartReader,
	agent *GrowthAgent,
	dismissals DismissalStore,
) *CartEventConsumer {
	return &CartEventConsumer{
		client:     client,
		stream:     stream,
		group:      group,
		catalog:    catalogSearcher,
		cart:       cartReader,
		agent:      agent,
		dismissals: dismissals,
	}
}

// Run mirrors events.StreamConsumer.Run's polling loop (same
// consumer-group-per-consumer shape, same 500ms/200ms cadence) --
// duplicated rather than shared because the two consumers persist
// genuinely different things (a raw event-log row vs. a scored cross-
// sell recommendation), and forcing both through one generic "handler
// function" abstraction would cost more clarity than the ~15
// duplicated lines are worth at this codebase's size. See
// events/stream_consumer.go's own Run for the original.
func (c *CartEventConsumer) Run(ctx context.Context) error {
	// Ignore the AlreadyExists error on every run after the first --
	// same as events.StreamConsumer.Run.
	_ = c.client.XGroupCreateMkStream(ctx, c.stream, c.group, "$").Err()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-time.After(500 * time.Millisecond):
			c.consumeBatch(ctx)
		}
	}
}

func (c *CartEventConsumer) consumeBatch(ctx context.Context) {
	msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    c.group,
		Consumer: "growth-suggestion-consumer",
		Streams:  []string{c.stream, ">"},
		Count:    10,
		Block:    200 * time.Millisecond,
	}).Result()
	if err != nil {
		// No messages / timeout -- fine.
		return
	}

	for _, stream := range msgs {
		for _, msg := range stream.Messages {
			eventType, _ := msg.Values["event_type"].(string)

			// Every other event type on this shared stream
			// (payment.captured/payment.failed today) is simply not
			// this consumer group's job -- ack and move on rather
			// than leaving it permanently pending in this group.
			if eventType == "cart.item_added" {
				payload, _ := msg.Values["payload"].(string)
				c.handleCartItemAdded(ctx, payload)
			}

			_, _ = c.client.XAck(ctx, c.stream, c.group, msg.ID).Result()
		}
	}
}

type cartItemAddedPayload struct {
	CartID string `json:"cart_id"`
}

func (c *CartEventConsumer) handleCartItemAdded(ctx context.Context, rawPayload string) {
	var payload cartItemAddedPayload
	if err := json.Unmarshal([]byte(rawPayload), &payload); err != nil || payload.CartID == "" {
		log.Printf("[growth:cart-event-consumer] malformed cart.item_added payload: %q", rawPayload)
		return
	}

	if err := c.precomputeSuggestion(ctx, payload.CartID); err != nil {
		// Best-effort by design (see the type doc comment above) -- the
		// synchronous /growth/suggest path recomputes correctly
		// regardless of whether this ever ran, so a failure here is
		// only ever a missed optimization, never a buyer-facing error.
		log.Printf("[growth:cart-event-consumer] precompute for cart %s: %v", payload.CartID, err)
	}
}

// precomputeSuggestion mirrors SuggestHandler.Suggest's own cart-based
// scoring (buildSignals over every product in the cart, bestCandidate
// against the catalog, excluding items already in the cart and
// anything dismissed for it) but stops short of
// SuggestHandler.evaluate -- no frequency cap, no impression
// recording, just GrowthAgent.EvaluateCandidate's scoring + persist.
func (c *CartEventConsumer) precomputeSuggestion(ctx context.Context, cartID string) error {
	cartState, err := c.cart.GetCart(ctx, cartID)
	if err != nil {
		return fmt.Errorf("get cart: %w", err)
	}
	if len(cartState.Items) == 0 {
		return nil
	}

	exclude := make(map[string]bool, len(cartState.Items))

	var cartProducts []catalog.Product

	for _, item := range cartState.Items {
		exclude[item.ProductID] = true

		product, err := c.catalog.GetProduct(ctx, item.ProductID)
		if err != nil {
			continue // a stale/removed product shouldn't fail the whole precompute
		}
		cartProducts = append(cartProducts, product)
	}

	if c.dismissals != nil {
		dismissedIDs, err := c.dismissals.ListDismissedProductIDs(ctx, cartID)
		if err != nil {
			return fmt.Errorf("list dismissals: %w", err)
		}
		for _, id := range dismissedIDs {
			exclude[id] = true
		}
	}

	catalogProducts, err := c.catalog.ListProducts(ctx)
	if err != nil {
		return fmt.Errorf("list catalog: %w", err)
	}

	best, ok := bestCandidate(catalogProducts, cartState.MerchantID, buildSignals(cartProducts...), exclude)
	if !ok {
		return nil
	}

	_, err = c.agent.EvaluateCandidate(
		ctx,
		cartID,
		cartState.Subtotal,
		BudgetCheck{CartTotal: cartState.Subtotal, Budget: DemoBudgetCeiling, Tolerance: DemoBudgetTolerance},
		best.product.ID,
		heuristicEVInputs(best.product.Price.Amount, best.score, best.product.AverageRating),
	)
	if err != nil {
		return fmt.Errorf("evaluate candidate: %w", err)
	}

	return nil
}
