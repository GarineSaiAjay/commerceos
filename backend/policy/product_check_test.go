package policy

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestCheckProductsUsesStaticListWithoutLiveCheck proves the pre-
// existing, still-default behavior is unchanged: an Engine built via
// plain NewEngine (no WithProductExistsFunc call) falls back to
// config.AllowedProducts's static membership check -- exactly what
// every other test in this package already exercises implicitly, and
// what any deployment that never wires a live checker keeps getting.
func TestCheckProductsUsesStaticListWithoutLiveCheck(t *testing.T) {
	repo := newFakeRepo()
	repo.mandates["mand"] = activeMandate("merchant_001", 3_000_000, time.Now().Add(time.Hour))
	engine := NewEngine(DefaultConfig(), repo)
	svc := NewService(engine, NewRiskEngine(), repo)

	// airpods-pro-2 is in DefaultConfig().AllowedProducts.
	allowed := ProposedAction{
		Action: "CREATE_ORDER", Amount: 100_000, Currency: "INR",
		Merchant: "merchant_001", Items: []string{"airpods-pro-2"},
	}
	decision, err := svc.Propose(context.Background(), allowed, "mand")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision == DecisionRejected && decision.FailedCheck == CheckProductPermitted {
		t.Fatalf("expected airpods-pro-2 to pass the static allowlist, got rejection: %s", decision.Reason)
	}

	// A product that has never been in the static list (this is
	// literally the reported bug: a product added at runtime through
	// frontend/app/dashboard/catalog/page.tsx, item 14, is not in
	// DefaultConfig().AllowedProducts and never can be without a code
	// change) must still be rejected when no live checker is wired --
	// this is the documented, intentional fallback behavior, not a bug.
	notListed := ProposedAction{
		Action: "CREATE_ORDER", Amount: 100_000, Currency: "INR",
		Merchant: "merchant_001", Items: []string{"sony-wh-1000xm5"},
	}
	decision, err = svc.Propose(context.Background(), notListed, "mand")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != DecisionRejected || decision.FailedCheck != CheckProductPermitted {
		t.Fatalf("expected CheckProductPermitted rejection for an unlisted product, got %+v", decision)
	}
}

// TestCheckProductsUsesLiveCheckWhenWired is the actual regression test
// for the reported bug: with WithProductExistsFunc wired (as
// backend/cmd/server/main.go now does against the real catalog), a
// product that was never in the static list -- exactly like a product
// added at runtime through the dashboard -- is permitted, because the
// live check says it exists. This proves the fix, not just the
// fallback's continued correctness.
func TestCheckProductsUsesLiveCheckWhenWired(t *testing.T) {
	repo := newFakeRepo()
	repo.mandates["mand"] = activeMandate("merchant_001", 3_000_000, time.Now().Add(time.Hour))
	engine := NewEngine(DefaultConfig(), repo).
		WithProductExistsFunc(func(ctx context.Context, productID string) (bool, error) {
			// Simulates a live catalog lookup: only "sony-wh-1000xm5"
			// exists (as if just added via the dashboard), everything
			// else -- including products in the static fallback list --
			// does not, proving the live check fully replaces the
			// static one rather than merely adding to it.
			return productID == "sony-wh-1000xm5", nil
		})
	svc := NewService(engine, NewRiskEngine(), repo)

	action := ProposedAction{
		Action: "CREATE_ORDER", Amount: 100_000, Currency: "INR",
		Merchant: "merchant_001", Items: []string{"sony-wh-1000xm5"},
	}
	decision, err := svc.Propose(context.Background(), action, "mand")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision == DecisionRejected && decision.FailedCheck == CheckProductPermitted {
		t.Fatalf("expected the live-checked product to be permitted, got rejection: %s", decision.Reason)
	}

	// A product the live check says doesn't exist must still be
	// rejected, even though it's in the static AllowedProducts list --
	// the live check is authoritative once wired.
	staleListed := ProposedAction{
		Action: "CREATE_ORDER", Amount: 100_000, Currency: "INR",
		Merchant: "merchant_001", Items: []string{"airpods-pro-2"},
	}
	decision, err = svc.Propose(context.Background(), staleListed, "mand")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != DecisionRejected || decision.FailedCheck != CheckProductPermitted {
		t.Fatalf("expected the live check to reject a product it doesn't recognize, got %+v", decision)
	}
}

// TestCheckProductsFailsClosedOnLiveCheckError proves an infra error
// from the live checker rejects the proposal rather than silently
// falling back to the static list or, worse, approving -- payment
// authorization is the wrong place for optimistic error handling.
func TestCheckProductsFailsClosedOnLiveCheckError(t *testing.T) {
	repo := newFakeRepo()
	repo.mandates["mand"] = activeMandate("merchant_001", 3_000_000, time.Now().Add(time.Hour))
	boom := errors.New("catalog database unreachable")
	engine := NewEngine(DefaultConfig(), repo).
		WithProductExistsFunc(func(ctx context.Context, productID string) (bool, error) {
			return false, boom
		})
	svc := NewService(engine, NewRiskEngine(), repo)

	action := ProposedAction{
		Action: "CREATE_ORDER", Amount: 100_000, Currency: "INR",
		Merchant: "merchant_001", Items: []string{"airpods-pro-2"},
	}
	decision, err := svc.Propose(context.Background(), action, "mand")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != DecisionRejected || decision.FailedCheck != CheckProductPermitted {
		t.Fatalf("expected a fail-closed CheckProductPermitted rejection on a live-check error, got %+v", decision)
	}
}
