-- Module: execution. Parse errors carry a severity: "error" (data lost or
-- unknown) or "warning" (stored but suspicious, e.g. a 0 ms pass/fail).

-- +goose Up
ALTER TABLE test_run_parse_errors
    ADD COLUMN severity TEXT NOT NULL DEFAULT 'error' CHECK (severity IN ('error', 'warning'));

-- +goose Down
ALTER TABLE test_run_parse_errors DROP COLUMN severity;
