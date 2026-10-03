-- Module: execution. A run's status is how the CI execution ended; "failed" is
-- renamed "interrupted" so that "failed" only ever describes test outcomes
-- (the run verdict is derived from the summary). created/running stay allowed
-- for the live run lifecycle (Core MVP) but are not part of the API yet.

-- +goose Up
ALTER TABLE test_runs DROP CONSTRAINT test_runs_status_check;
UPDATE test_runs SET status = 'interrupted' WHERE status = 'failed';
ALTER TABLE test_runs ADD CONSTRAINT test_runs_status_check
    CHECK (status IN ('created', 'running', 'completed', 'interrupted', 'cancelled'));

-- +goose Down
ALTER TABLE test_runs DROP CONSTRAINT test_runs_status_check;
UPDATE test_runs SET status = 'failed' WHERE status = 'interrupted';
ALTER TABLE test_runs ADD CONSTRAINT test_runs_status_check
    CHECK (status IN ('created', 'running', 'completed', 'failed', 'cancelled'));
