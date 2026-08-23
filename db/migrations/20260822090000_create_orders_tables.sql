-- +goose Up

CREATE TABLE orders (
    id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(id),

    cart_id TEXT NOT NULL UNIQUE REFERENCES carts(id),

    currency CHAR(3) NOT NULL,
    subtotal BIGINT NOT NULL CHECK (subtotal >= 0),

    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'paid', 'cancelled')),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE order_items (
    id BIGSERIAL PRIMARY KEY,

    order_id TEXT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,

    product_id TEXT NOT NULL,
    variant_id TEXT NOT NULL,

    title TEXT NOT NULL,

    quantity INTEGER NOT NULL CHECK (quantity > 0),
    unit_price BIGINT NOT NULL CHECK (unit_price >= 0),
    total BIGINT NOT NULL CHECK (total >= 0)
);

CREATE INDEX idx_orders_merchant_id
    ON orders(merchant_id);

CREATE INDEX idx_order_items_order_id
    ON order_items(order_id);


-- +goose Down

DROP TABLE order_items;
DROP TABLE orders;
