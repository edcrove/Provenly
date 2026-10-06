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
	"github.com/edcrove/provenly/backend/internal/platform/auditnote"
	"github.com/edcrove/provenly/backend/internal/platform/clientinfo"
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
	_, note := auditnote.Open(r.Context())
	svc.record(r.WithContext(identity.WithAPIKey(r.Context(), identity.APIKey{Prefix: "p", Name: strings.Repeat("n", 300)})), "POST /x", 204, note, target{})
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
	for _, f := range []Filter{{Actor: strings.Repeat("a", 201)}, {Actor: "a\x00"}, {ProjectKey: strings.Repeat("P", 51)}, {ProjectKey: "\xff"}, {TestCaseKey: strings.Repeat("K", 41)}} {
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
	repo.events[0].Summary, repo.events[0].TestCaseKey = "deleted CHK-4 step 3", "CHK-4"
	repo.events[0].IP, repo.events[0].UserAgent = "203.0.113.9", "curl/8"
	rec := get("/api/v1/audit?project=CHK&actor=ana&testCase=CHK-4&pageSize=10")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"page":1,"pageSize":10,"totalItems":2,"totalPages":1,"items":[
		{"id":2,"occurredAt":"0001-01-01T00:00:00Z","actor":"ana","action":"DELETE /api/v1/x","path":"/api/v1/x","project":"CHK","status":204,"summary":"deleted CHK-4 step 3","testCase":"CHK-4","ip":"203.0.113.9","userAgent":"curl/8"},
		{"id":1,"occurredAt":"0001-01-01T00:00:00Z","actor":"bob","action":"POST /api/v1/y","path":"/api/v1/y","project":null,"status":201,"summary":null,"testCase":null,"ip":null,"userAgent":null}]}`, rec.Body.String())
	assert.Equal(t, Filter{ProjectKey: "CHK", Actor: "ana", TestCaseKey: "CHK-4"}, repo.filter)
	for _, target := range []string{"/api/v1/audit?page=0", "/api/v1/audit?project=chk", "/api/v1/audit?project=", "/api/v1/audit?actor=", "/api/v1/audit?actor=" + strings.Repeat("a", 201),
		"/api/v1/audit?testCase=", "/api/v1/audit?testCase=chk-4", "/api/v1/audit?testCase=CHK-0", "/api/v1/audit?testCase=CHK"} {
		assert.Equal(t, http.StatusBadRequest, get(target).Code, target)
	}
}

type fakeNames struct{ err error }

func (n fakeNames) ProjectKey(_ context.Context, id int64) (string, error) {
	return map[int64]string{2: "CHK"}[id], n.err
}

func (n fakeNames) TestCaseKey(_ context.Context, id int64) (string, error) {
	if id != 4 {
		return "", errBoom
	}
	return "CHK-4", n.err
}

func (n fakeNames) StepPosition(_ context.Context, testCaseID, stepID int64) (int32, error) {
	if testCaseID != 4 || stepID != 30 {
		return 0, errBoom
	}
	return 3, n.err
}

// A change without a project in its path is attributed to the project its authorization allowed, and reads in
// words with its test case (card #48).
func TestRouterNamesWhatChanged(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, fakeAccess{})
	svc.SetResolver(fakeNames{})
	m := http.NewServeMux()
	a := Wrap(m, svc)
	allow := func(w http.ResponseWriter, r *http.Request) {
		auditnote.Project(r.Context(), 2)
		auditnote.Project(r.Context(), 9) // the first project allowed wins
		w.WriteHeader(http.StatusNoContent)
	}
	a.HandleFunc("DELETE /api/v1/test-cases/{testCaseId}/steps/{stepId}", allow)
	a.HandleFunc("PATCH /api/v1/test-cases/{testCaseId}", allow)
	a.HandleFunc("POST /api/v1/test-runs/{testRunId}/finish", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	a.HandleFunc("POST /api/v1/unknown", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { m.ServeHTTP(w, r.WithContext(asUser(r.Context()))) })
	serve(h, http.MethodDelete, "/api/v1/test-cases/4/steps/30")
	serve(h, http.MethodDelete, "/api/v1/test-cases/4/steps/31")
	serve(h, http.MethodPatch, "/api/v1/test-cases/5")
	serve(h, http.MethodPatch, "/api/v1/test-cases/x")
	serve(h, http.MethodPost, "/api/v1/test-runs/12/finish")
	serve(h, http.MethodPost, "/api/v1/unknown")
	require.Len(t, repo.events, 6)
	assert.Equal(t, Event{ID: 1, Actor: "ana", Action: "DELETE /api/v1/test-cases/{testCaseId}/steps/{stepId}", Path: "/api/v1/test-cases/4/steps/30",
		ProjectKey: "CHK", Status: 204, Summary: "deleted CHK-4 step 3", TestCaseKey: "CHK-4"}, repo.events[0])
	assert.Equal(t, "deleted CHK-4 a step", repo.events[1].Summary, "an unknown step")
	assert.Equal(t, "edited #5", repo.events[2].Summary, "an unknown test case keeps its id")
	assert.Empty(t, repo.events[2].TestCaseKey)
	assert.Equal(t, "edited #x", repo.events[3].Summary)
	assert.Equal(t, Event{ID: 5, Actor: "ana", Action: "POST /api/v1/test-runs/{testRunId}/finish", Path: "/api/v1/test-runs/12/finish",
		Status: 200, Summary: "finished run #12"}, repo.events[4], "no project allowed: none recorded")
	assert.Empty(t, repo.events[5].Summary, "an operation without a summary")

	// Without a resolver, or when it fails, the ids stay.
	repo.events = nil
	svc.SetResolver(fakeNames{err: errBoom})
	serve(h, http.MethodDelete, "/api/v1/test-cases/4/steps/30")
	svc.SetResolver(nil)
	serve(h, http.MethodDelete, "/api/v1/test-cases/4/steps/30")
	for _, e := range repo.events {
		assert.Equal(t, "deleted #4 a step", e.Summary)
		assert.Empty(t, e.ProjectKey)
	}
}

// Every audited route the API registers says in words what it did.
func TestEverySummaryNamesItsPathValues(t *testing.T) {
	for pattern, tmpl := range summaries {
		_, path, _ := strings.Cut(pattern, " ")
		for _, name := range placeholders(tmpl) {
			assert.Contains(t, path, "{"+name+"}", pattern)
		}
	}
	assert.Equal(t, []string{"a", "b"}, placeholders("x {a} y {b}"))
}

// Sign-in events are recorded with the client and logged; an unknown account reads "unknown" (card #49).
func TestAuthEvent(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, fakeAccess{})
	ctx := clientinfo.With(context.Background(), clientinfo.Info{IP: "203.0.113.9", UserAgent: "Mozilla\x00/5"})
	svc.AuthEvent(ctx, identity.AuthEvent{Kind: identity.AuthLoginFailed, Status: 401})
	svc.AuthEvent(ctx, identity.AuthEvent{Kind: identity.AuthLogin, Username: "ana", Status: 200})
	svc.AuthEvent(ctx, identity.AuthEvent{Kind: "auth.nonsense", Status: 200})
	require.Len(t, repo.events, 2, "an unknown kind is not recorded")
	assert.Equal(t, Event{ID: 1, Actor: "unknown", Action: "POST /api/v1/auth/login", Path: "/api/v1/auth/login", Status: 401,
		Summary: "failed to sign in", IP: "203.0.113.9", UserAgent: "Mozilla/5"}, repo.events[0])
	assert.Equal(t, "ana", repo.events[1].Actor)
	assert.Equal(t, "signed in", repo.events[1].Summary)
	for kind := range signIns {
		svc.AuthEvent(ctx, identity.AuthEvent{Kind: kind, Username: "ana", Status: 200})
	}
	assert.Len(t, repo.events, 2+len(signIns))
	repo.insertErr = errBoom
	svc.AuthEvent(ctx, identity.AuthEvent{Kind: identity.AuthLogout, Username: "ana", Status: 204}) // logged, not returned
}

// Changes through the router keep the client too.
func TestRouterKeepsTheClient(t *testing.T) {
	repo := &fakeRepo{}
	h := mux(NewService(repo, fakeAccess{}), func(ctx context.Context) context.Context {
		return clientinfo.With(asUser(ctx), clientinfo.Info{IP: "198.51.100.7", UserAgent: "provenly-cli"})
	}, http.StatusNoContent)
	serve(h, http.MethodDelete, "/api/v1/things/1")
	require.Len(t, repo.events, 1)
	assert.Equal(t, "198.51.100.7", repo.events[0].IP)
	assert.Equal(t, "provenly-cli", repo.events[0].UserAgent)
}
