package payment

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// WebhookSignatureVerifier verifies Razorpay webhook signatures.
//
// Razorpay signs the raw request body with HMAC-SHA256 using the key
// secret, and sends the digest in the `x-razorpay-signature` header.
type WebhookSignatureVerifier struct {
	keySecret string
}

func NewWebhookSignatureVerifier(keySecret string) *WebhookSignatureVerifier {
	return &WebhookSignatureVerifier{keySecret: keySecret}
}

// Verify returns nil if the signature matches the body, or an error
// otherwise. A failed verification is a security event — the caller must
// not process the payload.
func (v *WebhookSignatureVerifier) Verify(body []byte, signature string) error {
	if signature == "" {
		return fmt.Errorf("missing x-razorpay-signature header")
	}

	mac := hmac.New(sha256.New, []byte(v.keySecret))
	_, _ = mac.Write(body)

	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("invalid razorpay webhook signature")
	}

	return nil
}
