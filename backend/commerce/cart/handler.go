package cart

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/garinesaiajay/commerceos/events"
)

type Handler struct {
	service   *Service
	publisher events.EventBus
	stream    string
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

// WithEventPublisher wires in best-effort "cart.item_added" event
// publishing (item 42, P3, PLAN-06-ADDITIONAL-OPPORTUNITIES.md §4 /
// PLAN-03-PROACTIVE-GROWTH-AGENT.md §7). Matches this codebase's WithX
// convention for an optional capability (payment.Handler.
// WithCallCounter, order.PostgresRepository.WithAuditWriter) -- every
// existing caller of NewHandler keeps compiling unchanged; main.go is
// the only one that needs to also call this to turn publishing on.
//
// Deliberately publishes straight to the event bus rather than going
// through the durable outbox the way payment.captured/payment.failed
// do (webhook_applier.go): the outbox's own contract requires Insert
// to run inside the same DB transaction as the state change it
// describes, and cart.Service has no transaction boundary today (its
// repo calls are plain, un-wrapped Postgres writes) -- introducing one
// just for this stretch item would risk a well-tested, currently
// transaction-free service for a notification whose loss has zero
// correctness impact. Unlike a lost payment event, a lost
// cart.item_added event just means one cart's cross-sell suggestion
// doesn't get precomputed early; POST /growth/suggest still computes
// it correctly on demand regardless (growth/suggest.go), so this is
// an honest best-effort, at-most-once notification, not a downgrade
// of anything that needs stronger guarantees.
func (h *Handler) WithEventPublisher(publisher events.EventBus, stream string) *Handler {
	h.publisher = publisher
	h.stream = stream
	return h
}

type cartItemAddedEvent struct {
	CartID string `json:"cart_id"`
}

// publishCartItemAdded is called after AddItem succeeds. Never fails
// the request and is never retried -- see WithEventPublisher's doc
// comment for why that's the right trade-off here.
func (h *Handler) publishCartItemAdded(ctx context.Context, cartID string) {
	if h.publisher == nil {
		return
	}

	payload, err := json.Marshal(cartItemAddedEvent{CartID: cartID})
	if err != nil {
		return
	}

	if err := h.publisher.Publish(ctx, h.stream, events.OutboxEvent{
		EventType: "cart.item_added",
		Payload:   payload,
		CreatedAt: time.Now(),
	}); err != nil {
		log.Printf("cart: publish cart.item_added for %s: %v", cartID, err)
	}
}

type createCartRequest struct {
	ID         string `json:"cart_id"`
	MerchantID string `json:"merchant_id"`
	Currency   string `json:"currency"`
}

type addItemRequest struct {
	ProductID string `json:"product_id"`
	VariantID string `json:"variant_id"`
	Title     string `json:"title"`
	Quantity  int    `json:"quantity"`
	UnitPrice int64  `json:"unit_price"`
}

type updateItemQuantityRequest struct {
	Quantity int `json:"quantity"`
}

func (h *Handler) CreateCart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req createCartRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	cart, err := h.service.CreateCart(
		r.Context(),
		req.ID,
		req.MerchantID,
		req.Currency,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, cart)
}

func (h *Handler) GetCart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cartID := strings.TrimPrefix(r.URL.Path, "/carts/")

	if cartID == "" {
		http.Error(w, "cart ID required", http.StatusBadRequest)
		return
	}

	cart, err := h.service.GetCart(r.Context(), cartID)
	if err != nil {
		if err == ErrCartNotFound {
			http.Error(w, "cart not found", http.StatusNotFound)
			return
		}

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, cart)
}

func (h *Handler) AddItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cartID := strings.TrimPrefix(r.URL.Path, "/carts/")
	cartID = strings.TrimSuffix(cartID, "/items")

	if cartID == "" {
		http.Error(w, "cart ID required", http.StatusBadRequest)
		return
	}

	var req addItemRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	err := h.service.AddItem(r.Context(), cartID, CartItem{
		ProductID: req.ProductID,
		VariantID: req.VariantID,
		Title:     req.Title,
		Quantity:  req.Quantity,
		UnitPrice: req.UnitPrice,
	})
	if err != nil {
		if err == ErrCartNotFound {
			http.Error(w, "cart not found", http.StatusNotFound)
			return
		}

		if err == ErrInvalidQuantity {
			http.Error(w, "invalid quantity", http.StatusBadRequest)
			return
		}

		if err == ErrInsufficientAvailability {
			http.Error(w, "insufficient availability", http.StatusConflict)
			return
		}

		if err == ErrCartConflict {
			// Full-codebase re-audit (P2): Service.mutateCart already
			// retries a concurrent-write conflict internally (see
			// service.go) -- this only surfaces after
			// maxCartMutateRetries attempts under sustained
			// contention, genuinely rare. 409, not 500: the request
			// itself was fine, the cart just needs re-reading.
			http.Error(w, "cart was concurrently modified, please retry", http.StatusConflict)
			return
		}

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.publishCartItemAdded(r.Context(), cartID)

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UpdateItemQuantity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/carts/")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	if len(parts) != 3 || parts[1] != "items" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	cartID := parts[0]
	variantID := parts[2]

	var req updateItemQuantityRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	err := h.service.UpdateItemQuantity(
		r.Context(),
		cartID,
		variantID,
		req.Quantity,
	)
	if err != nil {
		if err == ErrCartNotFound {
			http.Error(w, "cart not found", http.StatusNotFound)
			return
		}

		if err == ErrItemNotFound {
			http.Error(w, "cart item not found", http.StatusNotFound)
			return
		}

		if err == ErrInvalidQuantity {
			http.Error(w, "invalid quantity", http.StatusBadRequest)
			return
		}

		if err == ErrInsufficientAvailability {
			http.Error(w, "insufficient availability", http.StatusConflict)
			return
		}

		if err == ErrCartConflict {
			http.Error(w, "cart was concurrently modified, please retry", http.StatusConflict)
			return
		}

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemoveItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/carts/")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	if len(parts) != 3 || parts[1] != "items" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	cartID := parts[0]
	variantID := parts[2]

	err := h.service.RemoveItem(r.Context(), cartID, variantID)
	if err != nil {
		if err == ErrCartNotFound {
			http.Error(w, "cart not found", http.StatusNotFound)
			return
		}

		if err == ErrItemNotFound {
			http.Error(w, "cart item not found", http.StatusNotFound)
			return
		}

		if err == ErrCartConflict {
			http.Error(w, "cart was concurrently modified, please retry", http.StatusConflict)
			return
		}

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(value)
}
