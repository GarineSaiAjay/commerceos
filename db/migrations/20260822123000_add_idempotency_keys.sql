-- +goose Up

-- Idempotency key for money-moving commands. When the same command
-- arrives twice with the same key, the existing result is returned and
-- no second Razorpay order is created.
ALTER TABLE payments
    ADD COLUMN idempotency_key TEXT;

CREATE UNIQUE INDEX idx_payments_idempotency_key
    ON payments(idempotency_key)
    WHERE idempotency_key IS NOT NULL;

ALTER TABLE payment_attempts
    ADD COLUMN idempotency_key TEXT;

CREATE UNIQUE INDEX idx_payment_attempts_idempotency_key
    ON payment_attempts(idempotency_key)
    WHERE idempotency_key IS NOT NULL;


-- +goose Down

DROP INDEX idx_payments_idempotency_key;
ALTER TABLE payments DROP COLUMN idempotency_key;

DROP INDEX idx_payment_attempts_idempotency_key;
ALTER TABLE payment_attempts DROP COLUMN idempotency_key;