package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
)

type fakeAccess struct {
	authz.Guard
	err error
}

func (f fakeAccess) Require(context.Context, int64, authz.Role, error) error { return f.err }
func (fakeAccess) KeyProject(context.Context) (int64, bool)                  { return 0, false }

type fakeProjects map[int64]catalog.Project

func (f fakeProjects) ProjectByID(_ context.Context, id int64) (catalog.Project, error) {
	p, ok := f[id]
	if !ok {
		return catalog.Project{}, errors.New("boom")
	}
	return p, nil
}

// Archived projects are read-only (Ed, 2026-10-11): changes are a 409 after the access check; reads stay allowed,
// even those that need a maintainer.
func TestArchiveGuard(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	projects := fakeProjects{1: {ID: 1, Key: "CHK"}, 2: {ID: 2, Key: "OLD", ArchivedAt: &at}}
	g := archiveGuard{accessGuard: fakeAccess{}, projects: projects}

	assert.NoError(t, g.Require(ctx, 1, authz.RoleMember, nil), "an active project")
	assert.NoError(t, g.Require(ctx, 2, authz.RoleViewer, nil), "reading an archived project")
	err := g.Require(ctx, 2, authz.RoleMaintainer, nil)
	e, _ := apperr.As(err)
	if assert.NotNil(t, e) {
		assert.Equal(t, apperr.KindConflict, e.Kind)
		assert.Contains(t, e.Error(), "project OLD is archived")
	}
	assert.EqualError(t, g.Require(ctx, 3, authz.RoleMember, nil), "boom")

	// What markReads records: GET and HEAD read; anything else (and a request without the mark) changes.
	seen := map[string]bool{}
	marked := markReads(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen[r.Method] = reading(r.Context())
		if r.Method == http.MethodGet {
			assert.NoError(t, g.Require(r.Context(), 2, authz.RoleMaintainer, nil), "a maintainer reads an archived project's settings")
		}
	}))
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPatch, http.MethodDelete} {
		marked.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(m, "/", nil))
	}
	assert.Equal(t, map[string]bool{"GET": true, "HEAD": true, "POST": false, "PATCH": false, "DELETE": false}, seen)
	assert.False(t, reading(ctx))

	denied := apperr.Forbidden("no")
	g.accessGuard = fakeAccess{err: denied}
	assert.Equal(t, denied, g.Require(ctx, 2, authz.RoleMember, nil), "the access check answers first")
}
