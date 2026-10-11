-- Module: catalog. Archived projects (Ed, 2026-10-09): an administrator archives a project nobody works on any more
-- (the projects E2E runs leave on a deployed instance, a finished product). It leaves the lists and the project
-- switcher and becomes read-only; nothing is deleted and unarchiving brings it back. The default project TC, where
-- reports without ?project= go, is never archived.

-- +goose Up
ALTER TABLE projects
    ADD COLUMN archived_at TIMESTAMPTZ,
    ADD CONSTRAINT projects_archived_after_creation CHECK (archived_at IS NULL OR archived_at >= created_at),
    ADD CONSTRAINT projects_default_never_archived CHECK (key <> 'TC' OR archived_at IS NULL);

-- +goose Down
ALTER TABLE projects
    DROP CONSTRAINT projects_default_never_archived,
    DROP CONSTRAINT projects_archived_after_creation,
    DROP COLUMN archived_at;
