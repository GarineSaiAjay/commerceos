-- +goose Up

-- Merchant-side promotional discount campaigns, proposed by the
-- CampaignAgent from observed REJECT demand in `recommendations` (real
-- data, not synthetic), gated by CampaignPolicyEngine, and approved by
-- an operator before going ACTIVE -- mirrors the mandate/authorization/
-- approval_requests lifecycle in 20260822130000_create_policy_tables.sql
-- and 20260824170000_create_approval_requests.sql.
CREATE TABLE campaigns (
    id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(id),
    product_id TEXT NOT NULL REFERENCES products(id),

    discount_percent INTEGER NOT NULL CHECK (discount_percent > 0 AND discount_percent <= 100),
    budget_cap BIGINT NOT NULL CHECK (budget_cap > 0),
    spent BIGINT NOT NULL DEFAULT 0 CHECK (spent >= 0),

    duration_days INTEGER NOT NULL CHECK (duration_days > 0),
    starts_at TIMESTAMPTZ,
    ends_at TIMESTAMPTZ,

    status TEXT NOT NULL DEFAULT 'PROPOSED'
        CHECK (status IN ('PROPOSED', 'APPROVED', 'REJECTED', 'ACTIVE', 'COMPLETED', 'EXPIRED')),

    policy_version TEXT NOT NULL,
    rejected_demand_count INTEGER NOT NULL DEFAULT 0,
    reasoning TEXT,

    approved_by TEXT,
    rejected_reason TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- spent can never exceed budget_cap -- belt-and-suspenders alongside
    -- the atomic UPDATE ... WHERE spent + $n <= budget_cap guard used in
    -- order/postgres_repository.go (the DB constraint catches any future
    -- code path that forgets the guard; the guard is still what avoids
    -- the read-then-write race in the hot path).
    CONSTRAINT campaigns_spent_within_cap CHECK (spent <= budget_cap)
);

CREATE INDEX idx_campaigns_merchant_status ON campaigns(merchant_id, status);
CREATE INDEX idx_campaigns_product_status ON campaigns(product_id, status)
    WHERE status = 'ACTIVE';

-- Every discount actually applied at checkout -- the redemption ledger.
-- One row per order the discount touched, so "how much of this
-- campaign's spend came from which orders" is answerable without
-- re-deriving it from orders.discount_amount.
CREATE TABLE campaign_redemptions (
    id BIGSERIAL PRIMARY KEY,
    campaign_id TEXT NOT NULL REFERENCES campaigns(id),
    order_id TEXT NOT NULL REFERENCES orders(id),
    discount_amount BIGINT NOT NULL CHECK (discount_amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (order_id)
);

CREATE INDEX idx_campaign_redemptions_campaign_id ON campaign_redemptions(campaign_id);

-- +goose Down

DROP TABLE campaign_redemptions;
DROP TABLE campaigns;
