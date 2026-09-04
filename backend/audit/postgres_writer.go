package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Writer appends immutable, hash-chained audit records.
type Writer interface {
	Write(
		ctx context.Context,
		actor string,
		action string,
		entityType string,
		entityID string,
		detail map[string]any,
	) error
}

type PostgresWriter struct {
	db *pgxpool.Pool
}

func NewPostgresWriter(db *pgxpool.Pool) *PostgresWriter {
	return &PostgresWriter{db: db}
}

// auditChainLockKey is an arbitrary, fixed Postgres advisory-lock key
// used to serialize every Write call against every other one -- see the
// pg_advisory_xact_lock call below for why. Picked with no significance
// beyond "not zero and not likely to collide with an advisory lock some
// future feature adds" -- there is exactly one hash chain
// (audit_events) in this codebase today, so one fixed key is enough;
// if a second independently-chained table is ever added, it needs its
// own distinct key here.
const auditChainLockKey = 0x415544495430 // "AUDIT0" in hex, arbitrary

// Write inserts an audit row with event_hash = SHA256(content + prev_hash),
// forming a tamper-evident chain. The hash is computed over the DB's
// canonical JSONB text so the verifier reproduces it exactly.
func (w *PostgresWriter) Write(
	ctx context.Context,
	actor string,
	action string,
	entityType string,
	entityID string,
	detail map[string]any,
) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("marshal audit detail: %w", err)
	}

	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin audit tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Serialize the whole "read the previous link, compute this row's
	// hash from it, write it" sequence below against every other
	// concurrent Write call. Without this (P0 fix, full-codebase
	// re-audit 2026-09-04), two Write calls overlapping -- trivially
	// triggered by any two audit-worthy events happening close together,
	// e.g. two orders paid at once -- could both read the same "last
	// hashed row" before either committed, compute the same prev_hash,
	// and both commit: two rows pointing at the same ancestor instead of
	// a linear chain. Verifier.Verify walks strictly by id and expects
	// each row's prev_hash to equal the row immediately before it, so
	// the second of the forked pair would fail verification and the
	// whole chain gets reported ChainBroken=true -- a FALSE tamper
	// report, on the exact endpoint (GET /trust/summary) meant to prove
	// the chain hasn't been tampered with. pg_advisory_xact_lock blocks
	// every other transaction trying to acquire the same key until this
	// one commits or rolls back (automatic release either way), which
	// is exactly the "only one appender at a time" guarantee a
	// singly-linked hash chain needs; a plain row lock on the previous
	// row (e.g. SELECT ... FOR UPDATE on the query just below) would NOT
	// be sufficient on its own, because the query that finds "the last
	// hashed row" has to
	// re-evaluate ORDER BY/LIMIT against the table's current state, not
	// just re-check one already-identified row.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(auditChainLockKey)); err != nil {
		return fmt.Errorf("acquire audit chain lock: %w", err)
	}

	// Insert the row without hashes first.
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO audit_events (actor, action, entity_type, entity_id, detail)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, actor, action, entityType, entityID, raw).Scan(&id)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}

	// Fetch the previous event_hash (the last row's).
	var prevHash *string
	_ = tx.QueryRow(ctx, `
		SELECT event_hash
		FROM audit_events
		WHERE id < $1
		AND event_hash IS NOT NULL
		ORDER BY id DESC
		LIMIT 1
	`, id).Scan(&prevHash)

	// Read back the canonical detail text from the DB.
	var detailText string
	err = tx.QueryRow(ctx, `
		SELECT detail::text
		FROM audit_events
		WHERE id = $1
	`, id).Scan(&detailText)
	if err != nil && err != pgx.ErrNoRows {
		return fmt.Errorf("read canonical detail: %w", err)
	}

	content := detailText + "|" + actor + "|" + action + "|" + entityType + "|" + entityID
	if prevHash != nil {
		content += "|" + *prevHash
	}

	sum := sha256.Sum256([]byte(content))
	eventHash := hex.EncodeToString(sum[:])

	_, err = tx.Exec(ctx, `
		UPDATE audit_events
		SET event_hash = $1, prev_hash = $2
		WHERE id = $3
	`, eventHash, prevHash, id)
	if err != nil {
		return fmt.Errorf("set audit hash: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit audit event: %w", err)
	}

	return nil
}
