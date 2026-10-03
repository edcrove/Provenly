-- Module: catalog. The database backs the test case rules the service already
-- enforces: a non-blank title, description and expected result of at most 10000
-- characters, deprecated_at set exactly while the test case is deprecated, and
-- updated_at never before created_at. NOT VALID: enforced for new and updated
-- rows; existing rows are not rechecked.

-- +goose Up
ALTER TABLE test_cases
    ADD CONSTRAINT test_cases_title_not_blank CHECK (title ~ '[^[:space:]]') NOT VALID,
    ADD CONSTRAINT test_cases_description_length CHECK (length(description) <= 10000) NOT VALID,
    ADD CONSTRAINT test_cases_expected_result_length CHECK (length(expected_result) <= 10000) NOT VALID,
    ADD CONSTRAINT test_cases_deprecated_at_matches_status CHECK ((status = 'deprecated') = (deprecated_at IS NOT NULL)) NOT VALID,
    ADD CONSTRAINT test_cases_updated_after_created CHECK (updated_at >= created_at) NOT VALID;

-- +goose Down
ALTER TABLE test_cases
    DROP CONSTRAINT test_cases_updated_after_created,
    DROP CONSTRAINT test_cases_deprecated_at_matches_status,
    DROP CONSTRAINT test_cases_expected_result_length,
    DROP CONSTRAINT test_cases_description_length,
    DROP CONSTRAINT test_cases_title_not_blank;
