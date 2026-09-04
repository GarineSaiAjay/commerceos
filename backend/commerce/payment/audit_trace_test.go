package payment

import (
	"context"
	"testing"

	"github.com/garinesaiajay/commerceos/audit"
	"github.com/garinesaiajay/commerceos/commerce/order"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestAuditTraceComplete proves the Phase 2 checklist item: the
// audit_events table shows a complete, ordered trace for a full
// successful run and a full failed run.
func TestAuditTraceComplete(t *testing.T) {
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

	repo := NewPostgresRepository(pool)

	// Clean up prior test rows.
	_, _ = pool.Exec(ctx, `DELETE FROM webhook_events WHERE event_id LIKE 'audit_test_%'`)
	_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE entity_id LIKE 'pay_audit_test_%'`)
	_, _ = pool.Exec(ctx, `DELETE FROM payments WHERE order_id LIKE 'audit_test_%'`)
	_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id LIKE 'audit_test_%'`)
	_, _ = pool.Exec(ctx, `DELETE FROM carts WHERE id LIKE 'audit_test_%'`)

	// Seed two carts (orders.cart_id has a FK to carts).
	for _, orderID := range []string{"audit_test_success", "audit_test_fail"} {
		_, err = pool.Exec(ctx, `
			INSERT INTO carts (id, merchant_id, currency, subtotal_amount, status, expires_at)
			VALUES ($1, 'merchant_001', 'INR', 24900, 'checked_out', NOW() + INTERVAL '9 minutes')
		`, orderID+"-cart")
		if err != nil {
			t.Fatal(err)
		}
	}

	// Seed two orders in payment_pending state.
	for _, orderID := range []string{"audit_test_success", "audit_test_fail"} {
		_, err = pool.Exec(ctx, `
			INSERT INTO orders (id, merchant_id, cart_id, currency, subtotal, status)
			VALUES ($1, 'merchant_001', $1 || '-cart', 'INR', 24900, 'payment_pending')
		`, orderID)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Seed payments in pending state.
	for _, orderID := range []string{"audit_test_success", "audit_test_fail"} {
		_, err = pool.Exec(ctx, `
			INSERT INTO payments (id, order_id, provider, provider_order_id, amount, currency, status)
			VALUES ($1, $2, 'razorpay', $1 || '-rzp', 24900, 'INR', 'pending')
		`, "pay_"+orderID, orderID)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Build the applier with the real audit writer and order repo.
	orderRepo := order.NewPostgresRepository(pool)
	auditWriter := audit.NewPostgresWriter(pool)
	applier := NewWebhookApplier(repo, orderRepo, auditWriter, nil)

	// Success run: payment.captured.
	err = applier.ApplyCaptured(ctx, RazorpayWebhookPayload{
		Payload: struct {
			Payment struct {
				Entity struct {
					ID        string `json:"id"`
					OrderID   string `json:"order_id"`
					Amount    int64  `json:"amount"`
					Currency  string `json:"currency"`
					ErrorCode string `json:"error_code"`
					ErrorDesc string `json:"error_description"`
				} `json:"entity"`
			} `json:"payment"`
		}{
			Payment: struct {
				Entity struct {
					ID        string `json:"id"`
					OrderID   string `json:"order_id"`
					Amount    int64  `json:"amount"`
					Currency  string `json:"currency"`
					ErrorCode string `json:"error_code"`
					ErrorDesc string `json:"error_description"`
				} `json:"entity"`
			}{
				Entity: struct {
					ID        string `json:"id"`
					OrderID   string `json:"order_id"`
					Amount    int64  `json:"amount"`
					Currency  string `json:"currency"`
					ErrorCode string `json:"error_code"`
					ErrorDesc string `json:"error_description"`
				}{
					ID:       "pay_audit_test_success",
					OrderID:  "pay_audit_test_success-rzp",
					Amount:   24900,
					Currency: "INR",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("apply captured: %v", err)
	}

	// Failure run: payment.failed.
	err = applier.ApplyFailed(ctx, RazorpayWebhookPayload{
		Payload: struct {
			Payment struct {
				Entity struct {
					ID        string `json:"id"`
					OrderID   string `json:"order_id"`
					Amount    int64  `json:"amount"`
					Currency  string `json:"currency"`
					ErrorCode string `json:"error_code"`
					ErrorDesc string `json:"error_description"`
				} `json:"entity"`
			} `json:"payment"`
		}{
			Payment: struct {
				Entity struct {
					ID        string `json:"id"`
					OrderID   string `json:"order_id"`
					Amount    int64  `json:"amount"`
					Currency  string `json:"currency"`
					ErrorCode string `json:"error_code"`
					ErrorDesc string `json:"error_description"`
				} `json:"entity"`
			}{
				Entity: struct {
					ID        string `json:"id"`
					OrderID   string `json:"order_id"`
					Amount    int64  `json:"amount"`
					Currency  string `json:"currency"`
					ErrorCode string `json:"error_code"`
					ErrorDesc string `json:"error_description"`
				}{
					ID:        "pay_audit_test_fail",
					OrderID:   "pay_audit_test_fail-rzp",
					Amount:    24900,
					Currency:  "INR",
					ErrorCode: "BAD_REQUEST_ERROR",
					ErrorDesc: "The payment was declined",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	// Verify the audit trail: both events recorded, in order.
	rows, err := pool.Query(ctx, `
		SELECT action, entity_id
		FROM audit_events
		WHERE entity_id LIKE 'pay_audit_test_%'
		ORDER BY created_at, id
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var actions []string
	var entities []string

	for rows.Next() {
		var action, entity string
		if err := rows.Scan(&action, &entity); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, action)
		entities = append(entities, entity)
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if len(actions) != 2 {
		t.Fatalf("expected 2 audit events, got %d: %v", len(actions), actions)
	}

	if actions[0] != "payment.captured" || actions[1] != "payment.failed" {
		t.Fatalf("expected captured then failed, got %v", actions)
	}

	// Verify the payment states.
	successPay, err := repo.GetByOrderID(ctx, "audit_test_success")
	if err != nil {
		t.Fatal(err)
	}
	if successPay.Status != "captured" {
		t.Fatalf("expected success payment captured, got %s", successPay.Status)
	}

	failPay, err := repo.GetByOrderID(ctx, "audit_test_fail")
	if err != nil {
		t.Fatal(err)
	}
	if failPay.Status != "failed" {
		t.Fatalf("expected fail payment failed, got %s", failPay.Status)
	}

	// Clean up.
	_, _ = pool.Exec(ctx, `DELETE FROM webhook_events WHERE event_id LIKE 'audit_test_%'`)
	_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE entity_id LIKE 'pay_audit_test_%'`)
	_, _ = pool.Exec(ctx, `DELETE FROM payments WHERE order_id LIKE 'audit_test_%'`)
	_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id LIKE 'audit_test_%'`)
	_, _ = pool.Exec(ctx, `DELETE FROM carts WHERE id LIKE 'audit_test_%'`)
}
