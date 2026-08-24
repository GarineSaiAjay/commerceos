package payment

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

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
	service   *Service
	orderRepo OrderReader
	counter   CallCounter
	carts     CartReader
	attempts  AttemptReader
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
