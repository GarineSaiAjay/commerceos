-- +goose Up
-- Data-only backfill. CreateProduct (backend/commerce/catalog/
-- repository.go) previously inserted only into products, never
-- product_variants -- but every cart/checkout path (commerce/
-- cart.Service.AddItem, the MCP add_item tool, checkout.tsx's
-- addToCart, which hardcodes `${product_id}-default`) requires a
-- variant_id, not just a product_id. A product created through
-- POST /products (including frontend/app/dashboard/catalog/page.tsx,
-- item 14) before this fix has zero variants, so "Add to cart" fails
-- for it while working for every seeded product (which gets its
-- "-default" variant by hand in db/seeds/001_catalog.sql).
--
-- CreateProduct itself is fixed in the same commit to provision this
-- variant transactionally going forward; this migration backfills any
-- product that already slipped through before that fix, mirroring the
-- exact same "<product_id>-default" convention.
INSERT INTO product_variants (id, product_id, sku, price_amount, availability, attributes)
SELECT
    p.id || '-default',
    p.id,
    UPPER(p.id),
    p.price_amount,
    p.availability,
    '{}'::jsonb
FROM products p
WHERE NOT EXISTS (
    SELECT 1 FROM product_variants pv WHERE pv.product_id = p.id
)
ON CONFLICT (id) DO NOTHING;

-- +goose Down
-- Deliberately a no-op: reversing this would require knowing which
-- rows this migration created vs. which already existed, which isn't
-- recoverable from schema alone once applied.
