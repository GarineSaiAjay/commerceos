package payment

import (
	"context"
	"fmt"
	"time"

	"github.com/garinesaiajay/commerceos/commerce/cart"
)

// RecoveryView is the server-owned read model for the failure-recovery UX.
type RecoveryView struct {
	OrderID              string       `json:"order_id"`
	PaymentStatus        string       `json:"payment_status"`
	AttemptStatus        string       `json:"attempt_status"`
	ErrorCode            string       `json:"error_code"`
	ErrorDescription     string       `json:"error_description"`
	SafeMessage          string       `json:"safe_message"`
	ReservationExpiresAt time.Time    `json:"reservation_expires_at"`
	Cart                 CartSnapshot `json:"cart"`
	RemovableItems       []string     `json:"removable_items"`
	RetryAllowed         bool         `json:"retry_allowed"`
}

// CartSnapshot is the cart as presented on the recovery screen.
type CartSnapshot struct {
	CartID    string     `json:"cart_id"`
	Subtotal  int64      `json:"subtotal"`
	Currency  string     `json:"currency"`
	ExpiresAt time.Time  `json:"expires_at"`
	Items     []CartItem `json:"items"`
}

// CartItem is a recoverable cart line.
type CartItem struct {
	ProductID string `json:"product_id"`
	VariantID string `json:"variant_id"`
	Title     string `json:"title"`
	Quantity  int    `json:"quantity"`
	UnitPrice int64  `json:"unit_price"`
	Total     int64  `json:"total"`
}

// CartReader is the cart surface the recovery read model needs.
type CartReader interface {
	GetCart(ctx context.Context, id string) (cart.Cart, error)
}

// AttemptReader surfaces the latest payment attempt for an order.
type AttemptReader interface {
	GetLatestForOrder(ctx context.Context, orderID string) (PaymentAttempt, error)
}

// BuildRecovery assembles the authoritative recovery view for an order. It
// is driven by server state (payment + order + attempt + cart), never by
// the browser, so a Razorpay modal dismissal is not treated as a failure.
func BuildRecovery(
	ctx context.Context,
	orderID string,
	payments Repository,
	orders OrderReader,
	carts CartReader,
	attempts AttemptReader,
) (RecoveryView, error) {
	view := RecoveryView{
		OrderID:      orderID,
		RetryAllowed: false,
		SafeMessage:  "We could not determine the payment state. Please try again.",
	}

	pay, err := payments.GetByOrderID(ctx, orderID)
	if err != nil {
		return RecoveryView{}, fmt.Errorf("get payment for recovery: %w", err)
	}
	view.PaymentStatus = pay.Status

	ord, err := orders.GetOrder(ctx, orderID)
	if err != nil {
		return RecoveryView{}, fmt.Errorf("get order for recovery: %w", err)
	}
	if ord.CartID != "" && carts != nil {
		if c, err := carts.GetCart(ctx, ord.CartID); err == nil {
			view.ReservationExpiresAt = c.ExpiresAt
			view.Cart = CartSnapshot{
				CartID: c.ID, Subtotal: c.Subtotal, Currency: c.Currency,
				ExpiresAt: c.ExpiresAt,
			}
			for _, item := range c.Items {
				view.Cart.Items = append(view.Cart.Items, CartItem{
					ProductID: item.ProductID, VariantID: item.VariantID, Title: item.Title,
					Quantity: item.Quantity, UnitPrice: item.UnitPrice, Total: item.Total,
				})
			}
		}
	}

	if attempts != nil {
		if a, err := attempts.GetLatestForOrder(ctx, orderID); err == nil {
			view.AttemptStatus = a.Status
			view.ErrorCode = a.ErrorCode
			view.ErrorDescription = a.ErrorDescription
		}
	}

	switch pay.Status {
	case "failed":
		view.RetryAllowed = view.ReservationExpiresAt.IsZero() || time.Now().Before(view.ReservationExpiresAt)
		if view.ErrorCode != "" {
			view.SafeMessage = fmt.Sprintf(
				"Payment wasn't completed. Razorpay reported that the payment failed (%s). Your order has not been charged twice. The cart remains reserved until %s.",
				view.ErrorCode,
				view.ReservationExpiresAt.Format("15:04:05"),
			)
		} else {
			view.SafeMessage = "Payment wasn't completed. Razorpay reported that the payment failed. Your order has not been charged twice. The cart remains reserved for 9 minutes."
		}
	case "pending", "created":
		view.RetryAllowed = true
		view.SafeMessage = "Payment is still pending. You can retry, change payment method, or cancel."
	default:
		view.SafeMessage = "This order is already " + pay.Status + "."
	}

	return view, nil
}
