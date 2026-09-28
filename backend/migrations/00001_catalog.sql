-- Module: catalog (Test Catalog). Only the catalog module reads/writes these tables.

-- +goose Up
CREATE TABLE test_cases (
    id              BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
    title           TEXT        NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    description     TEXT        NOT NULL DEFAULT '',
    expected_result TEXT        NOT NULL DEFAULT '',
    status          TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'deprecated')),
    automated       BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deprecated_at   TIMESTAMPTZ
);

CREATE INDEX test_cases_expected_universe_idx ON test_cases (id) WHERE status = 'active' AND automated;

-- TC-IDs are immutable and never reused: forbid changing an id and deleting rows.
-- +goose StatementBegin
CREATE FUNCTION test_cases_protect_identity() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'test cases cannot be deleted (TC-%s); deprecate instead', OLD.id;
    END IF;
    IF NEW.id <> OLD.id THEN
        RAISE EXCEPTION 'test case id is immutable (TC-%s)', OLD.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER test_cases_protect_identity
    BEFORE UPDATE OR DELETE ON test_cases
    FOR EACH ROW EXECUTE FUNCTION test_cases_protect_identity();

CREATE TABLE test_steps (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    test_case_id    BIGINT      NOT NULL REFERENCES test_cases (id),
    position        INTEGER     NOT NULL CHECK (position >= 1),
    action          TEXT        NOT NULL CHECK (length(action) BETWEEN 1 AND 2000),
    expected_result TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT test_steps_position_unique UNIQUE (test_case_id, position) DEFERRABLE INITIALLY DEFERRED
);

-- +goose Down
DROP TABLE test_steps;
DROP TRIGGER test_cases_protect_identity ON test_cases;
DROP FUNCTION test_cases_protect_identity();
DROP TABLE test_cases;
