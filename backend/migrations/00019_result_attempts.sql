-- Module: execution. Retries (MVP D1): each result records which attempt of its
-- test it was (1 = first). The logical result of a test in a run is its last
-- attempt; a pass after failed attempts is passed and flaky. Existing results
-- were single attempts.

-- +goose Up
ALTER TABLE test_results ADD COLUMN attempt INT NOT NULL DEFAULT 1 CHECK (attempt BETWEEN 1 AND 100);
-- One attempt number per test of a run (the identity of a test is its suite, class and name).
CREATE INDEX test_results_execution_idx ON test_results (test_run_id, suite_name, class_name, test_name, attempt);

-- +goose Down
DROP INDEX test_results_execution_idx;
ALTER TABLE test_results DROP COLUMN attempt;
