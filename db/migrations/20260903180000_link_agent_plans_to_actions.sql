-- +goose Up

-- Connects the two previously-independent Run trails a fresh audit
-- against PLAN-01-AGENTIC-CORE.md found: "the agent's reasoning trail
-- (plan_... records) and the policy/payment authorization trail
-- (action_... records) are two disconnected Run objects. A judge
-- clicking into Runs sees either the reasoning or the financial
-- outcome, never one continuous arc from intent -> risk assessment ->
-- authorization."
--
-- 20260830190015_create_agent_plans_table.sql's own comment explained
-- why agent_plans has no action_id column: "there is no existing
-- action_id to attach this reasoning to, and inventing one would
-- fabricate a link the system doesn't actually have" -- true at the
-- moment a plan is saved (add-to-cart/recommendation time), since the
-- buyer hasn't checked out yet. That's still true then. But it stops
-- being true the moment the buyer DOES check out: policy.Service.
-- Propose already knows the exact cart_id it's proposing payment for
-- (ProposedAction.CartID, sent by the frontend on every
-- /policy/propose call), and agent_plans rows already carry the same
-- cart_id (see cart_id below) from whichever /agent/checkout or
-- /agent/loop call produced them. At Propose time this is a REAL,
-- temporally-grounded correlation to discover, not a fabricated one.
--
-- cart_id lets Service.Propose find "the most recent agent_plans row
-- for this cart, created before this Propose call" (there is no
-- existing FK to hang this off of at plan-save time, since a plan
-- exists before its cart necessarily even has a checkout in progress).
ALTER TABLE agent_plans ADD COLUMN cart_id TEXT;
CREATE INDEX idx_agent_plans_cart_id ON agent_plans (cart_id, created_at DESC);

-- plan_id is set (best-effort, after the fact) once Service.Propose
-- has found a matching agent_plans row for the cart being checked
-- out. Nullable: most agent_actions rows will never have one, either
-- because the buyer never used the agent at all for this cart (added
-- straight from the catalog) or because no matching plan was found.
ALTER TABLE agent_actions ADD COLUMN plan_id TEXT REFERENCES agent_plans(id);
CREATE INDEX idx_agent_actions_plan_id ON agent_actions (plan_id);

-- +goose Down

DROP INDEX idx_agent_actions_plan_id;
ALTER TABLE agent_actions DROP COLUMN plan_id;
DROP INDEX idx_agent_plans_cart_id;
ALTER TABLE agent_plans DROP COLUMN cart_id;
