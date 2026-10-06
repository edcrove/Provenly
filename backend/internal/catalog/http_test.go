package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/etag"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

// stubAPI answers every call with the configured error, or with canned data.
type stubAPI struct {
	err           error
	getErr        error
	projectErr    error
	gotFilter     ListFilter
	gotProjectIDs []int64
	gotCreate     CreateInput
	gotProject    CreateProjectInput
	gotStatus     *Status
	gotPage       pagination.Page
	gotUpdate     UpdateInput
	gotStep       CreateStepInput
	gotOrder      []int64
	gotMatch      etag.Match
}

var sample = TestCase{ID: 153, ProjectID: 1, ProjectKey: "TC", Number: 153, Title: "Login", Status: StatusActive, Automated: true,
	CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Version: 4}

var sampleStep = TestStep{ID: 9, TestCaseID: 153, Position: 1, Action: "open"}

var sampleProject = Project{ID: 1, Key: "TC", Name: "Default",
	CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

func (s *stubAPI) Create(_ context.Context, in CreateInput) (TestCase, error) {
	s.gotCreate = in
	tc := sample
	tc.Title = in.Title
	return tc, s.err
}
func (s *stubAPI) CreateProject(_ context.Context, in CreateProjectInput) (Project, error) {
	s.gotProject = in
	return sampleProject, s.err
}
func (s *stubAPI) ProjectByKey(_ context.Context, key string) (Project, error) {
	p := sampleProject
	p.Key = key
	return p, s.projectErr
}
func (s *stubAPI) ListProjects(_ context.Context, ids []int64, p pagination.Page) (pagination.Result[Project], error) {
	s.gotProjectIDs = ids
	return pagination.Result[Project]{Items: []Project{sampleProject}, Page: p, Total: 1}, s.err
}
func (s *stubAPI) UpdateProject(context.Context, string, UpdateProjectInput) (Project, error) {
	return sampleProject, s.err
}
func (s *stubAPI) Get(context.Context, int64) (TestCase, error) { return sample, s.getErr }
func (s *stubAPI) List(_ context.Context, f ListFilter, p pagination.Page) (pagination.Result[TestCase], error) {
	s.gotFilter = f
	s.gotStatus = f.Status
	s.gotPage = p
	return pagination.Result[TestCase]{Items: []TestCase{sample}, Page: p, Total: 1}, s.err
}
func (s *stubAPI) Update(_ context.Context, _ int64, in UpdateInput, m etag.Match) (TestCase, error) {
	s.gotUpdate, s.gotMatch = in, m
	return sample, s.err
}
func (s *stubAPI) Deprecate(_ context.Context, _ int64, m etag.Match) (TestCase, error) {
	s.gotMatch = m
	return sample, s.err
}
func (s *stubAPI) Reactivate(_ context.Context, _ int64, m etag.Match) (TestCase, error) {
	s.gotMatch = m
	return sample, s.err
}
func (s *stubAPI) ListSteps(_ context.Context, _ int64, p pagination.Page) (pagination.Result[TestStep], error) {
	return pagination.Result[TestStep]{Items: []TestStep{sampleStep}, Page: p, Total: 1}, s.err
}
func (s *stubAPI) CreateStep(_ context.Context, _ int64, in CreateStepInput, m etag.Match) (TestStep, int64, error) {
	s.gotStep, s.gotMatch = in, m
	return sampleStep, stepVersion, s.err
}
func (s *stubAPI) UpdateStep(_ context.Context, _, _ int64, _ UpdateStepInput, m etag.Match) (TestStep, int64, error) {
	s.gotMatch = m
	return sampleStep, stepVersion, s.err
}
func (s *stubAPI) DeleteStep(_ context.Context, _, _ int64, m etag.Match) (int64, error) {
	s.gotMatch = m
	return stepVersion, s.err
}
func (s *stubAPI) ReorderSteps(_ context.Context, _ int64, ids []int64, m etag.Match) ([]TestStep, int64, error) {
	s.gotOrder, s.gotMatch = ids, m
	return []TestStep{sampleStep}, stepVersion, s.err
}

// stepVersion is the test case version the stub reports after a step write.
const stepVersion = 9

func serve(api API, method, target, body string) *httptest.ResponseRecorder {
	return serveAs(adminGuard, api, method, target, body)
}

func serveAs(guard authz.Guard, api API, method, target, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	NewHandler(api, guard).Register(mux)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHandlerHappyPaths(t *testing.T) {
	cases := []struct {
		method, target, body string
		status               int
		contains             string
	}{
		{"GET", "/api/v1/test-cases?status=active&page=1&pageSize=5", "", 200, `"key":"TC-153"`},
		{"GET", "/api/v1/test-cases", "", 200, `"totalItems":1`},
		{"POST", "/api/v1/test-cases", `{"title":"New","automated":true}`, 201, `"title":"New"`},
		{"GET", "/api/v1/test-cases/153", "", 200, `"id":153`},
		{"PATCH", "/api/v1/test-cases/153", `{"title":"x"}`, 200, `"deprecatedAt":null`},
		{"POST", "/api/v1/test-cases/153/deprecate", "", 200, `"key":"TC-153"`},
		{"POST", "/api/v1/test-cases/153/reactivate", "", 200, `"status":"active"`},
		{"GET", "/api/v1/test-cases/153/steps", "", 200, `"action":"open"`},
		{"POST", "/api/v1/test-cases/153/steps", `{"action":"a","position":2}`, 201, `"testCaseId":153`},
		{"PUT", "/api/v1/test-cases/153/steps/order", `{"stepIds":[9]}`, 200, `"items":[{"id":9`},
		{"PATCH", "/api/v1/test-cases/153/steps/9", `{"action":"b"}`, 200, `"position":1`},
		{"DELETE", "/api/v1/test-cases/153/steps/9", "", 204, ""},
		{"GET", "/api/v1/test-cases?project=CHK", "", 200, `"projectKey":"TC"`},
		{"GET", "/api/v1/projects", "", 200, `"key":"TC"`},
		{"POST", "/api/v1/projects", `{"key":"CHK","name":"Checkout"}`, 201, `"name":"Default"`},
		{"GET", "/api/v1/projects/CHK", "", 200, `"key":"CHK"`},
		{"PATCH", "/api/v1/projects/TC", `{"name":"x"}`, 200, `"description":""`},
	}
	for _, c := range cases {
		api := &stubAPI{}
		rec := serve(api, c.method, c.target, c.body)
		assert.Equal(t, c.status, rec.Code, "%s %s", c.method, c.target)
		assert.Contains(t, rec.Body.String(), c.contains, "%s %s", c.method, c.target)
	}
}

func TestHandlerPassesInputs(t *testing.T) {
	api := &stubAPI{}
	serve(api, "GET", "/api/v1/test-cases?status=deprecated", "")
	require.NotNil(t, api.gotStatus)
	assert.Equal(t, StatusDeprecated, *api.gotStatus)

	serve(api, "PATCH", "/api/v1/test-cases/1", `{"automated":false}`)
	require.NotNil(t, api.gotUpdate.Automated)
	assert.False(t, *api.gotUpdate.Automated)
	assert.Nil(t, api.gotUpdate.Title)

	serve(api, "POST", "/api/v1/test-cases/1/steps", `{"action":"a","expectedResult":"e","position":3}`)
	assert.Equal(t, CreateStepInput{Action: "a", ExpectedResult: "e", Position: ptr(int32(3))}, api.gotStep)

	serve(api, "PUT", "/api/v1/test-cases/1/steps/order", `{"stepIds":[3,1,2]}`)
	assert.Equal(t, []int64{3, 1, 2}, api.gotOrder)

	// The project filter and the project of a new test case are resolved by key; TC is the default.
	serve(api, "GET", "/api/v1/test-cases?project=CHK", "")
	assert.Equal(t, []int64{1}, api.gotFilter.ProjectIDs)
	serve(api, "GET", "/api/v1/test-cases", "")
	assert.Nil(t, api.gotFilter.ProjectIDs, "administrators: every project")
	serve(api, "POST", "/api/v1/test-cases", `{"title":"t","project":"CHK"}`)
	assert.Equal(t, int64(1), api.gotCreate.ProjectID)
	serve(api, "POST", "/api/v1/projects", `{"key":"web","name":"Web","description":"d"}`)
	assert.Equal(t, CreateProjectInput{Key: "web", Name: "Web", Description: "d"}, api.gotProject)
}

// Unknown query parameters are ignored; every known parameter still applies and
// is still validated.
func TestHandlerIgnoresUnknownQueryParameters(t *testing.T) {
	api := &stubAPI{}
	rec := serve(api, "GET", "/api/v1/test-cases?status=deprecated&page=2&pageSize=5&automated=true&limit=1&foo=bar", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, api.gotStatus)
	assert.Equal(t, StatusDeprecated, *api.gotStatus)
	assert.Equal(t, pagination.Page{Number: 2, Size: 5}, api.gotPage)

	rec = serve(api, "GET", "/api/v1/test-cases?pageSize=0&foo=bar", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"field":"pageSize"`)
}

func TestHandlerErrors(t *testing.T) {
	notFound := &stubAPI{err: apperr.NotFound("missing")}
	requests := []struct{ method, target, body string }{
		{"GET", "/api/v1/test-cases", ""},
		{"POST", "/api/v1/test-cases", `{"title":"a"}`},
		{"PATCH", "/api/v1/test-cases/1", `{"title":"a"}`},
		{"POST", "/api/v1/test-cases/1/deprecate", ""},
		{"POST", "/api/v1/test-cases/1/reactivate", ""},
		{"GET", "/api/v1/test-cases/1/steps", ""},
		{"POST", "/api/v1/test-cases/1/steps", `{"action":"a"}`},
		{"PUT", "/api/v1/test-cases/1/steps/order", `{"stepIds":[]}`},
		{"PATCH", "/api/v1/test-cases/1/steps/2", `{"action":"a"}`},
		{"DELETE", "/api/v1/test-cases/1/steps/2", ""},
		{"GET", "/api/v1/projects", ""},
		{"POST", "/api/v1/projects", `{"key":"CHK","name":"n"}`},
		{"PATCH", "/api/v1/projects/CHK", `{"name":"n"}`},
	}
	// Each operation's own failure, and (for /test-cases/{id}) the lookup that authorizes it.
	lookup := &stubAPI{getErr: apperr.NotFound("missing")}
	for _, r := range requests {
		for _, api := range []*stubAPI{notFound, lookup} {
			if api == lookup && !strings.Contains(r.target, "/test-cases/1") {
				continue
			}
			rec := serve(api, r.method, r.target, r.body)
			assert.Equal(t, http.StatusNotFound, rec.Code, "%s %s", r.method, r.target)
			var p map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
			assert.Equal(t, "not_found", p["code"])
		}
	}

	badRequests := []struct{ method, target, body string }{
		{"GET", "/api/v1/test-cases?page=0", ""},
		{"GET", "/api/v1/test-cases?q=%20", ""},
		{"GET", "/api/v1/test-cases?status=gone", ""},
		{"POST", "/api/v1/test-cases", `{"id":5,"title":"a"}`},
		{"GET", "/api/v1/test-cases/abc", ""},
		{"PATCH", "/api/v1/test-cases/abc", `{}`},
		{"PATCH", "/api/v1/test-cases/1", `nope`},
		{"POST", "/api/v1/test-cases/0/deprecate", ""},
		{"POST", "/api/v1/test-cases/x/reactivate", ""},
		{"GET", "/api/v1/test-cases/x/steps", ""},
		{"GET", "/api/v1/test-cases/1/steps?pageSize=1000", ""},
		{"POST", "/api/v1/test-cases/x/steps", `{"action":"a"}`},
		{"POST", "/api/v1/test-cases/1/steps", `{"action":1}`},
		{"PUT", "/api/v1/test-cases/x/steps/order", `{"stepIds":[]}`},
		{"PUT", "/api/v1/test-cases/1/steps/order", `{"stepIds":"x"}`},
		{"PATCH", "/api/v1/test-cases/x/steps/1", `{"action":"a"}`},
		{"PATCH", "/api/v1/test-cases/1/steps/x", `{"action":"a"}`},
		{"PATCH", "/api/v1/test-cases/1/steps/1", `[]`},
		{"DELETE", "/api/v1/test-cases/1/steps/0", ""},
		{"GET", "/api/v1/test-cases?project=", ""},
		{"GET", "/api/v1/test-cases?project=chk", ""},
		{"POST", "/api/v1/test-cases", `{"title":"a","project":"x"}`},
		{"GET", "/api/v1/projects?page=0", ""},
		{"POST", "/api/v1/projects", `{"key":1}`},
		{"GET", "/api/v1/projects/c", ""},
		{"PATCH", "/api/v1/projects/c", `{"name":"n"}`},
		{"PATCH", "/api/v1/projects/CHK", `nope`},
	}
	for _, r := range badRequests {
		rec := serve(&stubAPI{}, r.method, r.target, r.body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "%s %s", r.method, r.target)
	}

	// An unknown project in a filter, in a new test case or addressed directly is a 404.
	missing := &stubAPI{projectErr: apperr.NotFound("project CHK not found")}
	for _, target := range []string{"/api/v1/test-cases?project=CHK", "/api/v1/projects/CHK"} {
		assert.Equal(t, http.StatusNotFound, serve(missing, "GET", target, "").Code, target)
	}
	assert.Equal(t, http.StatusNotFound, serve(missing, "POST", "/api/v1/test-cases", `{"title":"a","project":"CHK"}`).Code)
	// A duplicate project key is a 409.
	rec := serve(&stubAPI{err: apperr.Conflict("project CHK already exists")}, "POST", "/api/v1/projects", `{"key":"CHK","name":"n"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"conflict"`)
}

// Roles per project: administrators do everything; maintainers manage the project and deprecate; members edit
// test cases and steps; viewers read; a project without a role is invisible (404, never 403).
func TestHandlerAuthorization(t *testing.T) {
	api := &stubAPI{}
	as := func(role authz.Role) authz.Guard { return memberOf(map[int64]authz.Role{1: role}) }
	status := func(g authz.Guard, method, target, body string) int {
		return serveAs(g, api, method, target, body).Code
	}

	cases := []struct {
		method, target, body string
		min                  authz.Role
		success              int
	}{
		{"GET", "/api/v1/test-cases/153", "", authz.RoleViewer, 200},
		{"GET", "/api/v1/test-cases/153/steps", "", authz.RoleViewer, 200},
		{"PATCH", "/api/v1/test-cases/153", `{"title":"a"}`, authz.RoleMember, 200},
		{"POST", "/api/v1/test-cases/153/steps", `{"action":"a"}`, authz.RoleMember, 201},
		{"PUT", "/api/v1/test-cases/153/steps/order", `{"stepIds":[9]}`, authz.RoleMember, 200},
		{"PATCH", "/api/v1/test-cases/153/steps/9", `{"action":"a"}`, authz.RoleMember, 200},
		{"DELETE", "/api/v1/test-cases/153/steps/9", "", authz.RoleMember, 204},
		{"POST", "/api/v1/test-cases/153/deprecate", "", authz.RoleMaintainer, 200},
		{"POST", "/api/v1/test-cases/153/reactivate", "", authz.RoleMaintainer, 200},
		{"POST", "/api/v1/test-cases", `{"title":"a"}`, authz.RoleMember, 201},
		{"GET", "/api/v1/projects/TC", "", authz.RoleViewer, 200},
		{"PATCH", "/api/v1/projects/TC", `{"name":"n"}`, authz.RoleMaintainer, 200},
	}
	for _, c := range cases {
		name := c.method + " " + c.target
		assert.Equal(t, http.StatusNotFound, status(memberOf(nil), c.method, c.target, c.body), "no role: invisible, "+name)
		for r := authz.RoleViewer; r <= authz.RoleMaintainer; r++ {
			want := c.success
			if r < c.min {
				want = http.StatusForbidden
			}
			assert.Equal(t, want, status(as(r), c.method, c.target, c.body), "%s as %s", name, r)
		}
	}
	rec := serveAs(memberOf(nil), api, "GET", "/api/v1/test-cases/153", "")
	assert.Contains(t, rec.Body.String(), "test case 153 not found", "the same answer as an unknown test case")

	// Lists are narrowed to the user's projects; ?project of another project is not found.
	serveAs(as(authz.RoleViewer), api, "GET", "/api/v1/test-cases", "")
	assert.Equal(t, []int64{1}, api.gotFilter.ProjectIDs)
	assert.Equal(t, http.StatusNotFound, status(memberOf(nil), "GET", "/api/v1/test-cases?project=TC", ""))
	rec = serveAs(as(authz.RoleMember), api, "GET", "/api/v1/projects", "")
	assert.Equal(t, []int64{1}, api.gotProjectIDs)
	assert.Contains(t, rec.Body.String(), `"myRole":"member"`)
	rec = serveAs(adminGuard, api, "GET", "/api/v1/projects/TC", "")
	assert.Contains(t, rec.Body.String(), `"myRole":"admin"`)

	// Only administrators create projects; a failing guard fails the request.
	assert.Equal(t, http.StatusForbidden, status(as(authz.RoleMaintainer), "POST", "/api/v1/projects", `{"key":"CHK","name":"n"}`))
	assert.Equal(t, http.StatusCreated, status(adminGuard, "POST", "/api/v1/projects", `{"key":"CHK","name":"n"}`))
	broken := stubGuard{err: errors.New("db down")}
	for _, target := range []string{"/api/v1/test-cases", "/api/v1/projects", "/api/v1/projects/TC"} {
		assert.Equal(t, http.StatusInternalServerError, status(broken, "GET", target, ""), target)
	}
	assert.Equal(t, http.StatusInternalServerError, status(broken, "POST", "/api/v1/projects", `{"key":"CHK","name":"n"}`))
}

// Optimistic locking over HTTP: reads and writes of a test case and its steps carry the version as the ETag,
// writes pass If-Match to the service, a malformed If-Match is a 400 and a stale one the service's 412.
func TestHandlerVersions(t *testing.T) {
	api := &stubAPI{}
	for _, c := range []struct {
		method, target, body string
		etag                 string
	}{
		{"GET", "/api/v1/test-cases/153", "", `"4"`},
		{"PATCH", "/api/v1/test-cases/153", `{"title":"x"}`, `"4"`},
		{"POST", "/api/v1/test-cases/153/deprecate", "", `"4"`},
		{"POST", "/api/v1/test-cases/153/reactivate", "", `"4"`},
		{"GET", "/api/v1/test-cases/153/steps", "", `"4"`},
		{"POST", "/api/v1/test-cases/153/steps", `{"action":"a"}`, `"9"`},
		{"PATCH", "/api/v1/test-cases/153/steps/9", `{"action":"a"}`, `"9"`},
		{"PUT", "/api/v1/test-cases/153/steps/order", `{"stepIds":[9]}`, `"9"`},
		{"DELETE", "/api/v1/test-cases/153/steps/9", "", `"9"`},
	} {
		rec := serveWith(api, c.method, c.target, c.body, `"4", "5"`)
		require.Less(t, rec.Code, 300, c.method+" "+c.target)
		assert.Equal(t, c.etag, rec.Header().Get("ETag"), c.method+" "+c.target)
		if c.method != "GET" {
			assert.True(t, api.gotMatch.Matches(5) && !api.gotMatch.Matches(6), c.method+" "+c.target+" passes If-Match")
		}
		api.gotMatch = etag.Match{}

		if c.method != "GET" {
			rec = serveWith(api, c.method, c.target, c.body, `4`)
			assert.Equal(t, http.StatusBadRequest, rec.Code, c.method+" "+c.target+" malformed If-Match")
			assert.Contains(t, rec.Body.String(), `"field":"If-Match"`)
		}
	}
	assert.Contains(t, serveWith(api, "GET", "/api/v1/test-cases/153", "", "").Body.String(), `"version":4`)

	stale := &stubAPI{err: apperr.PreconditionFailed("test case 153 changed")}
	rec := serveWith(stale, "PATCH", "/api/v1/test-cases/153", `{"title":"x"}`, `"1"`)
	assert.Equal(t, http.StatusPreconditionFailed, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"precondition_failed"`)
}

func serveWith(api API, method, target, body, ifMatch string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	NewHandler(api, adminGuard).Register(mux)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// ?key=<PROJECT>-<number> narrows the list to that test case: zero or one item (card #53).
func TestHandlerKeyFilter(t *testing.T) {
	api := &stubAPI{}
	rec := serve(api, "GET", "/api/v1/test-cases?key=CHK-12", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []int64{1}, api.gotFilter.ProjectIDs)
	assert.Equal(t, ptr(int64(12)), api.gotFilter.Number)
	serve(api, "GET", "/api/v1/test-cases?key=CHK-999999999999999999&status=active", "")
	assert.Equal(t, ptr(int64(999999999999999999)), api.gotFilter.Number)

	for _, bad := range []string{"", "chk-12", "CHK12", "CHK-0", "CHK-012", "C-1", "CHK-1000000000000000000", "CHK-12,CHK-13"} {
		rec = serve(&stubAPI{}, "GET", "/api/v1/test-cases?key="+bad, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, bad)
		assert.Contains(t, rec.Body.String(), `"field":"key"`, bad)
	}

	empty := func(guard authz.Guard, api *stubAPI, target string) {
		t.Helper()
		api.gotFilter = ListFilter{}
		rec := serveAs(guard, api, "GET", target, "")
		assert.Equal(t, http.StatusOK, rec.Code, target)
		assert.Contains(t, rec.Body.String(), `"totalItems":0`, target)
		assert.Contains(t, rec.Body.String(), `"items":[]`, target)
		assert.Nil(t, api.gotFilter.Number, "nothing is listed: %s", target)
	}
	empty(adminGuard, &stubAPI{projectErr: apperr.NotFound("project NOPE not found")}, "/api/v1/test-cases?key=NOPE-1")
	empty(memberOf(map[int64]authz.Role{9: authz.RoleViewer}), &stubAPI{}, "/api/v1/test-cases?key=CHK-1")
	rec = serve(&stubAPI{projectErr: errors.New("db down")}, "GET", "/api/v1/test-cases?key=CHK-1", "")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
