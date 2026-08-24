-- +goose Up

CREATE TABLE experiments (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    metric TEXT NOT NULL,
    population INTEGER NOT NULL,
    control_size INTEGER NOT NULL,
    treatment_size INTEGER NOT NULL,
    control_value DOUBLE PRECISION NOT NULL,
    treatment_value DOUBLE PRECISION NOT NULL,
    lift DOUBLE PRECISION NOT NULL,
    ci_lower DOUBLE PRECISION NOT NULL,
    ci_upper DOUBLE PRECISION NOT NULL,
    source TEXT NOT NULL DEFAULT 'simulated',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE experiment_assignments (
    id BIGSERIAL PRIMARY KEY,
    experiment_id TEXT NOT NULL REFERENCES experiments(id),
    session_id INTEGER NOT NULL,
    group_name TEXT NOT NULL CHECK (group_name IN ('control', 'treatment')),
    UNIQUE (experiment_id, session_id)
);

-- +goose Down

DROP TABLE experiment_assignments;
DROP TABLE experiments;