// Package postgres is the PostgreSQL adapter of the identity module.
package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcrove/provenly/backend/internal/identity"
	"github.com/edcrove/provenly/backend/internal/identity/identitydb"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
)

// Store implements identity.Repository.
type Store struct {
	pool *pgxpool.Pool
	q    *identitydb.Queries
}

// NewStore builds a Store on a pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: identitydb.New(pool)}
}

// InTx implements identity.Repository.
func (s *Store) InTx(ctx context.Context, fn func(identity.Repository) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(&Store{pool: s.pool, q: s.q.WithTx(tx)})
	})
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	return err
}

func text(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func strPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

func toUser(r identitydb.User) identity.User {
	return identity.User{
		ID: r.ID, Username: r.Username, DisplayName: r.DisplayName, Email: strPtr(r.Email), PasswordHash: r.PasswordHash,
		IsAdmin: r.IsAdmin, CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time, DeactivatedAt: timePtr(r.DeactivatedAt),
	}
}

func toInvitation(r identitydb.Invitation) identity.Invitation {
	inv := identity.Invitation{
		ID: r.ID, TokenSHA256: r.TokenSha256, Email: strPtr(r.Email), Note: r.Note, CreatedBy: r.CreatedBy,
		CreatedAt: r.CreatedAt.Time, ExpiresAt: r.ExpiresAt.Time, AcceptedAt: timePtr(r.AcceptedAt), RevokedAt: timePtr(r.RevokedAt),
	}
	if r.AcceptedUserID.Valid {
		id := r.AcceptedUserID.Int64
		inv.AcceptedUserID = &id
	}
	if r.ProjectID.Valid {
		id := r.ProjectID.Int64
		inv.ProjectID = &id
		inv.ProjectRole = role(r.ProjectRole.String)
	}
	return inv
}

// role parses a role stored under the CHECK constraint (always valid).
func role(s string) authz.Role {
	r, _ := authz.ParseMemberRole(s)
	return r
}

func optInt8(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

func users(rows []identitydb.User) []identity.User {
	out := make([]identity.User, len(rows))
	for i, r := range rows {
		out[i] = toUser(r)
	}
	return out
}

// CountUsers implements identity.Repository.
func (s *Store) CountUsers(ctx context.Context) (int64, error) { return s.q.CountUsers(ctx) }

// CreateUser implements identity.Repository.
func (s *Store) CreateUser(ctx context.Context, u identity.NewUser) (identity.User, error) {
	r, err := s.q.CreateUser(ctx, identitydb.CreateUserParams{
		Username: u.Username, DisplayName: u.DisplayName, Email: text(u.Email), PasswordHash: u.PasswordHash, IsAdmin: u.IsAdmin,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.User{}, identity.ErrConflict
	}
	return toUser(r), err
}

// GetUser implements identity.Repository.
func (s *Store) GetUser(ctx context.Context, id int64) (identity.User, error) {
	r, err := s.q.GetUser(ctx, id)
	return toUser(r), notFound(err)
}

// GetUserByUsername implements identity.Repository.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (identity.User, error) {
	r, err := s.q.GetUserByUsername(ctx, username)
	return toUser(r), notFound(err)
}

// ListUsers implements identity.Repository.
func (s *Store) ListUsers(ctx context.Context, limit, offset int32) ([]identity.User, error) {
	rows, err := s.q.ListUsers(ctx, identitydb.ListUsersParams{PageLimit: limit, PageOffset: offset})
	return users(rows), err
}

// SetPasswordHash implements identity.Repository.
func (s *Store) SetPasswordHash(ctx context.Context, id int64, hash string) (identity.User, error) {
	r, err := s.q.SetPasswordHash(ctx, identitydb.SetPasswordHashParams{ID: id, PasswordHash: hash})
	return toUser(r), notFound(err)
}

// CreateInvitation implements identity.Repository.
func (s *Store) CreateInvitation(ctx context.Context, in identity.NewInvitation) (identity.Invitation, error) {
	r, err := s.q.CreateInvitation(ctx, identitydb.CreateInvitationParams{
		TokenSha256: in.TokenSHA256, Email: text(in.Email), Note: in.Note, CreatedBy: in.CreatedBy,
		ExpiresAt: pgtype.Timestamptz{Time: in.ExpiresAt, Valid: true},
		ProjectID: optInt8(in.ProjectID), ProjectRole: pgtype.Text{String: in.ProjectRole.String(), Valid: in.ProjectID != nil},
	})
	return toInvitation(r), err
}

// ListInvitations implements identity.Repository.
func (s *Store) ListInvitations(ctx context.Context, limit, offset int32) ([]identity.Invitation, error) {
	rows, err := s.q.ListInvitations(ctx, identitydb.ListInvitationsParams{PageLimit: limit, PageOffset: offset})
	out := make([]identity.Invitation, len(rows))
	for i, r := range rows {
		out[i] = toInvitation(r)
	}
	return out, err
}

// CountInvitations implements identity.Repository.
func (s *Store) CountInvitations(ctx context.Context) (int64, error) {
	return s.q.CountInvitations(ctx)
}

// GetInvitation implements identity.Repository.
func (s *Store) GetInvitation(ctx context.Context, id int64) (identity.Invitation, error) {
	r, err := s.q.GetInvitation(ctx, id)
	return toInvitation(r), notFound(err)
}

// LockInvitationByToken implements identity.Repository.
func (s *Store) LockInvitationByToken(ctx context.Context, digest []byte) (identity.Invitation, error) {
	r, err := s.q.LockInvitationByToken(ctx, digest)
	return toInvitation(r), notFound(err)
}

// MarkInvitationAccepted implements identity.Repository.
func (s *Store) MarkInvitationAccepted(ctx context.Context, id, userID int64) error {
	return s.q.MarkInvitationAccepted(ctx, identitydb.MarkInvitationAcceptedParams{ID: id, UserID: pgtype.Int8{Int64: userID, Valid: true}})
}

// RevokeInvitation implements identity.Repository.
func (s *Store) RevokeInvitation(ctx context.Context, id int64) (identity.Invitation, error) {
	r, err := s.q.RevokeInvitation(ctx, id)
	return toInvitation(r), notFound(err)
}

// MemberRole implements identity.Repository.
func (s *Store) MemberRole(ctx context.Context, projectID, userID int64) (authz.Role, error) {
	r, err := s.q.GetMemberRole(ctx, identitydb.GetMemberRoleParams{ProjectID: projectID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return authz.RoleNone, nil
	}
	return role(r), err
}

// ListUserMemberships implements identity.Repository.
func (s *Store) ListUserMemberships(ctx context.Context, userID int64) (map[int64]authz.Role, error) {
	rows, err := s.q.ListUserMemberships(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]authz.Role, len(rows))
	for _, r := range rows {
		out[r.ProjectID] = role(r.Role)
	}
	return out, nil
}

// ListProjectMembers implements identity.Repository.
func (s *Store) ListProjectMembers(ctx context.Context, projectID int64, limit, offset int32) ([]identity.Member, error) {
	rows, err := s.q.ListProjectMembers(ctx, identitydb.ListProjectMembersParams{ProjectID: projectID, PageLimit: limit, PageOffset: offset})
	out := make([]identity.Member, len(rows))
	for i, r := range rows {
		out[i] = identity.Member{User: toUser(r.User), Role: role(r.MemberRole), Since: r.MemberSince.Time}
	}
	return out, err
}

// CountProjectMembers implements identity.Repository.
func (s *Store) CountProjectMembers(ctx context.Context, projectID int64) (int64, error) {
	return s.q.CountProjectMembers(ctx, projectID)
}

// UpsertMember implements identity.Repository.
func (s *Store) UpsertMember(ctx context.Context, projectID, userID int64, r authz.Role) error {
	_, err := s.q.UpsertMember(ctx, identitydb.UpsertMemberParams{ProjectID: projectID, UserID: userID, Role: r.String()})
	return err
}

// DeleteMember implements identity.Repository.
func (s *Store) DeleteMember(ctx context.Context, projectID, userID int64) (bool, error) {
	n, err := s.q.DeleteMember(ctx, identitydb.DeleteMemberParams{ProjectID: projectID, UserID: userID})
	return n > 0, err
}

func toAPIKey(r identitydb.ApiKey) identity.APIKey {
	return identity.APIKey{
		ID: r.ID, ProjectID: r.ProjectID, Name: r.Name, Prefix: r.Prefix, TokenSHA256: r.TokenSha256, CreatedBy: r.CreatedBy,
		CreatedAt: r.CreatedAt.Time, LastUsedAt: timePtr(r.LastUsedAt), RevokedAt: timePtr(r.RevokedAt),
	}
}

// CreateAPIKey implements identity.Repository.
func (s *Store) CreateAPIKey(ctx context.Context, k identity.NewAPIKey) (identity.APIKey, error) {
	r, err := s.q.CreateAPIKey(ctx, identitydb.CreateAPIKeyParams{
		ProjectID: k.ProjectID, Name: k.Name, Prefix: k.Prefix, TokenSha256: k.TokenSHA256, CreatedBy: k.CreatedBy,
	})
	return toAPIKey(r), err
}

// ListAPIKeys implements identity.Repository.
func (s *Store) ListAPIKeys(ctx context.Context, projectID int64, limit, offset int32) ([]identity.APIKey, error) {
	rows, err := s.q.ListAPIKeys(ctx, identitydb.ListAPIKeysParams{ProjectID: projectID, PageLimit: limit, PageOffset: offset})
	out := make([]identity.APIKey, len(rows))
	for i, r := range rows {
		out[i] = toAPIKey(r)
	}
	return out, err
}

// CountAPIKeys implements identity.Repository.
func (s *Store) CountAPIKeys(ctx context.Context, projectID int64) (int64, error) {
	return s.q.CountAPIKeys(ctx, projectID)
}

// GetAPIKey implements identity.Repository.
func (s *Store) GetAPIKey(ctx context.Context, projectID, id int64) (identity.APIKey, error) {
	r, err := s.q.GetAPIKey(ctx, identitydb.GetAPIKeyParams{ID: id, ProjectID: projectID})
	return toAPIKey(r), notFound(err)
}

// GetAPIKeyByToken implements identity.Repository.
func (s *Store) GetAPIKeyByToken(ctx context.Context, digest []byte) (identity.APIKey, error) {
	r, err := s.q.GetAPIKeyByToken(ctx, digest)
	return toAPIKey(r), notFound(err)
}

// RevokeAPIKey implements identity.Repository.
func (s *Store) RevokeAPIKey(ctx context.Context, projectID, id int64) (identity.APIKey, error) {
	r, err := s.q.RevokeAPIKey(ctx, identitydb.RevokeAPIKeyParams{ID: id, ProjectID: projectID})
	return toAPIKey(r), notFound(err)
}

// TouchAPIKey implements identity.Repository.
func (s *Store) TouchAPIKey(ctx context.Context, id int64) error {
	return s.q.TouchAPIKey(ctx, id)
}

// SetUserDeactivated implements identity.Repository.
func (s *Store) SetUserDeactivated(ctx context.Context, id int64, at *time.Time) (identity.User, error) {
	r, err := s.q.SetUserDeactivated(ctx, identitydb.SetUserDeactivatedParams{ID: id, DeactivatedAt: tsArg(at)})
	if err != nil {
		return identity.User{}, notFound(err)
	}
	return toUser(r), nil
}

// CountActiveAdmins implements identity.Repository.
func (s *Store) CountActiveAdmins(ctx context.Context) (int64, error) {
	return s.q.CountActiveAdmins(ctx)
}

// VoidPasswordResets implements identity.Repository.
func (s *Store) VoidPasswordResets(ctx context.Context, userID int64) error {
	return s.q.VoidPasswordResets(ctx, userID)
}

func toPasswordReset(r identitydb.PasswordReset) identity.PasswordReset {
	out := identity.PasswordReset{ID: r.ID, UserID: r.UserID, TokenSHA256: r.TokenSha256, CreatedAt: r.CreatedAt.Time,
		ExpiresAt: r.ExpiresAt.Time, UsedAt: timePtr(r.UsedAt)}
	if r.CreatedBy.Valid {
		by := r.CreatedBy.Int64
		out.CreatedBy = &by
	}
	return out
}

// CreatePasswordReset implements identity.Repository.
func (s *Store) CreatePasswordReset(ctx context.Context, p identity.PasswordReset) (identity.PasswordReset, error) {
	params := identitydb.CreatePasswordResetParams{UserID: p.UserID, TokenSha256: p.TokenSHA256,
		ExpiresAt: pgtype.Timestamptz{Time: p.ExpiresAt, Valid: true}}
	if p.CreatedBy != nil {
		params.CreatedBy = pgtype.Int8{Int64: *p.CreatedBy, Valid: true}
	}
	r, err := s.q.CreatePasswordReset(ctx, params)
	if err != nil {
		return identity.PasswordReset{}, err
	}
	return toPasswordReset(r), nil
}

// LockPasswordResetByToken implements identity.Repository.
func (s *Store) LockPasswordResetByToken(ctx context.Context, digest []byte) (identity.PasswordReset, error) {
	r, err := s.q.LockPasswordResetByToken(ctx, digest)
	if err != nil {
		return identity.PasswordReset{}, notFound(err)
	}
	return toPasswordReset(r), nil
}

// MarkPasswordResetUsed implements identity.Repository.
func (s *Store) MarkPasswordResetUsed(ctx context.Context, id int64) error {
	return s.q.MarkPasswordResetUsed(ctx, id)
}

func tsArg(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func toToken(r identitydb.PersonalAccessToken, projectIDs []int64) identity.PersonalAccessToken {
	return identity.PersonalAccessToken{
		ID: r.ID, UserID: r.UserID, Name: r.Name, Prefix: r.Prefix, TokenSHA256: r.TokenSha256, ProjectIDs: projectIDs,
		CreatedAt: r.CreatedAt.Time, ExpiresAt: r.ExpiresAt.Time, LastUsedAt: timePtr(r.LastUsedAt), RevokedAt: timePtr(r.RevokedAt),
	}
}

// CreateToken implements identity.Repository.
func (s *Store) CreateToken(ctx context.Context, t identity.NewPersonalAccessToken) (identity.PersonalAccessToken, error) {
	r, err := s.q.CreatePersonalAccessToken(ctx, identitydb.CreatePersonalAccessTokenParams{
		UserID: t.UserID, Name: t.Name, Prefix: t.Prefix, TokenSha256: t.TokenSHA256, CreatedAt: tsArg(&t.CreatedAt), ExpiresAt: tsArg(&t.ExpiresAt),
	})
	if err != nil {
		return identity.PersonalAccessToken{}, err
	}
	err = s.q.AddPersonalAccessTokenProjects(ctx, identitydb.AddPersonalAccessTokenProjectsParams{TokenID: r.ID, ProjectIds: t.ProjectIDs})
	return toToken(r, t.ProjectIDs), err
}

// ListTokens implements identity.Repository.
func (s *Store) ListTokens(ctx context.Context, userID int64, limit, offset int32) ([]identity.PersonalAccessToken, error) {
	rows, err := s.q.ListPersonalAccessTokens(ctx, identitydb.ListPersonalAccessTokensParams{UserID: userID, PageLimit: limit, PageOffset: offset})
	out := make([]identity.PersonalAccessToken, len(rows))
	for i, r := range rows {
		out[i] = toToken(r.PersonalAccessToken, r.ProjectIds)
	}
	return out, err
}

// CountTokens implements identity.Repository.
func (s *Store) CountTokens(ctx context.Context, userID int64) (int64, error) {
	return s.q.CountPersonalAccessTokens(ctx, userID)
}

// GetToken implements identity.Repository.
func (s *Store) GetToken(ctx context.Context, userID, id int64) (identity.PersonalAccessToken, error) {
	r, err := s.q.GetPersonalAccessToken(ctx, identitydb.GetPersonalAccessTokenParams{ID: id, UserID: userID})
	return toToken(r.PersonalAccessToken, r.ProjectIds), notFound(err)
}

// GetTokenByDigest implements identity.Repository.
func (s *Store) GetTokenByDigest(ctx context.Context, digest []byte) (identity.PersonalAccessToken, error) {
	r, err := s.q.GetPersonalAccessTokenByToken(ctx, digest)
	return toToken(r.PersonalAccessToken, r.ProjectIds), notFound(err)
}

// RevokeToken implements identity.Repository.
func (s *Store) RevokeToken(ctx context.Context, userID, id int64) (identity.PersonalAccessToken, error) {
	n, err := s.q.RevokePersonalAccessToken(ctx, identitydb.RevokePersonalAccessTokenParams{ID: id, UserID: userID})
	switch {
	case err != nil:
		return identity.PersonalAccessToken{}, err
	case n == 0:
		return identity.PersonalAccessToken{}, identity.ErrNotFound
	}
	return s.GetToken(ctx, userID, id)
}

// RevokeUserTokens implements identity.Repository.
func (s *Store) RevokeUserTokens(ctx context.Context, userID int64) error {
	return s.q.RevokeUserPersonalAccessTokens(ctx, userID)
}

// TouchToken implements identity.Repository.
func (s *Store) TouchToken(ctx context.Context, id int64) error {
	return s.q.TouchPersonalAccessToken(ctx, id)
}
