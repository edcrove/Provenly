package identity

import (
	"context"
	"errors"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
	"github.com/edcrove/provenly/backend/internal/platform/projectkey"
)

// The identity Service is the authz.Guard of the other modules.
var _ authz.Guard = (*Service)(nil)

func signedIn(ctx context.Context) (User, error) {
	u, ok := UserFrom(ctx)
	if !ok {
		return User{}, errSignIn
	}
	return u, nil
}

// Scope implements authz.Guard: every project for administrators, otherwise the user's memberships.
func (s *Service) Scope(ctx context.Context) (authz.Scope, error) {
	u, err := signedIn(ctx)
	if err != nil {
		return authz.Scope{}, err
	}
	if u.IsAdmin {
		return authz.Scope{All: true}, nil
	}
	roles, err := s.repo.ListUserMemberships(ctx, u.ID)
	return authz.Scope{Roles: roles}, err
}

// RoleIn returns the user's role in a project (authz.RoleAdmin for administrators).
func (s *Service) RoleIn(ctx context.Context, u User, projectID int64) (authz.Role, error) {
	if u.IsAdmin {
		return authz.RoleAdmin, nil
	}
	return s.repo.MemberRole(ctx, projectID, u.ID)
}

// Require implements authz.Guard.
func (s *Service) Require(ctx context.Context, projectID int64, minRole authz.Role, notFound error) error {
	if k, ok := APIKeyFrom(ctx); ok {
		return keyRequire(k, projectID, minRole, notFound)
	}
	u, err := signedIn(ctx)
	if err != nil {
		return err
	}
	role, err := s.RoleIn(ctx, u, projectID)
	switch {
	case err != nil:
		return err
	case role == authz.RoleNone:
		return notFound
	case role < minRole:
		return apperr.Forbidden("this needs the %s role in the project (you are %s)", minRole, role)
	}
	return nil
}

// Actor implements authz.Guard: the signed-in user (an API key is not a person and cannot make audited changes).
func (s *Service) Actor(ctx context.Context) (authz.Actor, error) {
	u, err := signedIn(ctx)
	return authz.Actor{ID: u.ID, Username: u.Username}, err
}

// RequireAdmin implements authz.Guard.
func (s *Service) RequireAdmin(ctx context.Context) error {
	u, err := signedIn(ctx)
	if err != nil {
		return err
	}
	return requireAdmin(u)
}

// errProjectHidden answers a project the caller cannot see. The HTTP adapter rewrites it with the requested key, so
// an invisible project reads exactly like an unknown one (no internal id, no hint that it exists).
var errProjectHidden = apperr.NotFound("project not found")

func projectNotFound(int64) error { return errProjectHidden }

// ProjectNotFound is the answer for an unknown or invisible project key.
func ProjectNotFound(key string) error { return projectkey.NotFound(key) }

// ListMembers returns a page of a project's members by username (anyone who can see the project).
func (s *Service) ListMembers(ctx context.Context, projectID int64, page pagination.Page) (pagination.Result[Member], error) {
	if err := s.Require(ctx, projectID, authz.RoleViewer, projectNotFound(projectID)); err != nil {
		return pagination.Result[Member]{}, err
	}
	n, err := s.repo.CountProjectMembers(ctx, projectID)
	if err != nil {
		return pagination.Result[Member]{}, err
	}
	items, err := s.repo.ListProjectMembers(ctx, projectID, page.Limit(), page.Offset())
	if err != nil {
		return pagination.Result[Member]{}, err
	}
	return pagination.Result[Member]{Items: items, Page: page, Total: n}, nil
}

func (s *Service) memberUser(ctx context.Context, username string) (User, error) {
	u, err := s.repo.GetUserByUsername(ctx, normalizeUsername(username))
	if errors.Is(err, ErrNotFound) {
		return User{}, apperr.NotFound("user %s not found", normalizeUsername(username))
	}
	return u, err
}

func parseRole(role string) (authz.Role, error) {
	r, err := authz.ParseMemberRole(role)
	if err != nil {
		return r, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "role", Message: "must be one of maintainer, member, viewer"})
	}
	return r, nil
}

// SetMember adds a user to a project or changes their role (maintainers and administrators).
func (s *Service) SetMember(ctx context.Context, projectID int64, username, role string) (Member, error) {
	if err := s.Require(ctx, projectID, authz.RoleMaintainer, projectNotFound(projectID)); err != nil {
		return Member{}, err
	}
	r, err := parseRole(role)
	if err != nil {
		return Member{}, err
	}
	u, err := s.memberUser(ctx, username)
	if err != nil {
		return Member{}, err
	}
	if err := s.repo.UpsertMember(ctx, projectID, u.ID, r); err != nil {
		return Member{}, err
	}
	return Member{User: u, Role: r, Since: s.now()}, nil
}

// RemoveMember takes a user out of a project (maintainers and administrators).
func (s *Service) RemoveMember(ctx context.Context, projectID int64, username string) error {
	if err := s.Require(ctx, projectID, authz.RoleMaintainer, projectNotFound(projectID)); err != nil {
		return err
	}
	u, err := s.memberUser(ctx, username)
	if err != nil {
		return err
	}
	removed, err := s.repo.DeleteMember(ctx, projectID, u.ID)
	if err == nil && !removed {
		return apperr.NotFound("%s is not a member of this project", u.Username)
	}
	return err
}
