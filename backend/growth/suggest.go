package growth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/garinesaiajay/commerceos/commerce/cart"
	"github.com/garinesaiajay/commerceos/commerce/catalog"
	"github.com/garinesaiajay/commerceos/commerce/order"
)

// CartReader is the cart surface SuggestHandler needs.
type CartReader interface {
	GetCart(ctx context.Context, id string) (cart.Cart, error)
}

// OrderReader is the order surface SuggestForOrder needs -- just enough
// to read back a completed order's line items and totals. GET
// /orders/{id} is already reachable without login so checkout.tsx can
// show the buyer their own order (P0.4); SuggestForOrder reuses that
// same "no separate buyer identity yet" posture (files/AUTH.md).
type OrderReader interface {
	GetOrder(ctx context.Context, orderID string) (order.Order, error)
}

// CatalogSearcher is the catalog surface SuggestHandler needs -- a
// superset of CatalogReader (GrowthAgent's own dependency) that adds
// ListProducts so candidates can be searched, not just looked up by ID.
type CatalogSearcher interface {
	CatalogReader
	ListProducts(ctx context.Context) ([]catalog.Product, error)
}

// DismissalStore persists and reads back per-cart "no thanks" responses
// to a suggestion, so a dismissed product isn't proposed again for the
// same cart. Implemented by *PostgresStore (postgres_store.go).
type DismissalStore interface {
	SaveDismissal(ctx context.Context, cartID, productID string) error
	ListDismissedProductIDs(ctx context.Context, cartID string) ([]string, error)
}

// ImpressionStore records and reads back suggestion impressions and
// acceptances (PLAN-03-PROACTIVE-GROWTH-AGENT.md §6, §8, item 20).
// Implemented by *PostgresStore. Wired in via WithImpressions rather
// than the constructor -- like DismissalStore's own nil-safety, an
// unwired SuggestHandler (an older caller, or a test) just means no
// frequency cap is enforced and no impressions/acceptances are
// recorded, not an error.
type ImpressionStore interface {
	RecordImpression(ctx context.Context, cartID, productID string) error
	CountRecentImpressions(ctx context.Context, cartID string, since time.Time) (int, error)
	RecordAcceptance(ctx context.Context, cartID, productID string) error
}

// SuggestionFrequencyCapCount/-Window (PLAN-03 §6): a buyer session
// sees at most this many suggestion impressions, across every
// /growth/suggest* surface COMBINED, within this rolling window -- a
// deliberate quality-over-quantity choice ("helpful, not annoying" is
// the user's own framing) so item 19's extra surfaces (product-detail,
// post-checkout) don't turn cross-sell into the nagging pattern those
// surfaces were explicitly designed to avoid.
const (
	SuggestionFrequencyCapCount  = 2
	SuggestionFrequencyCapWindow = 10 * time.Minute
)

// Demo-scoped budget defaults. This project has exactly one mandate
// (mandate_demo, db/seeds/001_catalog.sql, maximum_amount 3000000 paise
// = ₹30,000) and the growth agent has no per-buyer mandate lookup wired
// to it, so SuggestHandler checks candidates against that same ceiling.
// A multi-mandate deployment would look the buyer's real mandate up
// instead of using a package constant.
const (
	DemoBudgetCeiling   int64   = 3_000_000
	DemoBudgetTolerance float64 = 0.10
)

// Heuristic EV inputs. These are a documented, deterministic placeholder
// -- NOT machine-learned and NOT historical purchase data, because this
// project has no purchase-history table yet. They exist so the real EV
// formula (growth/ev.go, ExpectedValue) has real numbers to run instead
// of a caller fabricating purchase_probability/incremental_margin per
// request, which is what every other entry point into GrowthAgent
// (POST /growth/evaluate, the recommend_bundle MCP tool) still requires
// today. Swap heuristicEVInputs for a real model once purchase history
// exists; EvaluateCandidate and the EV formula itself don't change.
const (
	heuristicBaseProbability = 0.15
	heuristicProbabilityStep = 0.12
	heuristicMaxProbability  = 0.65
	heuristicMarginFraction  = 0.40 // assumed gross margin on the candidate's price
	heuristicConfidence      = 0.70
)

// heuristicRatingNeutral/-Step (PLAN-02-CATALOG-AND-COMMERCE.md §2) turn
// a candidate's real average_rating into a small, deterministic nudge
// on the tag-overlap probability estimate: a 4.8-rated accessory is a
// more defensible suggestion than a 3.1-rated one at equal tag overlap.
// 4.0 is neutral (no adjustment) rather than 0 or 5, since this
// catalog's real seeded ratings (db/seeds/003_reviews.sql) cluster in
// the 3-5 range -- a 0-anchored neutral point would make every rated
// product look artificially good. Capped small (at most +/-0.05 per
// full star away from neutral) so a single 1-star review can't swing
// the estimate on its own.
const (
	heuristicRatingNeutral = 4.0
	heuristicRatingStep    = 0.05
)

// heuristicRatingAdjustment returns 0 for an unrated candidate
// (average_rating == 0, i.e. review_count == 0) -- absence of review
// data is not evidence of a bad product, so it must never be scored as
// worse than a middling-rated one.
func heuristicRatingAdjustment(averageRating float64) float64 {
	if averageRating <= 0 {
		return 0
	}
	return (averageRating - heuristicRatingNeutral) * heuristicRatingStep
}

// heuristicEVInputs turns a content-overlap score and a candidate's
// real average_rating into EV inputs. Higher overlap with what's
// already in the cart, and a better-than-neutral rating, both push
// purchase probability up; both are capped, together, at
// heuristicMaxProbability.
func heuristicEVInputs(candidatePrice int64, overlapScore int, averageRating float64) EVInputs {
	prob := heuristicBaseProbability +
		heuristicProbabilityStep*float64(overlapScore) +
		heuristicRatingAdjustment(averageRating)
	if prob > heuristicMaxProbability {
		prob = heuristicMaxProbability
	}
	if prob < 0 {
		prob = 0
	}
	return EVInputs{
		PurchaseProbability: prob,
		IncrementalMargin:   int64(float64(candidatePrice) * heuristicMarginFraction),
		Confidence:          heuristicConfidence,
		RiskCost:            0,
	}
}

// SuggestHandler serves POST /growth/suggest (cart-based), POST
// /growth/suggest/product (product-detail-based, item 19 /
// PLAN-03-PROACTIVE-GROWTH-AGENT.md §3), and POST /growth/suggest/order
// (post-checkout-based, §4). All three pick the single best
// complementary product -- by content overlap with a signal set (a
// cart's aggregate tags, one viewed product's own tags, or a completed
// order's aggregate tags) -- score it with the same deterministic EV
// formula /growth/evaluate uses, and run it through
// GrowthAgent.EvaluateCandidate so the budget check and persistence
// path are identical no matter which surface triggered it (the
// resulting recommendation is saved to the same table the dashboard's
// ai_revenue metric already joins on via recommendations.cart_id). None
// of the three ever recommends when nothing in the catalog shares a
// signal with the input -- a safe no-op, the same posture
// agents.IntentExtractor takes on an ambiguous prompt (see
// agents/intent.go's Clarify field) -- rather than guessing at an
// unrelated product.
type SuggestHandler struct {
	catalog     CatalogSearcher
	cart        CartReader
	orders      OrderReader
	agent       *GrowthAgent
	dismissals  DismissalStore
	impressions ImpressionStore
}

func NewSuggestHandler(catalog CatalogSearcher, cartReader CartReader, orders OrderReader, agent *GrowthAgent, dismissals DismissalStore) *SuggestHandler {
	return &SuggestHandler{catalog: catalog, cart: cartReader, orders: orders, agent: agent, dismissals: dismissals}
}

// WithImpressions wires in frequency-cap enforcement and impression/
// acceptance recording (item 20). Matches this codebase's WithX
// convention for an optional capability (agents.BuyerAgent.
// WithConversationStore, policy.Engine.WithProductExistsFunc,
// payment.Handler.WithCallCounter) -- every existing caller of
// NewSuggestHandler keeps compiling unchanged; main.go is the only one
// that needs to also call this to turn the capability on.
func (h *SuggestHandler) WithImpressions(impressions ImpressionStore) *SuggestHandler {
	h.impressions = impressions
	return h
}

// SuggestedProduct carries just enough catalog detail to render the
// upsell card without a second round trip.
type SuggestedProduct struct {
	ProductID string `json:"product_id"`
	Title     string `json:"title"`
	Price     int64  `json:"price"`
	Currency  string `json:"currency"`
}

// SuggestResponse is the wire shape returned to the frontend.
type SuggestResponse struct {
	Available      bool              `json:"available"`
	Recommendation *Recommendation   `json:"recommendation,omitempty"`
	Product        *SuggestedProduct `json:"product,omitempty"`
	Message        string            `json:"message,omitempty"`
}

// scoredCandidate is a catalog product paired with its content-overlap
// score against a signal set -- shared between all three suggestion
// entry points below.
type scoredCandidate struct {
	product catalog.Product
	score   int
}

// buildSignals aggregates use_cases/compatibility/features tags across
// one or more products into a single signal set. The cart-based
// endpoint passes every product currently in the cart; the
// product-detail endpoint passes just the one viewed product; the
// post-checkout endpoint passes every product in the completed order.
func buildSignals(products ...catalog.Product) map[string]bool {
	signals := make(map[string]bool)
	for _, product := range products {
		for _, tag := range product.UseCases {
			signals[tag] = true
		}
		for _, tag := range product.Compatibility {
			signals[tag] = true
		}
		for _, tag := range product.Features {
			signals[tag] = true
		}
	}
	return signals
}

// bestCandidate scores every catalog product against signals, skipping
// anything in exclude, from a different merchant, or currently out of
// stock, and returns the highest-scoring match. Ties break toward the
// cheaper item -- a smaller, more plausible add-on beats an equally
// relevant but expensive one. This is the one scoring function shared
// by all three /growth/suggest* entry points (PLAN-03-PROACTIVE-GROWTH-
// AGENT.md §3: "One scoring function, ... call sites -- no duplicated
// logic").
func bestCandidate(catalogProducts []catalog.Product, merchantID string, signals map[string]bool, exclude map[string]bool) (scoredCandidate, bool) {
	var candidates []scoredCandidate

	for _, product := range catalogProducts {
		if exclude[product.ID] || product.Availability <= 0 || product.Merchant.ID != merchantID {
			continue
		}
		score := 0
		for _, tag := range product.UseCases {
			if signals[tag] {
				score++
			}
		}
		for _, tag := range product.Compatibility {
			if signals[tag] {
				score++
			}
		}
		for _, tag := range product.Features {
			if signals[tag] {
				score++
			}
		}
		if score > 0 {
			candidates = append(candidates, scoredCandidate{product: product, score: score})
		}
	}

	if len(candidates) == 0 {
		return scoredCandidate{}, false
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].product.Price.Amount < candidates[j].product.Price.Amount
	})

	return candidates[0], true
}

// loadDismissed reads back every product previously dismissed for id
// (a cart_id in every current caller -- the product-detail and
// post-checkout endpoints both key dismissals by the same cart_id the
// buyer's session already has, so a "No thanks" on one surface holds
// across all of them, not just the surface it was given on). A missing
// dismissals store (e.g. an older wiring or a test) just means nothing
// is excluded, not an error.
func (h *SuggestHandler) loadDismissed(ctx context.Context, id string) (map[string]bool, error) {
	dismissed := make(map[string]bool)
	if h.dismissals == nil || id == "" {
		return dismissed, nil
	}
	ids, err := h.dismissals.ListDismissedProductIDs(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list dismissals: %w", err)
	}
	for _, productID := range ids {
		dismissed[productID] = true
	}
	return dismissed, nil
}

// evaluate runs the shared frequency-cap check + EV-scoring +
// budget-check + persistence + impression-recording step for a chosen
// candidate and turns the result into the wire response. cartID is the
// key every recommendation is stored and deduplicated under
// (recommendations.id is "rec_<cartID>_<productID>", upserted on
// re-evaluation) AND the key the frequency cap and impression log are
// scoped by -- every caller below passes a real cart_id so a product
// suggested from several surfaces during the same shopping session
// converges on one recommendation row and one shared impression
// budget, not three of each.
func (h *SuggestHandler) evaluate(ctx context.Context, cartID string, cartTotal int64, best scoredCandidate) (SuggestResponse, error) {
	// Frequency cap (PLAN-03-PROACTIVE-GROWTH-AGENT.md §6): checked
	// before spending an EvaluateCandidate call (and its Save) on a
	// suggestion that would just be suppressed anyway. A nil
	// ImpressionStore (unwired caller, or a test) means no cap is
	// enforced -- matches DismissalStore's own nil-safety above.
	if h.impressions != nil {
		since := time.Now().Add(-SuggestionFrequencyCapWindow)
		count, err := h.impressions.CountRecentImpressions(ctx, cartID, since)
		if err != nil {
			return SuggestResponse{}, err
		}
		if count >= SuggestionFrequencyCapCount {
			return SuggestResponse{
				Available: false,
				Message:   "reached the suggestion limit for this session -- check back in a few minutes",
			}, nil
		}
	}

	rec, err := h.agent.EvaluateCandidate(
		ctx,
		cartID,
		cartTotal,
		BudgetCheck{CartTotal: cartTotal, Budget: DemoBudgetCeiling, Tolerance: DemoBudgetTolerance},
		best.product.ID,
		heuristicEVInputs(best.product.Price.Amount, best.score, best.product.AverageRating),
	)
	if err != nil {
		return SuggestResponse{}, err
	}

	if rec.Decision != "RECOMMEND" {
		return SuggestResponse{Available: false, Message: rec.Reason}, nil
	}

	// Only a suggestion actually about to be returned to the buyer
	// counts as a real impression -- a REJECT decision above (over
	// budget) or "no complementary product" never reaches here.
	if h.impressions != nil {
		if err := h.impressions.RecordImpression(ctx, cartID, best.product.ID); err != nil {
			return SuggestResponse{}, err
		}
	}

	return SuggestResponse{
		Available:      true,
		Recommendation: &rec,
		Product: &SuggestedProduct{
			ProductID: best.product.ID,
			Title:     best.product.Title,
			Price:     best.product.Price.Amount,
			Currency:  best.product.Price.Currency,
		},
	}, nil
}

// Suggest serves POST /growth/suggest -- cross-sell scored against
// everything currently in a live cart.
func (h *SuggestHandler) Suggest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CartID string `json:"cart_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CartID == "" {
		http.Error(w, "cart_id required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	c, err := h.cart.GetCart(ctx, req.CartID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if len(c.Items) == 0 {
		writeSuggestJSON(w, http.StatusOK, SuggestResponse{Available: false, Message: "cart is empty"})
		return
	}

	exclude := make(map[string]bool, len(c.Items))
	var cartProducts []catalog.Product

	for _, item := range c.Items {
		exclude[item.ProductID] = true

		product, err := h.catalog.GetProduct(ctx, item.ProductID)
		if err != nil {
			continue // a stale/removed product shouldn't fail the whole suggestion
		}
		cartProducts = append(cartProducts, product)
	}

	dismissed, err := h.loadDismissed(ctx, req.CartID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for productID := range dismissed {
		exclude[productID] = true
	}

	catalogProducts, err := h.catalog.ListProducts(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("list catalog: %v", err), http.StatusInternalServerError)
		return
	}

	best, ok := bestCandidate(catalogProducts, c.MerchantID, buildSignals(cartProducts...), exclude)
	if !ok {
		writeSuggestJSON(w, http.StatusOK, SuggestResponse{
			Available: false,
			Message:   "no complementary product found for this cart",
		})
		return
	}

	resp, err := h.evaluate(ctx, req.CartID, c.Subtotal, best)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeSuggestJSON(w, http.StatusOK, resp)
}

// SuggestForProduct serves POST /growth/suggest/product -- cross-sell
// scored against a single viewed product's own tags, independent of
// whatever is (or isn't yet) in the cart (PLAN-03-PROACTIVE-GROWTH-
// AGENT.md §3, item 19). This is what powers the "Frequently paired
// with" line in the product detail expand -- it reaches a buyer who
// never opens the agent chat or adds anything to a cart, who today gets
// zero cross-sell exposure whatsoever. cart_id is still required (the
// buyer's session always has one -- checkout.tsx mints one on mount,
// well before the first item is added) so a recommendation shown here
// dedupes/ties into the same shopping session's recommendation history
// as every other surface, and so a "No thanks" here is honored by the
// cart-badge suggestion later, not just on this one panel. A cart_id
// that doesn't exist in the DB yet (nothing added yet) is not an error
// here -- it just means there's no live cart context to layer on top
// (no items to exclude, no running total for the budget check).
func (h *SuggestHandler) SuggestForProduct(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ProductID string `json:"product_id"`
		CartID    string `json:"cart_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProductID == "" || req.CartID == "" {
		http.Error(w, "product_id and cart_id required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	viewed, err := h.catalog.GetProduct(ctx, req.ProductID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	exclude := map[string]bool{req.ProductID: true}
	var cartTotal int64

	if c, err := h.cart.GetCart(ctx, req.CartID); err == nil {
		cartTotal = c.Subtotal
		for _, item := range c.Items {
			exclude[item.ProductID] = true
		}
	}
	// else: no live cart yet for this cart_id -- proceed with cartTotal 0
	// and nothing extra excluded, see doc comment above.

	dismissed, err := h.loadDismissed(ctx, req.CartID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for productID := range dismissed {
		exclude[productID] = true
	}

	catalogProducts, err := h.catalog.ListProducts(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("list catalog: %v", err), http.StatusInternalServerError)
		return
	}

	best, ok := bestCandidate(catalogProducts, viewed.Merchant.ID, buildSignals(viewed), exclude)
	if !ok {
		writeSuggestJSON(w, http.StatusOK, SuggestResponse{
			Available: false,
			Message:   fmt.Sprintf("no complementary product found for %s", viewed.Title),
		})
		return
	}

	resp, err := h.evaluate(ctx, req.CartID, cartTotal, best)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeSuggestJSON(w, http.StatusOK, resp)
}

// SuggestForOrder serves POST /growth/suggest/order -- "complete the
// set" cross-sell scored against the items in a just-completed order,
// not a live cart (PLAN-03-PROACTIVE-GROWTH-AGENT.md §4, item 19). The
// order-complete screen is the one real, low-risk revenue surface most
// e-commerce checkouts use that this app previously had zero equivalent
// of. Reuses the order's own cart_id to key the recommendation, and its
// Subtotal (already net of any campaign discount) as the running total
// for the budget check -- "would this addition, on top of what you just
// bought, still fit the mandate."
func (h *SuggestHandler) SuggestForOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		OrderID string `json:"order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderID == "" {
		http.Error(w, "order_id required", http.StatusBadRequest)
		return
	}

	if h.orders == nil {
		http.Error(w, "order lookup unavailable", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()

	ord, err := h.orders.GetOrder(ctx, req.OrderID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if len(ord.Items) == 0 {
		writeSuggestJSON(w, http.StatusOK, SuggestResponse{Available: false, Message: "order has no items"})
		return
	}

	exclude := make(map[string]bool, len(ord.Items))
	var orderProducts []catalog.Product

	for _, item := range ord.Items {
		exclude[item.ProductID] = true

		product, err := h.catalog.GetProduct(ctx, item.ProductID)
		if err != nil {
			continue // a since-removed product shouldn't fail the whole suggestion
		}
		orderProducts = append(orderProducts, product)
	}

	dismissed, err := h.loadDismissed(ctx, ord.CartID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for productID := range dismissed {
		exclude[productID] = true
	}

	catalogProducts, err := h.catalog.ListProducts(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("list catalog: %v", err), http.StatusInternalServerError)
		return
	}

	best, ok := bestCandidate(catalogProducts, ord.MerchantID, buildSignals(orderProducts...), exclude)
	if !ok {
		writeSuggestJSON(w, http.StatusOK, SuggestResponse{
			Available: false,
			Message:   "no complementary product found to complete the set",
		})
		return
	}

	resp, err := h.evaluate(ctx, ord.CartID, ord.Subtotal, best)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeSuggestJSON(w, http.StatusOK, resp)
}

// Dismiss handles POST /growth/suggest/dismiss -- records that the
// buyer said "no thanks" to a specific product for a specific cart
// (see DismissalStore). Suggest/SuggestForProduct/SuggestForOrder all
// exclude it from candidates on every subsequent call keyed to that
// cart_id. Idempotent: dismissing the same product twice is a no-op
// (SaveDismissal's ON CONFLICT DO NOTHING).
func (h *SuggestHandler) Dismiss(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CartID    string `json:"cart_id"`
		ProductID string `json:"product_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CartID == "" || req.ProductID == "" {
		http.Error(w, "cart_id and product_id required", http.StatusBadRequest)
		return
	}

	if h.dismissals == nil {
		// Wired without a dismissal store (shouldn't happen in
		// production -- main.go always passes one) -- treat as success
		// rather than a 500 the buyer can do nothing about; the
		// suggestion is still hidden client-side via dismissedProductId
		// either way.
		writeSuggestJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	if err := h.dismissals.SaveDismissal(r.Context(), req.CartID, req.ProductID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeSuggestJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Accept handles POST /growth/suggest/accept -- records that a buyer
// actually added a suggested product to their cart (PLAN-03-PROACTIVE-
// GROWTH-AGENT.md §8, item 20). checkout.tsx calls this right after
// addToCart succeeds for a suggestion accepted from any surface
// (cart-based, product-detail, post-checkout). Best-effort like
// Dismiss above: a missing ImpressionStore, or a cart_id/product_id
// pair with no matching recommendation row (e.g. a stale client),
// still returns success -- the buyer's item is already in their cart
// either way, and a merchant undercounting one acceptance is a far
// smaller problem than a 500 the buyer can do nothing about.
func (h *SuggestHandler) Accept(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CartID    string `json:"cart_id"`
		ProductID string `json:"product_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CartID == "" || req.ProductID == "" {
		http.Error(w, "cart_id and product_id required", http.StatusBadRequest)
		return
	}

	if h.impressions == nil {
		writeSuggestJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	if err := h.impressions.RecordAcceptance(r.Context(), req.CartID, req.ProductID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeSuggestJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func writeSuggestJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
