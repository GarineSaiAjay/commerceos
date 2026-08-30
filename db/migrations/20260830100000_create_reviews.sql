-- +goose Up
-- reviews backs PLAN-02-CATALOG-AND-COMMERCE.md §2's real (not
-- synthetic) review/rating system: a real first-party data source that
-- grows the longer a demo/judging session runs, unlike a static seeded
-- set. order_id presence *is* verified_purchase -- deliberately no
-- separate boolean column to keep in sync; verified_purchase is always
-- computed at read time as `order_id IS NOT NULL`. order_id is
-- nullable so a small operator-seeded starter set (db/seeds) can avoid
-- an empty-state catalog on first run, clearly distinguishable from a
-- real buyer review by having no backing order.
CREATE TABLE reviews (
    id BIGSERIAL PRIMARY KEY,
    product_id TEXT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    order_id TEXT REFERENCES orders(id) ON DELETE SET NULL,
    buyer_reference TEXT NOT NULL DEFAULT 'Verified Buyer',
    rating SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- catalog.Repository.GetProduct joins this per product on every read
-- (average_rating/review_count, PLAN-02 §2) -- indexed the same way
-- every other per-parent lookup table in this schema is
-- (idx_order_items_order_id, idx_agent_conversations_cart_id_created_at).
CREATE INDEX idx_reviews_product_id ON reviews (product_id);
CREATE INDEX idx_reviews_order_id ON reviews (order_id);

-- +goose Down
DROP TABLE reviews;
