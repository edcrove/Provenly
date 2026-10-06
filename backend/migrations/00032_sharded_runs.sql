-- Module: execution. Sharded runs (card #57, refined decision 1): CI splits one logical run (same provider, run id
-- and attempt) into N reports, one per shard (?shard=i/N). The first shard creates the run (mode 'sharded', running,
-- with its snapshot); each shard adds its results once; the run completes when every shard arrived, or CI finalizes
-- it as interrupted. Shards, like results, are append-only.

-- +goose Up
ALTER TABLE test_runs DROP CONSTRAINT test_runs_mode_check;
ALTER TABLE test_runs
    ADD CONSTRAINT test_runs_mode_check CHECK (mode IN ('batch', 'manual', 'live', 'sharded')),
    ADD COLUMN shard_total INT CHECK (shard_total BETWEEN 2 AND 100),
    ADD CONSTRAINT test_runs_sharded_total CHECK ((mode = 'sharded') = (shard_total IS NOT NULL));

ALTER TABLE test_results ADD COLUMN shard INT CHECK (shard BETWEEN 1 AND 100);

-- Parse errors are numbered per report: with shards, per shard.
ALTER TABLE test_run_parse_errors ADD COLUMN shard INT CHECK (shard BETWEEN 1 AND 100);
ALTER TABLE test_run_parse_errors DROP CONSTRAINT test_run_parse_errors_pkey;
CREATE UNIQUE INDEX test_run_parse_errors_unique ON test_run_parse_errors (test_run_id, coalesce(shard, 0), case_index);

CREATE TABLE test_run_shards (
    test_run_id   BIGINT      NOT NULL REFERENCES test_runs (id),
    shard         INT         NOT NULL CHECK (shard BETWEEN 1 AND 100),
    report_sha256 TEXT        NOT NULL CHECK (report_sha256 ~ '^[0-9a-f]{64}$'),
    status        TEXT        NOT NULL CHECK (status IN ('completed', 'interrupted', 'cancelled')),
    received_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (test_run_id, shard)
);

-- +goose StatementBegin
CREATE FUNCTION test_run_shards_protect() RETURNS trigger AS $$
DECLARE
    run RECORD;
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'received shards are immutable (run %)', OLD.test_run_id;
    END IF;
    SELECT status, mode, shard_total INTO run FROM test_runs WHERE id = NEW.test_run_id;
    IF run.mode IS DISTINCT FROM 'sharded' OR run.status IS DISTINCT FROM 'running' THEN
        RAISE EXCEPTION 'shards are accepted only while a sharded run is running (run %)', NEW.test_run_id;
    END IF;
    IF NEW.shard > run.shard_total THEN
        RAISE EXCEPTION 'shard % is outside the run''s % shards (run %)', NEW.shard, run.shard_total, NEW.test_run_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER test_run_shards_protect BEFORE INSERT OR UPDATE OR DELETE ON test_run_shards
    FOR EACH ROW EXECUTE FUNCTION test_run_shards_protect();

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
DROP TABLE test_run_shards;
DROP FUNCTION test_run_shards_protect();
DROP INDEX test_run_parse_errors_unique;
ALTER TABLE test_run_parse_errors ADD PRIMARY KEY (test_run_id, case_index), DROP COLUMN shard;
ALTER TABLE test_results DROP COLUMN shard;
ALTER TABLE test_runs DROP CONSTRAINT test_runs_sharded_total, DROP COLUMN shard_total, DROP CONSTRAINT test_runs_mode_check;
ALTER TABLE test_runs ADD CONSTRAINT test_runs_mode_check CHECK (mode IN ('batch', 'manual', 'live'));
