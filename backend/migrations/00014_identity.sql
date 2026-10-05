-- Module: identity. Local accounts (MVP D13): users log in with a username and a
-- password; new users join through a single-use invitation link (email optional).
-- Users are never deleted, so audit and history can always name them.

-- +goose Up
CREATE TABLE users (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    username      TEXT        NOT NULL UNIQUE CHECK (username ~ '^[a-z0-9][a-z0-9._-]{2,31}$'),
    display_name  TEXT        NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 100),
    email         TEXT        CHECK (email IS NULL OR length(email) BETWEEN 3 AND 254),
    password_hash TEXT        NOT NULL CHECK (password_hash LIKE '$2%'),
    is_admin      BOOLEAN     NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Only a SHA-256 digest of the invitation token is stored: the link is shown once.
CREATE TABLE invitations (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    token_sha256     BYTEA       NOT NULL UNIQUE CHECK (length(token_sha256) = 32),
    email            TEXT        CHECK (email IS NULL OR length(email) BETWEEN 3 AND 254),
    note             TEXT        NOT NULL DEFAULT '' CHECK (length(note) <= 200),
    created_by       BIGINT      NOT NULL REFERENCES users (id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at       TIMESTAMPTZ NOT NULL,
    accepted_at      TIMESTAMPTZ,
    accepted_user_id BIGINT      REFERENCES users (id),
    revoked_at       TIMESTAMPTZ,
    CONSTRAINT invitations_expires_after_created CHECK (expires_at > created_at),
    CONSTRAINT invitations_accepted_consistent CHECK ((accepted_at IS NULL) = (accepted_user_id IS NULL)),
    CONSTRAINT invitations_single_outcome CHECK (accepted_at IS NULL OR revoked_at IS NULL)
);

-- +goose StatementBegin
CREATE FUNCTION users_protect_identity() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'users cannot be deleted (%)', OLD.username;
    END IF;
    IF NEW.id <> OLD.id OR NEW.username <> OLD.username THEN
        RAISE EXCEPTION 'user identity is immutable (%)', OLD.username;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER users_protect_identity BEFORE UPDATE OR DELETE ON users
    FOR EACH ROW EXECUTE FUNCTION users_protect_identity();

-- +goose StatementBegin
CREATE FUNCTION invitations_protect() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'invitations cannot be deleted; revoke instead';
    END IF;
    IF NEW.token_sha256 <> OLD.token_sha256 OR NEW.created_by <> OLD.created_by OR NEW.expires_at <> OLD.expires_at
        OR (OLD.accepted_at IS NOT NULL AND NEW.accepted_at IS DISTINCT FROM OLD.accepted_at)
        OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at) THEN
        RAISE EXCEPTION 'an invitation is used or revoked once and never changes otherwise';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER invitations_protect BEFORE UPDATE OR DELETE ON invitations
    FOR EACH ROW EXECUTE FUNCTION invitations_protect();

-- +goose Down
DROP TABLE invitations;
DROP TABLE users;
DROP FUNCTION invitations_protect();
DROP FUNCTION users_protect_identity();
