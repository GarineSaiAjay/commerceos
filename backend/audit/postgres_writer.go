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
