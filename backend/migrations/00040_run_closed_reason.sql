-- Module: execution. Provenly closes runs that would otherwise stay running for ever (CHG-003, Ed 2026-11-06):
-- CI finalizes a live or sharded run whose report will not arrive (finalized), a live or sharded run without
-- events or shards for a while times out (timeout), and archiving a project closes its running runs (archived).
-- closed_reason says which; it stays null for a run its report, its shards or a person ended. Set once.

-- +goose Up
ALTER TABLE test_runs
    ADD COLUMN closed_reason TEXT,
    ADD CONSTRAINT test_runs_closed_reason_check CHECK (closed_reason IN ('finalized', 'timeout', 'archived')),
    ADD CONSTRAINT test_runs_closed_reason_ended CHECK (closed_reason IS NULL OR status <> 'running');

-- The abandoned-run sweep reads the running runs only.
CREATE INDEX test_runs_running_idx ON test_runs (mode) WHERE status = 'running';

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
        NEW.suite_key, NEW.suite_name, NEW.mode, NEW.started_by, NEW.shard_total)
        IS DISTINCT FROM
       (OLD.id, OLD.project_id, OLD.external_run_id, OLD.provider, OLD.provider_run_id, OLD.run_attempt, OLD.created_at,
        OLD.suite_key, OLD.suite_name, OLD.mode, OLD.started_by, OLD.shard_total) THEN
        RAISE EXCEPTION 'the identity of a test run is immutable (run %, %)', OLD.id, OLD.external_run_id;
    END IF;
    IF OLD.status <> 'running' AND (NEW.status, NEW.closed_reason) IS DISTINCT FROM (OLD.status, OLD.closed_reason) THEN
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
    -- The digest of a live run's final report is set once, when the report completes it.
    IF NOT (OLD.mode = 'live' AND OLD.status = 'running' AND OLD.report_sha256 = '') AND NEW.report_sha256 IS DISTINCT FROM OLD.report_sha256 THEN
        RAISE EXCEPTION 'the identity of a test run is immutable (run %, %)', OLD.id, OLD.external_run_id;
    END IF;
    IF (NEW.id, NEW.project_id, NEW.external_run_id, NEW.provider, NEW.provider_run_id, NEW.run_attempt, NEW.created_at,
        NEW.suite_key, NEW.suite_name, NEW.mode, NEW.started_by, NEW.shard_total)
        IS DISTINCT FROM
       (OLD.id, OLD.project_id, OLD.external_run_id, OLD.provider, OLD.provider_run_id, OLD.run_attempt, OLD.created_at,
        OLD.suite_key, OLD.suite_name, OLD.mode, OLD.started_by, OLD.shard_total) THEN
        RAISE EXCEPTION 'the identity of a test run is immutable (run %, %)', OLD.id, OLD.external_run_id;
    END IF;
    IF OLD.status <> 'running' AND NEW.status IS DISTINCT FROM OLD.status THEN
        RAISE EXCEPTION 'a finished test run keeps its status (run %, %)', OLD.id, OLD.external_run_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
DROP INDEX test_runs_running_idx;
ALTER TABLE test_runs
    DROP CONSTRAINT test_runs_closed_reason_ended,
    DROP CONSTRAINT test_runs_closed_reason_check,
    DROP COLUMN closed_reason;
