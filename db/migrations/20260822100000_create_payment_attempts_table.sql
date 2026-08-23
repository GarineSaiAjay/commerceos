-- +goose Up

CREATE TABLE payment_attempts (
    id TEXT PRIMARY KEY,
    payment_id TEXT NOT NULL REFERENCES payments(id),
    order_id TEXT NOT NULL REFERENCES orders(id),

    provider_order_id TEXT,
    razorpay_payment_id TEXT,

    amount BIGINT NOT NULL CHECK (amount > 0),
    currency CHAR(3) NOT NULL,

    status TEXT NOT NULL DEFAULT 'created'
        CHECK (status IN ('created', 'attempted', 'paid', 'failed')),

    error_code TEXT,
    error_description TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_payment_attempts_payment_id
    ON payment_attempts(payment_id);

CREATE INDEX idx_payment_attempts_order_id
    ON payment_attempts(order_id);


-- +goose Down

DROP TABLE payment_attempts;