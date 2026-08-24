-- +goose Up

-- Phase 8: persisted safety-evaluation runs (the red-team/eval suite).
CREATE TABLE safety_evaluations (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    scenario_count INTEGER NOT NULL,
    unauthorized_payments INTEGER NOT NULL DEFAULT 0,
    duplicate_payments INTEGER NOT NULL DEFAULT 0,
    policy_bypasses INTEGER NOT NULL DEFAULT 0,
    wrong_merchant INTEGER NOT NULL DEFAULT 0,
    invalid_authorization INTEGER NOT NULL DEFAULT 0,
    graceful_failure_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    passed BOOLEAN NOT NULL,
    source TEXT NOT NULL DEFAULT 'suite',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_safety_evaluations_run ON safety_evaluations(run_id);
CREATE INDEX idx_safety_evaluations_created ON safety_evaluations(created_at DESC);

-- Individual attack results per evaluation, for the evidence panel.
CREATE TABLE safety_attack_results (
    id BIGSERIAL PRIMARY KEY,
    evaluation_id TEXT NOT NULL REFERENCES safety_evaluations(id),
    attack_id TEXT NOT NULL,
    attack_string TEXT NOT NULL,
    attack_kind TEXT NOT NULL,
    blocked BOOLEAN NOT NULL,
    decision TEXT NOT NULL,
    reason TEXT,
    policy_check TEXT,
    provider_call_delta INTEGER NOT NULL DEFAULT 0,
    run_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_safety_attack_results_eval ON safety_attack_results(evaluation_id);

-- +goose Down

DROP TABLE IF EXISTS safety_attack_results;
DROP TABLE IF EXISTS safety_evaluations;