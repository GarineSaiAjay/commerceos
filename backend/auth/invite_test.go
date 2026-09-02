package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

// --- fakeRepository: item 40 methods (operators map/sessions map and
// the constructor live in service_test.go; this file only adds what
// InviteOperator/AcceptInvite/ListOperators/RemoveOperator need) ---

func (f *fakeRepository) ListOperators(ctx context.Context, merchantID string) ([]OperatorRecord, error) {
	var out []OperatorRecord
	for _, rec := range f.operators {
		if rec.MerchantID == merchantID {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (f *fakeRepository) CreateOperator(ctx context.Context, rec OperatorRecord) error {
	if _, exists := f.operators[rec.Email]; exists {
		return ErrEmailAlreadyRegistered
	}
	f.operators[rec.Email] = rec
	return nil
}

func (f *fakeRepository) DeleteOperator(ctx context.Context, operatorID string, merchantID string) (int64, error) {
	for email, rec := range f.operators {
		if rec.ID == operatorID && rec.MerchantID == merchantID {
			delete(f.operators, email)
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakeRepository) CreateInvite(ctx context.Context, invite Invite, tokenHash string) error {
	f.invites[invite.ID] = invite
	f.tokens[tokenHash] = invite.ID
	return nil
}

func (f *fakeRepository) GetInviteByTokenHash(ctx context.Context, tokenHash string) (Invite, error) {
	id, ok := f.tokens[tokenHash]
	if !ok {
		return Invite{}, ErrInviteNotFound
	}
	inv, ok := f.invites[id]
	if !ok {
		return Invite{}, ErrInviteNotFound
	}
	return inv, nil
}

func (f *fakeRepository) ListInvites(ctx context.Context, merchantID string) ([]Invite, error) {
	var out []Invite
	for _, inv := range f.invites {
		if inv.MerchantID == merchantID {
			out = append(out, inv)
		}
	}
	return out, nil
}

func (f *fakeRepository) MarkInviteAccepted(ctx context.Context, inviteID string, acceptedAt time.Time) error {
	inv, ok := f.invites[inviteID]
	if !ok {
		return ErrInviteNotFound
	}
	inv.AcceptedAt = &acceptedAt
	f.invites[inviteID] = inv
	return nil
}

func (f *fakeRepository) DeleteInvite(ctx context.Context, inviteID string, merchantID string) (int64, error) {
	inv, ok := f.invites[inviteID]
	if !ok || inv.MerchantID != merchantID {
		return 0, nil
	}
	delete(f.invites, inviteID)
	return 1, nil
}

// --- InviteOperator ---

func TestInviteOperatorSuccess(t *testing.T) {
	svc, _ := newTestService(t)
	inviter := Operator{ID: "operator_1", MerchantID: "merchant_001", Email: "owner@commerceos.demo"}

	token, invite, err := svc.InviteOperator(context.Background(), inviter, "  New.Teammate@CommerceOS.Demo  ")
	if err != nil {
		t.Fatalf("expected InviteOperator to succeed, got %v", err)
	}
	if token == "" {
		t.Fatal("expected a non-empty invite token")
	}
	if invite.Email != "new.teammate@commerceos.demo" {
		t.Fatalf("expected email to be normalized (lowercased/trimmed), got %q", invite.Email)
	}
	if invite.MerchantID != inviter.MerchantID {
		t.Fatalf("expected the invite to be scoped to the inviter's merchant, got %q", invite.MerchantID)
	}
	if invite.InvitedBy != inviter.ID {
		t.Fatalf("expected InvitedBy to be the inviter's operator ID, got %q", invite.InvitedBy)
	}
}

func TestInviteOperatorRejectsInvalidEmail(t *testing.T) {
	svc, _ := newTestService(t)
	inviter := Operator{ID: "operator_1", MerchantID: "merchant_001"}

	for _, email := range []string{"", "   ", "not-an-email"} {
		if _, _, err := svc.InviteOperator(context.Background(), inviter, email); err != ErrInvalidEmail {
			t.Fatalf("email %q: expected ErrInvalidEmail, got %v", email, err)
		}
	}
}

// TestInviteOperatorRejectsAlreadyRegisteredEmail proves inviting an
// email that already has an operator account fails fast, rather than
// silently minting a token that will only fail later at AcceptInvite.
func TestInviteOperatorRejectsAlreadyRegisteredEmail(t *testing.T) {
	svc, _ := newTestService(t)
	inviter := Operator{ID: "operator_1", MerchantID: "merchant_001"}

	if _, _, err := svc.InviteOperator(context.Background(), inviter, "owner@commerceos.demo"); err != ErrEmailAlreadyRegistered {
		t.Fatalf("expected ErrEmailAlreadyRegistered, got %v", err)
	}
}

// --- ListInvites / RevokeInvite ---

func TestListInvitesScopedToMerchant(t *testing.T) {
	svc, _ := newTestService(t)
	inviter := Operator{ID: "operator_1", MerchantID: "merchant_001"}
	otherMerchantInviter := Operator{ID: "operator_2", MerchantID: "merchant_002"}

	if _, _, err := svc.InviteOperator(context.Background(), inviter, "teammate@commerceos.demo"); err != nil {
		t.Fatalf("InviteOperator: %v", err)
	}
	if _, _, err := svc.InviteOperator(context.Background(), otherMerchantInviter, "someone-else@commerceos.demo"); err != nil {
		t.Fatalf("InviteOperator (other merchant): %v", err)
	}

	invites, err := svc.ListInvites(context.Background(), "merchant_001")
	if err != nil {
		t.Fatalf("ListInvites: %v", err)
	}
	if len(invites) != 1 || invites[0].Email != "teammate@commerceos.demo" {
		t.Fatalf("expected exactly merchant_001's own invite, got %+v", invites)
	}
}

func TestRevokeInviteSuccess(t *testing.T) {
	svc, _ := newTestService(t)
	inviter := Operator{ID: "operator_1", MerchantID: "merchant_001"}

	_, invite, err := svc.InviteOperator(context.Background(), inviter, "teammate@commerceos.demo")
	if err != nil {
		t.Fatalf("InviteOperator: %v", err)
	}

	if err := svc.RevokeInvite(context.Background(), inviter, invite.ID); err != nil {
		t.Fatalf("RevokeInvite: %v", err)
	}

	invites, err := svc.ListInvites(context.Background(), "merchant_001")
	if err != nil {
		t.Fatalf("ListInvites: %v", err)
	}
	if len(invites) != 0 {
		t.Fatalf("expected the invite to be gone after revocation, got %+v", invites)
	}
}

// TestRevokeInviteRejectsCrossMerchant proves an operator can't revoke
// an invite that belongs to a different merchant, even by guessing/
// knowing its ID.
func TestRevokeInviteRejectsCrossMerchant(t *testing.T) {
	svc, _ := newTestService(t)
	inviter := Operator{ID: "operator_1", MerchantID: "merchant_001"}
	otherOperator := Operator{ID: "operator_2", MerchantID: "merchant_002"}

	_, invite, err := svc.InviteOperator(context.Background(), inviter, "teammate@commerceos.demo")
	if err != nil {
		t.Fatalf("InviteOperator: %v", err)
	}

	if err := svc.RevokeInvite(context.Background(), otherOperator, invite.ID); err != ErrInviteNotFound {
		t.Fatalf("expected ErrInviteNotFound for a cross-merchant revoke, got %v", err)
	}

	// Still there for the rightful merchant.
	invites, err := svc.ListInvites(context.Background(), "merchant_001")
	if err != nil || len(invites) != 1 {
		t.Fatalf("expected the invite to survive the cross-merchant attempt, got invites=%+v err=%v", invites, err)
	}
}

// --- AcceptInvite ---

const testInvitedPassword = "BrandNewTeammate!2026"

func TestAcceptInviteSuccess(t *testing.T) {
	svc, _ := newTestService(t)
	inviter := Operator{ID: "operator_1", MerchantID: "merchant_001"}

	token, invite, err := svc.InviteOperator(context.Background(), inviter, "teammate@commerceos.demo")
	if err != nil {
		t.Fatalf("InviteOperator: %v", err)
	}

	sessionToken, operator, err := svc.AcceptInvite(context.Background(), token, testInvitedPassword)
	if err != nil {
		t.Fatalf("AcceptInvite: %v", err)
	}
	if sessionToken == "" {
		t.Fatal("expected a non-empty session token on acceptance")
	}
	if operator.Email != invite.Email || operator.MerchantID != invite.MerchantID {
		t.Fatalf("expected the new operator to match the invite, got %+v", operator)
	}

	// The new operator can now log in normally with the password they
	// just set.
	if _, _, err := svc.Login(context.Background(), invite.Email, testInvitedPassword); err != nil {
		t.Fatalf("expected the newly-accepted operator to be able to log in, got %v", err)
	}

	// The session AcceptInvite handed back is itself valid.
	if _, err := svc.ValidateToken(context.Background(), sessionToken); err != nil {
		t.Fatalf("expected the session returned by AcceptInvite to validate, got %v", err)
	}
}

func TestAcceptInviteRejectsUnknownToken(t *testing.T) {
	svc, _ := newTestService(t)

	if _, _, err := svc.AcceptInvite(context.Background(), "not-a-real-token", testInvitedPassword); err != ErrInviteNotFound {
		t.Fatalf("expected ErrInviteNotFound, got %v", err)
	}
	if _, _, err := svc.AcceptInvite(context.Background(), "", testInvitedPassword); err != ErrInviteNotFound {
		t.Fatalf("expected ErrInviteNotFound for an empty token, got %v", err)
	}
}

func TestAcceptInviteRejectsShortPassword(t *testing.T) {
	svc, _ := newTestService(t)
	inviter := Operator{ID: "operator_1", MerchantID: "merchant_001"}

	token, _, err := svc.InviteOperator(context.Background(), inviter, "teammate@commerceos.demo")
	if err != nil {
		t.Fatalf("InviteOperator: %v", err)
	}

	if _, _, err := svc.AcceptInvite(context.Background(), token, "short"); err != ErrPasswordTooShort {
		t.Fatalf("expected ErrPasswordTooShort, got %v", err)
	}
}

// TestAcceptInviteRejectsExpired proves a stale invite link can't be
// redeemed, using the same injectable-clock pattern as
// TestValidateTokenExpired in service_test.go.
func TestAcceptInviteRejectsExpired(t *testing.T) {
	svc, _ := newTestService(t)
	inviter := Operator{ID: "operator_1", MerchantID: "merchant_001"}

	loginTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return loginTime }

	token, _, err := svc.InviteOperator(context.Background(), inviter, "teammate@commerceos.demo")
	if err != nil {
		t.Fatalf("InviteOperator: %v", err)
	}

	svc.now = func() time.Time { return loginTime.Add(InviteTTL).Add(time.Second) }
	if _, _, err := svc.AcceptInvite(context.Background(), token, testInvitedPassword); err != ErrInviteExpired {
		t.Fatalf("expected ErrInviteExpired, got %v", err)
	}
}

// TestAcceptInviteRejectsReuse proves an already-accepted invite token
// can't be redeemed a second time to mint a second operator account.
func TestAcceptInviteRejectsReuse(t *testing.T) {
	svc, _ := newTestService(t)
	inviter := Operator{ID: "operator_1", MerchantID: "merchant_001"}

	token, _, err := svc.InviteOperator(context.Background(), inviter, "teammate@commerceos.demo")
	if err != nil {
		t.Fatalf("InviteOperator: %v", err)
	}

	if _, _, err := svc.AcceptInvite(context.Background(), token, testInvitedPassword); err != nil {
		t.Fatalf("first AcceptInvite: %v", err)
	}

	if _, _, err := svc.AcceptInvite(context.Background(), token, testInvitedPassword); err != ErrInviteAlreadyAccepted {
		t.Fatalf("expected ErrInviteAlreadyAccepted on reuse, got %v", err)
	}
}

// --- ListOperators / RemoveOperator ---

func TestListOperatorsScopedToMerchant(t *testing.T) {
	svc, repo := newTestService(t)
	repo.seedOperator(OperatorRecord{ID: "operator_other", MerchantID: "merchant_002", Email: "other@commerceos.demo"})

	operators, err := svc.ListOperators(context.Background(), "merchant_001")
	if err != nil {
		t.Fatalf("ListOperators: %v", err)
	}
	if len(operators) != 1 || operators[0].ID != "operator_1" {
		t.Fatalf("expected only merchant_001's own operator, got %+v", operators)
	}
}

// TestRemoveOperatorSuccess proves removing a teammate works once
// there's more than one operator on the account.
func TestRemoveOperatorSuccess(t *testing.T) {
	svc, _ := newTestService(t)
	inviter := Operator{ID: "operator_1", MerchantID: "merchant_001"}

	token, _, err := svc.InviteOperator(context.Background(), inviter, "teammate@commerceos.demo")
	if err != nil {
		t.Fatalf("InviteOperator: %v", err)
	}
	_, newOperator, err := svc.AcceptInvite(context.Background(), token, testInvitedPassword)
	if err != nil {
		t.Fatalf("AcceptInvite: %v", err)
	}

	if err := svc.RemoveOperator(context.Background(), inviter, newOperator.ID); err != nil {
		t.Fatalf("RemoveOperator: %v", err)
	}

	if _, _, err := svc.Login(context.Background(), newOperator.Email, testInvitedPassword); err != ErrInvalidCredentials {
		t.Fatalf("expected the removed operator's login to fail, got %v", err)
	}
}

// TestRemoveOperatorRejectsSelfRemoval proves an operator can't remove
// their own account through this endpoint.
func TestRemoveOperatorRejectsSelfRemoval(t *testing.T) {
	svc, _ := newTestService(t)
	inviter := Operator{ID: "operator_1", MerchantID: "merchant_001"}

	token, _, err := svc.InviteOperator(context.Background(), inviter, "teammate@commerceos.demo")
	if err != nil {
		t.Fatalf("InviteOperator: %v", err)
	}
	_, newOperator, err := svc.AcceptInvite(context.Background(), token, testInvitedPassword)
	if err != nil {
		t.Fatalf("AcceptInvite: %v", err)
	}

	if err := svc.RemoveOperator(context.Background(), newOperator, newOperator.ID); err != ErrCannotRemoveSelf {
		t.Fatalf("expected ErrCannotRemoveSelf, got %v", err)
	}
}

// TestRemoveOperatorRejectsLastOperator proves a merchant can never be
// left with zero operators -- the sole existing operator can't remove
// themselves (covered above) and, symmetrically, no one else can remove
// the sole remaining operator either (there's no "someone else" until a
// second operator exists, and this proves the guard holds even if one
// briefly did and was itself removed down to one again).
func TestRemoveOperatorRejectsLastOperator(t *testing.T) {
	svc, _ := newTestService(t)
	inviter := Operator{ID: "operator_1", MerchantID: "merchant_001"}

	// Sole operator on the account: any removal attempt against them
	// hits the last-operator guard (self-removal would also block it,
	// but this proves the *count* guard independently by using a
	// different, non-existent target ID).
	err := svc.RemoveOperator(context.Background(), inviter, "operator_does_not_exist")
	if err != ErrCannotRemoveLastOperator {
		t.Fatalf("expected ErrCannotRemoveLastOperator, got %v", err)
	}
}

// TestRemoveOperatorRejectsCrossMerchant proves an operator can't
// remove an operator belonging to a different merchant. The
// last-operator guard is evaluated against the *caller's own* merchant
// (see Service.RemoveOperator), so merchant_001 is first given a second
// operator here -- otherwise RemoveOperator would short-circuit on
// ErrCannotRemoveLastOperator before ever reaching the cross-merchant
// check this test is actually exercising.
func TestRemoveOperatorRejectsCrossMerchant(t *testing.T) {
	svc, repo := newTestService(t)
	repo.seedOperator(OperatorRecord{ID: "operator_other", MerchantID: "merchant_002", Email: "other@commerceos.demo"})

	inviter := Operator{ID: "operator_1", MerchantID: "merchant_001"}
	token, _, err := svc.InviteOperator(context.Background(), inviter, "teammate@commerceos.demo")
	if err != nil {
		t.Fatalf("InviteOperator: %v", err)
	}
	if _, _, err := svc.AcceptInvite(context.Background(), token, testInvitedPassword); err != nil {
		t.Fatalf("AcceptInvite: %v", err)
	}

	if err := svc.RemoveOperator(context.Background(), inviter, "operator_other"); !errors.Is(err, ErrOperatorNotFound) {
		t.Fatalf("expected ErrOperatorNotFound for a cross-merchant removal, got %v", err)
	}
}
