-- Module: catalog. Requirements (Notion 04 Requirements Hub, Incubator "Requirements Federation"): a project's
-- requirements are native (written in Provenly) or mirrored from an external tool (Jira, GitHub, Azure DevOps) by
-- provider and external id, without migrating them. Test cases cover requirements (many to many); coverage is read
-- from each test case's latest result. Requirements are archived, never deleted; provider and external id never change.

-- +goose Up
-- Native requirements are numbered R-<n> per project from a counter, like test cases: concurrent creations never
-- compute the same number.
ALTER TABLE projects ADD COLUMN next_requirement_number BIGINT NOT NULL DEFAULT 1 CHECK (next_requirement_number >= 1);

CREATE TABLE requirements (
    id              BIGSERIAL   PRIMARY KEY,
    project_id      BIGINT      NOT NULL REFERENCES projects (id),
    provider        TEXT        NOT NULL CHECK (provider IN ('provenly', 'jira', 'github', 'azure_devops')),
    external_id     TEXT        NOT NULL CHECK (external_id ~ '^[A-Za-z0-9][A-Za-z0-9._#/-]{0,99}$'),
    title           TEXT        NOT NULL CHECK (title <> '' AND char_length(title) <= 300),
    description     TEXT        NOT NULL DEFAULT '' CHECK (char_length(description) <= 10000),
    url             TEXT        NOT NULL DEFAULT '' CHECK (url = '' OR url ~ '^https?://'),
    provider_status TEXT        NOT NULL DEFAULT '' CHECK (char_length(provider_status) <= 50),
    archived_at     TIMESTAMPTZ,
    last_synced_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT requirements_external_unique UNIQUE (project_id, provider, external_id),
    CONSTRAINT requirements_project_unique UNIQUE (id, project_id),
    CONSTRAINT requirements_synced_only_external CHECK (provider <> 'provenly' OR last_synced_at IS NULL)
);

CREATE TABLE requirement_test_cases (
    requirement_id BIGINT NOT NULL,
    project_id     BIGINT NOT NULL,
    test_case_id   BIGINT NOT NULL,
    PRIMARY KEY (requirement_id, test_case_id),
    FOREIGN KEY (requirement_id, project_id) REFERENCES requirements (id, project_id),
    FOREIGN KEY (test_case_id, project_id) REFERENCES test_cases (id, project_id)
);
CREATE INDEX requirement_test_cases_test_case ON requirement_test_cases (test_case_id);

-- +goose StatementBegin
CREATE FUNCTION requirements_protect() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'requirements are archived, never deleted (%)', OLD.external_id;
    END IF;
    IF (NEW.project_id, NEW.provider, NEW.external_id) IS DISTINCT FROM (OLD.project_id, OLD.provider, OLD.external_id) THEN
        RAISE EXCEPTION 'a requirement never changes project, provider or external id (%)', OLD.external_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER requirements_protect BEFORE UPDATE OR DELETE ON requirements
    FOR EACH ROW EXECUTE FUNCTION requirements_protect();

-- +goose Down
DROP TABLE requirement_test_cases;
DROP TABLE requirements;
DROP FUNCTION requirements_protect();
ALTER TABLE projects DROP COLUMN next_requirement_number;
