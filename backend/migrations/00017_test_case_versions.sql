-- Module: catalog. Optimistic locking (MVP D7): every change to a test case or
-- to its steps advances the test case's version, which reads expose as an ETag
-- and writes check through If-Match. The database advances it, so no write path
-- can forget to.

-- +goose Up
ALTER TABLE test_cases ADD COLUMN version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1);

-- +goose StatementBegin
CREATE FUNCTION test_cases_advance_version() RETURNS trigger AS $$
BEGIN
    IF NEW.version < OLD.version THEN
        RAISE EXCEPTION 'a test case version never goes back (%)', OLD.id;
    END IF;
    IF NEW.version = OLD.version AND (NEW.title, NEW.description, NEW.expected_result, NEW.automated, NEW.status)
        IS DISTINCT FROM (OLD.title, OLD.description, OLD.expected_result, OLD.automated, OLD.status) THEN
        NEW.version := OLD.version + 1;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER test_cases_advance_version BEFORE UPDATE ON test_cases
    FOR EACH ROW EXECUTE FUNCTION test_cases_advance_version();

-- +goose StatementBegin
CREATE FUNCTION test_steps_advance_test_case() RETURNS trigger AS $$
BEGIN
    UPDATE test_cases SET version = version + 1 WHERE id = coalesce(NEW.test_case_id, OLD.test_case_id);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER test_steps_advance_test_case AFTER INSERT OR UPDATE OR DELETE ON test_steps
    FOR EACH ROW EXECUTE FUNCTION test_steps_advance_test_case();

-- +goose Down
DROP TRIGGER test_steps_advance_test_case ON test_steps;
DROP FUNCTION test_steps_advance_test_case();
DROP TRIGGER test_cases_advance_version ON test_cases;
DROP FUNCTION test_cases_advance_version();
ALTER TABLE test_cases DROP COLUMN version;
