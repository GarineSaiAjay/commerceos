-- +goose Up

-- Order (backend/commerce/order/model.go) has only Subtotal today --
-- add the discount fields the CheckoutCart campaign-discount hook needs
-- to persist. discount_amount defaults to 0 so every existing order and
-- every checkout that doesn't match an active campaign is unaffected;
-- campaign_id is nullable for the same reason.
ALTER TABLE orders
    ADD COLUMN discount_amount BIGINT NOT NULL DEFAULT 0 CHECK (discount_amount >= 0),
    ADD COLUMN campaign_id TEXT REFERENCES campaigns(id);

-- +goose Down

ALTER TABLE orders
    DROP COLUMN discount_amount,
    DROP COLUMN campaign_id;
