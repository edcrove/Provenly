-- Module: execution. SHA-256 of the JUnit report that created the run, to warn
-- when a replay of the same attempt carries a different report.

-- +goose Up
ALTER TABLE test_runs ADD COLUMN report_sha256 TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE test_runs DROP COLUMN report_sha256;
