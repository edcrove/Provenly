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
INSERT INTO invitations (token_sha256, email, note, created_by, expires_at, project_id, project_role)
VALUES (@token_sha256, sqlc.narg('email'), @note, @created_by, @expires_at, sqlc.narg('project_id'), sqlc.narg('project_role'))
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

-- name: GetMemberRole :one
SELECT role FROM project_members WHERE project_id = @project_id AND user_id = @user_id;

-- name: ListUserMemberships :many
SELECT project_id, role FROM project_members WHERE user_id = @user_id;

-- name: ListProjectMembers :many
SELECT sqlc.embed(users), m.role AS member_role, m.created_at AS member_since
FROM project_members m JOIN users ON users.id = m.user_id
WHERE m.project_id = @project_id
ORDER BY users.username
LIMIT @page_limit OFFSET @page_offset;

-- name: CountProjectMembers :one
SELECT count(*) FROM project_members WHERE project_id = @project_id;

-- name: UpsertMember :one
INSERT INTO project_members (project_id, user_id, role) VALUES (@project_id, @user_id, @role)
ON CONFLICT (project_id, user_id) DO UPDATE SET role = EXCLUDED.role, updated_at = now()
RETURNING *;

-- name: DeleteMember :execrows
DELETE FROM project_members WHERE project_id = @project_id AND user_id = @user_id;
