-- +goose Up

-- A fresh audit against PLAN-02-CATALOG-AND-COMMERCE.md found no
-- constraint stopping a retried or duplicated POST /orders/{id}/review
-- call from inserting unlimited review rows for the same order+
-- product -- each one counted again by catalog.PostgresRepository.
-- GetProduct's AVG(rating)/COUNT(*) aggregate, which then directly
-- feeds growth.suggest.go's EV scoring (PLAN-02 §2's own stated
-- design). One real buyer purchase should be able to leave at most one
-- review per product.
--
-- order_id is nullable (the small operator-seeded starter set in
-- db/seeds/003_reviews.sql has no backing order, by design -- see
-- 20260830100000_create_reviews.sql's own comment) and Postgres never
-- treats two NULLs as equal in a unique index, so this constraint
-- leaves that seeded set completely unaffected: it only ever rejects a
-- second REAL (order_id IS NOT NULL) review for the same order+
-- product pair.
--
-- A real dev/demo database can already have exactly the duplicates
-- this migration exists to prevent going forward (this is not
-- hypothetical -- it happened running this migration against the
-- project's own dev database). Adding the constraint directly against
-- pre-existing violating rows fails outright (Postgres: "could not
-- create unique index ... SQLSTATE 23505"), so de-duplicate first:
-- for every (order_id, product_id) pair with more than one REAL
-- review, keep only the earliest (lowest id -- the first one actually
-- submitted) and drop the rest. Scoped to order_id IS NOT NULL so the
-- nullable-order_id seeded set is never touched by this cleanup
-- either, matching the constraint's own scope below.
DELETE FROM reviews r
USING reviews r2
WHERE r.order_id IS NOT NULL
  AND r.order_id = r2.order_id
  AND r.product_id = r2.product_id
  AND r.id > r2.id;

ALTER TABLE reviews ADD CONSTRAINT reviews_order_product_unique UNIQUE (order_id, product_id);

-- +goose Down
-- Reverses the constraint. Deliberately does not attempt to restore
-- the duplicate rows the Up migration deleted -- same as
-- 20260830110000_backfill_missing_default_variants.sql's own Down,
-- there is no way to recover which rows those were once they're gone.
ALTER TABLE reviews DROP CONSTRAINT reviews_order_product_unique;
