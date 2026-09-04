-- +goose Up

-- recommendations.cart_id was a free-form string with no referential
-- integrity, so the AI-revenue-attribution join in
-- analytics.Service.Compute (backend/analytics/service.go) could only be
-- documented as "best-effort". Make it a real FK, matching the existing
-- pattern on mandates.cart_id (see 20260822130000_create_policy_tables.sql).
--
-- Same class of bug as 20260904060537_add_reviews_order_product_unique.sql
-- (full-codebase re-audit, P1): adding a constraint directly against
-- pre-existing violating rows fails outright (Postgres: "insert or
-- update on table "recommendations" violates foreign key constraint
-- ... SQLSTATE 23503"), on any environment where the recommendations
-- table already has orphaned rows -- a cart_id with no matching row in
-- `carts` -- when this migration runs, whether that's this dev
-- database, a fresh CI run, or anyone else's clone. cart_id is NOT
-- NULL (20260822140000_create_recommendations_table.sql), so unlike
-- reviews.order_id there is no nullable case to carve out: every
-- orphaned row is unconditionally a violation. De-duplicate/clean
-- first, same as the reviews fix, instead of leaving this as a runbook
-- step for whoever hits the failure.
DELETE FROM recommendations r
WHERE NOT EXISTS (
    SELECT 1 FROM carts c WHERE c.id = r.cart_id
);

ALTER TABLE recommendations
    ADD CONSTRAINT fk_recommendations_cart_id
    FOREIGN KEY (cart_id) REFERENCES carts(id);

-- +goose Down
-- Reverses the constraint. Deliberately does not attempt to restore the
-- orphaned rows the Up migration deleted -- same as
-- 20260904060537_add_reviews_order_product_unique.sql's own Down, there
-- is no way to recover which rows those were once they're gone.
ALTER TABLE recommendations
    DROP CONSTRAINT fk_recommendations_cart_id;
