// Package authz defines project roles and the Guard port that REST adapters use
// to authorize a request. The identity module implements it; catalog and
// execution depend only on this package (no module imports another).
package authz

import (
	"context"
	"fmt"
)

// Role is what a user may do in a project, ordered from least to most.
type Role int

// Roles. RoleNone means the project is invisible to the user.
const (
	RoleNone Role = iota
	RoleViewer
	RoleMember
	RoleMaintainer
	// RoleAdmin is an administrator: every project, implicitly.
	RoleAdmin
)

var names = map[Role]string{RoleNone: "none", RoleViewer: "viewer", RoleMember: "member", RoleMaintainer: "maintainer", RoleAdmin: "admin"}

func (r Role) String() string { return names[r] }

// ParseMemberRole parses a role that can be granted in a project (not none nor admin).
func ParseMemberRole(s string) (Role, error) {
	for r, n := range names {
		if n == s && r >= RoleViewer && r <= RoleMaintainer {
			return r, nil
		}
	}
	return RoleNone, fmt.Errorf("unknown project role %q", s)
}

// MemberRoles lists the roles a project member can have, for messages and enums.
var MemberRoles = []string{"maintainer", "member", "viewer"}

// Scope is the set of projects a user can see.
type Scope struct {
	// All is true for administrators.
	All bool
	// Roles holds the user's role in each project they are a member of.
	Roles map[int64]Role
}

// RoleIn returns the user's role in a project of the scope.
func (s Scope) RoleIn(projectID int64) Role {
	if s.All {
		return RoleAdmin
	}
	return s.Roles[projectID]
}

// ProjectIDs is the list filter of the scope: nil means every project.
func (s Scope) ProjectIDs() []int64 {
	if s.All {
		return nil
	}
	ids := make([]int64, 0, len(s.Roles))
	for id := range s.Roles {
		ids = append(ids, id)
	}
	return ids
}

// Guard authorizes the current request's user.
type Guard interface {
	// Scope returns the projects the user can see.
	Scope(ctx context.Context) (Scope, error)
	// Require answers notFound (the project's resource is invisible) when the user has no role in the
	// project, and a forbidden error when the role is below min.
	Require(ctx context.Context, projectID int64, minRole Role, notFound error) error
	// RequireAdmin answers a forbidden error unless the user is an administrator.
	RequireAdmin(ctx context.Context) error
	// Actor returns who is making the request (recorded with audited changes).
	Actor(ctx context.Context) (Actor, error)
}

// Actor is the signed-in user making a change.
type Actor struct {
	ID       int64
	Username string
}
