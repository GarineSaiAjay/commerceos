-- +goose Up

-- Persists a buyer's "No thanks" on a cross-sell suggestion so
-- SuggestHandler.Suggest (backend/growth/suggest.go) can exclude that
-- product for the rest of the cart's life, instead of the dismissal
-- living only in frontend React state (dismissedProductId in
-- checkout.tsx), which was lost on every page reload -- a buyer who
-- said no once could be shown the exact same suggestion again a minute
-- later. See files/PLAN-03-PROACTIVE-GROWTH-AGENT.md §5.
--
-- Deliberately its own small table rather than a new `decision` value
-- on `recommendations`: a dismissal is a buyer action on an
-- already-RECOMMEND'd suggestion, not a new EV-scored decision, and
-- keeping it separate means this migration touches nothing about how
-- growth.GrowthAgent.EvaluateCandidate or PostgresStore.Save already
-- work -- purely additive.
CREATE TABLE suggestion_dismissals (
    cart_id TEXT NOT NULL,
    product_id TEXT NOT NULL,
    dismissed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (cart_id, product_id)
);

-- +goose Down

DROP TABLE suggestion_dismissals;
