-- Module: catalog. Issues (Notion 05 Issue Verification State Machine, DEC-8): defects tracked natively or mirrored
-- from an external tracker (Jira, GitHub, Azure DevOps) by provider and external id. Test cases that reproduce an issue
-- are linked to it; QA verification is derived on read from the issue's state and each linked test case's latest
-- conclusive result, never stored. Issues are never deleted; provider and external id never change.

-- +goose Up
ALTER TABLE projects ADD COLUMN next_issue_number BIGINT NOT NULL DEFAULT 1 CHECK (next_issue_number >= 1);

CREATE TABLE issues (
    id              BIGSERIAL   PRIMARY KEY,
    project_id      BIGINT      NOT NULL REFERENCES projects (id),
    provider        TEXT        NOT NULL CHECK (provider IN ('provenly', 'jira', 'github', 'azure_devops')),
    external_id     TEXT        NOT NULL CHECK (external_id ~ '^[A-Za-z0-9][A-Za-z0-9._#/-]{0,99}$'),
    title           TEXT        NOT NULL CHECK (title <> '' AND char_length(title) <= 300),
    description     TEXT        NOT NULL DEFAULT '' CHECK (char_length(description) <= 10000),
    url             TEXT        NOT NULL DEFAULT '' CHECK (url = '' OR url ~ '^https?://'),
    state           TEXT        NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'closed')),
    provider_status TEXT        NOT NULL DEFAULT '' CHECK (char_length(provider_status) <= 50),
    closed_at       TIMESTAMPTZ,
    last_synced_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT issues_external_unique UNIQUE (project_id, provider, external_id),
    CONSTRAINT issues_project_unique UNIQUE (id, project_id),
    CONSTRAINT issues_closed_at CHECK ((state = 'closed') = (closed_at IS NOT NULL)),
    CONSTRAINT issues_synced_only_external CHECK (provider <> 'provenly' OR last_synced_at IS NULL)
);

CREATE TABLE issue_test_cases (
    issue_id     BIGINT NOT NULL,
    project_id   BIGINT NOT NULL,
    test_case_id BIGINT NOT NULL,
    PRIMARY KEY (issue_id, test_case_id),
    FOREIGN KEY (issue_id, project_id) REFERENCES issues (id, project_id),
    FOREIGN KEY (test_case_id, project_id) REFERENCES test_cases (id, project_id)
);
CREATE INDEX issue_test_cases_test_case ON issue_test_cases (test_case_id);

-- +goose StatementBegin
CREATE FUNCTION issues_protect() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'issues are closed, never deleted (%)', OLD.external_id;
    END IF;
    IF (NEW.project_id, NEW.provider, NEW.external_id) IS DISTINCT FROM (OLD.project_id, OLD.provider, OLD.external_id) THEN
        RAISE EXCEPTION 'an issue never changes project, provider or external id (%)', OLD.external_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER issues_protect BEFORE UPDATE OR DELETE ON issues
    FOR EACH ROW EXECUTE FUNCTION issues_protect();

-- +goose Down
DROP TABLE issue_test_cases;
DROP TABLE issues;
DROP FUNCTION issues_protect();
ALTER TABLE projects DROP COLUMN next_issue_number;
