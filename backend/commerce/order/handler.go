package order

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/garinesaiajay/commerceos/auth"
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

// ListOrders handles GET /orders?merchant_id=... -- the order-history
// list the buyer-facing UI reads. Scoped by merchant, not by buyer,
// because there is no buyer identity yet (files/AUTH.md); every order
// for this single-merchant demo qualifies.
func (h *Handler) ListOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	merchantID := r.URL.Query().Get("merchant_id")

	if merchantID == "" {
		http.Error(w, "merchant_id query parameter required", http.StatusBadRequest)
		return
	}

	orders, err := h.service.ListOrders(r.Context(), merchantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, orders)
}

// ListOrdersForOperator handles GET /dashboard/orders -- the merchant-
// authenticated equivalent of ListOrders above, scoped by the logged-in
// operator's own merchant_id (auth.OperatorFromContext) rather than a
// client-supplied merchant_id query parameter. Added for the merchant
// dashboard's Orders page (item 15, PLAN-05-SELLER-DASHBOARD.md §2) --
// mirrors how every other dashboard list route (campaigns, runs,
// growth) is scoped from the operator's own session instead of an
// untrusted client parameter; ListOrders itself is left exactly as-is
// since it's also the buyer-facing order-history endpoint, which has
// no buyer identity to scope by (files/AUTH.md) and must keep taking
// merchant_id from the query string.
func (h *Handler) ListOrdersForOperator(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	operator, ok := auth.OperatorFromContext(r.Context())
	if !ok {
		http.Error(w, "operator session required", http.StatusUnauthorized)
		return
	}

	orders, err := h.service.ListOrders(r.Context(), operator.MerchantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, orders)
}

// GetOrder handles GET /orders/{id} -- a single order's detail, used by
// the order-history view and by anything that only has an order ID.
func (h *Handler) GetOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	orderID := strings.TrimPrefix(r.URL.Path, "/orders/")
	orderID = strings.Trim(orderID, "/")

	if orderID == "" {
		http.Error(w, "order ID required", http.StatusBadRequest)
		return
	}

	order, err := h.service.GetOrder(r.Context(), orderID)
	if err != nil {
		if err == ErrOrderNotFound {
			http.Error(w, "order not found", http.StatusNotFound)
			return
		}

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, order)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(value)
}
