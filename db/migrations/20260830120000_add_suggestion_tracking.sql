-- +goose Up

-- suggestion_impressions logs every time a cross-sell suggestion was
-- actually shown to a buyer (backend/growth/suggest.go's shared
-- evaluate() helper), across all three /growth/suggest* surfaces
-- (cart, product-detail, post-checkout -- item 19). PLAN-03-PROACTIVE-
-- GROWTH-AGENT.md §6's frequency cap ("at most 2 shown per cart per 10
-- minutes") queries this table's recent rows per cart_id, and §8's
-- impression/acceptance tracking aggregates it for the merchant
-- dashboard. A dedicated append-only log, not a shown_count column on
-- recommendations, because the frequency cap needs a real windowed
-- count -- a single running counter on the upserted recommendations
-- row (id = "rec_<cart_id>_<product_id>", one row per cart+product
-- pair) can only ever answer "how many times, all-time", never "how
-- many times in the last 10 minutes."
CREATE TABLE suggestion_impressions (
    id BIGSERIAL PRIMARY KEY,
    cart_id TEXT NOT NULL,
    product_id TEXT NOT NULL,
    shown_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexed on (cart_id, shown_at) -- exactly the shape of the frequency
-- cap's own query (COUNT(*) WHERE cart_id = $1 AND shown_at >= $2).
CREATE INDEX idx_suggestion_impressions_cart_shown
    ON suggestion_impressions (cart_id, shown_at);

-- accepted (PLAN-03 §8) is set when a buyer actually adds the
-- suggested product to their cart (POST /growth/suggest/accept),
-- distinguishing "shown and ignored" from "shown and acted on" --
-- recommendations.decision only ever records the growth AGENT's own
-- decision (RECOMMEND/REJECT), never the buyer's response to it. Not
-- touched by PostgresStore.Save's ON CONFLICT DO UPDATE (that clause
-- only ever sets expected_value/decision/reason), so a re-evaluation
-- of an already-accepted cart+product pair can never silently flip
-- this back to false.
ALTER TABLE recommendations ADD COLUMN accepted BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down

ALTER TABLE recommendations DROP COLUMN accepted;
DROP TABLE suggestion_impressions;
