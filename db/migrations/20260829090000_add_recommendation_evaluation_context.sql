-- +goose Up

-- The campaign agent (backend/campaign/) needs to know, for each REJECT
-- recommendation, what cart total and budget produced that rejection --
-- otherwise "a 15% discount would bring N of them under budget" is an
-- unverifiable claim. GrowthAgent.EvaluateCandidate already has both
-- values in scope (cartTotal, budget.Budget); this just persists them.
-- Nullable because existing rows predate this column and have no
-- reconstructable value -- the campaign agent treats NULL rows as
-- "rejected volume observed, reinstatement-at-discount unknown" rather
-- than backfilling a guess.
ALTER TABLE recommendations
    ADD COLUMN cart_total_at_evaluation BIGINT,
    ADD COLUMN budget_at_evaluation BIGINT;

-- +goose Down

ALTER TABLE recommendations
    DROP COLUMN cart_total_at_evaluation,
    DROP COLUMN budget_at_evaluation;
