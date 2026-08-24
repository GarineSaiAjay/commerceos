-- +goose Up

-- Durable human-approval requests for Level 2/3 proposals. Level 1 is
-- auto-approved inline; Level 2/3 produce a PENDING request here, and a
-- one-time authorization is only issued on approval.
CREATE TABLE approval_requests (
    id TEXT PRIMARY KEY,
    mandate_id TEXT NOT NULL REFERENCES mandates(id),
    action TEXT NOT NULL,
    amount BIGINT NOT NULL,
    currency CHAR(3) NOT NULL,
    merchant TEXT NOT NULL,
    items JSONB NOT NULL DEFAULT '[]'::jsonb,
    cart_id TEXT,
    policy_version TEXT NOT NULL,
    risk_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    level INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING'
        CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED', 'EXPIRED', 'REVOKED')),
    authorization_id TEXT,
    reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_approval_requests_status ON approval_requests(status);
CREATE INDEX idx_approval_requests_cart ON approval_requests(cart_id);

-- +goose Down

DROP TABLE approval_requests;