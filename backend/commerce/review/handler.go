package review

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/garinesaiajay/commerceos/commerce/order"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

type submitRequest struct {
	ProductID      string `json:"product_id"`
	Rating         int    `json:"rating"`
	Comment        string `json:"comment"`
	BuyerReference string `json:"buyer_reference"`
}

// Submit handles POST /orders/{id}/review -- the post-checkout "Rate
// this order" prompt (PLAN-02-CATALOG-AND-COMMERCE.md §2). order_id is
// parsed from the URL path, matching order.Handler.GetOrder's own
// trim-prefix/trim-suffix convention for /orders/{id} routes.
func (h *Handler) Submit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/orders/")
	path = strings.TrimSuffix(path, "/review")
	orderID := strings.Trim(path, "/")
	if orderID == "" {
		http.Error(w, "order ID required", http.StatusBadRequest)
		return
	}

	var req submitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ProductID) == "" {
		http.Error(w, "product_id is required", http.StatusBadRequest)
		return
	}

	rev, err := h.service.Submit(r.Context(), orderID, req.ProductID, req.BuyerReference, req.Rating, req.Comment)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidRating), errors.Is(err, ErrProductNotInOrder), errors.Is(err, ErrOrderNotEligibleForReview):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, order.ErrOrderNotFound):
			http.Error(w, "order not found", http.StatusNotFound)
		case errors.Is(err, ErrDuplicateReview):
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, http.StatusCreated, rev)
}

// ListByProduct handles GET /products/{id}/reviews -- the individual
// review comments backing a product's average_rating/review_count
// aggregate (catalog.Product, computed separately by
// catalog.PostgresRepository.GetProduct's own join over the same
// table).
func (h *Handler) ListByProduct(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/products/")
	path = strings.TrimSuffix(path, "/reviews")
	productID := strings.Trim(path, "/")
	if productID == "" {
		http.Error(w, "product ID required", http.StatusBadRequest)
		return
	}

	reviews, err := h.service.ListByProduct(r.Context(), productID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, reviews)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
