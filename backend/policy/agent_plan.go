package policy

import "time"

// PlanDecisionSentinel is the Run.Decision value an AgentPlan-backed
// run always carries. It is deliberately never a real policy decision
// (APPROVED, REJECTED, PENDING_HUMAN_APPROVAL) -- no policy evaluation
// happens at agent-reasoning time, only later, when the buyer's whole
// cart reaches checkout (policy.Service.Propose). A dashboard or
// checkout.tsx that branches on Run.Decision must treat this sentinel
// as its own case, not fold it into an approved/rejected reading.
const PlanDecisionSentinel = "PLANNED"

// AgentPlan is a persisted record of the buyer-facing agent's own
// product-selection reasoning -- BuyerAgent.planFromIntent's single-shot
// pipeline or ToolCallingAgent.Run's bounded tool-calling loop (item 18),
// both of which produce a policy.ProposedAction plus a step-by-step
// trace but, until now, never persisted either one. See
// db/migrations/*_create_agent_plans_table.sql for why this is a
// separate table from agent_actions rather than a column or join
// target on it.
type AgentPlan struct {
	// ID is always prefixed "plan_" (the caller mints it, the same way
	// Service.Propose mints "action_<unixnano>" for agent_actions) --
	// this is what lets PostgresRepository.GetRun route a lookup to
	// agent_plans instead of agent_actions without an extra query.
	ID string

	// CartID is the cart this reasoning was produced for, when the
	// caller had one (agents.Handler.recordPlan passes through
	// planRequest/loopRequest's own CartID -- empty for a memoryless
	// /agent/checkout or /agent/loop call with no cart_id at all).
	// This is the correlation key Service.Propose uses, later, to find
	// "the plan (if any) that led to this checkout" -- see
	// db/migrations/*_link_agent_plans_to_actions.sql for why that
	// link can only be discovered at Propose time, not fabricated here.
	CartID string

	// Proposal is the same policy.ProposedAction the agent produced --
	// never independently evaluated or authorized, just recorded as
	// what the agent proposed at this stage.
	Proposal ProposedAction

	// Steps is the reasoning trail: intent_extracted, tool_called,
	// tool_result_summary, alternatives_considered, proposed (in
	// whichever order and subset the producing agent path actually
	// went through -- BuyerAgent's single-shot path never has
	// tool_called/tool_result_summary steps, ToolCallingAgent's loop
	// path always does).
	Steps []RunStep

	CreatedAt time.Time
}

// toRun projects an AgentPlan into the same Run shape agent_actions-
// backed runs use, so both are retrievable through the identical
// GET /runs / GET /runs/{id} endpoints and the identical frontend
// RunsPage -- no new UI surface (PLAN-01-AGENTIC-CORE.md §4).
func (p AgentPlan) toRun() Run {
	return Run{
		ID:        p.ID,
		Action:    p.Proposal.Action,
		Amount:    p.Proposal.Amount,
		Currency:  p.Proposal.Currency,
		Merchant:  p.Proposal.Merchant,
		Items:     p.Proposal.Items,
		Decision:  PlanDecisionSentinel,
		CreatedAt: p.CreatedAt,
		Steps:     p.Steps,
	}
}
