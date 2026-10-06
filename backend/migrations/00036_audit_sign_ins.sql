-- Module: audit. Sign-in events (card #49): sign-ins that succeed, fail (401) or are locked out (429), accepted
-- invitations, password resets and sign-outs are recorded too, so the status may be any HTTP answer; every event keeps
-- the client IP (X-Forwarded-For only through PROVENLY_TRUSTED_PROXIES) and user agent. Never a password or token.

-- +goose Up
ALTER TABLE audit_events DROP CONSTRAINT audit_events_status_check;
ALTER TABLE audit_events
    ADD CONSTRAINT audit_events_status_check CHECK (status BETWEEN 200 AND 599),
    ADD COLUMN ip         TEXT CHECK (char_length(ip) <= 45),
    ADD COLUMN user_agent TEXT CHECK (char_length(user_agent) <= 500);

-- +goose Down
ALTER TABLE audit_events DROP COLUMN user_agent, DROP COLUMN ip, DROP CONSTRAINT audit_events_status_check;
ALTER TABLE audit_events ADD CONSTRAINT audit_events_status_check CHECK (status BETWEEN 200 AND 299);
