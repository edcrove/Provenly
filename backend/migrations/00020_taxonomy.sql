-- Module: catalog. Test taxonomy (Planning #26): each project classifies its
-- test cases along orthogonal dimensions with controlled values, plus free tags.
-- Dimensions and values are never deleted, only archived, so the values used
-- for selection and reporting stay stable. A test case has at most one value
-- per dimension, always a value of that dimension and of its own project. Tags
-- and classification are test case content: changing them advances its version.

-- +goose Up
CREATE TABLE classification_dimensions (
    id          BIGSERIAL   PRIMARY KEY,
    project_id  BIGINT      NOT NULL REFERENCES projects (id),
    key         TEXT        NOT NULL CHECK (key ~ '^[a-z][a-z0-9-]{0,29}$'),
    name        TEXT        NOT NULL CHECK (name <> '' AND char_length(name) <= 60),
    built_in    BOOLEAN     NOT NULL DEFAULT false,
    archived_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT classification_dimensions_key_unique UNIQUE (project_id, key),
    CONSTRAINT classification_dimensions_project_unique UNIQUE (id, project_id)
);

CREATE TABLE classification_values (
    id           BIGSERIAL   PRIMARY KEY,
    dimension_id BIGINT      NOT NULL REFERENCES classification_dimensions (id),
    key          TEXT        NOT NULL CHECK (key ~ '^[a-z0-9][a-z0-9-]{0,29}$'),
    name         TEXT        NOT NULL CHECK (name <> '' AND char_length(name) <= 60),
    position     INT         NOT NULL,
    archived_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT classification_values_key_unique UNIQUE (dimension_id, key),
    CONSTRAINT classification_values_dimension_unique UNIQUE (id, dimension_id)
);

ALTER TABLE test_cases ADD CONSTRAINT test_cases_project_unique UNIQUE (id, project_id);

CREATE TABLE test_case_classifications (
    test_case_id BIGINT NOT NULL,
    project_id   BIGINT NOT NULL,
    dimension_id BIGINT NOT NULL,
    value_id     BIGINT NOT NULL,
    PRIMARY KEY (test_case_id, dimension_id),
    FOREIGN KEY (test_case_id, project_id) REFERENCES test_cases (id, project_id),
    FOREIGN KEY (dimension_id, project_id) REFERENCES classification_dimensions (id, project_id),
    FOREIGN KEY (value_id, dimension_id) REFERENCES classification_values (id, dimension_id)
);
CREATE INDEX test_case_classifications_value ON test_case_classifications (value_id);

CREATE TABLE test_case_tags (
    test_case_id BIGINT NOT NULL REFERENCES test_cases (id),
    tag          TEXT   NOT NULL CHECK (tag ~ '^[a-z0-9][a-z0-9._-]{0,39}$'),
    PRIMARY KEY (test_case_id, tag)
);
CREATE INDEX test_case_tags_tag ON test_case_tags (tag);

-- +goose StatementBegin
CREATE FUNCTION classification_protect() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'classification dimensions and values are archived, never deleted (%)', OLD.key;
    END IF;
    IF NEW.key <> OLD.key THEN
        RAISE EXCEPTION 'a classification key never changes (%)', OLD.key;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER classification_dimensions_protect BEFORE UPDATE OR DELETE ON classification_dimensions
    FOR EACH ROW EXECUTE FUNCTION classification_protect();
CREATE TRIGGER classification_values_protect BEFORE UPDATE OR DELETE ON classification_values
    FOR EACH ROW EXECUTE FUNCTION classification_protect();

-- +goose StatementBegin
CREATE FUNCTION classification_dimensions_identity() RETURNS trigger AS $$
BEGIN
    IF NEW.project_id <> OLD.project_id OR NEW.built_in <> OLD.built_in THEN
        RAISE EXCEPTION 'a dimension never changes project or kind (%)', OLD.key;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER classification_dimensions_identity BEFORE UPDATE ON classification_dimensions
    FOR EACH ROW EXECUTE FUNCTION classification_dimensions_identity();

-- Tags and classification are content of the test case: the same trigger as steps advances its version.
CREATE TRIGGER test_case_classifications_advance_test_case AFTER INSERT OR UPDATE OR DELETE ON test_case_classifications
    FOR EACH ROW EXECUTE FUNCTION test_steps_advance_test_case();
CREATE TRIGGER test_case_tags_advance_test_case AFTER INSERT OR UPDATE OR DELETE ON test_case_tags
    FOR EACH ROW EXECUTE FUNCTION test_steps_advance_test_case();

-- Every project starts with the built-in dimensions (Planning #26). Execution mode is not one: it is the
-- test case's automated flag. Feature, component and platform start empty: their values are the project's own.
-- +goose StatementBegin
CREATE FUNCTION seed_builtin_dimensions(p_project_id BIGINT) RETURNS void AS $$
DECLARE
    d RECORD;
    dim_id BIGINT;
    v TEXT[];
    i INT;
BEGIN
    FOR d IN SELECT * FROM (VALUES
        (1, 'feature', 'Feature', ARRAY[]::TEXT[]),
        (2, 'component', 'Component', ARRAY[]::TEXT[]),
        (3, 'level', 'Test level', ARRAY['unit:Unit', 'integration:Integration', 'system:System', 'e2e:End to end']),
        (4, 'depth', 'Depth', ARRAY['smoke:Smoke', 'sanity:Sanity', 'regression:Regression']),
        (5, 'type', 'Test type', ARRAY['functional:Functional', 'performance:Performance', 'security:Security', 'accessibility:Accessibility', 'usability:Usability']),
        (6, 'risk', 'Risk', ARRAY['critical:Critical', 'high:High', 'medium:Medium', 'low:Low']),
        (7, 'platform', 'Platform', ARRAY[]::TEXT[])
    ) AS t (ord, key, name, vals) ORDER BY ord LOOP
        INSERT INTO classification_dimensions (project_id, key, name, built_in)
        VALUES (p_project_id, d.key, d.name, true) RETURNING id INTO dim_id;
        v := d.vals;
        FOR i IN 1 .. coalesce(array_length(v, 1), 0) LOOP
            INSERT INTO classification_values (dimension_id, key, name, position)
            VALUES (dim_id, split_part(v[i], ':', 1), split_part(v[i], ':', 2), i);
        END LOOP;
    END LOOP;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION projects_seed_builtin_dimensions() RETURNS trigger AS $$
BEGIN
    PERFORM seed_builtin_dimensions(NEW.id);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER projects_seed_builtin_dimensions AFTER INSERT ON projects
    FOR EACH ROW EXECUTE FUNCTION projects_seed_builtin_dimensions();

-- Existing projects get them too.
SELECT seed_builtin_dimensions(id) FROM projects ORDER BY id;

-- +goose Down
DROP TRIGGER projects_seed_builtin_dimensions ON projects;
DROP FUNCTION projects_seed_builtin_dimensions();
DROP FUNCTION seed_builtin_dimensions(BIGINT);
DROP TRIGGER classification_values_protect ON classification_values;
DROP TRIGGER classification_dimensions_protect ON classification_dimensions;
DROP TABLE test_case_tags;
DROP TABLE test_case_classifications;
ALTER TABLE test_cases DROP CONSTRAINT test_cases_project_unique;
DROP TABLE classification_values;
DROP TABLE classification_dimensions;
DROP FUNCTION classification_dimensions_identity();
DROP FUNCTION classification_protect();
