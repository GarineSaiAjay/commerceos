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
ALTER TABLE reviews ADD CONSTRAINT reviews_order_product_unique UNIQUE (order_id, product_id);

-- +goose Down
ALTER TABLE reviews DROP CONSTRAINT reviews_order_product_unique;
