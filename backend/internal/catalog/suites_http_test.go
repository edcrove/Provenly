package catalog

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
)

var (
	sampleStatic = Suite{ID: 5, ProjectID: 1, Key: "release", Name: "Release", Kind: SuiteKindStatic, CaseCount: 2, CaseIDs: []int64{153, 154},
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	sampleQuery = Suite{ID: 6, ProjectID: 1, Key: "smoke", Name: "Smoke", Kind: SuiteKindQuery,
		Query: SuiteQuery{Tag: ptr("smoke"), Classified: []string{"risk:critical"}}}
)

// suiteCall records the last suite call of the stub.
type suiteCall struct {
	projectID int64
	key       string
	in        SuiteInput
	update    UpdateSuiteInput
	ids       []int64
}

var lastSuiteCall suiteCall

func (s *stubAPI) Suites(_ context.Context, projectID int64) ([]Suite, error) {
	lastSuiteCall = suiteCall{projectID: projectID}
	return []Suite{sampleStatic, sampleQuery}, s.err
}
func (s *stubAPI) Suite(_ context.Context, projectID int64, key string) (Suite, error) {
	lastSuiteCall = suiteCall{projectID: projectID, key: key}
	if key == "smoke" {
		return sampleQuery, s.err
	}
	return sampleStatic, s.err
}
func (s *stubAPI) CreateSuite(_ context.Context, projectID int64, in SuiteInput) (Suite, error) {
	lastSuiteCall = suiteCall{projectID: projectID, in: in}
	return sampleStatic, s.err
}
func (s *stubAPI) UpdateSuite(_ context.Context, projectID int64, key string, in UpdateSuiteInput) (Suite, error) {
	lastSuiteCall = suiteCall{projectID: projectID, key: key, update: in}
	return sampleQuery, s.err
}
func (s *stubAPI) SetSuiteCases(_ context.Context, projectID int64, key string, ids []int64) (Suite, error) {
	lastSuiteCall = suiteCall{projectID: projectID, key: key, ids: ids}
	return sampleStatic, s.err
}

func TestSuiteHandlers(t *testing.T) {
	cases := []struct {
		method, target, body string
		status               int
		contains             string
		want                 suiteCall
	}{
		{"GET", "/api/v1/projects/TC/suites", "", 200, `"items":[{"key":"release","name":"Release","description":"","kind":"static","query":null`, suiteCall{projectID: 1}},
		{"POST", "/api/v1/projects/TC/suites", `{"key":"q","name":"Q","kind":"query","query":{"tag":"smoke","classification":["risk:high"]}}`, 201, `"testCaseIds":[153,154]`,
			suiteCall{projectID: 1, in: SuiteInput{Key: "q", Name: "Q", Kind: SuiteKindQuery, Query: SuiteQuery{Tag: ptr("smoke"), Classified: []string{"risk:high"}}}}},
		{"POST", "/api/v1/projects/TC/suites", `{"key":"r","name":"R","kind":"static","testCaseIds":[1]}`, 201, `"caseCount":2`,
			suiteCall{projectID: 1, in: SuiteInput{Key: "r", Name: "R", Kind: SuiteKindStatic, TestCaseIDs: []int64{1}}}},
		{"GET", "/api/v1/projects/TC/suites/smoke", "", 200, `"query":{"tag":"smoke","classification":["risk:critical"]}`, suiteCall{projectID: 1, key: "smoke"}},
		{"PATCH", "/api/v1/projects/TC/suites/smoke", `{"archived":true,"query":{"tag":null,"classification":["risk:low"]}}`, 200, `"kind":"query"`,
			suiteCall{projectID: 1, key: "smoke", update: UpdateSuiteInput{Archived: ptr(true), Query: &SuiteQuery{Classified: []string{"risk:low"}}}}},
		{"PUT", "/api/v1/projects/TC/suites/release/cases", `{"testCaseIds":[]}`, 200, `"key":"release"`, suiteCall{projectID: 1, key: "release", ids: []int64{}}},
	}
	for _, c := range cases {
		rec := serve(&stubAPI{}, c.method, c.target, c.body)
		assert.Equal(t, c.status, rec.Code, "%s %s", c.method, c.target)
		assert.Contains(t, rec.Body.String(), c.contains, "%s %s", c.method, c.target)
		assert.Equal(t, c.want, lastSuiteCall, "%s %s", c.method, c.target)
		assert.Equal(t, http.StatusNotFound, serve(&stubAPI{err: apperr.NotFound("x")}, c.method, c.target, c.body).Code, "%s %s", c.method, c.target)
	}
	assert.NotContains(t, serve(&stubAPI{}, "GET", "/api/v1/projects/TC/suites", "").Body.String(), "testCaseIds", "lists leave members out")

	for _, b := range []struct{ method, target, body, field string }{
		{"GET", "/api/v1/projects/tc/suites", "", "projectKey"},
		{"GET", "/api/v1/projects/TC/suites?page=0", "", "page"},
		{"GET", "/api/v1/projects/TC/suites/Bad", "", "suiteKey"},
		{"PATCH", "/api/v1/projects/TC/suites/Bad", `{"name":"x"}`, "suiteKey"},
		{"PUT", "/api/v1/projects/TC/suites/Bad/cases", `{"testCaseIds":[]}`, "suiteKey"},
		{"PUT", "/api/v1/projects/TC/suites/release/cases", `{}`, "testCaseIds"},
		{"POST", "/api/v1/projects/TC/suites", `nope`, ""},
		{"PATCH", "/api/v1/projects/TC/suites/x", `nope`, ""},
		{"PUT", "/api/v1/projects/TC/suites/x/cases", `{"testCaseIds":"x"}`, ""},
	} {
		rec := serve(&stubAPI{}, b.method, b.target, b.body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "%s %s", b.method, b.target)
		assert.Contains(t, rec.Body.String(), b.field, "%s %s", b.method, b.target)
	}
}

func TestSuiteAuthorization(t *testing.T) {
	as := func(role authz.Role) authz.Guard { return memberOf(map[int64]authz.Role{1: role}) }
	for _, c := range []struct {
		method, target, body string
		min                  authz.Role
		success              int
	}{
		{"GET", "/api/v1/projects/TC/suites", "", authz.RoleViewer, 200},
		{"GET", "/api/v1/projects/TC/suites/release", "", authz.RoleViewer, 200},
		{"POST", "/api/v1/projects/TC/suites", `{"key":"a","name":"A","kind":"static"}`, authz.RoleMaintainer, 201},
		{"PATCH", "/api/v1/projects/TC/suites/release", `{"name":"R"}`, authz.RoleMaintainer, 200},
		{"PUT", "/api/v1/projects/TC/suites/release/cases", `{"testCaseIds":[]}`, authz.RoleMaintainer, 200},
	} {
		assert.Equal(t, http.StatusNotFound, serveAs(memberOf(nil), &stubAPI{}, c.method, c.target, c.body).Code, c.target)
		for r := authz.RoleViewer; r <= authz.RoleMaintainer; r++ {
			want := c.success
			if r < c.min {
				want = http.StatusForbidden
			}
			assert.Equal(t, want, serveAs(as(r), &stubAPI{}, c.method, c.target, c.body).Code, "%s %s as %s", c.method, c.target, r)
		}
	}
}

// ?suite=<key> narrows the test case list to a suite of ?project=.
func TestTestCaseListBySuite(t *testing.T) {
	api := &stubAPI{}
	require.Equal(t, http.StatusOK, serve(api, "GET", "/api/v1/test-cases?project=TC&suite=release", "").Code)
	assert.Equal(t, ptr(int64(5)), api.gotFilter.SuiteID)
	serve(api, "GET", "/api/v1/test-cases?project=TC&suite=smoke", "")
	assert.Equal(t, ptr("smoke"), api.gotFilter.Tag)
	assert.Equal(t, []string{"risk:critical"}, api.gotFilter.Classified)
	for _, q := range []string{"suite=release", "project=TC&suite=", "project=TC&suite=Bad", "project=TC&suite=release&tag=a", "project=TC&suite=release&classification=a:b"} {
		assert.Equal(t, http.StatusBadRequest, serve(api, "GET", "/api/v1/test-cases?"+q, "").Code, q)
	}
	assert.Equal(t, http.StatusNotFound, serve(&stubAPI{err: apperr.NotFound("suite x not found")}, "GET", "/api/v1/test-cases?project=TC&suite=x", "").Code)
}

func TestSuiteDTONeverNullLists(t *testing.T) {
	dto := suiteDTO(Suite{Kind: SuiteKindQuery, Query: SuiteQuery{Tag: ptr("a")}}, true)
	assert.Equal(t, []string{}, dto.Query.Classification)
}
