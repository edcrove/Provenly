-- Module: execution. Snapshot amendment (DEC-42): a maintainer may include in a
-- run's universe a TC-ID that has valid results in that run but was outside its
-- snapshot (e.g. it was marked manual when the run was created). The snapshot
-- itself never changes; amendments are a separate, append-only and audited list
-- (who, when, why) that the summary adds to the universe and that marks the run
-- as edited. test_case_id and amended_by hold other modules' ids by value.

-- +goose Up
CREATE TABLE test_run_amendments (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    test_run_id         BIGINT      NOT NULL REFERENCES test_runs (id),
    test_case_id        BIGINT      NOT NULL,
    amended_by          BIGINT      NOT NULL,
    amended_by_username TEXT        NOT NULL CHECK (length(amended_by_username) BETWEEN 1 AND 32),
    reason              TEXT        NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (test_run_id, test_case_id)
);

-- +goose StatementBegin
CREATE FUNCTION test_run_amendments_protect() RETURNS trigger AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'snapshot amendments are append-only (run %)', OLD.test_run_id;
    END IF;
    IF EXISTS (SELECT 1 FROM test_run_expected_cases WHERE test_run_id = NEW.test_run_id AND test_case_id = NEW.test_case_id) THEN
        RAISE EXCEPTION 'TC-ID % is already in the snapshot of run %', NEW.test_case_id, NEW.test_run_id;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM test_results
                   WHERE test_run_id = NEW.test_run_id AND test_case_id = NEW.test_case_id AND correlation = 'valid') THEN
        RAISE EXCEPTION 'TC-ID % has no valid result in run %', NEW.test_case_id, NEW.test_run_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER test_run_amendments_protect BEFORE INSERT OR UPDATE OR DELETE ON test_run_amendments
    FOR EACH ROW EXECUTE FUNCTION test_run_amendments_protect();

-- +goose Down
DROP TABLE test_run_amendments;
DROP FUNCTION test_run_amendments_protect();
