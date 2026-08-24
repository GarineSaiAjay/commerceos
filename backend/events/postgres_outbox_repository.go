package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresOutboxRepository struct {
	db *pgxpool.Pool
}

func NewPostgresOutboxRepository(db *pgxpool.Pool) *PostgresOutboxRepository {
	return &PostgresOutboxRepository{db: db}
}

func (r *PostgresOutboxRepository) Insert(
	ctx context.Context,
	eventType string,
	payload any,
) (int64, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal outbox payload: %w", err)
	}

	var id int64

	err = r.db.QueryRow(ctx, `
		INSERT INTO outbox_events (event_type, payload)
		VALUES ($1, $2)
		RETURNING id
	`, eventType, raw).Scan(&id)

	if err != nil {
		return 0, fmt.Errorf("insert outbox event: %w", err)
	}

	return id, nil
}

func (r *PostgresOutboxRepository) PollUnpublished(
	ctx context.Context,
	limit int,
) ([]OutboxEvent, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, event_type, payload, created_at
		FROM outbox_events
		WHERE published_at IS NULL
		ORDER BY id
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("poll outbox: %w", err)
	}
	defer rows.Close()

	var events []OutboxEvent

	for rows.Next() {
		var ev OutboxEvent

		if err := rows.Scan(
			&ev.ID,
			&ev.EventType,
			&ev.Payload,
			&ev.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan outbox event: %w", err)
		}

		events = append(events, ev)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox: %w", err)
	}

	return events, nil
}

func (r *PostgresOutboxRepository) MarkPublished(
	ctx context.Context,
	ids []int64,
) error {
	if len(ids) == 0 {
		return nil
	}

	_, err := r.db.Exec(ctx, `
		UPDATE outbox_events
		SET published_at = NOW()
		WHERE id = ANY($1)
		  AND published_at IS NULL
	`, ids)

	if err != nil {
		return fmt.Errorf("mark outbox published: %w", err)
	}

	return nil
}

// Ensure PostgresOutboxRepository satisfies OutboxRepository.
var _ OutboxRepository = (*PostgresOutboxRepository)(nil)

// pgx import is used for the ANY($1) array binding.
var _ = pgx.ErrNoRows
