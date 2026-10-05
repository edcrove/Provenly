package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

// APIKeyPrefix starts every API key: pvk_<8 hex>_<43 base64url>. The first 12 characters are its
// public prefix, shown in lists so people can tell keys apart.
const APIKeyPrefix = "pvk_"

const (
	apiKeyPublicLen = len(APIKeyPrefix) + 8
	apiKeyLen       = apiKeyPublicLen + 1 + 43
	maxAPIKeyName   = 100
)

var errBadKey = apperr.Unauthorized("the API key is invalid or revoked")

// IsAPIKey tells whether a bearer token is an API key rather than a session.
func IsAPIKey(token string) bool { return strings.HasPrefix(token, APIKeyPrefix) }

// newAPIKeyToken returns a fresh key and its public prefix.
func newAPIKeyToken() (token, prefix string) {
	id := make([]byte, 4)
	secret := make([]byte, 32)
	_, _ = rand.Read(id)
	_, _ = rand.Read(secret)
	prefix = APIKeyPrefix + hex.EncodeToString(id)
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(secret), prefix
}

// CreateAPIKey creates a project API key (maintainers) and returns it with its token, shown only once.
func (s *Service) CreateAPIKey(ctx context.Context, projectID int64, name string) (APIKey, string, error) {
	if err := s.Require(ctx, projectID, authz.RoleMaintainer, projectNotFound(projectID)); err != nil {
		return APIKey{}, "", err
	}
	u, _ := UserFrom(ctx)
	name = strings.TrimSpace(name)
	var v apperr.Validator
	v.Check(name != "" && utf8.RuneCountInString(name) <= maxAPIKeyName, "name", fmt.Sprintf("must be 1 to %d characters", maxAPIKeyName))
	v.CheckText("name", name)
	if err := v.Err(); err != nil {
		return APIKey{}, "", err
	}
	token, prefix := newAPIKeyToken()
	k, err := s.repo.CreateAPIKey(ctx, NewAPIKey{ProjectID: projectID, Name: name, Prefix: prefix, TokenSHA256: TokenDigest(token), CreatedBy: u.ID})
	if err != nil {
		return APIKey{}, "", err
	}
	return k, token, nil
}

// ListAPIKeys returns a page of a project's API keys, newest first (maintainers).
func (s *Service) ListAPIKeys(ctx context.Context, projectID int64, page pagination.Page) (pagination.Result[APIKey], error) {
	if err := s.Require(ctx, projectID, authz.RoleMaintainer, projectNotFound(projectID)); err != nil {
		return pagination.Result[APIKey]{}, err
	}
	n, err := s.repo.CountAPIKeys(ctx, projectID)
	if err != nil {
		return pagination.Result[APIKey]{}, err
	}
	items, err := s.repo.ListAPIKeys(ctx, projectID, page.Limit(), page.Offset())
	if err != nil {
		return pagination.Result[APIKey]{}, err
	}
	return pagination.Result[APIKey]{Items: items, Page: page, Total: n}, nil
}

// RevokeAPIKey makes a key unusable at once (maintainers).
func (s *Service) RevokeAPIKey(ctx context.Context, projectID, id int64) (APIKey, error) {
	if err := s.Require(ctx, projectID, authz.RoleMaintainer, projectNotFound(projectID)); err != nil {
		return APIKey{}, err
	}
	k, err := s.repo.RevokeAPIKey(ctx, projectID, id)
	if !errors.Is(err, ErrNotFound) {
		return k, err
	}
	if _, err := s.repo.GetAPIKey(ctx, projectID, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return APIKey{}, apperr.NotFound("API key %d not found", id)
		}
		return APIKey{}, err
	}
	return APIKey{}, apperr.Conflict("API key %d is already revoked", id)
}

// AuthenticateKey returns the active API key a token belongs to and records its use.
func (s *Service) AuthenticateKey(ctx context.Context, token string) (APIKey, error) {
	if len(token) != apiKeyLen || !IsAPIKey(token) {
		return APIKey{}, errBadKey
	}
	k, err := s.repo.GetAPIKeyByToken(ctx, TokenDigest(token))
	switch {
	case errors.Is(err, ErrNotFound):
		return APIKey{}, errBadKey
	case err != nil:
		return APIKey{}, err
	case k.RevokedAt != nil:
		return APIKey{}, errBadKey
	}
	return k, s.repo.TouchAPIKey(ctx, k.ID)
}

// KeyProject returns the project of the API key that authenticated the request, if any.
func (s *Service) KeyProject(ctx context.Context) (int64, bool) {
	k, ok := APIKeyFrom(ctx)
	return k.ProjectID, ok
}

// keyRequire authorizes a request made with an API key: it reports runs into its own project only.
func keyRequire(k APIKey, projectID int64, minRole authz.Role, notFound error) error {
	switch {
	case projectID != k.ProjectID:
		return notFound
	case minRole > authz.RoleMember:
		return apperr.Forbidden("an API key can only report runs")
	}
	return nil
}
