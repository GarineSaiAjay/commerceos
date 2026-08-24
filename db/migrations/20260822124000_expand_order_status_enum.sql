-- +goose Up

-- Phase 2: expand the order status enum to match the state machine:
-- DRAFT → AUTHORIZED → PAYMENT_PENDING → PAID → FULFILLMENT_PENDING → COMPLETED
-- PAYMENT_PENDING → FAILED

-- Drop the old constraint first so existing rows can be remapped.
ALTER TABLE orders
    DROP CONSTRAINT orders_status_check;

-- Existing Phase 1 rows use 'pending'; map them to 'payment_pending'.
UPDATE orders
SET status = 'payment_pending'
WHERE status = 'pending';

ALTER TABLE orders
    ADD CONSTRAINT orders_status_check
    CHECK (status IN (
        'draft',
        'authorized',
        'payment_pending',
        'paid',
        'fulfillment_pending',
        'completed',
        'failed',
        'cancelled'
    ));


-- +goose Down

ALTER TABLE orders
    DROP CONSTRAINT orders_status_check;

ALTER TABLE orders
    ADD CONSTRAINT orders_status_check
    CHECK (status IN ('pending', 'paid', 'cancelled'));