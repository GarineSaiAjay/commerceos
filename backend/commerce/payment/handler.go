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

type Handler struct {
	service   *Service
	orderRepo OrderReader
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
	)
	if err != nil {
		http.Error(
			w,
			err.Error(),
			http.StatusInternalServerError,
		)
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
