-- Module: execution. A run links to its CI job (Ed, 2026-10-12): the developer's first click from a red run. CI sends
-- the job's URL with the report (?ciUrl=) or when it starts a live run; the reporter fills it in on GitHub Actions.
-- Empty when CI did not send one. Only http(s) URLs: the UI renders it as a link.

-- +goose Up
ALTER TABLE test_runs
    ADD COLUMN ci_url TEXT NOT NULL DEFAULT '',
    ADD CONSTRAINT test_runs_ci_url_http CHECK (ci_url = '' OR (length(ci_url) <= 2000 AND ci_url ~ '^https?://'));

-- +goose Down
ALTER TABLE test_runs
    DROP CONSTRAINT test_runs_ci_url_http,
    DROP COLUMN ci_url;
