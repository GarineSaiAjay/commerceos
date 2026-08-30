package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresConversationStore is the concrete ConversationStore backing
// the agent_conversations table (db/migrations/20260830090000_create_agent_conversations.sql).
type PostgresConversationStore struct {
	db *pgxpool.Pool
}

func NewPostgresConversationStore(db *pgxpool.Pool) *PostgresConversationStore {
	return &PostgresConversationStore{db: db}
}

func (s *PostgresConversationStore) AppendTurn(ctx context.Context, cartID, role, content string, intent *Intent) error {
	var toolCalls any
	if intent != nil {
		// Stored as {"intent": {...}} rather than the bare Intent so
		// this column's shape can grow to hold real tool-call records
		// (PLAN-01 §2) alongside an intent snapshot later, without a
		// migration or a breaking change to what's already stored here.
		payload, err := json.Marshal(map[string]Intent{"intent": *intent})
		if err != nil {
			return fmt.Errorf("marshal conversation intent snapshot: %w", err)
		}
		toolCalls = payload
	}

	_, err := s.db.Exec(ctx, `
		INSERT INTO agent_conversations (cart_id, role, content, tool_calls)
		VALUES ($1, $2, $3, $4)
	`, cartID, role, content, toolCalls)

	if err != nil {
		return fmt.Errorf("append conversation turn: %w", err)
	}

	return nil
}

func (s *PostgresConversationStore) History(ctx context.Context, cartID string) ([]ConversationTurn, error) {
	rows, err := s.db.Query(ctx, `
		SELECT role, content, created_at
		FROM agent_conversations
		WHERE cart_id = $1
		ORDER BY created_at ASC, id ASC
	`, cartID)
	if err != nil {
		return nil, fmt.Errorf("query conversation history: %w", err)
	}
	defer rows.Close()

	var turns []ConversationTurn
	for rows.Next() {
		var t ConversationTurn
		if err := rows.Scan(&t.Role, &t.Content, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan conversation turn: %w", err)
		}
		turns = append(turns, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversation history: %w", err)
	}

	return turns, nil
}

func (s *PostgresConversationStore) LastKnownIntent(ctx context.Context, cartID string) (Intent, bool, error) {
	var raw []byte

	err := s.db.QueryRow(ctx, `
		SELECT tool_calls
		FROM agent_conversations
		WHERE cart_id = $1 AND tool_calls IS NOT NULL
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, cartID).Scan(&raw)

	if errors.Is(err, pgx.ErrNoRows) {
		return Intent{}, false, nil
	}
	if err != nil {
		return Intent{}, false, fmt.Errorf("query last known intent: %w", err)
	}

	var payload struct {
		Intent Intent `json:"intent"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Intent{}, false, fmt.Errorf("unmarshal last known intent: %w", err)
	}

	return payload.Intent, true, nil
}
