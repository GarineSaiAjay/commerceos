package audit

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestAuditChainVerifier proves: Verified on untouched log, Chain broken
// after editing one historical row.
func TestAuditChainVerifier(t *testing.T) {
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

	// Use a dedicated prefix and clean any prior rows.
	_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE entity_id LIKE 'chain_test_%'`)

	writer := NewPostgresWriter(pool)

	for i := 0; i < 3; i++ {
		if err := writer.Write(ctx, "test", "action", "entity", "chain_test_1", map[string]any{"n": i}); err != nil {
			t.Fatal(err)
		}
	}

	verifier := NewVerifier(pool)

	// 1. Untouched chain → Verified.
	result, err := verifier.Verify(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified {
		t.Fatalf("expected verified chain, got Verified=%v Broken=%v at %d", result.Verified, result.ChainBroken, result.BrokenAtID)
	}

	// 2. Tamper with a historical row (change the actor).
	_, err = pool.Exec(ctx, `
		UPDATE audit_events
		SET actor = 'TAMPERED'
		WHERE id = (
			SELECT id
			FROM audit_events
			WHERE entity_id = 'chain_test_1'
			  AND action = 'action'
			ORDER BY id
			LIMIT 1
		)
	`)
	if err != nil {
		t.Fatal(err)
	}

	result, err = verifier.Verify(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ChainBroken {
		t.Fatal("expected chain broken after tampering")
	}

	// Clean up.
	_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE entity_id LIKE 'chain_test_%'`)
}
