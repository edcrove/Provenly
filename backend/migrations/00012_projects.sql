-- Module: catalog. Projects (MVP D9) and per-project test case keys (MVP D10):
-- a test case belongs to one project and is identified by <PROJECT_KEY>-<number>,
-- numbered per project, immutable and never reused. Existing test cases move to
-- the default project TC with number = id, so TC-153 stays TC-153.

-- +goose Up
CREATE TABLE projects (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key         TEXT        NOT NULL UNIQUE CHECK (key ~ '^[A-Z][A-Z0-9]{1,9}$'),
    name        TEXT        NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    description TEXT        NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
    next_number BIGINT      NOT NULL DEFAULT 1 CHECK (next_number >= 1),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO projects (key, name, description, next_number)
SELECT 'TC', 'Default', 'Test cases created before projects existed.', coalesce(max(id), 0) + 1 FROM test_cases;

ALTER TABLE test_cases ADD COLUMN project_id BIGINT REFERENCES projects (id), ADD COLUMN number BIGINT;
UPDATE test_cases SET project_id = (SELECT id FROM projects WHERE key = 'TC'), number = id;
ALTER TABLE test_cases
    ALTER COLUMN project_id SET NOT NULL,
    ALTER COLUMN number SET NOT NULL,
    ADD CONSTRAINT test_cases_number_positive CHECK (number >= 1),
    ADD CONSTRAINT test_cases_project_number_unique UNIQUE (project_id, number);

CREATE INDEX test_cases_project_expected_idx ON test_cases (project_id, id) WHERE status = 'active' AND automated;

-- The key (project + number) joins the id as immutable identity.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION test_cases_protect_identity() RETURNS trigger AS $$
DECLARE
    tc_key TEXT := (SELECT key FROM projects WHERE id = OLD.project_id) || '-' || OLD.number;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'test cases cannot be deleted (%); deprecate instead', tc_key;
    END IF;
    IF NEW.id <> OLD.id OR NEW.project_id <> OLD.project_id OR NEW.number <> OLD.number THEN
        RAISE EXCEPTION 'test case identity is immutable (%)', tc_key;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- Projects keep their key and are never deleted (their test case keys depend on it).
-- +goose StatementBegin
CREATE FUNCTION projects_protect_identity() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'projects cannot be deleted (%)', OLD.key;
    END IF;
    IF NEW.id <> OLD.id OR NEW.key <> OLD.key THEN
        RAISE EXCEPTION 'the key of a project is immutable (%)', OLD.key;
    END IF;
    IF NEW.next_number < OLD.next_number THEN
        RAISE EXCEPTION 'test case numbers are never reused (%)', OLD.key;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER projects_protect_identity
    BEFORE UPDATE OR DELETE ON projects
    FOR EACH ROW EXECUTE FUNCTION projects_protect_identity();

-- +goose Down
DROP TRIGGER projects_protect_identity ON projects;
DROP FUNCTION projects_protect_identity();
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION test_cases_protect_identity() RETURNS trigger AS $$
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
DROP INDEX test_cases_project_expected_idx;
ALTER TABLE test_cases DROP COLUMN number, DROP COLUMN project_id;
DROP TABLE projects;
