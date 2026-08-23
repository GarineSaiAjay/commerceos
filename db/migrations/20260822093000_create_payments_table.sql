-- +goose Up

CREATE TABLE payments (
    id TEXT PRIMARY KEY,
    order_id TEXT NOT NULL UNIQUE REFERENCES orders(id),

    provider TEXT NOT NULL,
    provider_order_id TEXT NOT NULL UNIQUE,

    amount BIGINT NOT NULL CHECK (amount > 0),
    currency CHAR(3) NOT NULL,

    status TEXT NOT NULL DEFAULT 'created'
        CHECK (status IN ('created', 'attempted', 'paid', 'failed', 'refunded')),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_payments_order_id
    ON payments(order_id);

CREATE INDEX idx_payments_provider_order_id
    ON payments(provider_order_id);


-- +goose Down

DROP TABLE payments;
