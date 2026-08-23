-- +goose Up

ALTER TABLE payments
    ADD COLUMN provider_payment_id TEXT;

CREATE INDEX idx_payments_provider_payment_id
    ON payments(provider_payment_id);


-- +goose Down

DROP INDEX idx_payments_provider_payment_id;

ALTER TABLE payments
    DROP COLUMN provider_payment_id;