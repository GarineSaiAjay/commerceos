-- +goose Up

CREATE TABLE mandates (
    id TEXT PRIMARY KEY,
    buyer TEXT NOT NULL,
    merchant TEXT NOT NULL,
    allowed_categories JSONB NOT NULL DEFAULT '[]'::jsonb,
    maximum_amount BIGINT NOT NULL,
    currency CHAR(3) NOT NULL,
    requires_confirmation_above BIGINT NOT NULL DEFAULT 0,
    allowed_payment_methods JSONB NOT NULL DEFAULT '[]'::jsonb,
    expires_at TIMESTAMPTZ NOT NULL,
    purpose TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'REVOKED', 'EXPIRED')),
    cart_id TEXT REFERENCES carts(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE authorizations (
    id TEXT PRIMARY KEY,
    mandate_id TEXT NOT NULL REFERENCES mandates(id),
    action TEXT NOT NULL,
    amount BIGINT NOT NULL,
    currency CHAR(3) NOT NULL,
    merchant TEXT NOT NULL,
    items JSONB NOT NULL DEFAULT '[]'::jsonb,
    policy_version TEXT NOT NULL,
    decision TEXT NOT NULL,
    risk_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    level INTEGER NOT NULL DEFAULT 1,
    expires_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'USED', 'EXPIRED', 'REVOKED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE agent_actions (
    id TEXT PRIMARY KEY,
    action TEXT NOT NULL,
    amount BIGINT NOT NULL,
    currency CHAR(3) NOT NULL,
    merchant TEXT NOT NULL,
    items JSONB NOT NULL DEFAULT '[]'::jsonb,
    proposal JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE agent_decisions (
    id TEXT PRIMARY KEY,
    action_id TEXT NOT NULL REFERENCES agent_actions(id),
    decision TEXT NOT NULL,
    reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE policy_evaluations (
    id TEXT PRIMARY KEY,
    action_id TEXT NOT NULL REFERENCES agent_actions(id),
    policy_version TEXT NOT NULL,
    decision TEXT NOT NULL,
    reason TEXT,
    authorization_id TEXT,
    risk_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    level INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE risk_assessments (
    id TEXT PRIMARY KEY,
    action_id TEXT NOT NULL REFERENCES agent_actions(id),
    risk_score DOUBLE PRECISION NOT NULL,
    factors JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down

DROP TABLE risk_assessments;
DROP TABLE policy_evaluations;
DROP TABLE agent_decisions;
DROP TABLE agent_actions;
DROP TABLE authorizations;
DROP TABLE mandates;