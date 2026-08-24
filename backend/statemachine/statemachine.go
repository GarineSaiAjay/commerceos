package statemachine

import (
	"errors"
	"fmt"
)

// ErrIllegalTransition is returned when a state transition is not an
// allowed edge in the explicit transition table.
var ErrIllegalTransition = errors.New("illegal state transition")

// TransitionTable maps (from, to) pairs that are legal. This is the
// single source of truth for what transitions are allowed — no scattered
// `if` statements anywhere in the codebase may decide legality.
type TransitionTable struct {
	allowed map[string]bool
}

func NewTransitionTable(edges [][2]string) *TransitionTable {
	allowed := map[string]bool{}

	for _, edge := range edges {
		allowed[edgeKey(edge[0], edge[1])] = true
	}

	return &TransitionTable{allowed: allowed}
}

func edgeKey(from, to string) string {
	return from + "->" + to
}

// CanTransition reports whether the edge from -> to is legal.
func (t *TransitionTable) CanTransition(from, to string) bool {
	_, ok := t.allowed[edgeKey(from, to)]
	return ok
}

// Transition returns the destination state if legal, or
// ErrIllegalTransition otherwise. It never silently coerces.
// Transition returns the new state if legal, or ErrIllegalTransition.
// It never silently coerces.
func (t *TransitionTable) Transition(from, to string) (string, error) {
	if !t.CanTransition(from, to) {
		return "", errors.Join(
			ErrIllegalTransition,
			errors.New(fmt.Sprintf("%s -> %s", from, to)),
		)
	}

	return to, nil
}

// Payment states.
const (
	PaymentCreated    = "created"
	PaymentPending    = "pending"
	PaymentAuthorized = "authorized"
	PaymentCaptured   = "captured"
	PaymentCompleted  = "completed"
	PaymentFailed     = "failed"
)

// PaymentTransitionTable is the explicit allowed-edge table for the
// payment lifecycle:
//
//	CREATED → PENDING → AUTHORIZED → CAPTURED → COMPLETED
//	PENDING → FAILED
func PaymentTransitionTable() *TransitionTable {
	return NewTransitionTable([][2]string{
		{PaymentCreated, PaymentPending},
		{PaymentPending, PaymentAuthorized},
		{PaymentAuthorized, PaymentCaptured},
		{PaymentCaptured, PaymentCompleted},
		{PaymentPending, PaymentFailed},
	})
}

// Order states.
const (
	OrderDraft              = "draft"
	OrderAuthorized         = "authorized"
	OrderPaymentPending     = "payment_pending"
	OrderPaid               = "paid"
	OrderFulfillmentPending = "fulfillment_pending"
	OrderCompleted          = "completed"
	OrderFailed             = "failed"
	OrderCancelled          = "cancelled"
)

// OrderTransitionTable is the allowed-edge graph for the order lifecycle:
//
//	DRAFT → AUTHORIZED → PAYMENT_PENDING → PAID → FULFILLMENT_PENDING → COMPLETED
//	PAYMENT_PENDING → FAILED
//	DRAFT → CANCELLED
//
// Note: the checkout path creates orders directly in PAYMENT_PENDING
// (the cart is validated + inventory committed in one transaction), so
// DRAFT → PAYMENT_PENDING is a legal edge to keep that entry consistent.
func OrderTransitionTable() *TransitionTable {
	return NewTransitionTable([][2]string{
		{OrderDraft, OrderAuthorized},
		{OrderDraft, OrderPaymentPending},
		{OrderAuthorized, OrderPaymentPending},
		{OrderPaymentPending, OrderPaid},
		{OrderPaid, OrderFulfillmentPending},
		{OrderFulfillmentPending, OrderCompleted},
		{OrderPaymentPending, OrderFailed},
		{OrderDraft, OrderCancelled},
	})
}
