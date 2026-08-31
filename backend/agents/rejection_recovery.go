package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/garinesaiajay/commerceos/commerce/cart"
	"github.com/garinesaiajay/commerceos/commerce/catalog"
	"github.com/garinesaiajay/commerceos/commerce/order"
	"github.com/garinesaiajay/commerceos/tools"
)

// This file implements PLAN-01-AGENTIC-CORE.md §6's second proactive
// agent turn, ROADMAP-PRIORITIZED.md P2 item 33: "when a proposal is
// rejected for budget reasons, have the agent loop proactively re-run
// search_products with a lower implied budget and propose a
// substitute, instead of just asking the buyer to manually remove
// items."
//
// Two honest departures from that framing, found while implementing it
// (same "adapt to what the code actually does, document the gap"
// posture as every other item in this project):
//
//  1. "The agent loop" (ToolCallingAgent.Run, item 18) is not used
//     here. Run takes only a free-text prompt/merchant -- no cart_id,
//     no notion of a target budget -- and every call is a real,
//     non-deterministic LLM round trip. Computing "how much would a
//     substitute need to cost" is a hard arithmetic fact derivable
//     from the order and the policy ceiling; there is nothing for an
//     LLM to reason about here that a deterministic budget filter
//     doesn't already answer better. So this calls straight into
//     tools.NewSearcher(...).Search(...) -- the exact same ranking
//     code search_products (both the MCP tool and the loop's own
//     dispatch case) already delegates to -- rather than routing a
//     synthetic prompt through the LLM loop for no benefit.
//  2. RemoveItemAndRecheckout (backend/commerce/payment/handler.go,
//     NOT backend/commerce/order as a literal reading of the plan
//     doc's package grouping might suggest) is a remove-one-existing-
//     item primitive; it has no notion of budget, search, or
//     substitution, so it cannot itself produce a "propose a
//     substitute" result. What IS reused here is its exact mechanical
//     shape -- build a fresh cart from CartBuilder, re-derive every
//     price from the catalog inside AddItem (never trust a stale
//     order total), then run the normal checkout saga via
//     OrderCheckout, and deliberately do NOT re-run the policy engine
//     here (the caller re-proposes through the normal /policy/propose
//     path, same as RemoveItemAndRecheckout's own contract) -- see
//     ReplaceItemAndRecheckout below, which is RemoveItemAndRecheckout
//     with one extra AddItem call for the substitute rather than a
//     new implementation of that plumbing.
//
// Scope: unlike RemoveItemAndRecheckout, this is deliberately usable
// ONLY from a policy rejection, never from a failed/declined payment.
// A policy rejection never reaches Razorpay (no Payment row exists
// for the order), so there is no payment-recovery state machine
// (payment.BuildRecovery's RetryAllowed/expiry checks) to consult --
// the order itself is the only state that matters. The existing
// remove-item recovery action already covers the failed-payment case
// and is untouched by this file.

// RecoveryCartBuilder mirrors payment.CartBuilder -- a fresh cart, and
// catalog-priced items added to it. Kept as its own narrow interface
// here (rather than importing payment.CartBuilder) so this package
// doesn't take a dependency on backend/commerce/payment just for a
// two-method shape both already satisfy identically via *cart.Service.
type RecoveryCartBuilder interface {
	CreateCart(ctx context.Context, id, merchantID, currency string) (cart.Cart, error)
	AddItem(ctx context.Context, cartID string, item cart.CartItem) error
}

// RecoveryOrderCheckout mirrors payment.OrderCheckout.
type RecoveryOrderCheckout interface {
	Checkout(ctx context.Context, cartID string, orderID string) (order.Order, error)
}

// RecoveryOrderReader is the order surface this handler needs.
type RecoveryOrderReader interface {
	GetOrder(ctx context.Context, orderID string) (order.Order, error)
}

// RecoveryCatalogReader is the catalog surface this handler needs --
// both to look up the item being replaced (for its use_cases/features,
// used as a coarse similarity signal) and to search for a substitute.
// Satisfied by *catalog.Service.
type RecoveryCatalogReader interface {
	tools.CatalogReader
	GetProduct(ctx context.Context, id string) (catalog.Product, error)
}

// RejectionRecoveryHandler serves the two /orders/{id}/recovery/
// substitute endpoints. Constructed with the same service instances
// already wired for paymentHandler's own recovery actions (main.go) --
// no new state, no new database access pattern.
type RejectionRecoveryHandler struct {
	orders   RecoveryOrderReader
	catalog  RecoveryCatalogReader
	carts    RecoveryCartBuilder
	checkout RecoveryOrderCheckout
	// ceiling is the policy engine's configured platform amount
	// ceiling (policy.PolicyConfig.Ceiling, paise) -- passed in from
	// main.go's own policyConfig rather than re-declaring
	// policy.DefaultConfig() here, so this can never silently drift
	// from the ceiling the policy engine is actually enforcing (the
	// exact staleness policy.ExplainRejection's own hardcoded literal
	// already has -- deliberately not repeating that mistake here).
	ceiling int64
}

func NewRejectionRecoveryHandler(
	orders RecoveryOrderReader,
	catalog RecoveryCatalogReader,
	carts RecoveryCartBuilder,
	checkout RecoveryOrderCheckout,
	ceiling int64,
) *RejectionRecoveryHandler {
	return &RejectionRecoveryHandler{
		orders:   orders,
		catalog:  catalog,
		carts:    carts,
		checkout: checkout,
		ceiling:  ceiling,
	}
}

// SubstituteItem is one side of a proposed swap -- either the
// over-budget item being replaced or the cheaper candidate replacing
// it.
type SubstituteItem struct {
	ProductID string `json:"product_id"`
	VariantID string `json:"variant_id,omitempty"`
	Title     string `json:"title"`
	Price     int64  `json:"price"`
}

// RejectionRecoverySuggestion is the result of SuggestSubstitute.
// Available is false, with Reason explaining why, whenever no
// actionable substitute could be proposed -- the frontend's existing
// manual "remove an item" list stays the fallback in every such case,
// unchanged.
type RejectionRecoverySuggestion struct {
	Available    bool            `json:"available"`
	Reason       string          `json:"reason,omitempty"`
	ReplacedItem *SubstituteItem `json:"replaced_item,omitempty"`
	Substitute   *SubstituteItem `json:"substitute,omitempty"`
	NewSubtotal  int64           `json:"new_subtotal,omitempty"`
	Reasoning    string          `json:"reasoning,omitempty"`
}

// suggestSubstitute is the pure decision logic behind SuggestSubstitute
// (HTTP), split out for direct testing without a request/response.
//
// Whether this order's rejection was actually "over budget" is
// determined the simple, self-contained way: ord.Subtotal > ceiling.
// This deliberately does NOT require the caller to pass along which
// policy.CheckX failed -- that value lives only in the Decision the
// frontend already received from /policy/propose and was never
// persisted against the order, so re-deriving it from the ceiling
// directly (rather than threading FailedCheck through an extra request
// field) keeps this endpoint self-contained and correct regardless of
// which specific check produced the rejection. In this project's
// shipped flow this is, in practice, always policy.CheckAmountCeiling
// -- see the doc comment on policy.CheckBudgetTolerance's caller in
// checkout.tsx: the mandate's maximum_amount is always set equal to
// the proposed order subtotal immediately before /policy/propose is
// called, so that check can never actually fire from this app's real
// checkout flow. If Subtotal <= ceiling, this order was rejected for a
// different reason entirely (merchant allowlist, currency, an
// unpermitted item, mandate drift) and no budget-driven substitute
// makes sense -- Available is false.
func (h *RejectionRecoveryHandler) suggestSubstitute(ctx context.Context, ord order.Order) (RejectionRecoverySuggestion, error) {
	overBudgetBy := ord.Subtotal - h.ceiling
	if overBudgetBy <= 0 {
		return RejectionRecoverySuggestion{
			Available: false,
			Reason:    "this order is within the platform amount ceiling; the rejection was for a different reason",
		}, nil
	}
	if len(ord.Items) == 0 {
		return RejectionRecoverySuggestion{Available: false, Reason: "order has no items"}, nil
	}

	// The single most expensive line item is the replace candidate --
	// swapping it buys back the most headroom per item touched. Ties
	// keep the first one encountered (order.Items has no meaningful
	// secondary sort to break on).
	candidate := ord.Items[0]
	for _, item := range ord.Items[1:] {
		if item.Total > candidate.Total {
			candidate = item
		}
	}

	budget := candidate.Total - overBudgetBy
	if budget <= 0 {
		return RejectionRecoverySuggestion{
			Available: false,
			Reason:    "even replacing the most expensive item alone can't bring this order under the ceiling; try removing an item instead",
		}, nil
	}

	// tools.SearchFilter.Budget is rupees, not paise (Searcher.Search
	// converts it back to paise internally) -- integer-divide down,
	// never up, so the converted-back paise figure this produces can
	// only ever be <= budget, never accidentally over it.
	budgetRupees := budget / 100
	if budgetRupees <= 0 {
		return RejectionRecoverySuggestion{
			Available: false,
			Reason:    "the remaining budget after replacing the most expensive item is too small to search against",
		}, nil
	}

	// A coarse similarity signal, not the tag-overlap scorer
	// backend/growth/suggest.go's bestCandidate already implements --
	// duplicating that scorer here (or importing the growth package
	// into agents for it) was judged out of scope for what is, at
	// bottom, a budget-recovery fallback, not a new cross-sell surface.
	// Category/Priority just bias the search toward something roughly
	// similar to what's being replaced; the hard budget constraint
	// below is what actually matters.
	replacedProduct, err := h.catalog.GetProduct(ctx, candidate.ProductID)
	category, priority := "", ""
	if err == nil {
		if len(replacedProduct.UseCases) > 0 {
			category = replacedProduct.UseCases[0]
		}
		if len(replacedProduct.Features) > 0 {
			priority = replacedProduct.Features[0]
		}
	}
	// A failed GetProduct here (e.g. the product was deleted since the
	// order was placed) isn't fatal -- category/priority just stay
	// empty and the search falls back to price-only ranking.

	excluded := make(map[string]bool, len(ord.Items))
	for _, item := range ord.Items {
		excluded[item.ProductID] = true
	}

	searcher := tools.NewSearcher(h.catalog)
	results, err := searcher.Search(ctx, tools.SearchFilter{
		Budget:   budgetRupees,
		Category: category,
		Priority: priority,
	})
	if err != nil {
		return RejectionRecoverySuggestion{}, fmt.Errorf("search for substitute: %w", err)
	}

	var substitute *catalog.Product
	for i := range results {
		p := results[i].Product
		if excluded[p.ID] {
			continue
		}
		substitute = &p
		break
	}
	if substitute == nil {
		return RejectionRecoverySuggestion{
			Available: false,
			Reason:    "no in-budget substitute was found in the catalog",
		}, nil
	}

	newSubtotal := ord.Subtotal - candidate.Total + substitute.Price.Amount

	return RejectionRecoverySuggestion{
		Available: true,
		ReplacedItem: &SubstituteItem{
			ProductID: candidate.ProductID,
			VariantID: candidate.VariantID,
			Title:     candidate.Title,
			Price:     candidate.Total,
		},
		Substitute: &SubstituteItem{
			ProductID: substitute.ID,
			VariantID: defaultVariantID(*substitute),
			Title:     substitute.Title,
			Price:     substitute.Price.Amount,
		},
		NewSubtotal: newSubtotal,
		Reasoning: fmt.Sprintf(
			"Swapping %q for %q brings this order to %d, back under the %d ceiling.",
			candidate.Title, substitute.Title, newSubtotal, h.ceiling,
		),
	}, nil
}

// defaultVariantID picks the variant AddItem should use for a product
// the buyer never explicitly chose a variant for -- the same "<id>-
// default" convention frontend/app/checkout/helpers.ts's
// defaultVariantFor already establishes client-side, reimplemented
// here server-side since this proposal is generated entirely on the
// backend with no buyer variant pick involved. Every real product is
// guaranteed to have its own "<id>-default" entry
// (catalog.PostgresRepository.CreateProduct provisions it
// transactionally), so the final fallback below is only ever reached
// for a product built by hand (e.g. in a test) with no variants at all.
func defaultVariantID(p catalog.Product) string {
	want := p.ID + "-default"
	for _, v := range p.Variants {
		if v.ID == want {
			return want
		}
	}
	if len(p.Variants) > 0 {
		return p.Variants[0].ID
	}
	return want
}

// SuggestSubstitute serves POST /orders/{id}/recovery/suggest-
// substitute -- computes (never persists) a proposed cheaper swap for
// a policy-rejected order. Side-effect-free; safe to call repeatedly
// (e.g. the frontend calls this once, automatically, the moment the
// policy_rejected screen mounts).
func (h *RejectionRecoveryHandler) SuggestSubstitute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	orderID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/orders/"), "/recovery/suggest-substitute")
	orderID = strings.Trim(orderID, "/")
	if orderID == "" {
		http.Error(w, "order ID required", http.StatusBadRequest)
		return
	}

	ord, err := h.orders.GetOrder(r.Context(), orderID)
	if err != nil {
		if errors.Is(err, order.ErrOrderNotFound) {
			http.Error(w, "order not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	suggestion, err := h.suggestSubstitute(r.Context(), ord)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, suggestion)
}

type replaceItemRequest struct {
	VariantID           string `json:"variant_id"`
	SubstituteProductID string `json:"substitute_product_id"`
}

// ReplaceItemAndRecheckout serves POST /orders/{id}/recovery/replace-
// item -- accepts a proposed substitute from SuggestSubstitute above
// (or, in principle, any variant_id/substitute_product_id pair the
// caller names; the substitute's price/availability are always
// re-derived live from the catalog inside AddItem below, never trusted
// from the request). Mechanically identical to
// payment.Handler.RemoveItemAndRecheckout -- rebuild a fresh cart with
// every remaining item, add the substitute, run the normal checkout
// saga -- with one extra AddItem call for the substitute. Policy is
// deliberately NOT re-run here, for the same reason
// RemoveItemAndRecheckout doesn't: the caller re-proposes the new
// (still-unauthorized) order through the normal /policy/propose path,
// which stays the only chokepoint that can ever produce an
// authorization.
func (h *RejectionRecoveryHandler) ReplaceItemAndRecheckout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	orderID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/orders/"), "/recovery/replace-item")
	orderID = strings.Trim(orderID, "/")
	if orderID == "" {
		http.Error(w, "order ID required", http.StatusBadRequest)
		return
	}

	var req replaceItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.VariantID == "" || req.SubstituteProductID == "" {
		http.Error(w, "variant_id and substitute_product_id are required", http.StatusBadRequest)
		return
	}

	ord, err := h.orders.GetOrder(r.Context(), orderID)
	if err != nil {
		if errors.Is(err, order.ErrOrderNotFound) {
			http.Error(w, "order not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	found := false
	remaining := make([]order.OrderItem, 0, len(ord.Items))
	for _, item := range ord.Items {
		if item.VariantID == req.VariantID {
			found = true
			continue
		}
		remaining = append(remaining, item)
	}
	if !found {
		http.Error(w, "item not found on this order", http.StatusNotFound)
		return
	}

	substitute, err := h.catalog.GetProduct(r.Context(), req.SubstituteProductID)
	if err != nil {
		if errors.Is(err, catalog.ErrProductNotFound) {
			http.Error(w, "substitute product not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	newCartID := fmt.Sprintf("cart_%d", time.Now().UnixNano())
	if _, err := h.carts.CreateCart(r.Context(), newCartID, ord.MerchantID, ord.Currency); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, item := range remaining {
		if err := h.carts.AddItem(r.Context(), newCartID, cart.CartItem{
			ProductID: item.ProductID,
			VariantID: item.VariantID,
			Title:     item.Title,
			Quantity:  item.Quantity,
		}); err != nil {
			http.Error(w, fmt.Sprintf("could not rebuild cart: %v", err), http.StatusConflict)
			return
		}
	}
	if err := h.carts.AddItem(r.Context(), newCartID, cart.CartItem{
		ProductID: substitute.ID,
		VariantID: defaultVariantID(substitute),
		Title:     substitute.Title,
		Quantity:  1,
	}); err != nil {
		http.Error(w, fmt.Sprintf("could not add substitute: %v", err), http.StatusConflict)
		return
	}

	newOrder, err := h.checkout.Checkout(r.Context(), newCartID, order.NewOrderID(newCartID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	writeJSON(w, http.StatusCreated, newOrder)
}
