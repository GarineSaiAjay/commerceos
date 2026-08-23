package order

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

func (h *Handler) Checkout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/carts/")
	path = strings.TrimSuffix(path, "/checkout")

	cartID := strings.Trim(path, "/")

	if cartID == "" {
		http.Error(w, "cart ID required", http.StatusBadRequest)
		return
	}

	orderID := NewOrderID(cartID)

	order, err := h.service.Checkout(
		r.Context(),
		cartID,
		orderID,
	)

	if err != nil {
		switch err {
		case ErrCartNotFound:
			http.Error(w, "cart not found", http.StatusNotFound)

		case ErrCartExpired:
			http.Error(w, "cart reservation expired", http.StatusConflict)

		case ErrCartEmpty:
			http.Error(w, "cart is empty", http.StatusBadRequest)

		case ErrCartAlreadyCheckedOut:
			http.Error(w, "cart already checked out", http.StatusConflict)

		case ErrInsufficientAvailability:
			http.Error(w, "insufficient availability", http.StatusConflict)

		default:
			http.Error(
				w,
				err.Error(),
				http.StatusInternalServerError,
			)
		}

		return
	}

	writeJSON(w, http.StatusCreated, order)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(value)
}
