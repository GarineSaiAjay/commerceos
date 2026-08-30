-- +goose Up

-- agent_plans persists the buyer-facing agent's own product-selection
-- reasoning (BuyerAgent.planFromIntent / ToolCallingAgent.Run --
-- "propose" time) as its own independently-retrievable Run
-- (PLAN-01-AGENTIC-CORE.md §4, ROADMAP-PRIORITIZED.md P1 item 16).
--
-- This is deliberately a separate table from agent_actions, not a new
-- column or a join target on it. agent_actions/policy_evaluations/
-- authorizations form the Propose -> Evaluate -> Authorize chain that
-- policy.Service.Propose persists at checkout/payment-authorization
-- time, scoped to a whole cart -- see policy/replay.go's Run struct.
-- The reasoning captured here happens earlier and separately, at
-- "which single product should I recommend" time (add-to-cart, not
-- checkout), for BOTH agent paths (BuyerAgent's single-shot pipeline
-- and ToolCallingAgent's bounded tool-calling loop, item 18). Neither
-- agent path calls policy.Service.Propose itself -- only the buyer's
-- own checkout action does, later, against the whole cart -- so there
-- is no existing action_id to attach this reasoning to, and inventing
-- one would fabricate a link the system doesn't actually have (the
-- same honest gap frontend/app/dashboard/orders/page.tsx already
-- discloses: an order isn't tagged with the run that authorized it).
--
-- Rather than force a fake correlation, an agent_plans row is its own
-- first-class Run, reachable through the exact same GET /runs and
-- GET /runs/{id} endpoints agent_actions-backed runs use today (see
-- policy.PostgresRepository's ListRuns/GetRun, which merge both
-- sources) -- zero new UI surface, exactly as the plan document
-- specifies. Its id is always prefixed "plan_" (vs. agent_actions'
-- "action_" prefix) so GetRun can route a lookup to the right table
-- without an extra query, and its decision is always the sentinel
-- "PLANNED" (see policy.PlanDecisionSentinel) -- deliberately never a
-- real APPROVED/REJECTED/PENDING_HUMAN_APPROVAL policy decision value,
-- since no policy evaluation happens at this stage at all.
CREATE TABLE agent_plans (
    id TEXT PRIMARY KEY,
    action TEXT NOT NULL,
    amount BIGINT NOT NULL,
    currency TEXT NOT NULL,
    merchant TEXT NOT NULL,
    items JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- steps is the reasoning trail itself -- a JSON array of
    -- policy.RunStep (stage/detail/timestamp), the exact same shape
    -- agent_actions-backed runs populate in GetRun. New stage names
    -- introduced alongside this table: intent_extracted, tool_called,
    -- tool_result_summary, alternatives_considered, proposed.
    steps JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Same access pattern as agent_actions: ListRuns orders by recency.
CREATE INDEX idx_agent_plans_created_at ON agent_plans (created_at DESC);

-- +goose Down

DROP TABLE agent_plans;
