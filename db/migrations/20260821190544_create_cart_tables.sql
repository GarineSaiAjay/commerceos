-- +goose Up

CREATE TABLE carts (
    id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(id),
    currency TEXT NOT NULL,
    subtotal_amount BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'active',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE cart_items (
    id BIGSERIAL PRIMARY KEY,
    cart_id TEXT NOT NULL REFERENCES carts(id) ON DELETE CASCADE,
    product_id TEXT NOT NULL REFERENCES products(id),
    variant_id TEXT NOT NULL REFERENCES product_variants(id),
    title TEXT NOT NULL,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    unit_price_amount BIGINT NOT NULL,
    total_amount BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (cart_id, variant_id)
);

CREATE INDEX idx_carts_expires_at
    ON carts(expires_at);

CREATE INDEX idx_cart_items_cart_id
    ON cart_items(cart_id);


-- +goose Down

DROP TABLE cart_items;
DROP TABLE carts;
