// Package identity is the identity module: local user accounts, sign-in with a
// signed session token (JWT) and single-use invitation links (MVP D13).
package identity

import (
	"context"
	"errors"
	"time"

	"github.com/edcrove/provenly/backend/internal/platform/authz"
)

// User is a local account. Users are never deleted.
type User struct {
	ID           int64
	Username     string
	DisplayName  string
	Email        *string
	PasswordHash string
	IsAdmin      bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// InvitationStatus is derived from an invitation's timestamps.
type InvitationStatus string

// Invitation statuses.
const (
	InvitationPending  InvitationStatus = "pending"
	InvitationAccepted InvitationStatus = "accepted"
	InvitationRevoked  InvitationStatus = "revoked"
	InvitationExpired  InvitationStatus = "expired"
)

// Invitation lets one person create an account. Only a digest of its token is stored.
type Invitation struct {
	ID             int64
	TokenSHA256    []byte
	Email          *string
	Note           string
	CreatedBy      int64
	CreatedAt      time.Time
	ExpiresAt      time.Time
	AcceptedAt     *time.Time
	AcceptedUserID *int64
	RevokedAt      *time.Time
	// ProjectID and ProjectRole: the project role the account gets on accept, if any.
	ProjectID   *int64
	ProjectRole authz.Role
}

// Member is a user's role in a project.
type Member struct {
	User  User
	Role  authz.Role
	Since time.Time
}

// APIKey lets CI report runs into one project. Only a digest of the key is stored.
type APIKey struct {
	ID          int64
	ProjectID   int64
	Name        string
	Prefix      string
	TokenSHA256 []byte
	CreatedBy   int64
	CreatedAt   time.Time
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
}

// NewAPIKey is the content of an API key to create.
type NewAPIKey struct {
	ProjectID   int64
	Name        string
	Prefix      string
	TokenSHA256 []byte
	CreatedBy   int64
}

// Status of the invitation at time now.
func (i Invitation) Status(now time.Time) InvitationStatus {
	switch {
	case i.AcceptedAt != nil:
		return InvitationAccepted
	case i.RevokedAt != nil:
		return InvitationRevoked
	case !now.Before(i.ExpiresAt):
		return InvitationExpired
	default:
		return InvitationPending
	}
}

// NewUser is the content of a user to create (the password is already hashed).
type NewUser struct {
	Username     string
	DisplayName  string
	Email        *string
	PasswordHash string
	IsAdmin      bool
}

// NewInvitation is the content of an invitation to create.
type NewInvitation struct {
	TokenSHA256 []byte
	Email       *string
	Note        string
	CreatedBy   int64
	ExpiresAt   time.Time
	ProjectID   *int64
	ProjectRole authz.Role
}

// Errors returned by repositories.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

// Repository is the persistence port of the identity module.
type Repository interface {
	InTx(ctx context.Context, fn func(Repository) error) error
	CountUsers(ctx context.Context) (int64, error)
	// CreateUser returns ErrConflict when the username is taken.
	CreateUser(ctx context.Context, u NewUser) (User, error)
	GetUser(ctx context.Context, id int64) (User, error)
	GetUserByUsername(ctx context.Context, username string) (User, error)
	ListUsers(ctx context.Context, limit, offset int32) ([]User, error)
	SetPasswordHash(ctx context.Context, id int64, hash string) (User, error)
	CreateInvitation(ctx context.Context, in NewInvitation) (Invitation, error)
	ListInvitations(ctx context.Context, limit, offset int32) ([]Invitation, error)
	CountInvitations(ctx context.Context) (int64, error)
	GetInvitation(ctx context.Context, id int64) (Invitation, error)
	// LockInvitationByToken locks the invitation row until the transaction ends.
	LockInvitationByToken(ctx context.Context, digest []byte) (Invitation, error)
	MarkInvitationAccepted(ctx context.Context, id, userID int64) error
	// RevokeInvitation returns ErrNotFound when it does not exist or is already accepted or revoked.
	RevokeInvitation(ctx context.Context, id int64) (Invitation, error)
	// MemberRole returns authz.RoleNone when the user is not a member of the project.
	MemberRole(ctx context.Context, projectID, userID int64) (authz.Role, error)
	ListUserMemberships(ctx context.Context, userID int64) (map[int64]authz.Role, error)
	ListProjectMembers(ctx context.Context, projectID int64, limit, offset int32) ([]Member, error)
	CountProjectMembers(ctx context.Context, projectID int64) (int64, error)
	UpsertMember(ctx context.Context, projectID, userID int64, role authz.Role) error
	// DeleteMember reports whether the user was a member.
	DeleteMember(ctx context.Context, projectID, userID int64) (bool, error)
	CreateAPIKey(ctx context.Context, k NewAPIKey) (APIKey, error)
	ListAPIKeys(ctx context.Context, projectID int64, limit, offset int32) ([]APIKey, error)
	CountAPIKeys(ctx context.Context, projectID int64) (int64, error)
	GetAPIKey(ctx context.Context, projectID, id int64) (APIKey, error)
	GetAPIKeyByToken(ctx context.Context, digest []byte) (APIKey, error)
	// RevokeAPIKey returns ErrNotFound when the key does not exist in the project or is already revoked.
	RevokeAPIKey(ctx context.Context, projectID, id int64) (APIKey, error)
	// TouchAPIKey records a use (at most once a minute).
	TouchAPIKey(ctx context.Context, id int64) error
}

type userKey struct{}

// WithUser returns a context carrying the authenticated user.
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, userKey{}, u)
}

type apiKeyKey struct{}

// WithAPIKey returns a context authenticated by an API key (no user).
func WithAPIKey(ctx context.Context, k APIKey) context.Context {
	return context.WithValue(ctx, apiKeyKey{}, k)
}

// APIKeyFrom returns the API key that authenticated the request, if any.
func APIKeyFrom(ctx context.Context) (APIKey, bool) {
	k, ok := ctx.Value(apiKeyKey{}).(APIKey)
	return k, ok
}

// UserFrom returns the authenticated user of the request, if any.
func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userKey{}).(User)
	return u, ok
}
