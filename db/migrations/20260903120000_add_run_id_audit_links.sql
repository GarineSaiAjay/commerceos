-- +goose Up

-- PLAN-05-SELLER-DASHBOARD.md §2's "Orders -> Runs audit-trail link":
-- orders.run_id lets the merchant dashboard's Orders page link straight
-- to the agent run (policy.Run, GET /runs/{id}) that authorized each
-- order's payment. Nullable: a draft order that was created via POST
-- /carts/{id}/checkout but never reached a successful payment has no
-- authorizing run yet, and never will if it's abandoned.
--
-- authorizations.action_id and approval_requests.action_id carry the
-- same agent_actions.id (run_id) through the two paths an order's
-- payment can be authorized by: a Level 1 proposal issues an
-- authorization directly (policy.Service.Propose); a Level 2/3
-- proposal instead creates a durable approval_requests row first and
-- only issues the authorization once approved (policy.Service.Approve)
-- -- action_id has to survive that detour too, or a human-approved
-- (the more interesting, higher-risk) order would be exactly the one
-- that never gets a run link.
ALTER TABLE authorizations ADD COLUMN action_id TEXT REFERENCES agent_actions(id);
ALTER TABLE approval_requests ADD COLUMN action_id TEXT REFERENCES agent_actions(id);
ALTER TABLE orders ADD COLUMN run_id TEXT REFERENCES agent_actions(id);

CREATE INDEX idx_orders_run_id ON orders(run_id);

-- +goose Down

DROP INDEX idx_orders_run_id;
ALTER TABLE orders DROP COLUMN run_id;
ALTER TABLE approval_requests DROP COLUMN action_id;
ALTER TABLE authorizations DROP COLUMN action_id;
