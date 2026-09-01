package order

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

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

// ExportOrdersCSV handles GET /dashboard/orders/export -- item 27 (P2,
// PLAN-05-SELLER-DASHBOARD.md section 6): "CSV export of orders and
// campaigns ... a thin CSV-serialization endpoint over data the
// existing handlers already query; no new business logic." This reuses
// the exact same Service.ListOrders(ctx, operator.MerchantID) call
// ListOrdersForOperator already makes -- the only difference is the
// response is serialized as CSV instead of JSON, so there is nothing
// here that could disagree with what the Orders page already shows.
//
// One row per order, not per line item: OrderItem detail is
// deliberately left out of this CSV (available via the existing GET
// /orders/{id} JSON detail endpoint instead) because a merchant
// reconciling this against Razorpay settlement data or their own
// accounting wants one row per transaction with an item_count column,
// not a row per SKU that would repeat every order-level total N times
// and break any naive SUM() over the file.
//
// Monetary columns are raw paise (subtotal_paise, discount_amount_paise),
// matching every other amount in this codebase (see e.g.
// policy.PolicyConfig.Ceiling's own comment) rather than converting to
// rupees here -- an INR conversion is a presentation concern
// (frontend/lib/format.tsx's formatINR), and doing it in Go too would
// risk a rounding disagreement between the CSV and the UI for the
// exact same order. A merchant importing this into a spreadsheet can
// divide by 100 themselves.
func (h *Handler) ExportOrdersCSV(w http.ResponseWriter, r *http.Request) {
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

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="orders.csv"`)
	w.WriteHeader(http.StatusOK)

	writer := csv.NewWriter(w)
	_ = writer.Write([]string{
		"order_id", "cart_id", "status", "payment_status", "currency",
		"subtotal_paise", "discount_amount_paise", "campaign_id",
		"item_count", "created_at",
	})
	for _, o := range orders {
		_ = writer.Write([]string{
			o.ID,
			o.CartID,
			o.Status,
			o.PaymentStatus,
			o.Currency,
			strconv.FormatInt(o.Subtotal, 10),
			strconv.FormatInt(o.DiscountAmount, 10),
			o.CampaignID,
			strconv.Itoa(len(o.Items)),
			o.CreatedAt.Format(time.RFC3339),
		})
	}
	// Best-effort flush: the header/status line is already committed by
	// the time any per-row Write above could fail, so there is no
	// remaining error-response path -- same constraint every other CSV/
	// streaming writer in Go faces, nothing specific to this handler.
	writer.Flush()
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
