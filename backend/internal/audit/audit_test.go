package audit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/identity"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

var errBoom = errors.New("boom")

type fakeRepo struct {
	events            []Event
	insertErr         error
	listErr, countErr error
	filter            Filter
}

func (r *fakeRepo) Insert(_ context.Context, e Event) error {
	if r.insertErr != nil {
		return r.insertErr
	}
	e.ID = int64(len(r.events) + 1)
	r.events = append(r.events, e)
	return nil
}

func (r *fakeRepo) List(_ context.Context, f Filter, limit, offset int32) ([]Event, error) {
	r.filter = f
	out := r.events[min(int(offset), len(r.events)):]
	return out[:min(int(limit), len(out))], r.listErr
}

func (r *fakeRepo) Count(_ context.Context, _ Filter) (int64, error) {
	return int64(len(r.events)), r.countErr
}

type fakeAccess struct{ err error }

func (a fakeAccess) RequireAdmin(context.Context) error { return a.err }

// mux serves routes registered through the audit Router, with ctx setting who calls.
func mux(svc *Service, ctx func(context.Context) context.Context, status int) http.Handler {
	m := http.NewServeMux()
	a := Wrap(m, svc)
	h := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }
	for _, p := range []string{"GET /api/v1/things", "POST /api/v1/projects/{projectKey}/things", "PATCH /api/v1/things/{id}",
		"DELETE /api/v1/things/{id}", "PUT /api/v1/things/{id}", "POST /api/v1/mcp", "POST /api/v1/test-runs/{testRunId}/events", "POST /api/v1/ingestion/junit"} {
		a.HandleFunc(p, h)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { m.ServeHTTP(w, r.WithContext(ctx(r.Context()))) })
}

func asUser(ctx context.Context) context.Context {
	return identity.WithUser(ctx, identity.User{Username: "ana"})
}

func serve(h http.Handler, method, target string) int {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec.Code
}

func TestRouterRecordsSuccessfulChanges(t *testing.T) {
	repo := &fakeRepo{}
	h := mux(NewService(repo, fakeAccess{}), asUser, http.StatusCreated)
	assert.Equal(t, http.StatusCreated, serve(h, http.MethodPost, "/api/v1/projects/CHK/things"))
	serve(h, http.MethodPatch, "/api/v1/things/7?project=SHOP")
	serve(h, http.MethodPut, "/api/v1/things/7")
	serve(h, http.MethodDelete, "/api/v1/things/7")
	// Reads, MCP and live events are not audited.
	serve(h, http.MethodGet, "/api/v1/things")
	serve(h, http.MethodPost, "/api/v1/mcp")
	serve(h, http.MethodPost, "/api/v1/test-runs/3/events")
	require.Len(t, repo.events, 4)
	assert.Equal(t, Event{ID: 1, Actor: "ana", Action: "POST /api/v1/projects/{projectKey}/things", Path: "/api/v1/projects/CHK/things", ProjectKey: "CHK", Status: 201}, repo.events[0])
	assert.Equal(t, "SHOP", repo.events[1].ProjectKey, "the project of ?project=")
	assert.Equal(t, "", repo.events[2].ProjectKey)
	assert.Equal(t, "DELETE /api/v1/things/{id}", repo.events[3].Action)
}

func TestRouterSkipsFailuresAndNamesKeys(t *testing.T) {
	repo := &fakeRepo{}
	assert.Equal(t, http.StatusBadRequest, serve(mux(NewService(repo, fakeAccess{}), asUser, http.StatusBadRequest), http.MethodPost, "/api/v1/projects/CHK/things"))
	assert.Empty(t, repo.events, "a refused change changed nothing")

	key := func(ctx context.Context) context.Context {
		return identity.WithAPIKey(ctx, identity.APIKey{Prefix: "pvk_0000002a", Name: "GitHub Actions"})
	}
	serve(mux(NewService(repo, fakeAccess{}), key, http.StatusOK), http.MethodPost, "/api/v1/ingestion/junit?project=CHK")
	serve(mux(NewService(repo, fakeAccess{}), func(ctx context.Context) context.Context { return ctx }, http.StatusOK), http.MethodPost, "/api/v1/ingestion/junit")
	require.Len(t, repo.events, 2)
	assert.Equal(t, "api key pvk_0000002a… (GitHub Actions)", repo.events[0].Actor)
	assert.Equal(t, "CHK", repo.events[0].ProjectKey)
	assert.Equal(t, "unknown", repo.events[1].Actor)
	assert.Equal(t, int32(200), repo.events[1].Status, "a handler that never calls WriteHeader answered 200")

	// A failing store does not change the answer.
	repo.insertErr = errBoom
	assert.Equal(t, http.StatusOK, serve(mux(NewService(repo, fakeAccess{}), asUser, http.StatusOK), http.MethodDelete, "/api/v1/things/1"))
}

func TestRecordStoresOnlyStorableText(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, fakeAccess{})
	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	r.URL.Path = "/api/v1/a\x00b\xff/" + strings.Repeat("p", 3000)
	r.SetPathValue("projectKey", "K\x00"+strings.Repeat("Q", 60))
	svc.record(r.WithContext(identity.WithAPIKey(r.Context(), identity.APIKey{Prefix: "p", Name: strings.Repeat("n", 300)})), "POST /x", 204)
	e := repo.events[0]
	assert.True(t, strings.HasPrefix(e.Path, "/api/v1/ab?/"))
	assert.Len(t, []rune(e.Path), 2000)
	assert.Len(t, e.ProjectKey, 50)
	assert.NotContains(t, e.ProjectKey, "\x00")
	assert.Len(t, []rune(e.Actor), 200)
}

func TestEvents(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{events: []Event{{ID: 1}, {ID: 2}, {ID: 3}}}
	page, err := NewService(repo, fakeAccess{}).Events(ctx, Filter{ProjectKey: "CHK", Actor: "ana"}, pagination.Page{Number: 1, Size: 2})
	require.NoError(t, err)
	assert.Equal(t, int64(3), page.Total)
	assert.Len(t, page.Items, 2)
	assert.Equal(t, Filter{ProjectKey: "CHK", Actor: "ana"}, repo.filter)

	_, err = NewService(repo, fakeAccess{err: apperr.Forbidden("admins only")}).Events(ctx, Filter{}, pagination.Default())
	e, _ := apperr.As(err)
	require.NotNil(t, e)
	assert.Equal(t, apperr.KindForbidden, e.Kind)
	for _, f := range []Filter{{Actor: strings.Repeat("a", 201)}, {Actor: "a\x00"}, {ProjectKey: strings.Repeat("P", 51)}, {ProjectKey: "\xff"}} {
		_, err = NewService(repo, fakeAccess{}).Events(ctx, f, pagination.Default())
		e, _ = apperr.As(err)
		require.NotNil(t, e)
		assert.Equal(t, apperr.KindValidation, e.Kind)
	}
	repo.countErr = errBoom
	_, err = NewService(repo, fakeAccess{}).Events(ctx, Filter{}, pagination.Default())
	assert.ErrorIs(t, err, errBoom)
	repo.listErr = errBoom
	_, err = NewService(repo, fakeAccess{}).Events(ctx, Filter{}, pagination.Default())
	assert.ErrorIs(t, err, errBoom)
}

func TestHandler(t *testing.T) {
	repo := &fakeRepo{events: []Event{{ID: 2, Actor: "ana", Action: "DELETE /api/v1/x", Path: "/api/v1/x", ProjectKey: "CHK", Status: 204}, {ID: 1, Actor: "bob", Action: "POST /api/v1/y", Path: "/api/v1/y", Status: 201}}}
	m := http.NewServeMux()
	NewHandler(NewService(repo, fakeAccess{})).Register(m)
	get := func(target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec
	}
	rec := get("/api/v1/audit?project=CHK&actor=ana&pageSize=10")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"page":1,"pageSize":10,"totalItems":2,"totalPages":1,"items":[
		{"id":2,"occurredAt":"0001-01-01T00:00:00Z","actor":"ana","action":"DELETE /api/v1/x","path":"/api/v1/x","project":"CHK","status":204},
		{"id":1,"occurredAt":"0001-01-01T00:00:00Z","actor":"bob","action":"POST /api/v1/y","path":"/api/v1/y","project":null,"status":201}]}`, rec.Body.String())
	assert.Equal(t, Filter{ProjectKey: "CHK", Actor: "ana"}, repo.filter)
	for _, target := range []string{"/api/v1/audit?page=0", "/api/v1/audit?project=chk", "/api/v1/audit?project=", "/api/v1/audit?actor=", "/api/v1/audit?actor=" + strings.Repeat("a", 201)} {
		assert.Equal(t, http.StatusBadRequest, get(target).Code, target)
	}
}
