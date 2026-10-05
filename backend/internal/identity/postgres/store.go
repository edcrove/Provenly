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
		IsAdmin: r.IsAdmin, CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time,
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
	return inv
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
