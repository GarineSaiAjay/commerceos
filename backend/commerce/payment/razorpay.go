package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sync/atomic"

	"github.com/razorpay/razorpay-go"
)

type RazorpayClient struct {
	client    *razorpay.Client
	keyID     string
	keySecret string

	// callCount is the internal call counter required by Phase 1.
	// Later phases repeatedly need to prove "zero calls were made to
	// Razorpay" — this counter is how that is verified, not by
	// eyeballing logs. It counts every outbound Orders API call.
	callCount atomic.Int64
}

func NewRazorpayClient(
	keyID string,
	keySecret string,
) *RazorpayClient {
	return &RazorpayClient{
		client:    razorpay.NewClient(keyID, keySecret),
		keyID:     keyID,
		keySecret: keySecret,
	}
}

// CallCount returns the number of outbound Razorpay Orders API calls
// made through this adapter since it was constructed.
func (c *RazorpayClient) CallCount() int64 {
	return c.callCount.Load()
}

func (c *RazorpayClient) CreatePayment(
	ctx context.Context,
	req CreatePaymentRequest,
) (Payment, error) {
	if req.Amount <= 0 {
		return Payment{}, fmt.Errorf("payment amount must be greater than zero")
	}

	if req.Currency == "" {
		return Payment{}, fmt.Errorf("currency is required")
	}

	data := map[string]interface{}{
		"amount":   req.Amount,
		"currency": req.Currency,
		"receipt":  req.OrderID,
	}

	c.callCount.Add(1)

	response, err := c.client.Order.Create(data, nil)
	if err != nil {
		return Payment{}, fmt.Errorf(
			"failed to create razorpay order: %w",
			err,
		)
	}

	id, ok := response["id"].(string)
	if !ok || id == "" {
		return Payment{}, fmt.Errorf(
			"razorpay response missing order id",
		)
	}

	// Razorpay returns amount as an integer number of paise.
	// The SDK decodes numbers as float64; reject any fractional value
	// rather than silently truncating a wrong amount.
	responseAmount, ok := response["amount"].(float64)
	if !ok {
		return Payment{}, fmt.Errorf(
			"razorpay response missing amount",
		)
	}
	if responseAmount != math.Trunc(responseAmount) {
		return Payment{}, fmt.Errorf(
			"razorpay amount must be a whole number of paise, got %v",
			responseAmount,
		)
	}

	responseCurrency, ok := response["currency"].(string)
	if !ok {
		return Payment{}, fmt.Errorf(
			"razorpay response missing currency",
		)
	}

	status, _ := response["status"].(string)

	return Payment{
		ID:              id,
		OrderID:         req.OrderID,
		Provider:        "razorpay",
		ProviderOrderID: id,
		Amount:          int64(responseAmount),
		Currency:        responseCurrency,
		Status:          status,
		KeyID:           c.keyID,
	}, nil
}

func (c *RazorpayClient) VerifyPaymentSignature(
	razorpayOrderID string,
	razorpayPaymentID string,
	signature string,
) error {
	message := razorpayOrderID + "|" + razorpayPaymentID

	mac := hmac.New(
		sha256.New,
		[]byte(c.keySecret),
	)

	_, _ = mac.Write([]byte(message))

	expectedSignature := hex.EncodeToString(
		mac.Sum(nil),
	)

	if !hmac.Equal(
		[]byte(expectedSignature),
		[]byte(signature),
	) {
		return fmt.Errorf("invalid razorpay payment signature")
	}

	return nil
}
