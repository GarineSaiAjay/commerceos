package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// VerificationResult reports the health of the audit chain.
type VerificationResult struct {
	Verified    bool
	ChainBroken bool
	RowsChecked int
	BrokenAtID  int64
}

// Verifier walks the hash chain and reports whether it is intact.
type Verifier struct {
	db *pgxpool.Pool
}

func NewVerifier(db *pgxpool.Pool) *Verifier {
	return &Verifier{db: db}
}

// Verify walks every row, recomputing each event_hash from its content
// and prev_hash. Any mismatch means the chain is broken.
func (v *Verifier) Verify(ctx context.Context) (VerificationResult, error) {
	rows, err := v.db.Query(ctx, `
		SELECT id, actor, action, entity_type, entity_id, detail::text,
		       event_hash, prev_hash
		FROM audit_events
		ORDER BY id
	`)
	if err != nil {
		return VerificationResult{}, fmt.Errorf("query audit chain: %w", err)
	}
	defer rows.Close()

	result := VerificationResult{Verified: true}
	var expectedPrev *string
	// chainStarted becomes true on the first row with a non-NULL
	// event_hash. Full-codebase re-audit (P2): the NULL-hash skip below
	// used to apply unconditionally to ANY row, anywhere in the table --
	// but postgres_writer.go's Write always sets a real event_hash
	// before a row is ever visible to a legitimate reader (the INSERT
	// creates the row, then an UPDATE sets event_hash/prev_hash in the
	// same call), so a NULL hash is only ever legitimate for the
	// contiguous prefix of rows written before
	// 20260822131000_add_audit_hash_chain.sql added the columns (they
	// default to NULL with no backfill). A NULL hash anywhere AFTER the
	// chain has started is not a legacy row -- it's either direct DB
	// tampering (delete/replace real rows, then null out their hash to
	// dodge verification) or a crash between the INSERT and the
	// hash-computing UPDATE -- and skipping it silently defeated the
	// entire point of a tamper-evident chain by walking straight past
	// exactly the row an attacker would forge. Now only the true legacy
	// prefix is skipped; anything after is reported as ChainBroken.
	chainStarted := false

	for rows.Next() {
		var (
			id         int64
			actor      string
			action     string
			entityType string
			entityID   string
			detail     []byte
			eventHash  *string
			prevHash   *string
		)

		if err := rows.Scan(
			&id, &actor, &action, &entityType, &entityID, &detail,
			&eventHash, &prevHash,
		); err != nil {
			return VerificationResult{}, fmt.Errorf("scan audit row: %w", err)
		}

		if eventHash == nil {
			if chainStarted {
				// A NULL hash after real hashed rows have already been
				// seen -- not a legacy row, a break in the chain.
				result.Verified = false
				result.ChainBroken = true
				result.BrokenAtID = id
				break
			}
			// Still in the legacy pre-hash-chain prefix -- nothing to
			// verify for these, skip them.
			continue
		}
		chainStarted = true

		// Recompute the expected hash.
		content := string(detail) + "|" + actor + "|" + action + "|" + entityType + "|" + entityID
		if expectedPrev != nil {
			content += "|" + *expectedPrev
		}
		sum := sha256.Sum256([]byte(content))
		expected := hex.EncodeToString(sum[:])

		// Check prev_hash linkage.
		if (prevHash == nil && expectedPrev != nil) ||
			(prevHash != nil && (expectedPrev == nil || *prevHash != *expectedPrev)) {
			result.Verified = false
			result.ChainBroken = true
			result.BrokenAtID = id
			break
		}

		// Check the stored hash matches the recomputed one.
		if *eventHash != expected {
			result.Verified = false
			result.ChainBroken = true
			result.BrokenAtID = id
			break
		}

		result.RowsChecked++
		expectedPrev = eventHash
	}

	if err := rows.Err(); err != nil {
		return VerificationResult{}, fmt.Errorf("iterate audit chain: %w", err)
	}

	return result, nil
}
