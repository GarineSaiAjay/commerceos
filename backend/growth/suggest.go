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

// heuristicEVInputs turns a content-overlap score into EV inputs. Higher
// overlap with what's already in the cart -> higher assumed purchase
// probability, capped conservatively.
func heuristicEVInputs(candidatePrice int64, overlapScore int) EVInputs {
	prob := heuristicBaseProbability + heuristicProbabilityStep*float64(overlapScore)
	if prob > heuristicMaxProbability {
		prob = heuristicMaxProbability
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
	catalog CatalogSearcher
	cart    CartReader
	agent   *GrowthAgent
}

func NewSuggestHandler(catalog CatalogSearcher, cartReader CartReader, agent *GrowthAgent) *SuggestHandler {
	return &SuggestHandler{catalog: catalog, cart: cartReader, agent: agent}
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
		if inCart[product.ID] || product.Availability <= 0 {
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
		heuristicEVInputs(best.product.Price.Amount, best.score),
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

func writeSuggestJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
