-- Module: execution. Live events carry the attempt of the test they belong to (a retry is a new attempt), so a
-- retried test is not mistaken for a duplicate delivery and the live status follows the last attempt, as summaries do.

-- +goose Up
ALTER TABLE test_run_events ADD COLUMN attempt INT NOT NULL DEFAULT 1 CHECK (attempt BETWEEN 1 AND 100);

-- +goose Down
ALTER TABLE test_run_events DROP COLUMN attempt;
