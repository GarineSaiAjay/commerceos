-- +goose Up

-- PLAN-05-SELLER-DASHBOARD.md §4 minor polish: experiments.updated_at
-- lets the Analytics dashboard show when a named experiment was last
-- actually re-run, distinct from created_at (which experiment.go's
-- List doc comment deliberately leaves untouched by the upsert, so a
-- re-run keeps its original position in the history ordering rather
-- than jumping to the top -- updated_at is the new column that DOES
-- change on every run, for display only, not for ordering).
ALTER TABLE experiments ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- +goose Down

ALTER TABLE experiments DROP COLUMN updated_at;
