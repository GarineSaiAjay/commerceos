package cart

import (
	"encoding/json"
	"net/http"
	"strings"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
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

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

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
