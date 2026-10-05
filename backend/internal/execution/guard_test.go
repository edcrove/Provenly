package execution

import (
	"context"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
)

// stubGuard authorizes from a fixed scope; err makes every call fail.
type stubGuard struct {
	scope authz.Scope
	err   error
}

func (g stubGuard) Scope(context.Context) (authz.Scope, error) { return g.scope, g.err }

func (g stubGuard) Require(_ context.Context, projectID int64, minRole authz.Role, notFound error) error {
	if g.err != nil {
		return g.err
	}
	switch role := g.scope.RoleIn(projectID); {
	case role == authz.RoleNone:
		return notFound
	case role < minRole:
		return apperr.Forbidden("needs %s", minRole)
	}
	return nil
}

func (g stubGuard) RequireAdmin(context.Context) error {
	if g.err != nil {
		return g.err
	}
	if !g.scope.All {
		return apperr.Forbidden("administrators only")
	}
	return nil
}

var adminGuard = stubGuard{scope: authz.Scope{All: true}}

// memberOf is a guard for a user with the given roles by project id.
func memberOf(roles map[int64]authz.Role) stubGuard {
	return stubGuard{scope: authz.Scope{Roles: roles}}
}

func (g stubGuard) Actor(context.Context) (authz.Actor, error) {
	return authz.Actor{ID: 1, Username: "admin"}, g.err
}
