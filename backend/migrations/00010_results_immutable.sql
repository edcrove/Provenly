-- Module: execution. The ingested final result is the source of truth: results
-- and parse errors are written only by the transaction that creates their run
-- (its created_at is that transaction's now()) and never updated or deleted, so a
-- run's verdict, summary and history always reflect the report that was sent.

-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION test_run_children_immutable() RETURNS trigger AS $$
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

CREATE TRIGGER test_results_immutable
    BEFORE INSERT OR UPDATE OR DELETE ON test_results
    FOR EACH ROW EXECUTE FUNCTION test_run_children_immutable();

CREATE TRIGGER test_run_parse_errors_immutable
    BEFORE INSERT OR UPDATE OR DELETE ON test_run_parse_errors
    FOR EACH ROW EXECUTE FUNCTION test_run_children_immutable();

-- +goose Down
DROP TRIGGER test_run_parse_errors_immutable ON test_run_parse_errors;
DROP TRIGGER test_results_immutable ON test_results;
DROP FUNCTION test_run_children_immutable();
