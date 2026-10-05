-- Module: catalog. Test suites (Planning #27, MVP D2): a named selection of a
-- project's test cases, either static (an explicit list) or a query over tags
-- and classification (a "smart" suite). A run reported for a suite takes the
-- suite's active automated test cases as its expected universe. Suites are
-- archived, never deleted, and their keys never change, so runs keep naming them.

-- +goose Up
CREATE TABLE test_suites (
    id             BIGSERIAL   PRIMARY KEY,
    project_id     BIGINT      NOT NULL REFERENCES projects (id),
    key            TEXT        NOT NULL CHECK (key ~ '^[a-z][a-z0-9-]{0,29}$'),
    name           TEXT        NOT NULL CHECK (name <> '' AND char_length(name) <= 100),
    description    TEXT        NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    kind           TEXT        NOT NULL CHECK (kind IN ('static', 'query')),
    -- The query of a query suite: a tag and dimension:value pairs, all must hold (static suites have none).
    query_tag      TEXT        CHECK (query_tag ~ '^[a-z0-9][a-z0-9._-]{0,39}$'),
    query_classified TEXT[]    NOT NULL DEFAULT '{}',
    archived_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT test_suites_key_unique UNIQUE (project_id, key),
    CONSTRAINT test_suites_project_unique UNIQUE (id, project_id),
    CONSTRAINT test_suites_query_only_on_query CHECK (
        kind = 'query' OR (query_tag IS NULL AND cardinality(query_classified) = 0))
);

CREATE TABLE test_suite_cases (
    suite_id     BIGINT NOT NULL,
    project_id   BIGINT NOT NULL,
    test_case_id BIGINT NOT NULL,
    PRIMARY KEY (suite_id, test_case_id),
    FOREIGN KEY (suite_id, project_id) REFERENCES test_suites (id, project_id),
    FOREIGN KEY (test_case_id, project_id) REFERENCES test_cases (id, project_id)
);
CREATE INDEX test_suite_cases_test_case ON test_suite_cases (test_case_id);

-- +goose StatementBegin
CREATE FUNCTION test_suites_protect() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'test suites are archived, never deleted (%)', OLD.key;
    END IF;
    IF (NEW.key, NEW.project_id, NEW.kind) IS DISTINCT FROM (OLD.key, OLD.project_id, OLD.kind) THEN
        RAISE EXCEPTION 'a test suite never changes key, project or kind (%)', OLD.key;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER test_suites_protect BEFORE UPDATE OR DELETE ON test_suites
    FOR EACH ROW EXECUTE FUNCTION test_suites_protect();

-- +goose StatementBegin
CREATE FUNCTION test_suite_cases_static_only() RETURNS trigger AS $$
BEGIN
    IF (SELECT kind FROM test_suites WHERE id = NEW.suite_id) <> 'static' THEN
        RAISE EXCEPTION 'only static suites list their test cases (suite %)', NEW.suite_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER test_suite_cases_static_only BEFORE INSERT ON test_suite_cases
    FOR EACH ROW EXECUTE FUNCTION test_suite_cases_static_only();

-- +goose Down
DROP TABLE test_suite_cases;
DROP TABLE test_suites;
DROP FUNCTION test_suite_cases_static_only();
DROP FUNCTION test_suites_protect();
