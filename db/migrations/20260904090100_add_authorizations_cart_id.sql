-- +goose Up

-- Security fix (P0, full-codebase re-audit 2026-09-04): CreatePaymentOrder
-- previously verified only that an authorization_id was ACTIVE and
-- unexpired, never that it was actually issued for the order being paid.
-- Since policy.Service.Propose lets a caller self-report amount/currency/
-- merchant/items with no server-side link to a real cart total, an agent
-- could win a cheap, auto-approved (Level 1) authorization for a trivial
-- amount and then spend it against an arbitrarily expensive order via
-- execute_authorized_checkout -- the mandate ceiling and budget-tolerance
-- checks that ran against the cheap proposal never applied to the real
-- charge. See backend/commerce/payment/service.go's CreatePaymentOrder
-- and backend/policy/service.go's Propose/Approve for the fix: every
-- Authorization is now bound to the cart it was proposed for (mirroring
-- ProposedAction.CartID, already sent by every real checkout flow -- see
-- frontend/app/checkout/usePaymentFlow.ts), and CreatePaymentOrder
-- rejects any authorization whose cart_id doesn't match the order's own
-- cart_id, in addition to the pre-existing currency/merchant checks this
-- migration doesn't touch.
--
-- Nullable, no backfill: every existing authorizations row is already
-- unusable regardless of this change -- authorizations expire 10 minutes
-- after issuance (policy.Service.Propose/Approve), so nothing in this
-- table can still be ACTIVE by the time this migration runs.
ALTER TABLE authorizations ADD COLUMN cart_id TEXT;

-- +goose Down

ALTER TABLE authorizations DROP COLUMN cart_id;
