-- Module: execution (TestRun/Execution). Only the execution module reads/writes these tables.
-- test_case_id columns hold catalog TC-IDs by value: no cross-module foreign keys.

-- +goose Up
CREATE TABLE test_runs (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_run_id TEXT        NOT NULL UNIQUE,
    provider        TEXT        NOT NULL,
    provider_run_id TEXT        NOT NULL,
    run_attempt     INTEGER     NOT NULL CHECK (run_attempt >= 1),
    pipeline        TEXT        NOT NULL DEFAULT '',
    branch          TEXT        NOT NULL DEFAULT '',
    commit_sha      TEXT        NOT NULL DEFAULT '',
    status          TEXT        NOT NULL CHECK (status IN ('created', 'running', 'completed', 'failed', 'cancelled')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    CONSTRAINT test_runs_external_run_id_format CHECK (external_run_id = provider || ':' || provider_run_id || ':' || run_attempt)
);

-- Immutable snapshot of the expected universe (active AND automated TC-IDs) taken when the run is created.
CREATE TABLE test_run_expected_cases (
    test_run_id  BIGINT NOT NULL REFERENCES test_runs (id),
    test_case_id BIGINT NOT NULL,
    PRIMARY KEY (test_run_id, test_case_id)
);

-- +goose StatementBegin
CREATE FUNCTION test_run_expected_cases_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'the expected-universe snapshot of a test run is immutable';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER test_run_expected_cases_immutable
    BEFORE UPDATE OR DELETE ON test_run_expected_cases
    FOR EACH ROW EXECUTE FUNCTION test_run_expected_cases_immutable();

CREATE TABLE test_results (
    id                     BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    test_run_id            BIGINT      NOT NULL REFERENCES test_runs (id),
    test_case_id           BIGINT,
    requested_test_case_id TEXT,
    correlation            TEXT        NOT NULL CHECK (correlation IN ('valid', 'missing', 'malformed', 'unknown', 'deprecated')),
    test_name              TEXT        NOT NULL,
    class_name             TEXT        NOT NULL DEFAULT '',
    suite_name             TEXT        NOT NULL DEFAULT '',
    status                 TEXT        NOT NULL CHECK (status IN ('passed', 'failed', 'error', 'skipped')),
    duration_ms            BIGINT      NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    error_message          TEXT        NOT NULL DEFAULT '',
    error_details          TEXT        NOT NULL DEFAULT '',
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT test_results_valid_has_test_case CHECK ((correlation = 'valid') = (test_case_id IS NOT NULL))
);

CREATE INDEX test_results_run_idx ON test_results (test_run_id, id);
CREATE INDEX test_results_test_case_idx ON test_results (test_case_id, id DESC) WHERE test_case_id IS NOT NULL;

-- +goose Down
DROP TABLE test_results;
DROP TRIGGER test_run_expected_cases_immutable ON test_run_expected_cases;
DROP FUNCTION test_run_expected_cases_immutable();
DROP TABLE test_run_expected_cases;
DROP TABLE test_runs;
