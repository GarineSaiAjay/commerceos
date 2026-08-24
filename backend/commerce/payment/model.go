package payment

type Payment struct {
	ID                string `json:"payment_id"`
	OrderID           string `json:"order_id"`
	Provider          string `json:"provider"`
	ProviderOrderID   string `json:"provider_order_id"`
	ProviderPaymentID string `json:"provider_payment_id"`
	Amount            int64  `json:"amount"`
	Currency          string `json:"currency"`
	Status            string `json:"status"`
	KeyID             string `json:"key_id"`
	IdempotencyKey    string `json:"idempotency_key"`
}

type CreatePaymentRequest struct {
	OrderID  string
	Amount   int64
	Currency string
}
