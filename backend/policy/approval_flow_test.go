package policy

import (
	"context"
	"testing"
	"time"
)

// approvalRepo is an in-memory repo with real approval-request storage.
type approvalRepo struct {
	*fakeRepo
	requests map[string]ApprovalRequest
	next     int
}

func newApprovalRepo() *approvalRepo {
	r := &approvalRepo{fakeRepo: newFakeRepo(), requests: map[string]ApprovalRequest{}}
	r.mandates["mand"] = mandate("merchant_001", 3_000_000, time.Now().Add(time.Hour))
	return r
}

func (r *approvalRepo) SaveApprovalRequest(ctx context.Context, a ApprovalRequest) error {
	r.requests[a.ID] = a
	return nil
}

func (r *approvalRepo) GetApprovalRequest(ctx context.Context, id string) (ApprovalRequest, error) {
	if a, ok := r.requests[id]; ok {
		return a, nil
	}
	return ApprovalRequest{}, ErrApprovalRequestNotFound
}

func (r *approvalRepo) GetPendingApprovalForAction(ctx context.Context, a ProposedAction) (ApprovalRequest, error) {
	for _, req := range r.requests {
		if req.Status == "PENDING" && req.Action == a.Action && req.Amount == a.Amount &&
			req.Currency == a.Currency && req.Merchant == a.Merchant {
			return req, nil
		}
	}
	return ApprovalRequest{}, ErrApprovalRequestNotFound
}

func (r *approvalRepo) UpdateApprovalRequestStatus(ctx context.Context, id, status, authorizationID, reason string) error {
	a, ok := r.requests[id]
	if !ok {
		return ErrApprovalRequestNotFound
	}
	a.Status = status
	a.AuthorizationID = authorizationID
	a.Reason = reason
	r.requests[id] = a
	return nil
}

func (r *approvalRepo) ListApprovalRequests(ctx context.Context, status string, limit int) ([]ApprovalRequest, error) {
	var out []ApprovalRequest
	for _, a := range r.requests {
		if status == "" || a.Status == status {
			out = append(out, a)
		}
	}
	return out, nil
}

func TestApprovalFlowLevel2(t *testing.T) {
	ctx := context.Background()
	repo := newApprovalRepo()
	svc := NewService(NewEngine(DefaultConfig(), repo), NewRiskEngine(), repo)

	action := ProposedAction{
		Action: "CREATE_ORDER", Amount: 500_000, Currency: "INR",
		Merchant: "merchant_001", Items: []string{"airpods-pro-2"}, CartID: "cart_a",
	}

	// Level 2 proposal → PENDING_HUMAN_APPROVAL, no authorization yet.
	first, err := svc.Propose(ctx, action, "mand")
	if err != nil {
		t.Fatal(err)
	}
	if first.Decision != DecisionPendingApproval {
		t.Fatalf("expected PENDING_HUMAN_APPROVAL, got %s", first.Decision)
	}
	if first.ApprovalRequestID == "" {
		t.Fatal("expected an approval_request_id")
	}
	if len(repo.authorizations) != 0 {
		t.Fatalf("expected 0 authorizations before approval, got %d", len(repo.authorizations))
	}

	// Repeated propose reuses the same pending request.
	second, err := svc.Propose(ctx, action, "mand")
	if err != nil {
		t.Fatal(err)
	}
	if second.Decision != DecisionPendingApproval || second.ApprovalRequestID != first.ApprovalRequestID {
		t.Fatalf("expected same pending request reused, got %s vs %s", second.ApprovalRequestID, first.ApprovalRequestID)
	}

	// Approve → issues one-time authorization.
	approved, err := svc.Approve(ctx, first.ApprovalRequestID, "", "merchant_operator")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Decision != DecisionApproved || approved.AuthorizationID == "" {
		t.Fatalf("expected APPROVED with authorization, got %+v", approved)
	}
	if len(repo.authorizations) != 1 {
		t.Fatalf("expected 1 authorization after approval, got %d", len(repo.authorizations))
	}

	// Approving again is idempotent → same authorization, still 1 auth.
	again, err := svc.Approve(ctx, first.ApprovalRequestID, "", "merchant_operator")
	if err != nil {
		t.Fatal(err)
	}
	if again.AuthorizationID != approved.AuthorizationID {
		t.Fatalf("expected same authorization reused, got %s vs %s", again.AuthorizationID, approved.AuthorizationID)
	}
	if len(repo.authorizations) != 1 {
		t.Fatalf("expected still 1 authorization after repeat approve, got %d", len(repo.authorizations))
	}
}

func TestApprovalFlowReject(t *testing.T) {
	ctx := context.Background()
	repo := newApprovalRepo()
	svc := NewService(NewEngine(DefaultConfig(), repo), NewRiskEngine(), repo)

	action := ProposedAction{
		Action: "CREATE_ORDER", Amount: 2_000_000, Currency: "INR",
		Merchant: "merchant_001", Items: []string{"airpods-pro-2"}, CartID: "cart_b",
	}

	first, err := svc.Propose(ctx, action, "mand")
	if err != nil {
		t.Fatal(err)
	}
	if first.Decision != DecisionPendingApproval {
		t.Fatalf("expected PENDING_HUMAN_APPROVAL for Level 3, got %s", first.Decision)
	}

	// Reject → no authorization, marking REJECTED.
	if err := svc.Reject(ctx, first.ApprovalRequestID, "", "operator", "buyer changed mind"); err != nil {
		t.Fatal(err)
	}
	if len(repo.authorizations) != 0 {
		t.Fatalf("expected 0 authorizations after reject, got %d", len(repo.authorizations))
	}
	req, _ := repo.GetApprovalRequest(ctx, first.ApprovalRequestID)
	if req.Status != "REJECTED" {
		t.Fatalf("expected REJECTED status, got %s", req.Status)
	}

	// Approving a rejected request must fail.
	if _, err := svc.Approve(ctx, first.ApprovalRequestID, "", "operator"); err == nil {
		t.Fatal("expected error approving a rejected request")
	}
}

func TestApprovalRequiresVerifiedCaller(t *testing.T) {
	ctx := context.Background()
	repo := newApprovalRepo()
	svc := NewService(NewEngine(DefaultConfig(), repo), NewRiskEngine(), repo)

	action := ProposedAction{
		Action: "CREATE_ORDER", Amount: 2_000_000, Currency: "INR",
		Merchant: "merchant_001", Items: []string{"airpods-pro-2"}, CartID: "cart_c",
	}
	first, err := svc.Propose(ctx, action, "mand")
	if err != nil {
		t.Fatal(err)
	}

	// No cart_id and no operator email: the caller is neither the buyer who
	// created the request nor an authenticated operator, so both actions
	// must be refused.
	if _, err := svc.Approve(ctx, first.ApprovalRequestID, "", ""); err != ErrApprovalUnauthorized {
		t.Fatalf("expected ErrApprovalUnauthorized approving with no identity, got %v", err)
	}
	if err := svc.Reject(ctx, first.ApprovalRequestID, "", "", "no reason"); err != ErrApprovalUnauthorized {
		t.Fatalf("expected ErrApprovalUnauthorized rejecting with no identity, got %v", err)
	}

	// A cart_id that doesn't belong to this request proves nothing.
	if _, err := svc.Approve(ctx, first.ApprovalRequestID, "some_other_cart", ""); err != ErrApprovalUnauthorized {
		t.Fatalf("expected ErrApprovalUnauthorized approving with a mismatched cart_id, got %v", err)
	}

	// The buyer who owns the cart the request was created for can approve
	// it without an operator session.
	approved, err := svc.Approve(ctx, first.ApprovalRequestID, "cart_c", "")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Decision != DecisionApproved || approved.AuthorizationID == "" {
		t.Fatalf("expected the verified buyer's approval to succeed, got %+v", approved)
	}
}

func TestApprovalLevel1AutoApproved(t *testing.T) {
	ctx := context.Background()
	repo := newApprovalRepo()
	svc := NewService(NewEngine(DefaultConfig(), repo), NewRiskEngine(), repo)

	// Level 1 amount → auto-approved immediately, no approval request.
	action := ProposedAction{
		Action: "CREATE_ORDER", Amount: 50_000, Currency: "INR",
		Merchant: "merchant_001", Items: []string{"airpods-case"},
	}
	d, err := svc.Propose(ctx, action, "mand")
	if err != nil {
		t.Fatal(err)
	}
	if d.Decision != DecisionApproved {
		t.Fatalf("expected Level 1 to auto-approve, got %s", d.Decision)
	}
	if d.AuthorizationID == "" {
		t.Fatal("expected an authorization for Level 1")
	}
	if len(repo.requests) != 0 {
		t.Fatalf("expected 0 approval requests for Level 1, got %d", len(repo.requests))
	}
}
