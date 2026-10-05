-- Module: execution. The suite a run was reported for (MVP D2, partial runs): its key and name as they were when
-- the run was created; like the rest of the run's identity they never change. The expected universe snapshot
-- already holds the suite's selection at that moment.

-- +goose Up
ALTER TABLE test_runs
    ADD COLUMN suite_key  TEXT CHECK (suite_key ~ '^[a-z][a-z0-9-]{0,29}$'),
    ADD COLUMN suite_name TEXT,
    ADD CONSTRAINT test_runs_suite_named CHECK ((suite_key IS NULL) = (suite_name IS NULL));
CREATE INDEX test_runs_suite ON test_runs (project_id, suite_key, id DESC) WHERE suite_key IS NOT NULL;

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

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION test_runs_protect_identity() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'test runs cannot be deleted (run %, %)', OLD.id, OLD.external_run_id;
    END IF;
    IF (NEW.id, NEW.project_id, NEW.external_run_id, NEW.provider, NEW.provider_run_id, NEW.run_attempt, NEW.report_sha256, NEW.created_at)
        IS DISTINCT FROM
       (OLD.id, OLD.project_id, OLD.external_run_id, OLD.provider, OLD.provider_run_id, OLD.run_attempt, OLD.report_sha256, OLD.created_at) THEN
        RAISE EXCEPTION 'the identity of a test run is immutable (run %, %)', OLD.id, OLD.external_run_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
DROP INDEX test_runs_suite;
ALTER TABLE test_runs DROP CONSTRAINT test_runs_suite_named, DROP COLUMN suite_name, DROP COLUMN suite_key;
