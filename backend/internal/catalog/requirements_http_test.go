package catalog

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
)

var sampleRequirement = RequirementView{
	Requirement: Requirement{ID: 4, ProjectID: 1, Provider: ProviderJira, ExternalID: "PAY-12", Title: "Refunds", TestCaseIDs: []int64{153, 154},
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	Coverage: Coverage{Status: CoveragePartial, Linked: 2, Passed: 1, NotRun: 1, Latest: map[int64]string{153: "passed", 154: ""}},
	Keys:     map[int64]string{153: "TC-153"},
}

// requirementCall records the last requirement call of the stub.
type requirementCall struct {
	projectID int64
	id        int64
	testCase  *int64
	provider  string
	in        RequirementInput
	items     []RequirementInput
	update    UpdateRequirementInput
	ids       []int64
}

var lastRequirementCall requirementCall

func (s *stubAPI) Requirements(_ context.Context, projectID int64, testCaseID *int64) ([]RequirementView, error) {
	lastRequirementCall = requirementCall{projectID: projectID, testCase: testCaseID}
	return []RequirementView{sampleRequirement}, s.err
}
func (s *stubAPI) Requirement(_ context.Context, projectID, id int64) (RequirementView, error) {
	lastRequirementCall = requirementCall{projectID: projectID, id: id}
	return sampleRequirement, s.err
}
func (s *stubAPI) CreateRequirement(_ context.Context, projectID int64, in RequirementInput) (RequirementView, error) {
	lastRequirementCall = requirementCall{projectID: projectID, in: in}
	return sampleRequirement, s.err
}
func (s *stubAPI) ImportRequirements(_ context.Context, projectID int64, provider string, items []RequirementInput) (ImportResult, error) {
	lastRequirementCall = requirementCall{projectID: projectID, provider: provider, items: items}
	return ImportResult{Created: 1, Updated: 2}, s.err
}
func (s *stubAPI) UpdateRequirement(_ context.Context, projectID, id int64, in UpdateRequirementInput) (RequirementView, error) {
	lastRequirementCall = requirementCall{projectID: projectID, id: id, update: in}
	return sampleRequirement, s.err
}
func (s *stubAPI) SetRequirementTestCases(_ context.Context, projectID, id int64, ids []int64) (RequirementView, error) {
	lastRequirementCall = requirementCall{projectID: projectID, id: id, ids: ids}
	return sampleRequirement, s.err
}

func TestRequirementHandlers(t *testing.T) {
	base := "/api/v1/projects/TC/requirements"
	cases := []struct {
		method, target, body string
		status               int
		contains             string
		want                 requirementCall
	}{
		{"GET", base, "", 200, `"items":[{"id":4,"provider":"jira","externalId":"PAY-12"`, requirementCall{projectID: 1}},
		{"GET", base + "?testCase=153", "", 200, `"coverage":{"status":"partial","linked":2,"passed":1,"failed":0,"notRun":1,"testCases":[{"testCaseId":153,"testCaseKey":"TC-153","status":"passed"},{"testCaseId":154,"testCaseKey":null,"status":null}]}`,
			requirementCall{projectID: 1, testCase: ptr(int64(153))}},
		{"POST", base, `{"title":"Pay","provider":"github","externalId":"4","url":"https://x","description":"d","providerStatus":"open"}`, 201, `"lastSyncedAt":null`,
			requirementCall{projectID: 1, in: RequirementInput{Title: "Pay", Provider: "github", ExternalID: "4", URL: "https://x", Description: "d", ProviderStatus: "open"}}},
		{"POST", base + "/import", `{"provider":"jira","items":[{"externalId":"PAY-1","title":"A","description":"d","url":"","providerStatus":"Done"}]}`, 200, `{"created":1,"updated":2}`,
			requirementCall{projectID: 1, provider: "jira", items: []RequirementInput{{ExternalID: "PAY-1", Title: "A", Description: "d", ProviderStatus: "Done"}}}},
		{"GET", base + "/4", "", 200, `"title":"Refunds"`, requirementCall{projectID: 1, id: 4}},
		{"PATCH", base + "/4", `{"archived":true}`, 200, `"testCaseIds":[153,154]`, requirementCall{projectID: 1, id: 4, update: UpdateRequirementInput{Archived: ptr(true)}}},
		{"PUT", base + "/4/test-cases", `{"testCaseIds":[153]}`, 200, `"id":4`, requirementCall{projectID: 1, id: 4, ids: []int64{153}}},
	}
	for _, c := range cases {
		rec := serve(&stubAPI{}, c.method, c.target, c.body)
		assert.Equal(t, c.status, rec.Code, "%s %s", c.method, c.target)
		assert.Contains(t, rec.Body.String(), c.contains, "%s %s", c.method, c.target)
		assert.Equal(t, c.want, lastRequirementCall, "%s %s", c.method, c.target)
		assert.Equal(t, http.StatusNotFound, serve(&stubAPI{err: apperr.NotFound("x")}, c.method, c.target, c.body).Code, "%s %s", c.method, c.target)
	}
	for _, b := range []struct{ method, target, body, field string }{
		{"GET", base + "?testCase=x", "", "testCase"},
		{"GET", base + "?testCase=0", "", "testCase"},
		{"GET", base + "?page=0", "", "page"},
		{"GET", "/api/v1/projects/tc/requirements", "", "projectKey"},
		{"GET", base + "/x", "", "requirementId"},
		{"PATCH", base + "/0", `{"title":"x"}`, "requirementId"},
		{"PUT", base + "/4/test-cases", `{}`, "testCaseIds"},
		{"POST", base, `nope`, ""},
		{"POST", base + "/import", `nope`, ""},
		{"PATCH", base + "/4", `nope`, ""},
		{"PUT", base + "/4/test-cases", `nope`, ""},
	} {
		rec := serve(&stubAPI{}, b.method, b.target, b.body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "%s %s", b.method, b.target)
		assert.Contains(t, rec.Body.String(), b.field, "%s %s", b.method, b.target)
	}
}

func TestRequirementAuthorization(t *testing.T) {
	as := func(role authz.Role) authz.Guard { return memberOf(map[int64]authz.Role{1: role}) }
	base := "/api/v1/projects/TC/requirements"
	for _, c := range []struct {
		method, target, body string
		min                  authz.Role
		success              int
	}{
		{"GET", base, "", authz.RoleViewer, 200},
		{"GET", base + "/4", "", authz.RoleViewer, 200},
		{"POST", base, `{"title":"x"}`, authz.RoleMember, 201},
		{"PATCH", base + "/4", `{"title":"x"}`, authz.RoleMember, 200},
		{"PUT", base + "/4/test-cases", `{"testCaseIds":[]}`, authz.RoleMember, 200},
		{"POST", base + "/import", `{"provider":"jira","items":[]}`, authz.RoleMaintainer, 200},
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
