-- Module: execution. Runs belong to a project (MVP D9, D11). project_id holds a
-- catalog project id by value (no cross-module foreign key); runs created before
-- projects existed belong to the default project (id 1, created by 00012). The
-- externalRunId is unique per project, so one CI run can report to several
-- projects. A tc-id declaring another project's key is a 'wrong_project' diagnostic.

-- +goose Up
ALTER TABLE test_runs ADD COLUMN project_id BIGINT NOT NULL DEFAULT 1;
ALTER TABLE test_runs ALTER COLUMN project_id DROP DEFAULT;
ALTER TABLE test_runs DROP CONSTRAINT test_runs_external_run_id_key;
ALTER TABLE test_runs ADD CONSTRAINT test_runs_project_external_run_id_key UNIQUE (project_id, external_run_id);
CREATE INDEX test_runs_project_idx ON test_runs (project_id, id DESC);

ALTER TABLE test_results DROP CONSTRAINT test_results_correlation_check;
ALTER TABLE test_results ADD CONSTRAINT test_results_correlation_check
    CHECK (correlation IN ('valid', 'missing', 'malformed', 'unknown', 'deprecated', 'wrong_project'));

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

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION test_runs_protect_identity() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'test runs cannot be deleted (run %, %)', OLD.id, OLD.external_run_id;
    END IF;
    IF (NEW.id, NEW.external_run_id, NEW.provider, NEW.provider_run_id, NEW.run_attempt, NEW.report_sha256, NEW.created_at)
        IS DISTINCT FROM
       (OLD.id, OLD.external_run_id, OLD.provider, OLD.provider_run_id, OLD.run_attempt, OLD.report_sha256, OLD.created_at) THEN
        RAISE EXCEPTION 'the identity of a test run is immutable (run %, %)', OLD.id, OLD.external_run_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
-- Results are immutable (00010): reclassify wrong_project rows as unknown with the trigger off.
ALTER TABLE test_results DISABLE TRIGGER test_results_immutable;
UPDATE test_results SET correlation = 'unknown' WHERE correlation = 'wrong_project';
ALTER TABLE test_results ENABLE TRIGGER test_results_immutable;
ALTER TABLE test_results DROP CONSTRAINT test_results_correlation_check;
ALTER TABLE test_results ADD CONSTRAINT test_results_correlation_check
    CHECK (correlation IN ('valid', 'missing', 'malformed', 'unknown', 'deprecated'));
DROP INDEX test_runs_project_idx;
ALTER TABLE test_runs DROP CONSTRAINT test_runs_project_external_run_id_key;
ALTER TABLE test_runs ADD CONSTRAINT test_runs_external_run_id_key UNIQUE (external_run_id);
ALTER TABLE test_runs DROP COLUMN project_id;
