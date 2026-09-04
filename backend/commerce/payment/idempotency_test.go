package payment

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/garinesaiajay/commerceos/commerce/order"
	"github.com/garinesaiajay/commerceos/policy"
)

// countingProvider counts CreatePayment calls (the adapter call counter
// equivalent).
type countingProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *countingProvider) CreatePayment(
	ctx context.Context,
	req CreatePaymentRequest,
) (Payment, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++

	return Payment{
		ID:       "pay_created_001",
		OrderID:  req.OrderID,
		Amount:   req.Amount,
		Currency: req.Currency,
		Status:   "created",
	}, nil
}

func (p *countingProvider) VerifyPaymentSignature(
	razorpayOrderID string,
	razorpayPaymentID string,
	signature string,
) error {
	return nil
}

func (p *countingProvider) CallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// memRepo is an in-memory payment repository for idempotency tests.
type memRepo struct {
	byOrder map[string]Payment
	byKey   map[string]Payment
}

func newMemRepo() *memRepo {
	return &memRepo{
		byOrder: map[string]Payment{},
		byKey:   map[string]Payment{},
	}
}

func (r *memRepo) GetByOrderID(ctx context.Context, orderID string) (Payment, error) {
	p, ok := r.byOrder[orderID]
	if !ok {
		return Payment{}, ErrPaymentNotFound
	}
	return p, nil
}

// GetByProviderOrderID: full-codebase re-audit (P2) added this method
// to the Repository interface (see repository.go's doc comment) --
// memRepo has no separate index for it, so it linear-scans byOrder for
// a matching ProviderOrderID, which is fine for this fake's tiny test
// fixtures.
func (r *memRepo) GetByProviderOrderID(ctx context.Context, providerOrderID string) (Payment, error) {
	for _, p := range r.byOrder {
		if p.ProviderOrderID == providerOrderID {
			return p, nil
		}
	}
	return Payment{}, ErrPaymentNotFound
}

func (r *memRepo) GetByIdempotencyKey(ctx context.Context, key string) (Payment, error) {
	p, ok := r.byKey[key]
	if !ok {
		return Payment{}, ErrPaymentNotFound
	}
	return p, nil
}

func (r *memRepo) Create(ctx context.Context, payment Payment) error {
	r.byOrder[payment.OrderID] = payment
	if payment.IdempotencyKey != "" {
		r.byKey[payment.IdempotencyKey] = payment
	}
	return nil
}

func (r *memRepo) TransitionStatus(
	ctx context.Context,
	orderID string,
	to string,
	providerPaymentID string,
) (Payment, error) {
	p, ok := r.byOrder[orderID]
	if !ok {
		return Payment{}, ErrPaymentNotFound
	}
	p.Status = to
	r.byOrder[orderID] = p
	return p, nil
}

// okAuthorizer approves any authorization — used for idempotency tests.
// CartID/Currency/Amount are echoed back on every VerifyAuthorization
// response so they satisfy CreatePaymentOrder's post-fix binding checks
// (P0 fix, full-codebase re-audit 2026-09-04) -- set these to match
// whatever order.Order the test is exercising, same convention as
// consumingAuthorizer in authorization_consume_test.go.
type okAuthorizer struct {
	CartID   string
	Currency string
	Amount   int64
}

func (a okAuthorizer) VerifyAuthorization(
	ctx context.Context,
	id string,
) (policy.Authorization, error) {
	return policy.Authorization{
		ID:        id,
		Status:    "ACTIVE",
		ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339),
		CartID:    a.CartID,
		Currency:  a.Currency,
		Amount:    a.Amount,
	}, nil
}

// TestIdempotencyKeySameKeyNoSecondRazorpayOrder proves spec §7.4:
// submitting the same idempotency key twice creates exactly one
// Razorpay order (provider call count increases by exactly one).
func TestIdempotencyKeySameKeyNoSecondRazorpayOrder(t *testing.T) {
	provider := &countingProvider{}
	repo := newMemRepo()
	service := NewServiceWithAuthorizer(provider, repo, nil, nil, okAuthorizer{
		CartID: "cart_923", Currency: "INR", Amount: 24900,
	})

	ord := order.Order{
		ID:       "order_idem_001",
		CartID:   "cart_923",
		Subtotal: 24900,
		Currency: "INR",
	}

	key := "merchant_001:cart_923:checkout_7:attempt_1"

	first, err := service.CreatePaymentOrder(
		context.Background(),
		ord,
		key,
		"auth_ok",
	)
	if err != nil {
		t.Fatal(err)
	}

	if provider.CallCount() != 1 {
		t.Fatalf("expected 1 provider call after first submit, got %d", provider.CallCount())
	}

	second, err := service.CreatePaymentOrder(
		context.Background(),
		ord,
		key,
		"auth_ok",
	)
	if err != nil {
		t.Fatal(err)
	}

	if provider.CallCount() != 1 {
		t.Fatalf(
			"expected provider call count to stay 1 after duplicate submit, got %d",
			provider.CallCount(),
		)
	}

	if second.ID != first.ID {
		t.Fatalf(
			"expected same payment returned, got %s vs %s",
			second.ID,
			first.ID,
		)
	}
}

var _ = errors.New
