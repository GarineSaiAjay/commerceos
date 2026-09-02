-- +goose Up

-- Pending (or accepted) invitations for a second, third, ... operator on
-- a merchant's account -- item 40 (PLAN-05-SELLER-DASHBOARD.md §7,
-- PLAN-06's phasing table: "Multi-operator invite flow, medium-high,
-- security-sensitive"). Until this migration, exactly one hardcoded
-- operator existed per merchant (db/seeds/002_operator.sql); this table
-- lets an existing operator (backend/auth/invite.go's InviteOperator,
-- gated behind RequireOperator like everything else operator-only) mint
-- an invite for a new teammate's email, and lets that teammate turn it
-- into their own operator account + session (AcceptInvite, unauthenticated
-- by necessity -- they don't have an account yet).
--
-- Only the SHA-256 hash of the invite token is stored, mirroring
-- operator_sessions.token_hash: the raw token exists only in the
-- InviteOperator API response, handed back to the inviting operator to
-- share out-of-band. This project has no outbound email delivery, so
-- unlike a production invite flow this does not send anything itself --
-- see backend/auth/invite.go's package-level doc comment.
CREATE TABLE operator_invites (
    id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(id),
    email TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    invited_by TEXT NOT NULL REFERENCES operators(id),
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Every listing (ListInvites, for the dashboard's "pending invites"
-- view) is scoped to one merchant.
CREATE INDEX idx_operator_invites_merchant_id ON operator_invites(merchant_id);

-- +goose Down

DROP TABLE operator_invites;
