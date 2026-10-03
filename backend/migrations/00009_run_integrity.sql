-- Module: execution. The database backs the run invariants the service already
-- keeps: a run's identity ({provider}:{run_id}:{run_attempt} and the digest of
-- the report that created it) never changes, runs are never deleted (attempt
-- history is preserved), no TC-ID joins a snapshot after the run's creating
-- transaction, and a run never starts after it completed. The lifecycle status
-- and timestamps may still change (live runs, Core MVP).

-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION test_runs_protect_identity() RETURNS trigger AS $$
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

CREATE TRIGGER test_runs_protect_identity
    BEFORE UPDATE OR DELETE ON test_runs
    FOR EACH ROW EXECUTE FUNCTION test_runs_protect_identity();

-- created_at defaults to now(), the start of the creating transaction: a snapshot
-- row may only be written by that same transaction.
-- +goose StatementBegin
CREATE FUNCTION test_run_expected_cases_insert_once() RETURNS trigger AS $$
BEGIN
    IF (SELECT created_at FROM test_runs WHERE id = NEW.test_run_id) IS DISTINCT FROM now() THEN
        RAISE EXCEPTION 'the expected-universe snapshot of a test run is immutable (run %)', NEW.test_run_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER test_run_expected_cases_insert_once
    BEFORE INSERT ON test_run_expected_cases
    FOR EACH ROW EXECUTE FUNCTION test_run_expected_cases_insert_once();

-- NOT VALID: enforced for new and updated rows; existing rows are not rechecked.
ALTER TABLE test_runs ADD CONSTRAINT test_runs_started_before_completed
    CHECK (started_at IS NULL OR completed_at IS NULL OR started_at <= completed_at) NOT VALID;

-- +goose Down
ALTER TABLE test_runs DROP CONSTRAINT test_runs_started_before_completed;
DROP TRIGGER test_run_expected_cases_insert_once ON test_run_expected_cases;
DROP FUNCTION test_run_expected_cases_insert_once();
DROP TRIGGER test_runs_protect_identity ON test_runs;
DROP FUNCTION test_runs_protect_identity();
