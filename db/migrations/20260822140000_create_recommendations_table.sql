-- +goose Up

CREATE TABLE recommendations (
    id TEXT PRIMARY KEY,
    cart_id TEXT NOT NULL,
    product_id TEXT NOT NULL,
    price BIGINT NOT NULL,
    purchase_probability DOUBLE PRECISION NOT NULL,
    incremental_margin BIGINT NOT NULL,
    confidence DOUBLE PRECISION NOT NULL,
    risk_cost BIGINT NOT NULL DEFAULT 0,
    expected_value DOUBLE PRECISION NOT NULL,
    decision TEXT NOT NULL,
    policy_version TEXT NOT NULL,
    reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_recommendations_cart
    ON recommendations(cart_id, created_at);

-- +goose Down

DROP TABLE recommendations;