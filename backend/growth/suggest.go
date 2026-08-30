package growth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/garinesaiajay/commerceos/commerce/cart"
	"github.com/garinesaiajay/commerceos/commerce/catalog"
)

// CartReader is the cart surface SuggestHandler needs.
type CartReader interface {
	GetCart(ctx context.Context, id string) (cart.Cart, error)
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

// SuggestHandler serves POST /growth/suggest. Given a cart_id, it picks
// the single best complementary product -- by content overlap with what
// is already in the cart (shared use_cases/compatibility/features tags)
// -- scores it with the same deterministic EV formula /growth/evaluate
// uses, and runs it through GrowthAgent.EvaluateCandidate so the budget
// check and persistence path are identical to the rest of the growth
// agent (the resulting recommendation is saved to the same table the
// dashboard's ai_revenue metric already joins on via recommendations.cart_id).
// It never recommends when nothing in the catalog shares a signal with
// the cart -- a safe no-op, the same posture agents.IntentExtractor
// takes on an ambiguous prompt (see agents/intent.go's Clarify field) --
// rather than guessing at an unrelated product.
type SuggestHandler struct {
	catalog    CatalogSearcher
	cart       CartReader
	agent      *GrowthAgent
	dismissals DismissalStore
}

func NewSuggestHandler(catalog CatalogSearcher, cartReader CartReader, agent *GrowthAgent, dismissals DismissalStore) *SuggestHandler {
	return &SuggestHandler{catalog: catalog, cart: cartReader, agent: agent, dismissals: dismissals}
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

	inCart := make(map[string]bool, len(c.Items))
	var cartTotal int64
	signals := make(map[string]bool)

	// Products the buyer already said "no thanks" to for this cart --
	// excluded the same way an in-cart product already is, so dismissal
	// actually sticks instead of resetting on the next fetch (previously
	// this only lived in frontend React state, lost on reload). A
	// missing/nil dismissals store (e.g. an older wiring or a test) just
	// means nothing is excluded, not an error.
	dismissed := make(map[string]bool)
	if h.dismissals != nil {
		ids, err := h.dismissals.ListDismissedProductIDs(ctx, req.CartID)
		if err != nil {
			http.Error(w, fmt.Sprintf("list dismissals: %v", err), http.StatusInternalServerError)
			return
		}
		for _, id := range ids {
			dismissed[id] = true
		}
	}

	for _, item := range c.Items {
		inCart[item.ProductID] = true
		cartTotal += item.Total

		product, err := h.catalog.GetProduct(ctx, item.ProductID)
		if err != nil {
			continue // a stale/removed product shouldn't fail the whole suggestion
		}
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

	catalogProducts, err := h.catalog.ListProducts(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("list catalog: %v", err), http.StatusInternalServerError)
		return
	}
	type scoredCandidate struct {
		product catalog.Product
		score   int
	}

	var candidates []scoredCandidate

	for _, product := range catalogProducts {
		if inCart[product.ID] || dismissed[product.ID] || product.Availability <= 0 {
			continue
		}
		if product.Merchant.ID != c.MerchantID {
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
		writeSuggestJSON(w, http.StatusOK, SuggestResponse{
			Available: false,
			Message:   "no complementary product found for this cart",
		})
		return
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		// Tie-break toward the cheaper item -- a smaller, more plausible
		// add-on beats an equally-relevant but expensive one.
		return candidates[i].product.Price.Amount < candidates[j].product.Price.Amount
	})
	best := candidates[0]

	rec, err := h.agent.EvaluateCandidate(
		ctx,
		req.CartID,
		cartTotal,
		BudgetCheck{CartTotal: cartTotal, Budget: DemoBudgetCeiling, Tolerance: DemoBudgetTolerance},
		best.product.ID,
		heuristicEVInputs(best.product.Price.Amount, best.score, best.product.AverageRating),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if rec.Decision != "RECOMMEND" {
		writeSuggestJSON(w, http.StatusOK, SuggestResponse{Available: false, Message: rec.Reason})
		return
	}

	writeSuggestJSON(w, http.StatusOK, SuggestResponse{
		Available:      true,
		Recommendation: &rec,
		Product: &SuggestedProduct{
			ProductID: best.product.ID,
			Title:     best.product.Title,
			Price:     best.product.Price.Amount,
			Currency:  best.product.Price.Currency,
		},
	})
}

// Dismiss handles POST /growth/suggest/dismiss -- records that the
// buyer said "no thanks" to a specific product for a specific cart
// (see DismissalStore). Suggest excludes it from candidates on every
// subsequent call for that cart. Idempotent: dismissing the same
// product twice is a no-op (SaveDismissal's ON CONFLICT DO NOTHING).
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

func writeSuggestJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
