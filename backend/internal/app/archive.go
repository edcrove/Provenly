package app

import (
	"context"
	"net/http"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
)

// projectByID is what archiveGuard reads from the catalog.
type projectByID interface {
	ProjectByID(ctx context.Context, id int64) (catalog.Project, error)
}

// archiveGuard is the guard the modules use: identity's, plus archived projects being read-only (Ed, 2026-10-11).
// A change that needs a member or more in a project (test cases, steps, runs and reports, suites, requirements,
// issues, classification, webhooks and the GitHub connection) is a 409 once the project is archived. Reads stay
// allowed, including those that need a maintainer (webhooks, the GitHub connection): markReads tells them apart. The
// check runs after the access check, so an invisible project still reads as not found.
type archiveGuard struct {
	accessGuard
	projects projectByID
}

// accessGuard is identity's side of the guard: roles, and the project of an API key (ingestion).
type accessGuard interface {
	authz.Guard
	KeyProject(ctx context.Context) (int64, bool)
}

// Require implements authz.Guard.
func (g archiveGuard) Require(ctx context.Context, projectID int64, minRole authz.Role, notFound error) error {
	if err := g.accessGuard.Require(ctx, projectID, minRole, notFound); err != nil || minRole < authz.RoleMember || reading(ctx) {
		return err
	}
	p, err := g.projects.ProjectByID(ctx, projectID)
	if err != nil {
		return err
	}
	if p.ArchivedAt != nil {
		return apperr.Conflict("project %s is archived: an administrator unarchives it to change it", p.Key)
	}
	return nil
}

type readingKey struct{}

// markReads records on every request whether it only reads (GET or HEAD), for archiveGuard. It wraps the routes, so
// the requests MCP tools make in-process are marked too; a request without the mark counts as a change.
func markReads(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		read := r.Method == http.MethodGet || r.Method == http.MethodHead
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), readingKey{}, read)))
	})
}

func reading(ctx context.Context) bool {
	read, _ := ctx.Value(readingKey{}).(bool)
	return read
}
