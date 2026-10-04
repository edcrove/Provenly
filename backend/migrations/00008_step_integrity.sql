-- Module: catalog. The database backs the step rules the service already enforces:
-- an action is never blank, expectedResult is at most 2000 characters (as action),
-- and a step never moves to another test case. Also fixes the identity trigger
-- messages, which printed "TC-1s" (RAISE takes a bare % placeholder).

-- +goose Up
ALTER TABLE test_steps
    ADD CONSTRAINT test_steps_action_not_blank CHECK (action ~ '[^[:space:]]'),
    ADD CONSTRAINT test_steps_expected_result_length CHECK (length(expected_result) <= 2000);

-- +goose StatementBegin
CREATE FUNCTION test_steps_protect_owner() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'a test step cannot move to another test case (step %, TC-%)', OLD.id, OLD.test_case_id;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER test_steps_protect_owner
    BEFORE UPDATE OF test_case_id ON test_steps
    FOR EACH ROW WHEN (NEW.test_case_id IS DISTINCT FROM OLD.test_case_id)
    EXECUTE FUNCTION test_steps_protect_owner();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION test_cases_protect_identity() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'test cases cannot be deleted (TC-%); deprecate instead', OLD.id;
    END IF;
    IF NEW.id <> OLD.id THEN
        RAISE EXCEPTION 'test case id is immutable (TC-%)', OLD.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION test_cases_protect_identity() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'test cases cannot be deleted (TC-%s); deprecate instead', OLD.id;
    END IF;
    IF NEW.id <> OLD.id THEN
        RAISE EXCEPTION 'test case id is immutable (TC-%s)', OLD.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
DROP TRIGGER test_steps_protect_owner ON test_steps;
DROP FUNCTION test_steps_protect_owner();
ALTER TABLE test_steps
    DROP CONSTRAINT test_steps_expected_result_length,
    DROP CONSTRAINT test_steps_action_not_blank;
