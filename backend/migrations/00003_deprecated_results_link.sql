-- Module: execution. Results for a deprecated TC-ID keep the link to the test
-- case so they appear in its history; they remain excluded from summaries.

-- +goose Up
ALTER TABLE test_results DROP CONSTRAINT test_results_valid_has_test_case;
ALTER TABLE test_results ADD CONSTRAINT test_results_linked_has_test_case
    CHECK ((correlation IN ('valid', 'deprecated')) = (test_case_id IS NOT NULL));

-- +goose Down
ALTER TABLE test_results DROP CONSTRAINT test_results_linked_has_test_case;
UPDATE test_results SET test_case_id = NULL WHERE correlation = 'deprecated';
ALTER TABLE test_results ADD CONSTRAINT test_results_valid_has_test_case
    CHECK ((correlation = 'valid') = (test_case_id IS NOT NULL));
