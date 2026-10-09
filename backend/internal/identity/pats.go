package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

// TokenPrefix starts every personal access token: pvly_pat_<8 hex>_<43 base64url>. The first 17 characters are its
// public prefix, shown in lists so people can tell tokens apart.
const TokenPrefix = "pvly_pat_"

const (
	tokenPublicLen = len(TokenPrefix) + 8
	tokenLen       = tokenPublicLen + 1 + 43
	// DefaultTokenDays is a token's life when none is asked for: every token expires (card #62).
	DefaultTokenDays = 90
	// MaxTokenDays is the longest life a token may have.
	MaxTokenDays = 365
	// MaxTokenProjects bounds the projects a token names (repeats included).
	MaxTokenProjects = 50
)

// A personal access token reads: it never changes anything, never administers, and only sees its projects.
var (
	errBadToken = apperr.Unauthorized("the personal access token is invalid, expired or revoked")
	// ErrTokenAdmin refuses administration (and the audit log) to a personal access token (card #62).
	ErrTokenAdmin   = apperr.Forbidden("a personal access token cannot be used for administration")
	errTokenProject = apperr.Forbidden("this personal access token does not cover the project")
	// ErrTokenReadOnly answers a change attempted with a personal access token.
	ErrTokenReadOnly = apperr.Forbidden("a personal access token is read-only: sign in to make changes")
)

// TokenStatus is where a personal access token is in its life.
type TokenStatus string

// Token statuses.
const (
	TokenActive  TokenStatus = "active"
	TokenExpired TokenStatus = "expired"
	TokenRevoked TokenStatus = "revoked"
)

// PersonalAccessToken lets a person's scripts and MCP clients read some of their projects. Only a digest is stored.
type PersonalAccessToken struct {
	ID          int64
	UserID      int64
	Name        string
	Prefix      string
	TokenSHA256 []byte
	ProjectIDs  []int64
	CreatedAt   time.Time
	ExpiresAt   time.Time
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
}

// Status of the token at time now.
func (t PersonalAccessToken) Status(now time.Time) TokenStatus {
	switch {
	case t.RevokedAt != nil:
		return TokenRevoked
	case !now.Before(t.ExpiresAt):
		return TokenExpired
	default:
		return TokenActive
	}
}

// covers tells whether the token was given the project.
func (t PersonalAccessToken) covers(projectID int64) bool {
	return slices.Contains(t.ProjectIDs, projectID)
}

// NewPersonalAccessToken is the content of a token to create.
type NewPersonalAccessToken struct {
	UserID      int64
	Name        string
	Prefix      string
	TokenSHA256 []byte
	CreatedAt   time.Time
	ExpiresAt   time.Time
	ProjectIDs  []int64
}

// CreateTokenInput is what a person asks for. A ProjectID of 0 is a project that does not exist.
type CreateTokenInput struct {
	Name       string
	ProjectIDs []int64
	// ExpiresInDays defaults to DefaultTokenDays.
	ExpiresInDays *int
}

// IsToken tells whether a bearer token is a personal access token rather than a session.
func IsToken(token string) bool { return strings.HasPrefix(token, TokenPrefix) }

func newToken() (token, prefix string) {
	id := make([]byte, 4)
	secret := make([]byte, 32)
	_, _ = rand.Read(id)
	_, _ = rand.Read(secret)
	prefix = TokenPrefix + hex.EncodeToString(id)
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(secret), prefix
}

// CreateToken makes a personal access token for the signed-in person over some of their projects, returned with
// its secret (shown only once).
func (s *Service) CreateToken(ctx context.Context, actor User, in CreateTokenInput) (PersonalAccessToken, string, error) {
	name := strings.TrimSpace(in.Name)
	days := DefaultTokenDays
	if in.ExpiresInDays != nil {
		days = *in.ExpiresInDays
	}
	var v apperr.Validator
	v.Check(name != "" && utf8.RuneCountInString(name) <= maxAPIKeyName, "name", fmt.Sprintf("must be 1 to %d characters", maxAPIKeyName))
	v.CheckText("name", name)
	v.Check(days >= 1 && days <= MaxTokenDays, "expiresInDays", fmt.Sprintf("must be 1 to %d days", MaxTokenDays))
	ids := slices.Clone(in.ProjectIDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	v.Check(len(in.ProjectIDs) >= 1 && len(in.ProjectIDs) <= MaxTokenProjects, "projects", fmt.Sprintf("must name 1 to %d projects", MaxTokenProjects))
	if err := v.Err(); err != nil {
		return PersonalAccessToken{}, "", err
	}
	for _, id := range ids {
		role := authz.RoleNone
		if id != 0 {
			r, err := s.RoleIn(ctx, actor, id)
			if err != nil {
				return PersonalAccessToken{}, "", err
			}
			role = r
		}
		if role == authz.RoleNone {
			return PersonalAccessToken{}, "", apperr.Validation(apperr.ValidationFailed,
				apperr.FieldError{Field: "projects", Message: "must be projects you belong to"})
		}
	}
	token, prefix := newToken()
	now := s.now()
	var out PersonalAccessToken
	err := s.repo.InTx(ctx, func(r Repository) error {
		var err error
		out, err = r.CreateToken(ctx, NewPersonalAccessToken{
			UserID: actor.ID, Name: name, Prefix: prefix, TokenSHA256: TokenDigest(token),
			CreatedAt: now, ExpiresAt: now.Add(time.Duration(days) * 24 * time.Hour), ProjectIDs: ids,
		})
		return err
	})
	if err != nil {
		return PersonalAccessToken{}, "", err
	}
	return out, token, nil
}

// ListTokens returns a page of the signed-in person's tokens, newest first.
func (s *Service) ListTokens(ctx context.Context, actor User, page pagination.Page) (pagination.Result[PersonalAccessToken], error) {
	n, err := s.repo.CountTokens(ctx, actor.ID)
	if err != nil {
		return pagination.Result[PersonalAccessToken]{}, err
	}
	items, err := s.repo.ListTokens(ctx, actor.ID, page.Limit(), page.Offset())
	if err != nil {
		return pagination.Result[PersonalAccessToken]{}, err
	}
	return pagination.Result[PersonalAccessToken]{Items: items, Page: page, Total: n}, nil
}

// RevokeToken makes one of the signed-in person's tokens unusable at once.
func (s *Service) RevokeToken(ctx context.Context, actor User, id int64) (PersonalAccessToken, error) {
	t, err := s.repo.RevokeToken(ctx, actor.ID, id)
	if !errors.Is(err, ErrNotFound) {
		return t, err
	}
	if _, err := s.repo.GetToken(ctx, actor.ID, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return PersonalAccessToken{}, apperr.NotFound("personal access token %d not found", id)
		}
		return PersonalAccessToken{}, err
	}
	return PersonalAccessToken{}, apperr.Conflict("personal access token %d is already revoked", id)
}

// AuthenticateToken returns the active person a personal access token belongs to, with the token, and records its
// use. Unknown, expired and revoked tokens, and tokens of deactivated people, are all the same 401.
func (s *Service) AuthenticateToken(ctx context.Context, token string) (User, PersonalAccessToken, error) {
	if len(token) != tokenLen || !IsToken(token) {
		return User{}, PersonalAccessToken{}, errBadToken
	}
	t, err := s.repo.GetTokenByDigest(ctx, TokenDigest(token))
	switch {
	case errors.Is(err, ErrNotFound):
		return User{}, PersonalAccessToken{}, errBadToken
	case err != nil:
		return User{}, PersonalAccessToken{}, err
	case t.Status(s.now()) != TokenActive:
		return User{}, PersonalAccessToken{}, errBadToken
	}
	u, err := s.repo.GetUser(ctx, t.UserID)
	if err != nil {
		return User{}, PersonalAccessToken{}, err
	}
	if u.DeactivatedAt != nil {
		return User{}, PersonalAccessToken{}, errBadToken
	}
	return u, t, s.repo.TouchToken(ctx, t.ID)
}

type tokenKey struct{}

// WithToken returns a context whose user authenticated with a personal access token.
func WithToken(ctx context.Context, t PersonalAccessToken) context.Context {
	return context.WithValue(ctx, tokenKey{}, t)
}

// TokenFrom returns the personal access token that authenticated the request, if any.
func TokenFrom(ctx context.Context) (PersonalAccessToken, bool) {
	t, ok := ctx.Value(tokenKey{}).(PersonalAccessToken)
	return t, ok
}
