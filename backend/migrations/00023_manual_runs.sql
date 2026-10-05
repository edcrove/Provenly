-- Module: execution. Manual execution (MVP D3, Planning #4): a person starts a run, records the result of each
-- expected test case one by one and completes it. A run's mode tells how its results arrive: 'batch' (one CI
-- report, written with the run as before), 'manual' (recorded while the run is running) and 'live' (reserved for
-- streamed runs). Results stay append-only: a manual re-test is a new attempt, never an update.

-- +goose Up
ALTER TABLE test_runs
    ADD COLUMN mode TEXT NOT NULL DEFAULT 'batch' CHECK (mode IN ('batch', 'manual', 'live')),
    ADD COLUMN started_by TEXT,
    ADD CONSTRAINT test_runs_running_is_not_batch CHECK (status <> 'running' OR mode <> 'batch');

ALTER TABLE test_results
    ADD COLUMN recorded_by TEXT,
    ADD COLUMN failed_step INT CHECK (failed_step >= 1);

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

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION test_run_children_immutable() RETURNS trigger AS $$
DECLARE
    what text := CASE TG_TABLE_NAME WHEN 'test_results' THEN 'ingested test results are immutable'
                                    ELSE 'parse errors of a test run are immutable' END;
    run RECORD;
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION '% (run %)', what, OLD.test_run_id;
    END IF;
    SELECT created_at, status, mode INTO run FROM test_runs WHERE id = NEW.test_run_id;
    IF run.created_at IS DISTINCT FROM now() AND NOT (run.status = 'running' AND run.mode <> 'batch') THEN
        RAISE EXCEPTION '%: they are written only when the run is created, or while a manual or live run is running (run %)', what, NEW.test_run_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION test_run_children_immutable() RETURNS trigger AS $$
DECLARE
    what text := CASE TG_TABLE_NAME WHEN 'test_results' THEN 'ingested test results are immutable'
                                    ELSE 'parse errors of a test run are immutable' END;
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION '% (run %)', what, OLD.test_run_id;
    END IF;
    IF (SELECT created_at FROM test_runs WHERE id = NEW.test_run_id) IS DISTINCT FROM now() THEN
        RAISE EXCEPTION '%: they are written only when the run is created (run %)', what, NEW.test_run_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION test_runs_protect_identity() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'test runs cannot be deleted (run %, %)', OLD.id, OLD.external_run_id;
    END IF;
    IF (NEW.id, NEW.project_id, NEW.external_run_id, NEW.provider, NEW.provider_run_id, NEW.run_attempt, NEW.report_sha256, NEW.created_at,
        NEW.suite_key, NEW.suite_name)
        IS DISTINCT FROM
       (OLD.id, OLD.project_id, OLD.external_run_id, OLD.provider, OLD.provider_run_id, OLD.run_attempt, OLD.report_sha256, OLD.created_at,
        OLD.suite_key, OLD.suite_name) THEN
        RAISE EXCEPTION 'the identity of a test run is immutable (run %, %)', OLD.id, OLD.external_run_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
ALTER TABLE test_results DROP COLUMN failed_step, DROP COLUMN recorded_by;
ALTER TABLE test_runs DROP CONSTRAINT test_runs_running_is_not_batch, DROP COLUMN started_by, DROP COLUMN mode;
