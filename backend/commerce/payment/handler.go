package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/garinesaiajay/commerceos/commerce/cart"
	"github.com/garinesaiajay/commerceos/commerce/order"
)

type OrderReader interface {
	GetOrder(ctx context.Context, id string) (order.Order, error)
}

// CallCounter exposes the provider adapter's outbound-call counter so
// the red-team/audit flows can prove "zero calls were made to Razorpay".
type CallCounter interface {
	CallCount() int64
}

type Handler struct {
	service       *Service
	orderRepo     OrderReader
	counter       CallCounter
	carts         CartReader
	attempts      AttemptReader
	cartBuilder   CartBuilder
	orderCheckout OrderCheckout
}

// CartBuilder creates a fresh cart and adds catalog-priced items to it.
// RemoveItemAndRecheckout uses it to build the smaller cart: prices and
// availability are re-derived from the catalog, never carried over from
// the failed order, so the recomputed total is authoritative.
type CartBuilder interface {
	CreateCart(ctx context.Context, id, merchantID, currency string) (cart.Cart, error)
	AddItem(ctx context.Context, cartID string, item cart.CartItem) error
}

// OrderCheckout re-runs the checkout saga (locking, expiry, inventory) on
// the freshly built cart, producing a new order.
type OrderCheckout interface {
	Checkout(ctx context.Context, cartID string, orderID string) (order.Order, error)
}

type verifyPaymentRequest struct {
	RazorpayPaymentID string `json:"razorpay_payment_id"`
	RazorpayOrderID   string `json:"razorpay_order_id"`
	RazorpaySignature string `json:"razorpay_signature"`
}

func NewHandler(
	service *Service,
	orderRepo OrderReader,
) *Handler {
	return &Handler{
		service:   service,
		orderRepo: orderRepo,
	}
}

// WithRecoveryReaders attaches the cart + attempt readers used by the
// server-driven recovery view.
func (h *Handler) WithRecoveryReaders(carts CartReader, attempts AttemptReader) *Handler {
	h.carts = carts
	h.attempts = attempts
	return h
}

// WithRecoveryActions attaches the cart builder and checkout used by the
// "Remove accessory" recovery action (the fourth failure-recovery path,
// alongside retry / change payment method / cancel).
func (h *Handler) WithRecoveryActions(cartBuilder CartBuilder, orderCheckout OrderCheckout) *Handler {
	h.cartBuilder = cartBuilder
	h.orderCheckout = orderCheckout
	return h
}

// Recovery serves GET /orders/{id}/recovery — the authoritative
// failure-recovery read model (payment + attempt + cart reservation).
func (h *Handler) Recovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	orderID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/orders/"), "/recovery")
	orderID = strings.Trim(orderID, "/")
	if orderID == "" {
		http.Error(w, "order ID required", http.StatusBadRequest)
		return
	}

	view, err := BuildRecovery(r.Context(), orderID, h.service.repo, h.orderRepo, h.carts, h.attempts)
	if err != nil {
		if errors.Is(err, ErrPaymentNotFound) {
			http.Error(w, "payment not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// WithCallCounter attaches the provider call counter to the handler.
func (h *Handler) WithCallCounter(counter CallCounter) *Handler {
	h.counter = counter
	return h
}

// CallCount serves GET /adapter/calls — the live Razorpay call counter.
func (h *Handler) CallCount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.counter == nil {
		http.Error(w, "call counter not available", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"razorpay_calls": h.counter.CallCount()})
}

func (h *Handler) CreatePaymentOrder(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	orderID := strings.TrimPrefix(r.URL.Path, "/orders/")
	orderID = strings.TrimSuffix(orderID, "/payment")
	orderID = strings.Trim(orderID, "/")

	if orderID == "" {
		http.Error(
			w,
			"order ID required",
			http.StatusBadRequest,
		)
		return
	}

	ord, err := h.orderRepo.GetOrder(
		r.Context(),
		orderID,
	)
	if err != nil {
		if errors.Is(err, order.ErrOrderNotFound) {
			http.Error(
				w,
				"order not found",
				http.StatusNotFound,
			)
			return
		}

		http.Error(
			w,
			err.Error(),
			http.StatusInternalServerError,
		)
		return
	}

	paymentOrder, err := h.service.CreatePaymentOrder(
		r.Context(),
		ord,
		r.Header.Get("Idempotency-Key"),
		r.Header.Get("Authorization-Id"),
	)
	if err != nil {
		// A missing/invalid authorization is a client error, not a
		// server error. Anything else is a genuine failure.
		if strings.Contains(err.Error(), "authorization") {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(
		w,
		http.StatusCreated,
		paymentOrder,
	)
}

func writeJSON(
	w http.ResponseWriter,
	status int,
	value any,
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(value)
}

func (h *Handler) VerifyPayment(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	orderID := strings.TrimPrefix(
		r.URL.Path,
		"/orders/",
	)

	orderID = strings.TrimSuffix(
		orderID,
		"/payment/verify",
	)

	orderID = strings.Trim(orderID, "/")

	if orderID == "" {
		http.Error(
			w,
			"order ID required",
			http.StatusBadRequest,
		)
		return
	}

	var req verifyPaymentRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(
			w,
			"invalid request body",
			http.StatusBadRequest,
		)
		return
	}

	payment, err := h.service.VerifyPayment(
		r.Context(),
		orderID,
		req.RazorpayPaymentID,
		req.RazorpayOrderID,
		req.RazorpaySignature,
	)

	if err != nil {
		switch {
		case errors.Is(err, ErrPaymentNotFound):
			http.Error(
				w,
				"payment not found",
				http.StatusNotFound,
			)

		case errors.Is(err, ErrPaymentAlreadyPaid):
			writeJSON(
				w,
				http.StatusOK,
				payment,
			)

		default:
			http.Error(
				w,
				err.Error(),
				http.StatusBadRequest,
			)
		}

		return
	}

	writeJSON(
		w,
		http.StatusOK,
		payment,
	)
}

type removeItemRequest struct {
	VariantID string `json:"variant_id"`
}

// RemoveItemAndRecheckout serves POST /orders/{id}/recovery/remove-item --
// a "remove one item and retry" recovery action usable from two
// different terminal states: a genuinely failed/declined Razorpay
// payment, or a policy rejection that never reached Razorpay at all
// (no Payment row exists for the order in that second case -- see the
// ErrPaymentNotFound handling below). It drops one removable line item
// from the order, rebuilds a fresh smaller cart with catalog-
// authoritative prices (never the stale order total), and re-runs the
// checkout saga (locking, expiry, inventory) on it.
//
// The policy engine is deliberately NOT re-run inside this handler: the
// caller re-proposes against the returned (smaller) order through the
// normal /policy/propose path, which remains the only chokepoint allowed
// to authorize a payment. This endpoint only ever produces a new,
// unauthorized order.
func (h *Handler) RemoveItemAndRecheckout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.cartBuilder == nil || h.orderCheckout == nil {
		http.Error(w, "recovery actions not available", http.StatusNotImplemented)
		return
	}

	orderID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/orders/"), "/recovery/remove-item")
	orderID = strings.Trim(orderID, "/")
	if orderID == "" {
		http.Error(w, "order ID required", http.StatusBadRequest)
		return
	}

	var req removeItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.VariantID == "" {
		http.Error(w, "variant_id is required", http.StatusBadRequest)
		return
	}

	ord, err := h.orderRepo.GetOrder(r.Context(), orderID)
	if err != nil {
		if errors.Is(err, order.ErrOrderNotFound) {
			http.Error(w, "order not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Only a genuinely recoverable order can be resized. Reuse the same
	// server-owned recovery view the failure screen itself uses, rather
	// than trusting the client's idea of the payment state -- but
	// BuildRecovery requires a Payment row to exist, and a policy
	// rejection never creates one (checkout never reaches Razorpay when
	// the policy engine rejects it before the "Pay" step). That is not
	// the same thing as "not recoverable": with no payment attempt at
	// all, there's no failed/declined/expired attempt to block a retry
	// on, so ErrPaymentNotFound here means "nothing to recover FROM",
	// not "this order can't be resized". Previously this fell through
	// to the generic error branch below and leaked the raw wrapped
	// error text ("get payment for recovery: payment not found")
	// straight to the buyer instead of letting the remove happen.
	view, err := BuildRecovery(r.Context(), orderID, h.service.repo, h.orderRepo, h.carts, h.attempts)
	switch {
	case err != nil && errors.Is(err, ErrPaymentNotFound):
		// No payment attempt exists yet -- nothing blocks a retry.
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	case !view.RetryAllowed:
		http.Error(w, "this order is no longer recoverable; start a new cart", http.StatusConflict)
		return
	}

	if len(ord.Items) <= 1 {
		http.Error(w, "cannot remove the only item; cancel instead", http.StatusBadRequest)
		return
	}

	found := false
	remaining := make([]order.OrderItem, 0, len(ord.Items)-1)
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

	newCartID := fmt.Sprintf("cart_%d", time.Now().UnixNano())
	if _, err := h.cartBuilder.CreateCart(r.Context(), newCartID, ord.MerchantID, ord.Currency); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, item := range remaining {
		if err := h.cartBuilder.AddItem(r.Context(), newCartID, cart.CartItem{
			ProductID: item.ProductID,
			VariantID: item.VariantID,
			Title:     item.Title,
			Quantity:  item.Quantity,
		}); err != nil {
			http.Error(w, fmt.Sprintf("could not rebuild cart: %v", err), http.StatusConflict)
			return
		}
	}

	newOrder, err := h.orderCheckout.Checkout(r.Context(), newCartID, order.NewOrderID(newCartID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	writeJSON(w, http.StatusCreated, newOrder)
}
