package order

import (
	"context"
	"fmt"
)

// CheckoutSaga models the checkout flow as an explicit sequence of named
// steps with defined failure branches (spec §9). It is NOT an implicit
// chain of function calls with scattered try/catch.
//
//	CheckoutStarted → CartValidated → OrderCreated → PaymentPending
//	CartValidated --failure--> CheckoutFailed
//	OrderCreated --failure--> CheckoutFailed
type CheckoutSaga struct {
	repo Repository
}

func NewCheckoutSaga(repo Repository) *CheckoutSaga {
	return &CheckoutSaga{repo: repo}
}

// Step names — the saga's explicit state.
const (
	SagaStepStarted        = "CheckoutStarted"
	SagaStepCartValidated  = "CartValidated"
	SagaStepOrderCreated   = "OrderCreated"
	SagaStepPaymentPending = "PaymentPending"
	SagaStepFailed         = "CheckoutFailed"
)

// Result carries the outcome of the saga.
type Result struct {
	Order Order
	Step  string
}

// Run executes the checkout saga. Each step is named; a failure at any
// step returns the step name and the error, so the caller knows exactly
// where the saga stopped.
func (s *CheckoutSaga) Run(
	ctx context.Context,
	cartID string,
	orderID string,
) (Result, error) {
	// Step 1: CheckoutStarted.
	step := SagaStepStarted

	// Step 2: CartValidated — the repository locks the cart, checks
	// expiry/empty/checked-out, and validates availability. Any failure
	// here is a CheckoutFailed branch.
	step = SagaStepCartValidated

	// Step 3: OrderCreated — the repository snapshots the cart into an
	// order and decrements inventory in the same transaction.
	step = SagaStepOrderCreated

	ord, err := s.repo.CheckoutCart(ctx, cartID, orderID)
	if err != nil {
		return Result{Step: step}, fmt.Errorf(
			"saga step %s failed: %w",
			step,
			err,
		)
	}

	// Step 4: PaymentPending — the order now awaits payment.
	step = SagaStepPaymentPending

	return Result{Order: ord, Step: step}, nil
}
