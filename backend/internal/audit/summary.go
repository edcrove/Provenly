package audit

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Resolver names what a change touched, for a readable entry (ports onto the catalog: project and test case keys,
// step positions). Lookups that fail fall back to the ids in the path.
type Resolver interface {
	ProjectKey(ctx context.Context, id int64) (string, error)
	TestCaseKey(ctx context.Context, id int64) (string, error)
	StepPosition(ctx context.Context, testCaseID, stepID int64) (int32, error)
}

// summaries says in words what each audited operation did. {name} is the route's path value of that name, made
// readable: {testCaseId} is the test case key (CHK-4), {stepId} "step N", {testRunId} "run #N".
var summaries = map[string]string{
	"POST /api/v1/test-cases":                                                         "created a test case",
	"PATCH /api/v1/test-cases/{testCaseId}":                                           "edited {testCaseId}",
	"POST /api/v1/test-cases/{testCaseId}/deprecate":                                  "deprecated {testCaseId}",
	"POST /api/v1/test-cases/{testCaseId}/reactivate":                                 "reactivated {testCaseId}",
	"POST /api/v1/test-cases/{testCaseId}/steps":                                      "added a step to {testCaseId}",
	"PATCH /api/v1/test-cases/{testCaseId}/steps/{stepId}":                            "edited {testCaseId} {stepId}",
	"DELETE /api/v1/test-cases/{testCaseId}/steps/{stepId}":                           "deleted {testCaseId} {stepId}",
	"PUT /api/v1/test-cases/{testCaseId}/steps/order":                                 "reordered the steps of {testCaseId}",
	"POST /api/v1/test-runs/manual":                                                   "started a manual run",
	"POST /api/v1/test-runs/live":                                                     "started a live run",
	"POST /api/v1/test-runs/{testRunId}/amendments":                                   "included a test case in {testRunId}",
	"POST /api/v1/test-runs/{testRunId}/finish":                                       "finished {testRunId}",
	"POST /api/v1/test-runs/{testRunId}/manual-results":                               "recorded a manual result in {testRunId}",
	"POST /api/v1/ingestion/junit":                                                    "uploaded a JUnit report",
	"POST /api/v1/ingestion/finalize":                                                 "finalized a sharded run",
	"POST /api/v1/projects":                                                           "created a project",
	"PATCH /api/v1/projects/{projectKey}":                                             "edited the project",
	"PUT /api/v1/projects/{projectKey}/members/{username}":                            "set the role of {username}",
	"DELETE /api/v1/projects/{projectKey}/members/{username}":                         "removed {username} from the project",
	"POST /api/v1/projects/{projectKey}/api-keys":                                     "created an API key",
	"POST /api/v1/projects/{projectKey}/api-keys/{apiKeyId}/revoke":                   "revoked API key #{apiKeyId}",
	"POST /api/v1/projects/{projectKey}/dimensions":                                   "created a dimension",
	"PATCH /api/v1/projects/{projectKey}/dimensions/{dimensionKey}":                   "edited dimension {dimensionKey}",
	"POST /api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values":             "added a value to dimension {dimensionKey}",
	"PATCH /api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values/{valueKey}": "edited value {valueKey} of dimension {dimensionKey}",
	"POST /api/v1/projects/{projectKey}/suites":                                       "created a suite",
	"PATCH /api/v1/projects/{projectKey}/suites/{suiteKey}":                           "edited suite {suiteKey}",
	"PUT /api/v1/projects/{projectKey}/suites/{suiteKey}/cases":                       "set the test cases of suite {suiteKey}",
	"POST /api/v1/projects/{projectKey}/requirements":                                 "created a requirement",
	"POST /api/v1/projects/{projectKey}/requirements/import":                          "imported requirements",
	"PATCH /api/v1/projects/{projectKey}/requirements/{requirementId}":                "edited requirement #{requirementId}",
	"PUT /api/v1/projects/{projectKey}/requirements/{requirementId}/test-cases":       "linked test cases to requirement #{requirementId}",
	"POST /api/v1/projects/{projectKey}/issues":                                       "created an issue",
	"POST /api/v1/projects/{projectKey}/issues/import":                                "imported issues",
	"PATCH /api/v1/projects/{projectKey}/issues/{issueId}":                            "edited issue #{issueId}",
	"PUT /api/v1/projects/{projectKey}/issues/{issueId}/test-cases":                   "linked test cases to issue #{issueId}",
	"POST /api/v1/projects/{projectKey}/webhooks":                                     "created a webhook",
	"PATCH /api/v1/projects/{projectKey}/webhooks/{webhookId}":                        "edited webhook #{webhookId}",
	"POST /api/v1/projects/{projectKey}/webhooks/{webhookId}/ping":                    "pinged webhook #{webhookId}",
	"PUT /api/v1/projects/{projectKey}/github":                                        "connected GitHub",
	"DELETE /api/v1/projects/{projectKey}/github":                                     "disconnected GitHub",
	"POST /api/v1/projects/{projectKey}/github/sync":                                  "synchronized GitHub",
	"POST /api/v1/auth/logout":                                                        "signed out",
	"POST /api/v1/auth/password":                                                      "changed their password",
	"POST /api/v1/invitations":                                                        "created an invitation",
	"POST /api/v1/invitations/{invitationId}/revoke":                                  "revoked invitation #{invitationId}",
	"POST /api/v1/users/{username}/deactivate":                                        "deactivated {username}",
	"POST /api/v1/users/{username}/reactivate":                                        "reactivated {username}",
	"POST /api/v1/users/{username}/password-reset":                                    "made a password reset link for {username}",
}

// target is what a change touched, resolved before the change runs (a deleted step has no position afterwards).
type target struct {
	values      map[string]string
	testCaseKey string
}

// resolve reads the path values a summary names and makes the test case and step readable.
func (s *Service) resolve(r *http.Request, pattern string) target {
	t := target{values: map[string]string{}}
	tmpl, ok := summaries[pattern]
	if !ok {
		return t
	}
	for _, name := range placeholders(tmpl) {
		t.values[name] = r.PathValue(name)
	}
	if raw, ok := t.values["testRunId"]; ok {
		t.values["testRunId"] = "run #" + raw
	}
	tc, err := strconv.ParseInt(t.values["testCaseId"], 10, 64)
	if raw, ok := t.values["testCaseId"]; ok {
		t.values["testCaseId"] = "#" + raw
		if err == nil && s.resolver != nil {
			if key, kerr := s.resolver.TestCaseKey(r.Context(), tc); kerr == nil {
				t.values["testCaseId"], t.testCaseKey = key, key
			}
		}
	}
	if raw, ok := t.values["stepId"]; ok {
		t.values["stepId"] = "a step"
		step, serr := strconv.ParseInt(raw, 10, 64)
		if err == nil && serr == nil && s.resolver != nil {
			if pos, perr := s.resolver.StepPosition(r.Context(), tc, step); perr == nil {
				t.values["stepId"] = fmt.Sprintf("step %d", pos)
			}
		}
	}
	return t
}

// summary is the readable form of an operation ("edited CHK-4 step 3"); empty for an unknown one.
func (t target) summary(pattern string) string {
	out := summaries[pattern]
	for name, v := range t.values {
		out = strings.ReplaceAll(out, "{"+name+"}", v)
	}
	return out
}

// placeholders lists the {names} of a summary template.
func placeholders(tmpl string) []string {
	var out []string
	for rest := tmpl; ; {
		i := strings.IndexByte(rest, '{')
		if i < 0 {
			return out
		}
		j := strings.IndexByte(rest[i:], '}')
		out = append(out, rest[i+1:i+j])
		rest = rest[i+j+1:]
	}
}
