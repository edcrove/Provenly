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

var sampleRun = int64(9)

var sampleIssue = IssueView{
	Issue: Issue{ID: 5, ProjectID: 1, Provider: ProviderJira, ExternalID: "PAY-7", Title: "Double refund", State: IssueOpen, TestCaseIDs: []int64{153, 154},
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	Verification: Verification{Status: VerificationKnownIssue, Links: []LinkVerification{
		{TestCaseID: 153, Status: VerificationKnownIssue, Evidence: "failed", EvidenceRunID: &sampleRun},
		{TestCaseID: 154, Status: VerificationUnverified, LatestInconclusive: true},
	}},
}

// issueCall records the last issue call of the stub.
type issueCall struct {
	projectID int64
	id        int64
	filter    IssueFilter
	provider  string
	in        IssueInput
	items     []IssueInput
	update    UpdateIssueInput
	ids       []int64
}

var lastIssueCall issueCall

func (s *stubAPI) Issues(_ context.Context, projectID int64, f IssueFilter) ([]IssueView, error) {
	lastIssueCall = issueCall{projectID: projectID, filter: f}
	return []IssueView{sampleIssue}, s.err
}
func (s *stubAPI) Issue(_ context.Context, projectID, id int64) (IssueView, error) {
	lastIssueCall = issueCall{projectID: projectID, id: id}
	return sampleIssue, s.err
}
func (s *stubAPI) CreateIssue(_ context.Context, projectID int64, in IssueInput) (IssueView, error) {
	lastIssueCall = issueCall{projectID: projectID, in: in}
	return sampleIssue, s.err
}
func (s *stubAPI) ImportIssues(_ context.Context, projectID int64, provider string, items []IssueInput) (ImportResult, error) {
	lastIssueCall = issueCall{projectID: projectID, provider: provider, items: items}
	return ImportResult{Created: 2, Updated: 0}, s.err
}
func (s *stubAPI) UpdateIssue(_ context.Context, projectID, id int64, in UpdateIssueInput) (IssueView, error) {
	lastIssueCall = issueCall{projectID: projectID, id: id, update: in}
	return sampleIssue, s.err
}
func (s *stubAPI) SetIssueTestCases(_ context.Context, projectID, id int64, ids []int64) (IssueView, error) {
	lastIssueCall = issueCall{projectID: projectID, id: id, ids: ids}
	return sampleIssue, s.err
}

func TestIssueHandlers(t *testing.T) {
	base := "/api/v1/projects/TC/issues"
	cases := []struct {
		method, target, body string
		status               int
		contains             string
		want                 issueCall
	}{
		{"GET", base, "", 200, `"items":[{"id":5,"provider":"jira","externalId":"PAY-7"`, issueCall{projectID: 1}},
		{"GET", base + "?testCase=153&state=open", "", 200,
			`"verification":{"status":"known_issue","testCases":[{"testCaseId":153,"testCaseKey":null,"status":"known_issue","evidence":"failed","evidenceRunId":9,"latestInconclusive":false},{"testCaseId":154,"testCaseKey":null,"status":"unverified","evidence":null,"evidenceRunId":null,"latestInconclusive":true}]}`,
			issueCall{projectID: 1, filter: IssueFilter{TestCaseID: ptr(int64(153)), State: ptr("open")}}},
		{"POST", base, `{"title":"Crash","provider":"github","externalId":"4","url":"https://x","description":"d","state":"closed","providerStatus":"done"}`, 201, `"closedAt":null`,
			issueCall{projectID: 1, in: IssueInput{Title: "Crash", Provider: "github", ExternalID: "4", URL: "https://x", Description: "d", State: "closed", ProviderStatus: "done"}}},
		{"POST", base + "/import", `{"provider":"jira","items":[{"externalId":"PAY-1","title":"A","description":"d","url":"","state":"open","providerStatus":"To do"}]}`, 200, `{"created":2,"updated":0}`,
			issueCall{projectID: 1, provider: "jira", items: []IssueInput{{ExternalID: "PAY-1", Title: "A", Description: "d", State: "open", ProviderStatus: "To do"}}}},
		{"GET", base + "/5", "", 200, `"state":"open"`, issueCall{projectID: 1, id: 5}},
		{"PATCH", base + "/5", `{"state":"closed"}`, 200, `"testCaseIds":[153,154]`, issueCall{projectID: 1, id: 5, update: UpdateIssueInput{State: ptr("closed")}}},
		{"PUT", base + "/5/test-cases", `{"testCaseIds":[153]}`, 200, `"id":5`, issueCall{projectID: 1, id: 5, ids: []int64{153}}},
	}
	for _, c := range cases {
		rec := serve(&stubAPI{}, c.method, c.target, c.body)
		assert.Equal(t, c.status, rec.Code, "%s %s", c.method, c.target)
		assert.Contains(t, rec.Body.String(), c.contains, "%s %s", c.method, c.target)
		assert.Equal(t, c.want, lastIssueCall, "%s %s", c.method, c.target)
		assert.Equal(t, http.StatusNotFound, serve(&stubAPI{err: apperr.NotFound("x")}, c.method, c.target, c.body).Code, "%s %s", c.method, c.target)
	}
	for _, b := range []struct{ method, target, body, field string }{
		{"GET", base + "?testCase=x", "", "testCase"},
		{"GET", base + "?testCase=0", "", "testCase"},
		{"GET", base + "?pageSize=101", "", "pageSize"},
		{"GET", "/api/v1/projects/tc/issues", "", "projectKey"},
		{"GET", base + "/x", "", "issueId"},
		{"PATCH", base + "/0", `{"title":"x"}`, "issueId"},
		{"PUT", base + "/5/test-cases", `{}`, "testCaseIds"},
		{"POST", base, `nope`, ""},
		{"POST", base + "/import", `nope`, ""},
		{"PATCH", base + "/5", `nope`, ""},
		{"PUT", base + "/5/test-cases", `nope`, ""},
	} {
		rec := serve(&stubAPI{}, b.method, b.target, b.body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "%s %s", b.method, b.target)
		assert.Contains(t, rec.Body.String(), b.field, "%s %s", b.method, b.target)
	}
}

func TestIssueAuthorization(t *testing.T) {
	as := func(role authz.Role) authz.Guard { return memberOf(map[int64]authz.Role{1: role}) }
	base := "/api/v1/projects/TC/issues"
	for _, c := range []struct {
		method, target, body string
		min                  authz.Role
		success              int
	}{
		{"GET", base, "", authz.RoleViewer, 200},
		{"GET", base + "/5", "", authz.RoleViewer, 200},
		{"POST", base, `{"title":"x"}`, authz.RoleMember, 201},
		{"PATCH", base + "/5", `{"title":"x"}`, authz.RoleMember, 200},
		{"PUT", base + "/5/test-cases", `{"testCaseIds":[]}`, authz.RoleMember, 200},
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
