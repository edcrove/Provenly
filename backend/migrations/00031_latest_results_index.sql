-- +goose Up
-- Review-panel audit 2026-10-06: the latest valid results of a test case (requirement coverage, issue verification,
-- dashboard) are read by test case and newest run first. Without this index each lookup walked the whole results
-- table backwards (seconds for a few hundred test cases).
CREATE INDEX test_results_case_run_valid_idx ON test_results (test_case_id, test_run_id DESC) WHERE correlation = 'valid';

-- +goose Down
DROP INDEX test_results_case_run_valid_idx;
