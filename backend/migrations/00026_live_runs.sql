-- Module: execution. Live runs (Trello "Live Test Run Streaming & Reconciliation", Planning #9): CI starts a run before
-- executing, streams events while tests run and finally sends its JUnit report, which stays the source of truth.
-- Events are append-only, idempotent by event id, and only accepted while the live run is running; the final report
-- completes the run and fixes its digest once.

-- +goose Up
CREATE TABLE test_run_events (
    id                     BIGSERIAL   PRIMARY KEY,
    test_run_id            BIGINT      NOT NULL REFERENCES test_runs (id),
    event_id               TEXT        NOT NULL CHECK (event_id ~ '^[A-Za-z0-9._:-]{1,100}$'),
    sequence               BIGINT      NOT NULL CHECK (sequence >= 0),
    event_type             TEXT        NOT NULL CHECK (event_type IN ('test.started', 'test.finished', 'step.started', 'step.completed', 'run.finished')),
    test_name              TEXT        NOT NULL DEFAULT '' CHECK (char_length(test_name) <= 1000),
    requested_test_case_id TEXT        CHECK (char_length(requested_test_case_id) <= 100),
    test_case_id           BIGINT,
    status                 TEXT        CHECK (status IN ('passed', 'failed', 'error', 'skipped')),
    occurred_at            TIMESTAMPTZ NOT NULL,
    received_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT test_run_events_unique UNIQUE (test_run_id, event_id),
    CONSTRAINT test_run_events_finished_status CHECK ((event_type = 'test.finished') = (status IS NOT NULL))
);
CREATE INDEX test_run_events_run ON test_run_events (test_run_id, sequence);

-- +goose StatementBegin
CREATE FUNCTION test_run_events_protect() RETURNS trigger AS $$
DECLARE
    run RECORD;
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'live events are immutable (run %)', OLD.test_run_id;
    END IF;
    SELECT status, mode INTO run FROM test_runs WHERE id = NEW.test_run_id;
    IF run.mode IS DISTINCT FROM 'live' OR run.status IS DISTINCT FROM 'running' THEN
        RAISE EXCEPTION 'live events are accepted only while a live run is running (run %)', NEW.test_run_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER test_run_events_protect BEFORE INSERT OR UPDATE OR DELETE ON test_run_events
    FOR EACH ROW EXECUTE FUNCTION test_run_events_protect();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION test_runs_protect_identity() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'test runs cannot be deleted (run %, %)', OLD.id, OLD.external_run_id;
    END IF;
    -- The digest of a live run's final report is set once, when the report completes it.
    IF NOT (OLD.mode = 'live' AND OLD.status = 'running' AND OLD.report_sha256 = '') AND NEW.report_sha256 IS DISTINCT FROM OLD.report_sha256 THEN
        RAISE EXCEPTION 'the identity of a test run is immutable (run %, %)', OLD.id, OLD.external_run_id;
    END IF;
    IF (NEW.id, NEW.project_id, NEW.external_run_id, NEW.provider, NEW.provider_run_id, NEW.run_attempt, NEW.created_at,
        NEW.suite_key, NEW.suite_name, NEW.mode, NEW.started_by)
        IS DISTINCT FROM
       (OLD.id, OLD.project_id, OLD.external_run_id, OLD.provider, OLD.provider_run_id, OLD.run_attempt, OLD.created_at,
        OLD.suite_key, OLD.suite_name, OLD.mode, OLD.started_by) THEN
        RAISE EXCEPTION 'the identity of a test run is immutable (run %, %)', OLD.id, OLD.external_run_id;
    END IF;
    IF OLD.status <> 'running' AND NEW.status IS DISTINCT FROM OLD.status THEN
        RAISE EXCEPTION 'a finished test run keeps its status (run %, %)', OLD.id, OLD.external_run_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION test_runs_protect_identity() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'test runs cannot be deleted (run %, %)', OLD.id, OLD.external_run_id;
    END IF;
    IF (NEW.id, NEW.project_id, NEW.external_run_id, NEW.provider, NEW.provider_run_id, NEW.run_attempt, NEW.report_sha256, NEW.created_at,
        NEW.suite_key, NEW.suite_name, NEW.mode, NEW.started_by)
        IS DISTINCT FROM
       (OLD.id, OLD.project_id, OLD.external_run_id, OLD.provider, OLD.provider_run_id, OLD.run_attempt, OLD.report_sha256, OLD.created_at,
        OLD.suite_key, OLD.suite_name, OLD.mode, OLD.started_by) THEN
        RAISE EXCEPTION 'the identity of a test run is immutable (run %, %)', OLD.id, OLD.external_run_id;
    END IF;
    IF OLD.status <> 'running' AND NEW.status IS DISTINCT FROM OLD.status THEN
        RAISE EXCEPTION 'a finished test run keeps its status (run %, %)', OLD.id, OLD.external_run_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
DROP TABLE test_run_events;
DROP FUNCTION test_run_events_protect();
