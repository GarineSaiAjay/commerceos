-- +goose Up

-- Item 25 (ROADMAP-PRIORITIZED.md P2, PLAN-05-SELLER-DASHBOARD.md §4):
-- the policy engine's deterministic knobs (amount ceiling, budget
-- tolerance, allowed currencies/merchants) were a Go-code literal
-- (policy.DefaultConfig(), backend/policy/model.go) with zero
-- persistence and zero runtime mutability -- editing them meant
-- editing source and redeploying. This table makes that the real
-- source of truth, backing a genuine /dashboard/settings control
-- rather than a form that writes nowhere. Still validated
-- deterministically server-side by policy.Engine exactly as before --
-- this is a window into existing config, not a new authority.
--
-- allowed_products is deliberately NOT a column here: it's a
-- test/campaign-only fallback list in production today, superseded by
-- a live catalog check (policy.Engine.WithProductExistsFunc, wired in
-- backend/cmd/server/main.go) -- persisting it here would add a
-- control that doesn't actually change what's enforced. See
-- backend/policy/engine.go's UpdateConfig doc comment.
CREATE TABLE policy_settings (
    merchant_id TEXT PRIMARY KEY REFERENCES merchants(id),
    ceiling BIGINT NOT NULL,
    budget_tolerance DOUBLE PRECISION NOT NULL,
    allowed_currencies JSONB NOT NULL,
    allowed_merchants JSONB NOT NULL,
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down

DROP TABLE policy_settings;
