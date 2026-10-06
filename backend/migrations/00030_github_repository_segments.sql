-- +goose Up
-- Security audit 2026-10-06: a GitHub repository is owner/name with no "." or ".." segment, so the path the
-- connector calls GitHub with cannot climb out of /repos/{owner}/{name} with a stored token.
ALTER TABLE github_connections
    ADD CONSTRAINT github_connections_repository_segments CHECK (repository !~ '(^|/)\.+(/|$)');

-- +goose Down
ALTER TABLE github_connections DROP CONSTRAINT github_connections_repository_segments;
