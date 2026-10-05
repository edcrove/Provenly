-- Module: identity. Roles per project (MVP D12): administrators see and do
-- everything; everyone else works in the projects they are members of, as
-- maintainer (manages the project and its members), member (edits test cases)
-- or viewer (read only). An invitation can grant one project role on accept.

-- +goose Up
CREATE TABLE project_members (
    project_id BIGINT      NOT NULL REFERENCES projects (id),
    user_id    BIGINT      NOT NULL REFERENCES users (id),
    role       TEXT        NOT NULL CHECK (role IN ('maintainer', 'member', 'viewer')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);
CREATE INDEX project_members_user_idx ON project_members (user_id);

ALTER TABLE invitations
    ADD COLUMN project_id BIGINT REFERENCES projects (id),
    ADD COLUMN project_role TEXT CHECK (project_role IN ('maintainer', 'member', 'viewer')),
    ADD CONSTRAINT invitations_grant_complete CHECK ((project_id IS NULL) = (project_role IS NULL));

-- +goose Down
ALTER TABLE invitations DROP CONSTRAINT invitations_grant_complete, DROP COLUMN project_role, DROP COLUMN project_id;
DROP TABLE project_members;
