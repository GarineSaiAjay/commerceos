-- +goose Up

CREATE TABLE merchants (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE products (
    id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(id),

    title TEXT NOT NULL,

    price_amount BIGINT NOT NULL CHECK (price_amount >= 0),
    price_currency CHAR(3) NOT NULL,

    availability INTEGER NOT NULL CHECK (availability >= 0),

    features JSONB NOT NULL DEFAULT '[]'::jsonb,
    compatibility JSONB NOT NULL DEFAULT '[]'::jsonb,
    use_cases JSONB NOT NULL DEFAULT '[]'::jsonb,

    return_policy JSONB NOT NULL,
    shipping JSONB NOT NULL,

    attributes JSONB NOT NULL DEFAULT '{}'::jsonb,
    purchase_constraints JSONB NOT NULL DEFAULT '{}'::jsonb,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE product_variants (
    id TEXT PRIMARY KEY,
    product_id TEXT NOT NULL REFERENCES products(id) ON DELETE CASCADE,

    sku TEXT NOT NULL UNIQUE,

    price_amount BIGINT,
    availability INTEGER NOT NULL DEFAULT 0 CHECK (availability >= 0),

    attributes JSONB NOT NULL DEFAULT '{}'::jsonb,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_products_merchant_id
    ON products(merchant_id);

CREATE INDEX idx_product_variants_product_id
    ON product_variants(product_id);


-- +goose Down

DROP TABLE product_variants;
DROP TABLE products;
DROP TABLE merchants;
