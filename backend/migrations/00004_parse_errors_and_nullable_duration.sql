-- Module: execution. Unknown/invalid durations are NULL (0 is a real, rounded
-- value) and parse errors of the ingested report are stored with the run.

-- +goose Up
ALTER TABLE test_results ALTER COLUMN duration_ms DROP NOT NULL;
ALTER TABLE test_results ALTER COLUMN duration_ms DROP DEFAULT;

CREATE TABLE test_run_parse_errors (
    test_run_id   BIGINT  NOT NULL REFERENCES test_runs (id),
    case_index    INTEGER NOT NULL CHECK (case_index >= 0),
    test_name     TEXT    NOT NULL,
    message       TEXT    NOT NULL,
    persisted     BOOLEAN NOT NULL,
    PRIMARY KEY (test_run_id, case_index)
);

-- +goose Down
DROP TABLE test_run_parse_errors;
UPDATE test_results SET duration_ms = 0 WHERE duration_ms IS NULL;
ALTER TABLE test_results ALTER COLUMN duration_ms SET DEFAULT 0;
ALTER TABLE test_results ALTER COLUMN duration_ms SET NOT NULL;
