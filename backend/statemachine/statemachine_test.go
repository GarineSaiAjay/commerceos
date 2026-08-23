package statemachine

import (
	"errors"
	"testing"
)

func TestPaymentLegalTransitions(t *testing.T) {
	table := PaymentTransitionTable()

	// Full happy path.
	state := PaymentCreated
	state, err := table.Transition(state, PaymentPending)
	if err != nil {
		t.Fatal(err)
	}
	state, err = table.Transition(state, PaymentAuthorized)
	if err != nil {
		t.Fatal(err)
	}
	state, err = table.Transition(state, PaymentCaptured)
	if err != nil {
		t.Fatal(err)
	}
	state, err = table.Transition(state, PaymentCompleted)
	if err != nil {
		t.Fatal(err)
	}

	if state != PaymentCompleted {
		t.Fatalf("expected completed, got %s", state)
	}
}

func TestPaymentPendingToFailed(t *testing.T) {
	table := PaymentTransitionTable()

	state, err := table.Transition(PaymentCreated, PaymentPending)
	if err != nil {
		t.Fatal(err)
	}

	state, err = table.Transition(state, PaymentFailed)
	if err != nil {
		t.Fatal(err)
	}

	if state != PaymentFailed {
		t.Fatalf("expected failed, got %s", state)
	}
}

func TestPaymentIllegalTransitionRejected(t *testing.T) {
	table := PaymentTransitionTable()

	// CREATED → CAPTURED skips PENDING and AUTHORIZED.
	_, err := table.Transition(PaymentCreated, PaymentCaptured)
	if err == nil {
		t.Fatal("expected illegal transition to be rejected")
	}

	if !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("expected ErrIllegalTransition, got %v", err)
	}
}

func TestOrderIllegalTransitionRejected(t *testing.T) {
	table := OrderTransitionTable()

	// DRAFT → COMPLETED directly, skipping every intermediate state.
	_, err := table.Transition(OrderDraft, OrderCompleted)
	if err == nil {
		t.Fatal("expected DRAFT -> COMPLETED to be rejected")
	}

	if !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("expected ErrIllegalTransition, got %v", err)
	}
}

func TestOrderLegalPath(t *testing.T) {
	table := OrderTransitionTable()

	state := OrderDraft
	state, err := table.Transition(state, OrderAuthorized)
	if err != nil {
		t.Fatal(err)
	}
	state, err = table.Transition(state, OrderPaymentPending)
	if err != nil {
		t.Fatal(err)
	}
	state, err = table.Transition(state, OrderPaid)
	if err != nil {
		t.Fatal(err)
	}
	state, err = table.Transition(state, OrderFulfillmentPending)
	if err != nil {
		t.Fatal(err)
	}
	state, err = table.Transition(state, OrderCompleted)
	if err != nil {
		t.Fatal(err)
	}

	if state != OrderCompleted {
		t.Fatalf("expected completed, got %s", state)
	}
}
