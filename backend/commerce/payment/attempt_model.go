package payment

type PaymentAttempt struct {
	ID                string `json:"attempt_id"`
	PaymentID         string `json:"payment_id"`
	OrderID           string `json:"order_id"`
	ProviderOrderID   string `json:"provider_order_id"`
	RazorpayPaymentID string `json:"razorpay_payment_id"`
	Amount            int64  `json:"amount"`
	Currency          string `json:"currency"`
	Status            string `json:"status"`
	ErrorCode         string `json:"error_code"`
	ErrorDescription  string `json:"error_description"`
}
