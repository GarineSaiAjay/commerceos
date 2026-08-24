-- +goose Up

-- Phase 2: expand the payment status enum to match the state machine:
-- CREATED → PENDING → AUTHORIZED → CAPTURED → COMPLETED
-- PENDING → FAILED

-- Drop the old constraint first so existing rows can be remapped.
ALTER TABLE payments
    DROP CONSTRAINT payments_status_check;

-- Phase 1 rows: 'paid' maps to 'captured', 'attempted' maps to
-- 'pending'. 'created' and 'failed' are unchanged.
UPDATE payments
SET status = 'captured'
WHERE status = 'paid';

UPDATE payments
SET status = 'pending'
WHERE status = 'attempted';

ALTER TABLE payments
    ADD CONSTRAINT payments_status_check
    CHECK (status IN (
        'created',
        'pending',
        'authorized',
        'captured',
        'completed',
        'failed',
        'refunded'
    ));


-- +goose Down

ALTER TABLE payments
    DROP CONSTRAINT payments_status_check;

ALTER TABLE payments
    ADD CONSTRAINT payments_status_check
    CHECK (status IN ('created', 'attempted', 'paid', 'failed', 'refunded'));