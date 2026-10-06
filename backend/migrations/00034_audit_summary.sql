-- Module: audit. Readable entries (card #48): what a change did in words ("edited CHK-4 step 3") and the test case
-- it touched, so the log reads and filters by test case. Events recorded before keep both empty: the log is
-- append-only, so they are not backfilled.

-- +goose Up
ALTER TABLE audit_events
    ADD COLUMN summary       TEXT CHECK (char_length(summary) <= 300),
    ADD COLUMN test_case_key TEXT CHECK (char_length(test_case_key) <= 40);
CREATE INDEX audit_events_test_case ON audit_events (test_case_key, id DESC) WHERE test_case_key IS NOT NULL;

-- +goose Down
DROP INDEX audit_events_test_case;
ALTER TABLE audit_events DROP COLUMN test_case_key, DROP COLUMN summary;
