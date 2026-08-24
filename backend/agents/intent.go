package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Intent is the strict, validated output schema of intent extraction.
type Intent struct {
	Budget    int64  `json:"budget"`
	Category  string `json:"category"`
	Priority  string `json:"priority"`
	Recipient string `json:"recipient"`
	Clarify   string `json:"clarify,omitempty"`
}

// ErrAmbiguousIntent is returned when the prompt is too vague to act on.
var ErrAmbiguousIntent = errors.New("ambiguous intent: clarification required")

// ValidateIntent strictly validates the schema. Malformed output is
// rejected before anything else touches it.
func ValidateIntent(i Intent) error {
	if i.Clarify != "" {
		return nil // clarification request is a valid safe no-op
	}

	if i.Budget <= 0 {
		return fmt.Errorf("invalid intent: budget must be positive")
	}
	if i.Category == "" {
		return fmt.Errorf("invalid intent: category required")
	}
	return nil
}

// IntentExtractor turns a natural-language prompt into a validated
// Intent. Implementations must return ErrAmbiguousIntent for vague
// prompts rather than guessing.
type IntentExtractor interface {
	Extract(ctx context.Context, prompt string) (Intent, error)
}

// rawIntent is the wire shape before validation.
type rawIntent struct {
	Budget    *int64  `json:"budget"`
	Category  *string `json:"category"`
	Priority  *string `json:"priority"`
	Recipient *string `json:"recipient"`
	Clarify   *string `json:"clarify,omitempty"`
}

// ParseIntentJSON validates the JSON shape and field types strictly.
func ParseIntentJSON(data []byte) (Intent, error) {
	var raw rawIntent

	if err := json.Unmarshal(data, &raw); err != nil {
		return Intent{}, fmt.Errorf("malformed intent JSON: %w", err)
	}

	if raw.Clarify != nil {
		return Intent{Clarify: *raw.Clarify}, nil
	}

	if raw.Budget == nil || raw.Category == nil {
		return Intent{}, ErrAmbiguousIntent
	}

	intent := Intent{
		Budget:    *raw.Budget,
		Category:  *raw.Category,
		Priority:  deref(raw.Priority),
		Recipient: deref(raw.Recipient),
	}

	if err := ValidateIntent(intent); err != nil {
		return Intent{}, err
	}

	return intent, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
