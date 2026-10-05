-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: CreateUser :one
-- No row when the username is taken.
INSERT INTO users (username, display_name, email, password_hash, is_admin)
VALUES (@username, @display_name, sqlc.narg('email'), @password_hash, @is_admin)
ON CONFLICT (username) DO NOTHING
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = @id;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = @username;

-- name: ListUsers :many
SELECT * FROM users ORDER BY username LIMIT @page_limit OFFSET @page_offset;

-- name: SetPasswordHash :one
UPDATE users SET password_hash = @password_hash, updated_at = now() WHERE id = @id RETURNING *;

-- name: CreateInvitation :one
INSERT INTO invitations (token_sha256, email, note, created_by, expires_at)
VALUES (@token_sha256, sqlc.narg('email'), @note, @created_by, @expires_at)
RETURNING *;

-- name: ListInvitations :many
SELECT * FROM invitations ORDER BY id DESC LIMIT @page_limit OFFSET @page_offset;

-- name: CountInvitations :one
SELECT count(*) FROM invitations;

-- name: LockInvitationByToken :one
-- Locks the invitation so two acceptances of one link cannot both create a user.
SELECT * FROM invitations WHERE token_sha256 = @token_sha256 FOR UPDATE;

-- name: MarkInvitationAccepted :exec
UPDATE invitations SET accepted_at = now(), accepted_user_id = @user_id WHERE id = @id;

-- name: RevokeInvitation :one
-- No row when the invitation does not exist or is already accepted or revoked.
UPDATE invitations SET revoked_at = now()
WHERE id = @id AND accepted_at IS NULL AND revoked_at IS NULL
RETURNING *;

-- name: GetInvitation :one
SELECT * FROM invitations WHERE id = @id;
