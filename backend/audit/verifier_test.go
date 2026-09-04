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

// TestAuditChainVerifierCatchesNulledHashMidChain is the regression test
// for the P2 fix (full-codebase re-audit): a NULL event_hash used to be
// skipped unconditionally, anywhere in the table -- including a row an
// attacker (or a crash between INSERT and the hash-computing UPDATE,
// postgres_writer.go) nulled out AFTER real hashed rows already existed.
// That let verification walk straight past exactly the row a tamperer
// would target. Nulling a row's hash after the chain has started must
// now be reported as ChainBroken, not silently skipped.
func TestAuditChainVerifierCatchesNulledHashMidChain(t *testing.T) {
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

	_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE entity_id LIKE 'chain_null_test_%'`)

	writer := NewPostgresWriter(pool)

	for i := 0; i < 3; i++ {
		if err := writer.Write(ctx, "test", "action", "entity", "chain_null_test_1", map[string]any{"n": i}); err != nil {
			t.Fatal(err)
		}
	}

	verifier := NewVerifier(pool)

	// Untouched chain verifies first, same as TestAuditChainVerifier.
	result, err := verifier.Verify(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified {
		t.Fatalf("expected verified chain before tampering, got Verified=%v Broken=%v at %d", result.Verified, result.ChainBroken, result.BrokenAtID)
	}

	// Null out the LAST of the 3 rows' event_hash/prev_hash --
	// simulating either direct DB tampering (edit or delete a row,
	// then null its hash to dodge the "stored hash matches recomputed"
	// check) or a crash mid-Write. Deliberately the last row, not a
	// middle one: with a middle row nulled, the row AFTER it still
	// carries the original prev_hash pointing at the tampered row's
	// real (pre-tamper) hash, so the old buggy code would still (by
	// accident) catch the tampering one row later via that linkage
	// check -- the actual, narrower hole the old code left open was
	// tampering with the CHAIN'S TAIL, where there is no subsequent
	// row left to catch a missing/nulled link. This is not a legacy
	// row either way: real hashed rows already exist before it.
	var nulledID int64
	err = pool.QueryRow(ctx, `
		UPDATE audit_events
		SET event_hash = NULL, prev_hash = NULL
		WHERE id = (
			SELECT id
			FROM audit_events
			WHERE entity_id = 'chain_null_test_1'
			  AND action = 'action'
			ORDER BY id DESC
			LIMIT 1
		)
		RETURNING id
	`).Scan(&nulledID)
	if err != nil {
		t.Fatal(err)
	}

	result, err = verifier.Verify(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ChainBroken {
		t.Fatal("expected chain broken after nulling the tail row's hash, got it silently skipped -- this is exactly the P2 bug: no subsequent row exists to catch a missing link at the chain's tail")
	}
	if result.BrokenAtID != nulledID {
		t.Fatalf("expected BrokenAtID %d (the nulled row), got %d", nulledID, result.BrokenAtID)
	}

	_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE entity_id LIKE 'chain_null_test_%'`)
}
