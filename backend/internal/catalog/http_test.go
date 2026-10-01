package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

// stubAPI answers every call with the configured error, or with canned data.
type stubAPI struct {
	err       error
	gotStatus *Status
	gotPage   pagination.Page
	gotUpdate UpdateInput
	gotStep   CreateStepInput
	gotOrder  []int64
}

var sample = TestCase{ID: 153, Title: "Login", Status: StatusActive, Automated: true,
	CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

var sampleStep = TestStep{ID: 9, TestCaseID: 153, Position: 1, Action: "open"}

func (s *stubAPI) Create(_ context.Context, in CreateInput) (TestCase, error) {
	tc := sample
	tc.Title = in.Title
	return tc, s.err
}
func (s *stubAPI) Get(context.Context, int64) (TestCase, error) { return sample, s.err }
func (s *stubAPI) List(_ context.Context, st *Status, p pagination.Page) (pagination.Result[TestCase], error) {
	s.gotStatus = st
	s.gotPage = p
	return pagination.Result[TestCase]{Items: []TestCase{sample}, Page: p, Total: 1}, s.err
}
func (s *stubAPI) Update(_ context.Context, _ int64, in UpdateInput) (TestCase, error) {
	s.gotUpdate = in
	return sample, s.err
}
func (s *stubAPI) Deprecate(context.Context, int64) (TestCase, error)  { return sample, s.err }
func (s *stubAPI) Reactivate(context.Context, int64) (TestCase, error) { return sample, s.err }
func (s *stubAPI) ListSteps(_ context.Context, _ int64, p pagination.Page) (pagination.Result[TestStep], error) {
	return pagination.Result[TestStep]{Items: []TestStep{sampleStep}, Page: p, Total: 1}, s.err
}
func (s *stubAPI) CreateStep(_ context.Context, _ int64, in CreateStepInput) (TestStep, error) {
	s.gotStep = in
	return sampleStep, s.err
}
func (s *stubAPI) UpdateStep(context.Context, int64, int64, UpdateStepInput) (TestStep, error) {
	return sampleStep, s.err
}
func (s *stubAPI) DeleteStep(context.Context, int64, int64) error { return s.err }
func (s *stubAPI) ReorderSteps(_ context.Context, _ int64, ids []int64) ([]TestStep, error) {
	s.gotOrder = ids
	return []TestStep{sampleStep}, s.err
}

func serve(api API, method, target, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	NewHandler(api).Register(mux)
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
		{"GET", "/api/v1/test-cases/1", ""},
		{"PATCH", "/api/v1/test-cases/1", `{"title":"a"}`},
		{"POST", "/api/v1/test-cases/1/deprecate", ""},
		{"POST", "/api/v1/test-cases/1/reactivate", ""},
		{"GET", "/api/v1/test-cases/1/steps", ""},
		{"POST", "/api/v1/test-cases/1/steps", `{"action":"a"}`},
		{"PUT", "/api/v1/test-cases/1/steps/order", `{"stepIds":[]}`},
		{"PATCH", "/api/v1/test-cases/1/steps/2", `{"action":"a"}`},
		{"DELETE", "/api/v1/test-cases/1/steps/2", ""},
	}
	for _, r := range requests {
		rec := serve(notFound, r.method, r.target, r.body)
		assert.Equal(t, http.StatusNotFound, rec.Code, "%s %s", r.method, r.target)
		var p map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
		assert.Equal(t, "not_found", p["code"])
	}

	badRequests := []struct{ method, target, body string }{
		{"GET", "/api/v1/test-cases?page=0", ""},
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
	}
	for _, r := range badRequests {
		rec := serve(&stubAPI{}, r.method, r.target, r.body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "%s %s", r.method, r.target)
	}
}
