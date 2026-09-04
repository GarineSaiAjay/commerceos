package campaign

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestCampaignCrossMerchantIsolation proves the P0 IDOR fix (full-
// codebase re-audit 2026-09-04): GetByID, Approve, and Reject all now
// require a merchantID that must match the campaign's own merchant_id,
// so one merchant's operator can never read, approve, or reject another
// merchant's campaign by guessing or knowing its id. Before this fix,
// all three methods took only a campaign id -- Handler.Get didn't even
// check for an authenticated operator, and Handler.Approve/Reject
// checked authentication but never authorization, so any operator of
// ANY merchant could act on ANY merchant's campaign.
func TestCampaignCrossMerchantIsolation(t *testing.T) {
	ctx := context.Background()

	pool, err := pgxpool.New(
		ctx,
		"postgres://commerceos:commerceos_dev_password@localhost:5433/commerceos?sslmode=disable",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	const (
		ownerMerchant    = "merchant_001" // already seeded (db/seeds/001_catalog.sql)
		outsiderMerchant = "merchant_campaign_idor_test"
		productID        = "airpods-pro-2" // already seeded, any merchant's campaign may target it
		campaignID       = "campaign_idor_test_owner"
	)

	// outsiderMerchant is not seeded anywhere -- create it directly so
	// campaigns.merchant_id's FK is satisfiable. merchants only has
	// (id, created_at DEFAULT NOW()), so this is the whole fixture.
	if _, err := pool.Exec(ctx, `INSERT INTO merchants (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, outsiderMerchant); err != nil {
		t.Fatal(err)
	}

	_, _ = pool.Exec(ctx, `DELETE FROM campaigns WHERE id = $1`, campaignID)

	repo := NewPostgresRepository(pool)

	owned := Campaign{
		ID:              campaignID,
		MerchantID:      ownerMerchant,
		ProductID:       productID,
		DiscountPercent: 10,
		BudgetCap:       100_000,
		DurationDays:    7,
		Status:          StatusProposed,
		PolicyVersion:   PolicyVersion,
	}
	if err := repo.Save(ctx, owned); err != nil {
		t.Fatalf("save owned campaign: %v", err)
	}

	// An outsider operator (real session, wrong merchant) must not be
	// able to even SEE ownerMerchant's campaign by id.
	if _, err := repo.GetByID(ctx, outsiderMerchant, campaignID); !errors.Is(err, ErrCampaignNotFound) {
		t.Fatalf("expected ErrCampaignNotFound for cross-merchant GetByID, got %v", err)
	}

	// The actual owner can see it.
	if got, err := repo.GetByID(ctx, ownerMerchant, campaignID); err != nil {
		t.Fatalf("owner GetByID: %v", err)
	} else if got.ID != campaignID {
		t.Fatalf("expected campaign %s, got %s", campaignID, got.ID)
	}

	// The outsider must not be able to approve it into existence as
	// ACTIVE (which would activate real ad spend against ownerMerchant's
	// budget without their consent).
	if _, err := repo.Approve(ctx, outsiderMerchant, campaignID, "outsider@evil.example"); !errors.Is(err, ErrCampaignNotProposed) {
		t.Fatalf("expected ErrCampaignNotProposed for cross-merchant Approve, got %v", err)
	}

	// ... nor reject it (killing a legitimate campaign out from under
	// its real owner).
	if _, err := repo.Reject(ctx, outsiderMerchant, campaignID, "not yours to reject"); !errors.Is(err, ErrCampaignNotProposed) {
		t.Fatalf("expected ErrCampaignNotProposed for cross-merchant Reject, got %v", err)
	}

	// Prove neither cross-merchant attempt above actually mutated the
	// campaign -- it must still be exactly PROPOSED, untouched.
	if got, err := repo.GetByID(ctx, ownerMerchant, campaignID); err != nil {
		t.Fatalf("owner re-read after blocked cross-merchant attempts: %v", err)
	} else if got.Status != StatusProposed {
		t.Fatalf("expected campaign to remain PROPOSED after blocked cross-merchant attempts, got %s", got.Status)
	}

	// The real owner can approve their own campaign.
	approved, err := repo.Approve(ctx, ownerMerchant, campaignID, "owner@merchant001.example")
	if err != nil {
		t.Fatalf("owner Approve: %v", err)
	}
	if approved.Status != StatusActive {
		t.Fatalf("expected ACTIVE after owner approval, got %s", approved.Status)
	}
}
