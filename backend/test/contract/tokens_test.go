//go:build contract

package contract

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Personal access tokens (card #62): every answer checked against the OpenAPI contract.
func TestPersonalAccessTokens(t *testing.T) {
	s := fresh(t)
	admin := api(t, s, 1<<20)
	e := anon(t, s, 1<<20)
	admin.POST("/api/v1/projects").WithJSON(map[string]any{"key": "CHK", "name": "Checkout"}).Expect().Status(http.StatusCreated)

	created := admin.POST("/api/v1/auth/tokens").WithJSON(map[string]any{"name": "MCP", "projects": []string{"CHK"}}).
		Expect().Status(http.StatusCreated).JSON().Object()
	created.Value("personalAccessToken").Object().HasValue("status", "active").HasValue("lastUsedAt", nil).
		HasValue("projects", []string{"CHK"})
	pat := as(e, created.Value("token").String().Raw())

	pat.GET("/api/v1/projects/CHK/suites").Expect().Status(http.StatusOK)
	pat.GET("/api/v1/projects/TC/suites").Expect().Status(http.StatusForbidden).JSON(problemOpts).Object().HasValue("code", "forbidden")
	pat.GET("/api/v1/users").Expect().Status(http.StatusForbidden)
	pat.POST("/api/v1/projects/CHK/suites").WithJSON(map[string]any{"key": "x", "name": "x", "kind": "static"}).
		Expect().Status(http.StatusForbidden).JSON(problemOpts).Object().HasValue("code", "forbidden")
	pat.POST("/api/v1/auth/tokens").WithJSON(map[string]any{"name": "x", "projects": []string{"CHK"}}).Expect().Status(http.StatusForbidden)

	// Every change, whatever the route, is refused to a token (MCP, whose tools only read, aside).
	params := strings.NewReplacer("{testCaseId}", "1", "{testRunId}", "1", "{stepId}", "1", "{projectKey}", "CHK", "{invitationId}", "1",
		"{username}", "admin", "{apiKeyId}", "1", "{dimensionKey}", "risk", "{valueKey}", "low", "{suiteKey}", "smoke",
		"{requirementId}", "1", "{issueId}", "1", "{webhookId}", "1", "{tokenId}", "1")
	writes := 0
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			if method == http.MethodGet || op.Responses.Value("401") == nil || op.OperationID == "login" || op.OperationID == "mcp" {
				continue
			}
			writes++
			pat.Request(method, params.Replace(path)).WithJSON(map[string]any{}).Expect().
				Status(http.StatusForbidden).JSON(problemOpts).Object().HasValue("code", "forbidden")
		}
	}
	assert.Greater(t, writes, 40)

	// Every read of a project the token does not cover is a 403, whatever the route (data in TC; the token covers CHK).
	tc := int64(admin.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "outside", "automated": true}).
		Expect().Status(http.StatusCreated).JSON().Object().Value("id").Number().Raw())
	run := int64(ingest(admin, "pat", 1, `<testsuite><testcase name="t"/></testsuite>`).Expect().Status(http.StatusCreated).
		JSON().Object().Value("testRun").Object().Value("id").Number().Raw())
	admin.POST("/api/v1/projects/TC/suites").WithJSON(map[string]any{"key": "smoke", "name": "Smoke", "kind": "static"}).Expect().Status(http.StatusCreated)
	req := int64(admin.POST("/api/v1/projects/TC/requirements").WithJSON(map[string]any{"title": "outside"}).
		Expect().Status(http.StatusCreated).JSON().Object().Value("id").Number().Raw())
	iss := int64(admin.POST("/api/v1/projects/TC/issues").WithJSON(map[string]any{"title": "outside"}).
		Expect().Status(http.StatusCreated).JSON().Object().Value("id").Number().Raw())
	ids := strings.NewReplacer("{testCaseId}", strconv.FormatInt(tc, 10), "{testRunId}", strconv.FormatInt(run, 10),
		"{projectKey}", "TC", "{suiteKey}", "smoke", "{requirementId}", strconv.FormatInt(req, 10), "{issueId}", strconv.FormatInt(iss, 10),
		"{webhookId}", "1", "{dimensionKey}", "risk", "{username}", "admin", "{invitationId}", "1")
	reads := 0
	for path, item := range doc.Paths.Map() {
		op := item.Get
		if op == nil || op.Responses.Value("403") == nil || strings.Contains(path, "/deliveries") || strings.Contains(path, "/github") {
			continue
		}
		reads++
		r := pat.GET(ids.Replace(path))
		if path == "/api/v1/test-cases" || path == "/api/v1/test-runs" {
			r = r.WithQuery("project", "TC")
		}
		r.Expect().Status(http.StatusForbidden).JSON(problemOpts).Object().HasValue("code", "forbidden")
	}
	assert.Greater(t, reads, 20)

	admin.GET("/api/v1/auth/tokens").Expect().Status(http.StatusOK).JSON().Object().
		Value("items").Array().Value(0).Object().Value("lastUsedAt").String().NotEmpty()
	admin.GET("/api/v1/auth/tokens").WithQuery("page", 0).Expect().Status(http.StatusBadRequest)
	for _, body := range []map[string]any{
		{"name": "x", "projects": []string{}},
		{"name": "x", "projects": []string{"NOPE"}},
		{"name": " ", "projects": []string{"CHK"}},
		{"name": "x", "projects": []string{"CHK"}, "expiresInDays": 366},
		{"name": "x", "projects": []string{"CHK"}, "expiresInDays": 0},
	} {
		admin.POST("/api/v1/auth/tokens").WithJSON(body).Expect().Status(http.StatusBadRequest)
	}
	admin.POST("/api/v1/auth/tokens").WithText(`{"name":"x","projects":["CHK"]}`).Expect().Status(http.StatusUnsupportedMediaType)

	admin.POST("/api/v1/auth/tokens/1/revoke").Expect().Status(http.StatusOK).JSON().Object().HasValue("status", "revoked")
	admin.POST("/api/v1/auth/tokens/1/revoke").Expect().Status(http.StatusConflict)
	admin.POST("/api/v1/auth/tokens/99/revoke").Expect().Status(http.StatusNotFound)
	admin.POST("/api/v1/auth/tokens/0/revoke").Expect().Status(http.StatusBadRequest)
	pat.GET("/api/v1/projects/CHK/suites").Expect().Status(http.StatusUnauthorized).JSON(problemOpts).Object().HasValue("code", "unauthorized")
	e.GET("/api/v1/auth/tokens").Expect().Status(http.StatusUnauthorized)
}
