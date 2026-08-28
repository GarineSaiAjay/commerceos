-- +goose Up

-- recommendations.cart_id was a free-form string with no referential
-- integrity, so the AI-revenue-attribution join in
-- analytics.Service.Compute (backend/analytics/service.go) could only be
-- documented as "best-effort". Make it a real FK, matching the existing
-- pattern on mandates.cart_id (see 20260822130000_create_policy_tables.sql).
--
-- NOTE: if this fails to apply, some existing rows in `recommendations`
-- reference a cart_id that no longer has a matching row in `carts` --
-- find them with:
--   SELECT r.id, r.cart_id FROM recommendations r
--   LEFT JOIN carts c ON c.id = r.cart_id WHERE c.id IS NULL;
-- and either delete those rows or backfill the missing carts before
-- re-running this migration.
ALTER TABLE recommendations
    ADD CONSTRAINT fk_recommendations_cart_id
    FOREIGN KEY (cart_id) REFERENCES carts(id);

-- +goose Down

ALTER TABLE recommendations
    DROP CONSTRAINT fk_recommendations_cart_id;
