//go:build contract

package contract

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gavv/httpexpect/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/app"
	"github.com/edcrove/provenly/backend/internal/platform/postgres"
	"github.com/edcrove/provenly/backend/internal/platform/telemetry"
)

const xmlType = "application/xml"

func report(tcID int64) string {
	id := strconv.FormatInt(tcID, 10)
	return `<testsuites><testsuite name="s" timestamp="2026-09-28T10:00:00">
<testcase name="chrome"><properties><property name="tc-id" value="` + id + `"/></properties></testcase>
<testcase name="firefox TC-` + id + `"><failure message="boom">trace</failure></testcase>
<testcase name="no id"/><testcase name="bad TC-x"/><testcase name="ghost TC-987654"/><testcase name=""/>
</testsuite></testsuites>`
}

func ingest(e *httpexpect.Expect, runID string, attempt int, body string) *httpexpect.Request {
	return e.POST("/api/v1/ingestion/junit").
		WithQuery("provider", "github").WithQuery("runId", runID).WithQuery("runAttempt", attempt).
		WithQuery("pipeline", "ci").WithQuery("branch", "main").WithQuery("commit", "abc").
		WithHeader("Content-Type", xmlType).WithText(body)
}

// TestRoutesMatchContract keeps the contract the authoritative inventory: every
// route the API registers is an operation of the spec, and vice versa.
func TestRoutesMatchContract(t *testing.T) {
	var spec []string
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			spec = append(spec, method+" "+path)
		}
	}
	require.ElementsMatch(t, spec, app.RoutePatterns(),
		"router and api/openapi.yaml disagree: add the missing operations to the contract or remove the extra routes")
}

func TestSystem(t *testing.T) {
	e := api(t, fresh(t), 1<<20)
	e.GET("/healthz").Expect().Status(http.StatusOK).JSON().Object().HasValue("status", "ok")
	e.GET("/readyz").Expect().Status(http.StatusOK).JSON().Object().HasValue("status", "ok")

	down := app.NewServicesWith(db.Pool, time.Now, identityConfig())
	down.Ready = func(context.Context) error { return errors.New("database is down") }
	api(t, down, 1<<20).GET("/readyz").Expect().Status(http.StatusServiceUnavailable).
		JSON(problemOpts).Object().HasValue("code", "service_unavailable")
}

func TestTestCases(t *testing.T) {
	e := api(t, fresh(t), 1<<20)

	tc := e.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "Login", "expectedResult": "dashboard", "automated": true}).
		Expect().Status(http.StatusCreated).JSON().Object()
	id := int64(tc.Value("id").Number().Raw())
	tc.HasValue("key", "TC-"+strconv.FormatInt(id, 10))
	e.POST("/api/v1/test-cases").WithJSON(map[string]any{"id": 5, "title": "x"}).Expect().Status(http.StatusBadRequest)
	e.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": ""}).Expect().Status(http.StatusBadRequest).
		JSON(problemOpts).Object().HasValue("code", "validation_error")

	e.GET("/api/v1/test-cases").WithQuery("status", "active").WithQuery("pageSize", 5).Expect().Status(http.StatusOK).
		JSON().Object().HasValue("totalItems", 1)
	e.GET("/api/v1/test-cases").WithQuery("page", 0).Expect().Status(http.StatusBadRequest)
	// Unknown parameters are ignored; the known ones still filter and paginate.
	e.GET("/api/v1/test-cases").WithQuery("status", "deprecated").WithQuery("pageSize", 5).
		WithQuery("automated", "true").WithQuery("limit", 1).Expect().Status(http.StatusOK).
		JSON().Object().HasValue("totalItems", 0).HasValue("pageSize", 5)
	e.GET("/api/v1/test-cases").WithQuery("pageSize", 0).WithQuery("foo", "bar").Expect().Status(http.StatusBadRequest)

	path := "/api/v1/test-cases/" + strconv.FormatInt(id, 10)
	e.GET(path).Expect().Status(http.StatusOK)
	e.GET("/api/v1/test-cases/abc").Expect().Status(http.StatusBadRequest)
	e.GET("/api/v1/test-cases/987654").Expect().Status(http.StatusNotFound).JSON(problemOpts).Object().HasValue("code", "not_found")

	e.PATCH(path).WithJSON(map[string]any{"title": "Login v2", "automated": true}).Expect().Status(http.StatusOK).
		JSON().Object().HasValue("id", id).HasValue("title", "Login v2")
	e.PATCH(path).WithJSON(map[string]any{}).Expect().Status(http.StatusBadRequest)
	e.PATCH("/api/v1/test-cases/987654").WithJSON(map[string]any{"title": "x"}).Expect().Status(http.StatusNotFound)

	other := e.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "Old"}).Expect().Status(http.StatusCreated).JSON().Object()
	otherPath := "/api/v1/test-cases/" + strconv.FormatInt(int64(other.Value("id").Number().Raw()), 10)
	e.POST(otherPath+"/deprecate").Expect().Status(http.StatusOK).JSON().Object().HasValue("status", "deprecated")
	// Implementation decision #10: a deprecated test case stays editable (content and steps), and stays deprecated.
	e.PATCH(otherPath).WithJSON(map[string]any{"title": "Old, reworded", "automated": true}).Expect().Status(http.StatusOK).
		JSON().Object().HasValue("title", "Old, reworded").HasValue("status", "deprecated").HasValue("automated", true)
	e.POST(otherPath + "/steps").WithJSON(map[string]any{"action": "open the old page"}).Expect().Status(http.StatusCreated)
	e.POST("/api/v1/test-cases/0/deprecate").Expect().Status(http.StatusBadRequest)
	e.POST("/api/v1/test-cases/987654/deprecate").Expect().Status(http.StatusNotFound)
	e.POST(otherPath+"/reactivate").Expect().Status(http.StatusOK).JSON().Object().HasValue("status", "active")
	e.POST("/api/v1/test-cases/0/reactivate").Expect().Status(http.StatusBadRequest)
	e.POST("/api/v1/test-cases/987654/reactivate").Expect().Status(http.StatusNotFound)

	e.GET("/api/v1/test-cases/xyz/results").Expect().Status(http.StatusBadRequest)
	e.GET("/api/v1/test-cases/987654/results").Expect().Status(http.StatusNotFound)
	e.GET(path+"/results").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 0)
}

func TestProjects(t *testing.T) {
	e := api(t, fresh(t), 1<<20)

	e.POST("/api/v1/projects").WithJSON(map[string]any{"key": "chk", "name": "Checkout", "description": "cart"}).
		Expect().Status(http.StatusCreated).JSON().Object().HasValue("key", "CHK").HasValue("name", "Checkout")
	e.POST("/api/v1/projects").WithJSON(map[string]any{"key": "CHK", "name": "again"}).
		Expect().Status(http.StatusConflict).JSON(problemOpts).Object().HasValue("code", "conflict")
	e.POST("/api/v1/projects").WithJSON(map[string]any{"key": "1X", "name": ""}).Expect().Status(http.StatusBadRequest).
		JSON(problemOpts).Object().HasValue("code", "validation_error")
	e.POST("/api/v1/projects").WithText(`{"key":"WEB","name":"w"}`).Expect().Status(http.StatusUnsupportedMediaType)

	e.GET("/api/v1/projects").WithQuery("pageSize", 1).Expect().Status(http.StatusOK).JSON().Object().
		HasValue("totalItems", 2).Value("items").Array().Value(0).Object().HasValue("key", "CHK")
	e.GET("/api/v1/projects").WithQuery("page", 0).Expect().Status(http.StatusBadRequest)
	e.GET("/api/v1/projects/CHK").Expect().Status(http.StatusOK).JSON().Object().HasValue("description", "cart")
	e.GET("/api/v1/projects/chk").Expect().Status(http.StatusBadRequest)
	e.GET("/api/v1/projects/NOPE").Expect().Status(http.StatusNotFound).JSON(problemOpts).Object().HasValue("code", "not_found")
	e.PATCH("/api/v1/projects/CHK").WithJSON(map[string]any{"name": "Checkout v2"}).Expect().Status(http.StatusOK).
		JSON().Object().HasValue("name", "Checkout v2").HasValue("key", "CHK")
	e.PATCH("/api/v1/projects/CHK").WithJSON(map[string]any{"key": "WEB"}).Expect().Status(http.StatusBadRequest)
	e.PATCH("/api/v1/projects/NOPE").WithJSON(map[string]any{"name": "x"}).Expect().Status(http.StatusNotFound)
	e.PATCH("/api/v1/projects/CHK").WithText(`{"name":"x"}`).Expect().Status(http.StatusUnsupportedMediaType)

	// Test cases are numbered per project; ?project= filters lists, an unknown key is a 404.
	e.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "pay", "project": "CHK", "automated": true}).
		Expect().Status(http.StatusCreated).JSON().Object().HasValue("key", "CHK-1").HasValue("projectKey", "CHK").HasValue("number", 1)
	e.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "x", "project": "NOPE"}).Expect().Status(http.StatusNotFound)
	e.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "x", "project": "no"}).Expect().Status(http.StatusBadRequest)
	e.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "default"}).Expect().Status(http.StatusCreated).
		JSON().Object().HasValue("key", "TC-1").HasValue("projectKey", "TC")
	e.GET("/api/v1/test-cases").WithQuery("project", "CHK").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 1)
	e.GET("/api/v1/test-cases").WithQuery("project", "NOPE").Expect().Status(http.StatusNotFound)
	e.GET("/api/v1/test-cases").WithQuery("project", "").Expect().Status(http.StatusBadRequest)

	run := ingest(e, "77", 1, `<testsuite><testcase name="pay CHK-1"/><testcase name="x"><properties><property name="tc-id" value="TC-1"/></properties></testcase></testsuite>`).
		WithQuery("project", "CHK").Expect().Status(http.StatusCreated).JSON().Object()
	run.Value("testRun").Object().HasValue("expectedCount", 1)
	run.Value("diagnostics").Array().Value(0).Object().HasValue("correlation", "wrong_project")
	runID := int64(run.Value("testRun").Object().Value("id").Number().Raw())
	e.GET("/api/v1/test-runs/"+strconv.FormatInt(runID, 10)+"/summary").Expect().Status(http.StatusOK).
		JSON().Object().Value("diagnostics").Object().HasValue("wrongProject", 1)
	e.GET("/api/v1/test-runs/"+strconv.FormatInt(runID, 10)+"/summary").Expect().Status(http.StatusOK).
		JSON().Object().Value("testCases").Array().Value(0).Object().HasValue("testCaseKey", "CHK-1")
	e.GET("/api/v1/test-runs/"+strconv.FormatInt(runID, 10)+"/results").WithQuery("correlation", "valid").Expect().Status(http.StatusOK).
		JSON().Object().Value("items").Array().Value(0).Object().HasValue("testCaseKey", "CHK-1")
	ingest(e, "77", 1, `<testsuite/>`).WithQuery("project", "NOPE").Expect().Status(http.StatusNotFound)
	e.GET("/api/v1/test-runs").WithQuery("project", "CHK").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 1)
	e.GET("/api/v1/test-runs").WithQuery("project", "TC").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 0)
	e.GET("/api/v1/test-runs").WithQuery("project", "NOPE").Expect().Status(http.StatusNotFound)
}

// TestAuthentication: every protected operation answers 401 without a session, sign-in and invitations
// work end to end, and administrator-only operations answer 403 to other users.
func TestAuthentication(t *testing.T) {
	s := fresh(t)
	e := anon(t, s, 1<<20)
	params := strings.NewReplacer("{testCaseId}", "1", "{testRunId}", "1", "{stepId}", "1", "{projectKey}", "TC", "{invitationId}", "1", "{username}", "admin", "{apiKeyId}", "1",
		"{dimensionKey}", "risk", "{valueKey}", "low", "{suiteKey}", "smoke", "{requirementId}", "1", "{issueId}", "1", "{webhookId}", "1")
	protected := 0
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			if op.Responses.Value("401") == nil || op.OperationID == "login" {
				continue
			}
			protected++
			e.Request(method, params.Replace(path)).WithJSON(map[string]any{}).Expect().
				Status(http.StatusUnauthorized).JSON(problemOpts).Object().HasValue("code", "unauthorized")
		}
	}
	assert.Equal(t, 76, protected, "every operation except health, readiness, sign-in, sign-out and accept")
	e.GET("/api/v1/auth/me").WithHeader("Authorization", "Bearer not-a-token").Expect().Status(http.StatusUnauthorized)

	e.POST("/api/v1/auth/login").WithJSON(map[string]any{"username": adminUser, "password": "wrong password"}).
		Expect().Status(http.StatusUnauthorized).JSON(problemOpts).Object().HasValue("detail", "invalid username or password")
	e.POST("/api/v1/auth/login").WithJSON(map[string]any{"user": "admin"}).Expect().Status(http.StatusBadRequest)
	// Found by the probe sweep: a NUL in the username used to reach Postgres as a 500.
	e.POST("/api/v1/auth/login").WithJSON(map[string]any{"username": "admin\x00", "password": "x"}).Expect().Status(http.StatusUnauthorized)
	e.POST("/api/v1/auth/login").WithText(`{"username":"admin"}`).Expect().Status(http.StatusUnsupportedMediaType)
	login := e.POST("/api/v1/auth/login").WithJSON(map[string]any{"username": "ADMIN", "password": adminPassword}).Expect().Status(http.StatusOK)
	login.Cookie("provenly_session").Value().NotEmpty()
	token := login.JSON().Object().Value("token").String().Raw()
	admin := as(e, token)
	admin.GET("/api/v1/auth/me").Expect().Status(http.StatusOK).JSON().Object().HasValue("username", "admin").HasValue("isAdmin", true)
	e.GET("/api/v1/auth/me").WithCookie("provenly_session", token).Expect().Status(http.StatusOK)
	e.POST("/api/v1/auth/logout").Expect().Status(http.StatusNoContent)

	created := admin.POST("/api/v1/invitations").WithJSON(map[string]any{"email": "ana@example.com", "note": "QA"}).
		Expect().Status(http.StatusCreated).JSON().Object()
	invToken := created.Value("token").String().Raw()
	created.Value("invitation").Object().HasValue("status", "pending").HasValue("email", "ana@example.com")
	admin.POST("/api/v1/invitations").WithJSON(map[string]any{"email": "nope"}).Expect().Status(http.StatusBadRequest)
	admin.POST("/api/v1/invitations").WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.GET("/api/v1/invitations").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 1)
	admin.GET("/api/v1/invitations").WithQuery("page", 0).Expect().Status(http.StatusBadRequest)

	accept := func(tok, username string) *httpexpect.Response {
		return e.POST("/api/v1/invitations/accept").WithJSON(map[string]any{"token": tok, "username": username, "displayName": "Ana", "password": "ana's password"}).Expect()
	}
	accept(invToken, "Admin").Status(http.StatusConflict).JSON(problemOpts).Object().HasValue("code", "conflict")
	accept(invToken, "x").Status(http.StatusBadRequest)
	// Passwords: at least 10 characters (5 two-byte letters are not enough), at most 72 bytes (bcrypt's limit).
	for _, pw := range []string{strings.Repeat("ñ", 5), strings.Repeat("p", 73)} {
		e.POST("/api/v1/invitations/accept").WithJSON(map[string]any{"token": invToken, "username": "pat", "displayName": "Pat", "password": pw}).
			Expect().Status(http.StatusBadRequest).JSON(problemOpts).Object().Value("errors").Array().Value(0).Object().HasValue("field", "password")
	}
	e.POST("/api/v1/invitations/accept").WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	ana := accept(invToken, "ana").Status(http.StatusCreated).JSON().Object()
	ana.Value("user").Object().HasValue("username", "ana").HasValue("isAdmin", false).HasValue("email", "ana@example.com")
	accept(invToken, "ana2").Status(http.StatusNotFound)
	anaAPI := as(e, ana.Value("token").String().Raw())

	anaAPI.GET("/api/v1/users").Expect().Status(http.StatusForbidden).JSON(problemOpts).Object().HasValue("code", "forbidden")
	anaAPI.GET("/api/v1/invitations").Expect().Status(http.StatusForbidden)
	anaAPI.POST("/api/v1/invitations").WithJSON(map[string]any{}).Expect().Status(http.StatusForbidden)
	anaAPI.POST("/api/v1/invitations/1/revoke").Expect().Status(http.StatusForbidden)
	anaAPI.GET("/api/v1/test-cases").Expect().Status(http.StatusOK)

	admin.GET("/api/v1/users").WithQuery("pageSize", 1).Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 2)
	admin.GET("/api/v1/users").WithQuery("pageSize", 0).Expect().Status(http.StatusBadRequest)
	admin.POST("/api/v1/invitations/1/revoke").Expect().Status(http.StatusConflict)
	admin.POST("/api/v1/invitations/987654/revoke").Expect().Status(http.StatusNotFound)
	admin.POST("/api/v1/invitations/0/revoke").Expect().Status(http.StatusBadRequest)
	second := admin.POST("/api/v1/invitations").WithJSON(map[string]any{}).Expect().Status(http.StatusCreated).JSON().Object()
	admin.POST("/api/v1/invitations/2/revoke").Expect().Status(http.StatusOK).JSON().Object().HasValue("status", "revoked")
	accept(second.Value("token").String().Raw(), "bob").Status(http.StatusNotFound)

	anaAPI.POST("/api/v1/auth/password").WithJSON(map[string]any{"currentPassword": "wrong one!", "newPassword": "a brand new password"}).
		Expect().Status(http.StatusBadRequest)
	anaAPI.POST("/api/v1/auth/password").WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	anaAPI.POST("/api/v1/auth/password").WithJSON(map[string]any{"currentPassword": "ana's password", "newPassword": "a brand new password"}).
		Expect().Status(http.StatusOK).JSON().Object().Value("user").Object().HasValue("username", "ana")
	anaAPI.GET("/api/v1/auth/me").Expect().Status(http.StatusUnauthorized)
}

// TestAPIKeys: maintainers manage a project's API keys; CI reports with one into that project only,
// and a revoked key is refused at once.
func TestAPIKeys(t *testing.T) {
	s := fresh(t)
	admin := api(t, s, 1<<20)
	e := anon(t, s, 1<<20)
	admin.POST("/api/v1/projects").WithJSON(map[string]any{"key": "CHK", "name": "Checkout"}).Expect().Status(http.StatusCreated)
	created := admin.POST("/api/v1/projects/CHK/api-keys").WithJSON(map[string]any{"name": "GitHub Actions"}).
		Expect().Status(http.StatusCreated).JSON().Object()
	created.Value("apiKey").Object().HasValue("status", "active").HasValue("lastUsedAt", nil)
	key := as(e, created.Value("token").String().Raw())
	admin.POST("/api/v1/projects/CHK/api-keys").WithJSON(map[string]any{"name": " "}).Expect().Status(http.StatusBadRequest)
	admin.POST("/api/v1/projects/CHK/api-keys").WithText(`{"name":"x"}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.POST("/api/v1/projects/NOPE/api-keys").WithJSON(map[string]any{"name": "x"}).Expect().Status(http.StatusNotFound)

	// The key's runs land in its project by default; another project is invisible to it; it opens nothing else.
	ingest(key, "1", 1, `<testsuite><testcase name="t"/></testsuite>`).Expect().Status(http.StatusCreated).
		JSON().Object().Value("testRun").Object().HasValue("projectId", 2)
	ingest(key, "2", 1, `<testsuite/>`).WithQuery("project", "TC").Expect().Status(http.StatusNotFound)
	key.GET("/api/v1/test-runs").Expect().Status(http.StatusUnauthorized)
	// Found by the probe sweep: a key sent as the session cookie used to be accepted.
	ingest(e, "5", 1, `<testsuite/>`).WithCookie("provenly_session", created.Value("token").String().Raw()).
		Expect().Status(http.StatusUnauthorized)
	admin.GET("/api/v1/projects/CHK/api-keys").Expect().Status(http.StatusOK).JSON().Object().
		Value("items").Array().Value(0).Object().Value("lastUsedAt").String().NotEmpty()
	admin.GET("/api/v1/projects/CHK/api-keys").WithQuery("page", 0).Expect().Status(http.StatusBadRequest)
	admin.GET("/api/v1/projects/NOPE/api-keys").Expect().Status(http.StatusNotFound)

	admin.POST("/api/v1/projects/CHK/api-keys/1/revoke").Expect().Status(http.StatusOK).JSON().Object().HasValue("status", "revoked")
	admin.POST("/api/v1/projects/CHK/api-keys/1/revoke").Expect().Status(http.StatusConflict)
	admin.POST("/api/v1/projects/CHK/api-keys/99/revoke").Expect().Status(http.StatusNotFound)
	admin.POST("/api/v1/projects/CHK/api-keys/0/revoke").Expect().Status(http.StatusBadRequest)
	ingest(key, "3", 1, `<testsuite/>`).Expect().Status(http.StatusUnauthorized).JSON(problemOpts).Object().HasValue("code", "unauthorized")

	// Only maintainers manage keys; viewers cannot report runs either.
	inv := admin.POST("/api/v1/invitations").WithJSON(map[string]any{"project": "CHK", "role": "viewer"}).Expect().Status(http.StatusCreated).JSON().Object()
	viewer := as(e, e.POST("/api/v1/invitations/accept").WithJSON(map[string]any{"token": inv.Value("token").String().Raw(), "username": "vic",
		"displayName": "Vic", "password": "vic's password"}).Expect().Status(http.StatusCreated).JSON().Object().Value("token").String().Raw())
	viewer.GET("/api/v1/projects/CHK/api-keys").Expect().Status(http.StatusForbidden)
	viewer.POST("/api/v1/projects/CHK/api-keys").WithJSON(map[string]any{"name": "x"}).Expect().Status(http.StatusForbidden)
	viewer.POST("/api/v1/projects/CHK/api-keys/1/revoke").Expect().Status(http.StatusForbidden)
	ingest(viewer, "4", 1, `<testsuite/>`).WithQuery("project", "CHK").Expect().Status(http.StatusForbidden)
}

// TestRoles: a member sees only their projects (others are 404) and each role unlocks its operations (else 403).
func TestRoles(t *testing.T) {
	s := fresh(t)
	admin := api(t, s, 1<<20)
	e := anon(t, s, 1<<20)
	admin.POST("/api/v1/projects").WithJSON(map[string]any{"key": "CHK", "name": "Checkout"}).Expect().Status(http.StatusCreated).
		JSON().Object().HasValue("myRole", "admin")
	tcID := int64(admin.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "pay", "project": "CHK"}).Expect().
		Status(http.StatusCreated).JSON().Object().Value("id").Number().Raw())
	hidden := int64(admin.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "default"}).Expect().
		Status(http.StatusCreated).JSON().Object().Value("id").Number().Raw())
	stepID := int64(admin.POST("/api/v1/test-cases/" + strconv.FormatInt(tcID, 10) + "/steps").WithJSON(map[string]any{"action": "a"}).Expect().
		Status(http.StatusCreated).JSON().Object().Value("id").Number().Raw())
	runID := int64(ingest(admin, "88", 1, `<testsuite><testcase name="pay CHK-1"/></testsuite>`).WithQuery("project", "CHK").
		Expect().Status(http.StatusCreated).JSON().Object().Value("testRun").Object().Value("id").Number().Raw())

	// Ana joins CHK as viewer through her invitation.
	inv := admin.POST("/api/v1/invitations").WithJSON(map[string]any{"project": "CHK", "role": "viewer"}).Expect().Status(http.StatusCreated).JSON().Object()
	inv.Value("invitation").Object().HasValue("projectRole", "viewer")
	admin.POST("/api/v1/invitations").WithJSON(map[string]any{"project": "NOPE", "role": "viewer"}).Expect().Status(http.StatusNotFound)
	admin.POST("/api/v1/invitations").WithJSON(map[string]any{"project": "CHK", "role": "owner"}).Expect().Status(http.StatusBadRequest)
	ana := e.POST("/api/v1/invitations/accept").WithJSON(map[string]any{"token": inv.Value("token").String().Raw(), "username": "ana",
		"displayName": "Ana", "password": "ana's password"}).Expect().Status(http.StatusCreated).JSON().Object()
	as := as(e, ana.Value("token").String().Raw())

	tc := "/api/v1/test-cases/" + strconv.FormatInt(tcID, 10)
	step := tc + "/steps/" + strconv.FormatInt(stepID, 10)
	run := "/api/v1/test-runs/" + strconv.FormatInt(runID, 10)
	// Visible: CHK only.
	as.GET("/api/v1/projects").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 1).
		Value("items").Array().Value(0).Object().HasValue("key", "CHK").HasValue("myRole", "viewer")
	// The items themselves are the visible ones (not only the counts), and paging counts only them.
	cases := as.GET("/api/v1/test-cases").WithQuery("pageSize", 1).Expect().Status(http.StatusOK).JSON().Object()
	cases.HasValue("totalItems", 1).HasValue("totalPages", 1)
	cases.Value("items").Array().Length().IsEqual(1)
	cases.Value("items").Array().Value(0).Object().HasValue("id", tcID).HasValue("projectKey", "CHK")
	as.GET("/api/v1/test-cases").WithQuery("pageSize", 1).WithQuery("page", 2).Expect().Status(http.StatusOK).
		JSON().Object().HasValue("totalItems", 1).Value("items").Array().IsEmpty()
	runs := as.GET("/api/v1/test-runs").Expect().Status(http.StatusOK).JSON().Object()
	runs.HasValue("totalItems", 1).Value("items").Array().Length().IsEqual(1)
	runs.Value("items").Array().Value(0).Object().HasValue("id", runID)
	admin.GET("/api/v1/test-cases").Expect().Status(http.StatusOK).JSON().Object().Value("totalItems").Number().Ge(2)
	for _, path := range []string{tc, tc + "/steps", tc + "/results", run, run + "/results", run + "/summary", run + "/parse-errors",
		"/api/v1/projects/CHK", "/api/v1/projects/CHK/members"} {
		as.GET(path).Expect().Status(http.StatusOK)
	}
	for _, path := range []string{"/api/v1/test-cases/" + strconv.FormatInt(hidden, 10), "/api/v1/projects/TC", "/api/v1/projects/TC/members"} {
		as.GET(path).Expect().Status(http.StatusNotFound)
	}
	// An invisible project reads exactly like an unknown one on members and API keys: same 404 detail, naming the
	// requested key, never the internal id.
	for _, sub := range []string{"members", "api-keys"} {
		as.GET("/api/v1/projects/TC/"+sub).Expect().Status(http.StatusNotFound).JSON(problemOpts).Object().HasValue("detail", "project TC not found")
		as.GET("/api/v1/projects/ZZZ/"+sub).Expect().Status(http.StatusNotFound).JSON(problemOpts).Object().HasValue("detail", "project ZZZ not found")
	}
	as.PUT("/api/v1/projects/TC/members/ana").WithJSON(map[string]any{"role": "viewer"}).Expect().Status(http.StatusNotFound).
		JSON(problemOpts).Object().HasValue("detail", "project TC not found")
	for _, path := range []string{"/api/v1/test-cases", "/api/v1/test-runs"} {
		as.GET(path).WithQuery("project", "TC").Expect().Status(http.StatusNotFound)
	}

	// Each operation needs its role: 403 below it.
	writes := []struct {
		method, path string
		body         map[string]any
		min          string
	}{
		{"POST", "/api/v1/test-cases", map[string]any{"title": "new", "project": "CHK"}, "member"},
		{"PATCH", tc, map[string]any{"title": "pay v2"}, "member"},
		{"PUT", tc + "/steps/order", map[string]any{"stepIds": []int64{stepID}}, "member"},
		{"POST", tc + "/steps", map[string]any{"action": "b"}, "member"},
		{"PATCH", step, map[string]any{"action": "a2"}, "member"},
		{"POST", tc + "/deprecate", nil, "maintainer"},
		{"POST", tc + "/reactivate", nil, "maintainer"},
		{"PATCH", "/api/v1/projects/CHK", map[string]any{"name": "Checkout v2"}, "maintainer"},
		{"PUT", "/api/v1/projects/CHK/members/admin", map[string]any{"role": "viewer"}, "maintainer"},
		{"DELETE", "/api/v1/projects/CHK/members/admin", nil, "maintainer"},
		{"DELETE", step, nil, "member"},
	}
	send := func(w struct {
		method, path string
		body         map[string]any
		min          string
	}) *httpexpect.Response {
		req := as.Request(w.method, w.path)
		if w.body != nil {
			req = req.WithJSON(w.body)
		}
		return req.Expect()
	}
	for _, role := range []string{"viewer", "member", "maintainer"} {
		admin.PUT("/api/v1/projects/CHK/members/ana").WithJSON(map[string]any{"role": role}).Expect().Status(http.StatusOK).
			JSON().Object().HasValue("role", role)
		for _, w := range writes {
			allowed := role == "maintainer" || (role == "member" && w.min == "member")
			if allowed {
				continue
			}
			send(w).Status(http.StatusForbidden).JSON(problemOpts).Object().HasValue("code", "forbidden")
		}
	}
	for _, w := range writes { // as maintainer everything goes through
		send(w).Status(success(w.method, w.path))
	}
	as.POST("/api/v1/projects").WithJSON(map[string]any{"key": "WEB", "name": "w"}).Expect().Status(http.StatusForbidden)

	// Member management: validation and unknown users.
	admin.PUT("/api/v1/projects/CHK/members/nobody").WithJSON(map[string]any{"role": "viewer"}).Expect().Status(http.StatusNotFound)
	admin.PUT("/api/v1/projects/CHK/members/ana").WithJSON(map[string]any{"role": "owner"}).Expect().Status(http.StatusBadRequest)
	admin.PUT("/api/v1/projects/CHK/members/ana").WithText(`{"role":"viewer"}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.PUT("/api/v1/projects/chk/members/ana").WithJSON(map[string]any{"role": "viewer"}).Expect().Status(http.StatusBadRequest)
	admin.DELETE("/api/v1/projects/CHK/members/nobody").Expect().Status(http.StatusNotFound)
	admin.DELETE("/api/v1/projects/chk/members/ana").Expect().Status(http.StatusBadRequest)
	admin.GET("/api/v1/projects/CHK/members").WithQuery("page", 0).Expect().Status(http.StatusBadRequest)
	admin.GET("/api/v1/projects/NOPE/members").Expect().Status(http.StatusNotFound)
	admin.DELETE("/api/v1/projects/CHK/members/ana").Expect().Status(http.StatusNoContent)
	as.GET("/api/v1/projects").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 0)
}

// success is the status of a successful write: 204 for deletes, 201 for creations, otherwise 200.
func success(method, path string) int {
	switch {
	case method == "DELETE":
		return http.StatusNoContent
	case method == "POST" && (path == "/api/v1/test-cases" || strings.HasSuffix(path, "/steps")):
		return http.StatusCreated
	}
	return http.StatusOK
}

func TestTestSteps(t *testing.T) {
	e := api(t, fresh(t), 1<<20)
	id := int64(e.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "Steps"}).Expect().Status(http.StatusCreated).
		JSON().Object().Value("id").Number().Raw())
	steps := "/api/v1/test-cases/" + strconv.FormatInt(id, 10) + "/steps"

	a := int64(e.POST(steps).WithJSON(map[string]any{"action": "open", "expectedResult": "page"}).Expect().Status(http.StatusCreated).
		JSON().Object().HasValue("position", 1).Value("id").Number().Raw())
	b := int64(e.POST(steps).WithJSON(map[string]any{"action": "first", "position": 1}).Expect().Status(http.StatusCreated).
		JSON().Object().HasValue("position", 1).Value("id").Number().Raw())
	e.POST(steps).WithJSON(map[string]any{"action": ""}).Expect().Status(http.StatusBadRequest)
	e.POST("/api/v1/test-cases/987654/steps").WithJSON(map[string]any{"action": "x"}).Expect().Status(http.StatusNotFound)

	e.GET(steps).Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 2)
	e.GET(steps).WithQuery("pageSize", 1000).Expect().Status(http.StatusBadRequest)
	e.GET("/api/v1/test-cases/987654/steps").Expect().Status(http.StatusNotFound)

	e.PUT(steps+"/order").WithJSON(map[string]any{"stepIds": []int64{a, b}}).Expect().Status(http.StatusOK).
		JSON().Object().Value("items").Array().Value(0).Object().HasValue("id", a).HasValue("position", 1)
	e.PUT(steps + "/order").WithJSON(map[string]any{"stepIds": []int64{a}}).Expect().Status(http.StatusBadRequest)
	e.PUT("/api/v1/test-cases/987654/steps/order").WithJSON(map[string]any{"stepIds": []int64{}}).Expect().Status(http.StatusNotFound)

	stepPath := steps + "/" + strconv.FormatInt(a, 10)
	e.PATCH(stepPath).WithJSON(map[string]any{"action": "open app"}).Expect().Status(http.StatusOK).JSON().Object().HasValue("action", "open app")
	e.PATCH(stepPath).WithJSON(map[string]any{}).Expect().Status(http.StatusBadRequest)
	e.PATCH(steps + "/987654").WithJSON(map[string]any{"action": "x"}).Expect().Status(http.StatusNotFound)

	e.DELETE(stepPath).Expect().Status(http.StatusNoContent)
	e.DELETE(steps + "/abc").Expect().Status(http.StatusBadRequest)
	e.DELETE(stepPath).Expect().Status(http.StatusNotFound)
}

func TestRunsAndIngestion(t *testing.T) {
	e := api(t, fresh(t), 1<<20)
	id := int64(e.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "Login", "automated": true}).Expect().Status(http.StatusCreated).
		JSON().Object().Value("id").Number().Raw())

	created := ingest(e, "1", 1, report(id)).Expect().Status(http.StatusCreated).JSON().Object()
	created.HasValue("created", true).HasValue("received", 6).HasValue("persisted", 5)
	created.Value("diagnostics").Array().Length().IsEqual(3)
	created.Value("parseErrors").Array().Length().IsEqual(1)
	runID := strconv.FormatInt(int64(created.Value("testRun").Object().Value("id").Number().Raw()), 10)

	ingest(e, "1", 1, report(id)).Expect().Status(http.StatusOK).JSON().Object().HasValue("created", false).
		Value("warnings").Array().IsEmpty()
	ingest(e, "1", 1, `<testsuite name="other"/>`).Expect().Status(http.StatusOK).JSON().Object().
		Value("warnings").Array().Length().IsEqual(1)
	ingest(e, "2", 1, `<testsuite name="no timestamp"/>`).Expect().Status(http.StatusCreated)

	ingest(e, "1", 0, report(id)).Expect().Status(http.StatusBadRequest).JSON(problemOpts).Object().HasValue("code", "validation_error")
	ingest(e, "3", 1, report(id)).WithQuery("status", "cancelled").Expect().Status(http.StatusCreated).
		JSON().Object().Value("testRun").Object().HasValue("executionStatus", "cancelled")
	ingest(e, "5", 1, report(id)).WithQuery("status", "interrupted").Expect().Status(http.StatusCreated).
		JSON().Object().Value("testRun").Object().HasValue("executionStatus", "interrupted")
	ingest(e, "4", 1, report(id)).WithQuery("status", "running").Expect().Status(http.StatusBadRequest)
	ingest(e, "4", 1, report(id)).WithQuery("status", "failed").Expect().Status(http.StatusBadRequest)
	ingest(e, "1", 1, "<not-xml").Expect().Status(http.StatusBadRequest).JSON(problemOpts).Object().HasValue("code", "invalid_junit")
	e.POST("/api/v1/ingestion/junit").WithQuery("provider", "github").WithQuery("runId", "1").WithQuery("runAttempt", 1).
		WithJSON(map[string]any{}).Expect().Status(http.StatusUnsupportedMediaType)
	small := api(t, app.NewServicesWith(db.Pool, time.Now, identityConfig()), 64)
	ingest(small, "9", 1, report(id)).Expect().Status(http.StatusRequestEntityTooLarge).JSON(problemOpts).Object().HasValue("code", "payload_too_large")

	e.GET("/api/v1/test-runs").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 4)
	e.GET("/api/v1/test-runs").WithQuery("pageSize", 0).Expect().Status(http.StatusBadRequest)

	run := e.GET("/api/v1/test-runs/" + runID).Expect().Status(http.StatusOK).JSON().Object()
	run.HasValue("externalRunId", "github:1:1").HasValue("executionStatus", "completed")
	run.Value("outcome").Object().HasValue("verdict", "failed").HasValue("executed", 1).HasValue("failed", 1).HasValue("passRate", 0)
	e.GET("/api/v1/test-runs/x").Expect().Status(http.StatusBadRequest)
	e.GET("/api/v1/test-runs/987654").Expect().Status(http.StatusNotFound)

	e.GET("/api/v1/test-runs/"+runID+"/results").WithQuery("status", "failed").Expect().Status(http.StatusOK).
		JSON().Object().HasValue("totalItems", 1)
	e.GET("/api/v1/test-runs/"+runID+"/results").WithQuery("correlation", "bogus").Expect().Status(http.StatusBadRequest)
	e.GET("/api/v1/test-runs/987654/results").Expect().Status(http.StatusNotFound)

	e.GET("/api/v1/test-runs/"+runID+"/parse-errors").Expect().Status(http.StatusOK).
		JSON().Object().HasValue("totalItems", 1).Value("items").Array().Value(0).Object().HasValue("persisted", false)
	e.GET("/api/v1/test-runs/"+runID+"/parse-errors").WithQuery("page", 0).Expect().Status(http.StatusBadRequest)
	e.GET("/api/v1/test-runs/987654/parse-errors").Expect().Status(http.StatusNotFound)

	sum := e.GET("/api/v1/test-runs/" + runID + "/summary").Expect().Status(http.StatusOK).JSON().Object()
	sum.Value("counts").Object().HasValue("failed", 1)
	sum.HasValue("executionPercent", 100)
	e.GET("/api/v1/test-runs/-1/summary").Expect().Status(http.StatusBadRequest)
	e.GET("/api/v1/test-runs/987654/summary").Expect().Status(http.StatusNotFound)

	e.GET("/api/v1/test-cases/"+strconv.FormatInt(id, 10)+"/results").Expect().Status(http.StatusOK).
		JSON().Object().HasValue("totalItems", 6)
}

// TestInternalErrors covers the 500 variant of every operation that declares
// it, by serving the API on a closed connection pool.
func TestInternalErrors(t *testing.T) {
	fresh(t) // the administrator whose token the closed-pool API cannot check
	pool, err := postgres.Open(context.Background(), db.URL)
	require.NoError(t, err)
	pool.Close()
	e := api(t, app.NewServicesWith(pool, time.Now, identityConfig()), 1<<20)
	problem := func(r *httpexpect.Response) {
		r.Status(http.StatusInternalServerError).JSON(problemOpts).Object().HasValue("code", "internal_error")
	}
	problem(e.GET("/api/v1/projects").Expect())
	problem(e.GET("/api/v1/auth/me").Expect())
	problem(e.GET("/api/v1/projects/TC/members").Expect())
	problem(e.PUT("/api/v1/projects/TC/members/admin").WithJSON(map[string]any{"role": "viewer"}).Expect())
	problem(e.DELETE("/api/v1/projects/TC/members/admin").Expect())
	problem(e.GET("/api/v1/projects/TC/api-keys").Expect())
	problem(e.POST("/api/v1/projects/TC/api-keys").WithJSON(map[string]any{"name": "ci"}).Expect())
	problem(e.POST("/api/v1/projects/TC/api-keys/1/revoke").Expect())
	problem(e.POST("/api/v1/auth/password").WithJSON(map[string]any{"currentPassword": "x", "newPassword": "a long password"}).Expect())
	problem(e.GET("/api/v1/users").Expect())
	problem(e.GET("/api/v1/invitations").Expect())
	problem(e.POST("/api/v1/invitations").WithJSON(map[string]any{}).Expect())
	problem(e.POST("/api/v1/invitations/1/revoke").Expect())
	problem(e.POST("/api/v1/auth/login").WithJSON(map[string]any{"username": "admin", "password": "x"}).Expect())
	problem(e.POST("/api/v1/invitations/accept").WithJSON(map[string]any{"token": "t", "username": "ana", "displayName": "Ana", "password": "a long password"}).Expect())
	problem(e.POST("/api/v1/projects").WithJSON(map[string]any{"key": "CHK", "name": "x"}).Expect())
	problem(e.GET("/api/v1/projects/CHK").Expect())
	problem(e.PATCH("/api/v1/projects/CHK").WithJSON(map[string]any{"name": "x"}).Expect())
	problem(e.GET("/api/v1/test-cases").Expect())
	problem(e.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "x"}).Expect())
	problem(e.GET("/api/v1/test-cases/1").Expect())
	problem(e.PATCH("/api/v1/test-cases/1").WithJSON(map[string]any{"title": "x"}).Expect())
	problem(e.POST("/api/v1/test-cases/1/deprecate").Expect())
	problem(e.POST("/api/v1/test-cases/1/reactivate").Expect())
	problem(e.GET("/api/v1/test-cases/1/steps").Expect())
	problem(e.POST("/api/v1/test-cases/1/steps").WithJSON(map[string]any{"action": "x"}).Expect())
	problem(e.PUT("/api/v1/test-cases/1/steps/order").WithJSON(map[string]any{"stepIds": []int{}}).Expect())
	problem(e.PATCH("/api/v1/test-cases/1/steps/1").WithJSON(map[string]any{"action": "x"}).Expect())
	problem(e.DELETE("/api/v1/test-cases/1/steps/1").Expect())
	problem(e.GET("/api/v1/test-cases/1/results").Expect())
	problem(e.GET("/api/v1/test-runs").Expect())
	problem(e.GET("/api/v1/test-runs/1").Expect())
	problem(e.GET("/api/v1/test-runs/1/results").Expect())
	problem(e.GET("/api/v1/test-runs/1/summary").Expect())
	problem(e.GET("/api/v1/test-runs/1/parse-errors").Expect())
	problem(e.GET("/api/v1/test-runs/1/amendments").Expect())
	problem(e.POST("/api/v1/test-runs/1/amendments").WithJSON(map[string]any{"testCaseId": 1, "reason": "x"}).Expect())
	problem(e.GET("/api/v1/projects/TC/dimensions").Expect())
	problem(e.GET("/api/v1/projects/TC/suites").Expect())
	problem(e.GET("/api/v1/projects/TC/requirements").Expect())
	problem(e.POST("/api/v1/projects/TC/requirements").WithJSON(map[string]any{"title": "x"}).Expect())
	problem(e.POST("/api/v1/projects/TC/requirements/import").WithJSON(map[string]any{"provider": "jira", "items": []map[string]any{{"externalId": "X-1", "title": "x"}}}).Expect())
	problem(e.GET("/api/v1/projects/TC/requirements/1").Expect())
	problem(e.PATCH("/api/v1/projects/TC/requirements/1").WithJSON(map[string]any{"title": "x"}).Expect())
	problem(e.PUT("/api/v1/projects/TC/requirements/1/test-cases").WithJSON(map[string]any{"testCaseIds": []int{}}).Expect())
	problem(e.GET("/api/v1/projects/TC/issues").Expect())
	problem(e.GET("/api/v1/projects/TC/quality").Expect())
	problem(e.POST("/api/v1/test-runs/live").WithJSON(map[string]any{"provider": "github", "runId": "1", "runAttempt": 1}).Expect())
	problem(e.POST("/api/v1/test-runs/1/events").WithJSON(map[string]any{"events": []map[string]any{{"eventId": "a", "sequence": 1, "type": "run.finished"}}}).Expect())
	problem(e.GET("/api/v1/test-runs/1/live").Expect())
	problem(e.POST("/api/v1/projects/TC/issues").WithJSON(map[string]any{"title": "x"}).Expect())
	problem(e.POST("/api/v1/projects/TC/issues/import").WithJSON(map[string]any{"provider": "jira", "items": []map[string]any{{"externalId": "X-1", "title": "x", "state": "open"}}}).Expect())
	problem(e.GET("/api/v1/projects/TC/issues/1").Expect())
	problem(e.PATCH("/api/v1/projects/TC/issues/1").WithJSON(map[string]any{"title": "x"}).Expect())
	problem(e.PUT("/api/v1/projects/TC/issues/1/test-cases").WithJSON(map[string]any{"testCaseIds": []int{}}).Expect())
	problem(e.POST("/api/v1/test-runs/manual").WithJSON(map[string]any{"project": "TC", "name": "x"}).Expect())
	problem(e.POST("/api/v1/test-runs/1/manual-results").WithJSON(map[string]any{"testCaseId": 1, "status": "passed"}).Expect())
	problem(e.POST("/api/v1/test-runs/1/finish").WithJSON(map[string]any{"status": "completed"}).Expect())
	problem(e.POST("/api/v1/projects/TC/suites").WithJSON(map[string]any{"key": "s", "name": "S", "kind": "static"}).Expect())
	problem(e.GET("/api/v1/projects/TC/suites/s").Expect())
	problem(e.PATCH("/api/v1/projects/TC/suites/s").WithJSON(map[string]any{"name": "S"}).Expect())
	problem(e.PUT("/api/v1/projects/TC/suites/s/cases").WithJSON(map[string]any{"testCaseIds": []int{}}).Expect())
	problem(e.POST("/api/v1/projects/TC/dimensions").WithJSON(map[string]any{"key": "os", "name": "OS"}).Expect())
	problem(e.PATCH("/api/v1/projects/TC/dimensions/risk").WithJSON(map[string]any{"name": "R"}).Expect())
	problem(e.POST("/api/v1/projects/TC/dimensions/risk/values").WithJSON(map[string]any{"key": "x", "name": "X"}).Expect())
	problem(e.PATCH("/api/v1/projects/TC/dimensions/risk/values/low").WithJSON(map[string]any{"name": "L"}).Expect())
	problem(e.GET("/api/v1/audit").Expect())
	problem(e.GET("/api/v1/projects/TC/webhooks").Expect())
	problem(e.POST("/api/v1/projects/TC/webhooks").WithJSON(map[string]any{"url": "https://x.test", "events": []string{"run.completed"}}).Expect())
	problem(e.PATCH("/api/v1/projects/TC/webhooks/1").WithJSON(map[string]any{"active": true}).Expect())
	problem(e.POST("/api/v1/projects/TC/webhooks/1/ping").Expect())
	problem(e.GET("/api/v1/projects/TC/webhooks/1/deliveries").Expect())
	problem(e.GET("/api/v1/projects/TC/github").Expect())
	problem(e.PUT("/api/v1/projects/TC/github").WithJSON(map[string]any{"repository": "acme/shop", "token": "t"}).Expect())
	problem(e.DELETE("/api/v1/projects/TC/github").Expect())
	problem(e.POST("/api/v1/projects/TC/github/sync").Expect())
	problem(ingest(e, "1", 1, strings.ReplaceAll(report(1), "\n", "")).Expect())
}

var problemOpts = httpexpect.ContentOpts{MediaType: "application/problem+json"}

// TestRobustness sends edge-case inputs to every list and JSON operation: the
// API never answers 5xx, every error is a Problem declared by the contract
// (validated by the recording transport), unknown parameters are ignored and
// pages beyond the int32 SQL offset are rejected instead of failing.
func TestRobustness(t *testing.T) {
	e := api(t, fresh(t), 1<<20)
	tc := e.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "Edge", "automated": true}).
		Expect().Status(http.StatusCreated).JSON().Object()
	id := strconv.FormatInt(int64(tc.Value("id").Number().Raw()), 10)
	run := ingest(e, "edge", 1, report(int64(tc.Value("id").Number().Raw()))).Expect().Status(http.StatusCreated).
		JSON().Object().Value("testRun").Object()
	runID := strconv.FormatInt(int64(run.Value("id").Number().Raw()), 10)

	lists := []string{
		"/api/v1/projects", "/api/v1/test-cases", "/api/v1/test-runs",
		"/api/v1/test-cases/" + id + "/steps", "/api/v1/test-cases/" + id + "/results",
		"/api/v1/test-runs/" + runID + "/results", "/api/v1/test-runs/" + runID + "/parse-errors",
	}
	cases := []struct {
		query  string
		status int
	}{
		{"page=21474838&pageSize=100", http.StatusBadRequest}, // offset past int32
		{"page=21474837&pageSize=100", http.StatusOK},         // last representable page: empty
		{"page=2147483648", http.StatusBadRequest},
		{"pageSize=5&pageSize=1&foo=bar&limit=1", http.StatusOK}, // first value wins, unknown ignored
		{"pageSize=5.0", http.StatusBadRequest},
		{"pageSize=1e1", http.StatusBadRequest},
		{"pageSize=%205", http.StatusBadRequest},
		{"page=05", http.StatusOK},
	}
	for _, path := range lists {
		for _, c := range cases {
			r := e.GET(path).WithQueryString(c.query).Expect()
			r.Status(c.status)
			if c.status == http.StatusBadRequest {
				r.JSON(problemOpts).Object().HasValue("code", "validation_error").HasValue("detail", "request validation failed")
			}
		}
	}
	e.GET("/api/v1/test-cases").WithQueryString("page=21474837&pageSize=100").Expect().Status(http.StatusOK).
		JSON().Object().Value("items").Array().IsEmpty()

	// Known parameters present but empty are invalid; enums are case-sensitive; both result filters combine.
	for _, q := range []string{"status=", "page=", "pageSize="} {
		e.GET("/api/v1/test-cases").WithQueryString(q).Expect().Status(http.StatusBadRequest)
	}
	e.GET("/api/v1/test-cases").WithQueryString("status=ACTIVE").Expect().Status(http.StatusBadRequest)
	e.GET("/api/v1/test-runs/"+runID+"/results").WithQueryString("status=failed&correlation=valid").Expect().
		Status(http.StatusOK).JSON().Object().HasValue("totalItems", 1)
	e.GET("/api/v1/test-runs/"+runID+"/results").WithQueryString("status=passed&correlation=missing").Expect().
		Status(http.StatusOK).JSON().Object().HasValue("totalItems", 1)

	// Path ids: leading zeros resolve, out of range is a validation error, the max int64 is just not found.
	e.GET("/api/v1/test-cases/0" + id).Expect().Status(http.StatusOK)
	e.GET("/api/v1/test-cases/9223372036854775807").Expect().Status(http.StatusNotFound)
	for _, bad := range []string{"0", "-1", "1.0", "9223372036854775808"} {
		e.GET("/api/v1/test-cases/"+bad).Expect().Status(http.StatusBadRequest).JSON(problemOpts).Object().
			HasValue("detail", "request validation failed")
	}

	// JSON operations require application/json.
	steps := "/api/v1/test-cases/" + id + "/steps"
	step := e.POST(steps).WithJSON(map[string]any{"action": "open"}).Expect().Status(http.StatusCreated).JSON().Object()
	stepPath := steps + "/" + strconv.FormatInt(int64(step.Value("id").Number().Raw()), 10)
	for _, op := range []struct{ method, path, body string }{
		{"POST", "/api/v1/test-cases", `{"title":"x"}`},
		{"PATCH", "/api/v1/test-cases/" + id, `{"title":"x"}`},
		{"POST", "/api/v1/projects", `{"key":"XX","name":"x"}`},
		{"PATCH", "/api/v1/projects/TC", `{"name":"x"}`},
		{"POST", steps, `{"action":"x"}`},
		{"PUT", steps + "/order", `{"stepIds":[1]}`},
		{"PATCH", stepPath, `{"action":"x"}`},
	} {
		e.Request(op.method, op.path).WithHeader("Content-Type", "text/plain").WithText(op.body).Expect().
			Status(http.StatusUnsupportedMediaType).JSON(problemOpts).Object().HasValue("code", "unsupported_media_type")
	}
	// Text that PostgreSQL cannot store (NUL, invalid UTF-8) is a 400, never a 500.
	unstorable := func(r *httpexpect.Request) {
		r.Expect().Status(http.StatusBadRequest).JSON(problemOpts).Object().
			HasValue("code", "validation_error").HasValue("detail", "request validation failed")
	}
	unstorable(e.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "a\x00b"}))
	unstorable(e.PATCH("/api/v1/test-cases/" + id).WithJSON(map[string]any{"description": "\x00"}))
	unstorable(e.POST(steps).WithJSON(map[string]any{"action": "a\x00"}))
	unstorable(e.PATCH(stepPath).WithJSON(map[string]any{"expectedResult": "\x00"}))
	for _, q := range [][2]string{{"branch", "ma\x00in"}, {"pipeline", "\xff"}, {"commit", "c\x00"}} {
		unstorable(e.POST("/api/v1/ingestion/junit").WithQuery("provider", "github").WithQuery("runId", "nul").
			WithQuery("runAttempt", 1).WithQuery(q[0], q[1]).WithHeader("Content-Type", xmlType).WithText(report(1)))
	}

	// A JUnit duration too large to store or read is kept as unknown with a parse error (it used to be a 500);
	// a second root element is rejected instead of silently dropped.
	ingest(e, "huge-time", 1, `<testsuite name="s"><testcase name="t" time="1e300"/></testsuite>`).Expect().
		Status(http.StatusCreated).JSON().Object().Value("parseErrors").Array().Length().IsEqual(1)
	ingest(e, "two-roots", 1, `<testsuite name="a"/><testsuite name="b"/>`).Expect().
		Status(http.StatusBadRequest).JSON(problemOpts).Object().HasValue("code", "invalid_junit")

	// JSON values out of range or of the wrong type are a 400, never a 500.
	for _, body := range []string{
		`{"action":"x","position":2147483648}`, `{"action":"x","position":-5}`, `{"action":"x","position":0}`,
		`{"action":"x","position":1.5}`, `{"action":"x","position":1e400}`,
	} {
		e.POST(steps).WithHeader("Content-Type", "application/json").WithBytes([]byte(body)).Expect().Status(http.StatusBadRequest)
	}
	for _, body := range []string{`{"title":12345}`, `{"title":"x","automated":"yes"}`,
		`{"title":"x","description":` + strings.Repeat("[", 100000) + strings.Repeat("]", 100000) + `}`} {
		e.POST("/api/v1/test-cases").WithHeader("Content-Type", "application/json").WithBytes([]byte(body)).Expect().Status(http.StatusBadRequest)
	}
	// runAttempt is a 32-bit integer.
	ingest(e, "attempt-max", 2147483647, report(1)).Expect().Status(http.StatusCreated)
	e.POST("/api/v1/ingestion/junit").WithQuery("provider", "github").WithQuery("runId", "attempt-over").
		WithQuery("runAttempt", "2147483648").WithHeader("Content-Type", xmlType).WithText(report(1)).
		Expect().Status(http.StatusBadRequest)

	// Duplicate JSON keys: the last value wins.
	e.POST("/api/v1/test-cases").WithHeader("Content-Type", "application/json").WithBytes([]byte(`{"title":"a","title":"b"}`)).
		Expect().Status(http.StatusCreated).JSON().Object().HasValue("title", "b")
}

// TestLargeFailureOutputIsKeptWhole: a megabyte-sized failure message and
// multi-megabyte details (stack traces, logs) are stored and returned unabridged.
func TestLargeFailureOutputIsKeptWhole(t *testing.T) {
	e := api(t, fresh(t), 16<<20)
	msg, details := strings.Repeat("m", 1<<20), strings.Repeat("d", 5<<20)
	run := ingest(e, "large", 1, `<testsuite name="s"><testcase name="big"><failure message="`+msg+`">`+details+`</failure></testcase></testsuite>`).
		Expect().Status(http.StatusCreated).JSON().Object().Value("testRun").Object()
	runID := strconv.FormatInt(int64(run.Value("id").Number().Raw()), 10)
	r := e.GET("/api/v1/test-runs/" + runID + "/results").Expect().Status(http.StatusOK).JSON().Object().Value("items").Array().Value(0).Object()
	r.Value("errorMessage").String().Length().IsEqual(len(msg))
	r.Value("errorDetails").String().Length().IsEqual(len(details))
}

// TestRunsAndResultsAreReadOnly: ingested runs and results cannot be edited or
// deleted through the API (e.g. a failure turned into a skip); the contract
// defines no such operation and the router rejects every write method.
func TestRunsAndResultsAreReadOnly(t *testing.T) {
	raw := offContract(t, fresh(t))
	run := ingest(raw, "ro", 1, report(1)).Expect().Status(http.StatusCreated).JSON().Object().Value("testRun").Object()
	runID := strconv.FormatInt(int64(run.Value("id").Number().Raw()), 10)
	for _, method := range []string{"PUT", "PATCH", "DELETE", "POST"} {
		for _, path := range []string{"/api/v1/test-runs/" + runID, "/api/v1/test-runs/" + runID + "/results",
			"/api/v1/test-runs/" + runID + "/summary", "/api/v1/test-runs/" + runID + "/parse-errors",
			"/api/v1/test-cases/1/results"} {
			raw.Request(method, path).WithJSON(map[string]any{"status": "skipped"}).Expect().Status(http.StatusMethodNotAllowed)
		}
		raw.Request(method, "/api/v1/test-runs/"+runID+"/results/1").WithJSON(map[string]any{"status": "skipped"}).
			Expect().Status(http.StatusNotFound) // there is no single-result resource at all
	}
	raw.GET("/api/v1/test-runs/"+runID+"/results").WithQuery("status", "failed").Expect().Status(http.StatusOK).
		JSON().Object().HasValue("totalItems", 1) // the failure is still a failure
}

// TestIngestionMediaTypes: the charset parameter overrides the document, text/xml
// and +xml types are XML, compressed bodies and unknown charsets are a 415, and
// every query parameter error is reported at once (docs/review.md finding 24); gzip is accepted (MVP D6).
func TestIngestionMediaTypes(t *testing.T) {
	e := api(t, fresh(t), 1<<20)
	send := func(runID, contentType string, body []byte) *httpexpect.Request {
		return e.POST("/api/v1/ingestion/junit").WithQuery("provider", "github").WithQuery("runId", runID).
			WithQuery("runAttempt", 1).WithHeader("Content-Type", contentType).WithBytes(body)
	}
	latin1 := []byte("<testsuite name=\"s\"><testcase name=\"caf\xe9\"/></testsuite>")
	run := send("latin1", "application/xml; charset=ISO-8859-1", latin1).Expect().Status(http.StatusCreated).
		JSON().Object().Value("testRun").Object()
	runID := strconv.FormatInt(int64(run.Value("id").Number().Raw()), 10)
	e.GET("/api/v1/test-runs/"+runID+"/results").Expect().Status(http.StatusOK).
		JSON().Object().Value("items").Array().Value(0).Object().HasValue("testName", "caf\u00e9")
	send("textxml", "text/xml", []byte(`<testsuite name="s"/>`)).Expect().Status(http.StatusCreated)
	send("charset", "application/xml; charset=shift_jis", []byte(`<testsuite/>`)).Expect().
		Status(http.StatusUnsupportedMediaType).JSON(problemOpts).Object().HasValue("code", "unsupported_media_type")
	// MVP D6: gzip reports are read with the size limit on the decompressed body; other encodings are a 415.
	var zipped bytes.Buffer
	zw := gzip.NewWriter(&zipped)
	_, _ = zw.Write([]byte(`<testsuite name="zipped"><testcase name="gz"/></testsuite>`))
	_ = zw.Close()
	send("gzip", "application/xml", zipped.Bytes()).WithHeader("Content-Encoding", "gzip").Expect().
		Status(http.StatusCreated).JSON().Object().HasValue("received", 1)
	// A gzip report is replayed like any other: the same report (compressed or not) is a silent replay, a different
	// one under the same run id is a replay with a warning (card #42).
	send("gzip", "application/xml", zipped.Bytes()).WithHeader("Content-Encoding", "gzip").Expect().
		Status(http.StatusOK).JSON().Object().HasValue("created", false).Value("warnings").Array().IsEmpty()
	send("gzip", "application/xml", []byte(`<testsuite name="zipped"><testcase name="gz"/></testsuite>`)).Expect().
		Status(http.StatusOK).JSON().Object().HasValue("created", false).Value("warnings").Array().IsEmpty()
	var other bytes.Buffer
	ow := gzip.NewWriter(&other)
	_, _ = ow.Write([]byte(`<testsuite name="zipped"><testcase name="another"/></testsuite>`))
	_ = ow.Close()
	send("gzip", "application/xml", other.Bytes()).WithHeader("Content-Encoding", "gzip").Expect().
		Status(http.StatusOK).JSON().Object().HasValue("created", false).Value("warnings").Array().Length().IsEqual(1)
	// A body that is not gzip, truncated or corrupt is an unreadable report: 400 invalid_junit.
	for name, body := range map[string][]byte{"broken": {0x1f, 0x8b}, "truncated": zipped.Bytes()[:15], "plain": []byte(`<testsuite/>`)} {
		send("gzip-"+name, "application/xml", body).WithHeader("Content-Encoding", "gzip").Expect().
			Status(http.StatusBadRequest).JSON(problemOpts).Object().HasValue("code", "invalid_junit").
			Value("detail").String().HasPrefix("body is not valid gzip")
	}
	send("br", "application/xml", []byte(`<testsuite/>`)).WithHeader("Content-Encoding", "br").Expect().
		Status(http.StatusUnsupportedMediaType).JSON(problemOpts).Object().HasValue("code", "unsupported_media_type")
	var bomb bytes.Buffer
	bw := gzip.NewWriter(&bomb)
	_, _ = bw.Write(bytes.Repeat([]byte(" "), 2<<20))
	_ = bw.Close()
	send("bomb", "application/xml", bomb.Bytes()).WithHeader("Content-Encoding", "gzip").Expect().
		Status(http.StatusRequestEntityTooLarge).JSON(problemOpts).Object().HasValue("code", "payload_too_large")

	errs := e.POST("/api/v1/ingestion/junit").WithHeader("Content-Type", xmlType).WithText(`<testsuite/>`).Expect().
		Status(http.StatusBadRequest).JSON(problemOpts).Object().Value("errors").Array()
	errs.Length().IsEqual(3)

	send("empty", xmlType, nil).Expect().Status(http.StatusBadRequest).JSON(problemOpts).Object().HasValue("code", "invalid_junit")

	raw := offContract(t, fresh(t))
	raw.POST("/api/v1/ingestion/junit").WithQuery("provider", "github").WithQuery("runId", "plusxml").WithQuery("runAttempt", 1).
		WithHeader("Content-Type", "application/junit+xml").WithText(`<testsuite name="s"/>`).Expect().Status(http.StatusCreated)
	raw.GET("/api/v1/ingestion/junit").Expect().Status(http.StatusMethodNotAllowed)
}

// TestOptimisticLocking: test case and step reads carry the version as the ETag; every write accepts If-Match and
// answers 412 when someone saved in between, changing nothing.
func TestOptimisticLocking(t *testing.T) {
	e := api(t, fresh(t), 1<<20)
	tc := e.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "Login"}).Expect().Status(http.StatusCreated).JSON().Object()
	path := "/api/v1/test-cases/" + strconv.FormatInt(int64(tc.Value("id").Number().Raw()), 10)
	tc.HasValue("version", 1)
	get := e.GET(path).Expect().Status(http.StatusOK)
	get.Header("ETag").IsEqual(`"1"`)
	e.GET(path + "/steps").Expect().Status(http.StatusOK).Header("ETag").IsEqual(`"1"`)

	saved := e.PATCH(path).WithHeader("If-Match", `"1"`).WithJSON(map[string]any{"title": "Login v2"}).Expect().Status(http.StatusOK)
	saved.Header("ETag").IsEqual(`"2"`)
	saved.JSON().Object().HasValue("version", 2)
	step := e.POST(path+"/steps").WithHeader("If-Match", `"2"`).WithJSON(map[string]any{"action": "open"}).Expect().Status(http.StatusCreated)
	step.Header("ETag").IsEqual(`"3"`)
	stepPath := path + "/steps/" + strconv.FormatInt(int64(step.JSON().Object().Value("id").Number().Raw()), 10)

	stale := `"1"`
	for _, r := range []*httpexpect.Request{
		e.PATCH(path).WithJSON(map[string]any{"title": "lost"}),
		e.POST(path + "/deprecate"),
		e.POST(path + "/reactivate"),
		e.POST(path + "/steps").WithJSON(map[string]any{"action": "lost"}),
		e.PUT(path + "/steps/order").WithJSON(map[string]any{"stepIds": []int{}}),
		e.PATCH(stepPath).WithJSON(map[string]any{"action": "lost"}),
		e.DELETE(stepPath),
	} {
		r.WithHeader("If-Match", stale).Expect().Status(http.StatusPreconditionFailed).
			JSON(problemOpts).Object().HasValue("code", "precondition_failed")
	}
	e.GET(path).Expect().Status(http.StatusOK).JSON().Object().HasValue("title", "Login v2").HasValue("version", 3)
	e.PATCH(path).WithHeader("If-Match", "3").WithJSON(map[string]any{"title": "x"}).Expect().Status(http.StatusBadRequest)
	e.DELETE(stepPath).WithHeader("If-Match", `"3"`).Expect().Status(http.StatusNoContent).Header("ETag").NotEmpty()
	e.POST(path + "/deprecate").Expect().Status(http.StatusOK).Header("ETag").NotEmpty()
}

// TestAmendments (DEC-42): a maintainer includes a reported TC-ID that was outside the snapshot; the run is marked as
// edited, the summary counts it and the amendment history says who and why.
func TestAmendments(t *testing.T) {
	s := fresh(t)
	admin := api(t, s, 1<<20)
	e := anon(t, s, 1<<20)
	manual := int64(admin.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "Refund"}).Expect().Status(http.StatusCreated).
		JSON().Object().Value("id").Number().Raw())
	run := ingest(admin, "60", 1, `<testsuite><testcase name="refund TC-`+strconv.FormatInt(manual, 10)+`"/></testsuite>`).
		Expect().Status(http.StatusCreated).JSON().Object().Value("testRun").Object()
	run.HasValue("amendmentCount", 0)
	path := "/api/v1/test-runs/" + strconv.FormatInt(int64(run.Value("id").Number().Raw()), 10)

	amended := admin.POST(path + "/amendments").WithJSON(map[string]any{"testCaseId": manual, "reason": "marked manual by mistake"}).
		Expect().Status(http.StatusCreated).JSON().Object()
	amended.HasValue("amendedByUsername", adminUser).HasValue("testCaseKey", "TC-"+strconv.FormatInt(manual, 10))
	admin.GET(path).Expect().Status(http.StatusOK).JSON().Object().HasValue("amendmentCount", 1).HasValue("expectedCount", 1)
	sum := admin.GET(path + "/summary").Expect().Status(http.StatusOK).JSON().Object()
	sum.HasValue("snapshotTotal", 0).HasValue("expectedTotal", 1).Value("amendedTestCaseIds").Array().IsEqual([]int64{manual})
	admin.GET(path+"/amendments").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 1)
	admin.GET(path+"/amendments").WithQuery("page", 0).Expect().Status(http.StatusBadRequest)
	admin.GET("/api/v1/test-runs/987654/amendments").Expect().Status(http.StatusNotFound)

	admin.POST(path + "/amendments").WithJSON(map[string]any{"testCaseId": manual, "reason": "again"}).Expect().Status(http.StatusConflict)
	admin.POST(path + "/amendments").WithJSON(map[string]any{"testCaseId": manual, "reason": " "}).Expect().Status(http.StatusBadRequest)
	admin.POST(path + "/amendments").WithJSON(map[string]any{"testCaseId": 987654, "reason": "x"}).Expect().Status(http.StatusBadRequest)
	admin.POST(path + "/amendments").WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.POST("/api/v1/test-runs/987654/amendments").WithJSON(map[string]any{"testCaseId": 1, "reason": "x"}).Expect().Status(http.StatusNotFound)

	inv := admin.POST("/api/v1/invitations").WithJSON(map[string]any{"project": "TC", "role": "member"}).Expect().Status(http.StatusCreated).JSON().Object()
	member := as(e, e.POST("/api/v1/invitations/accept").WithJSON(map[string]any{"token": inv.Value("token").String().Raw(), "username": "mia",
		"displayName": "Mia", "password": "mia's password"}).Expect().Status(http.StatusCreated).JSON().Object().Value("token").String().Raw())
	member.POST(path + "/amendments").WithJSON(map[string]any{"testCaseId": manual, "reason": "x"}).Expect().Status(http.StatusForbidden)
	member.GET(path + "/amendments").Expect().Status(http.StatusOK)
}

// TestTaxonomy: dimensions and values per project (maintainers write, anyone in the project reads), tags and
// classification in the test case body and the list filters.
func TestTaxonomy(t *testing.T) {
	s := fresh(t)
	admin := api(t, s, 1<<20)
	e := anon(t, s, 1<<20)
	dims := admin.GET("/api/v1/projects/TC/dimensions").Expect().Status(http.StatusOK).JSON().Object().Value("items").Array()
	dims.Length().IsEqual(7)
	dims.Value(5).Object().HasValue("key", "risk").HasValue("builtIn", true).Value("values").Array().Length().IsEqual(4)
	admin.GET("/api/v1/projects/tc/dimensions").Expect().Status(http.StatusBadRequest)
	admin.GET("/api/v1/projects/NOPE/dimensions").Expect().Status(http.StatusNotFound)

	admin.POST("/api/v1/projects/TC/dimensions").WithJSON(map[string]any{"key": "browser", "name": "Browser"}).Expect().
		Status(http.StatusCreated).JSON().Object().HasValue("builtIn", false).HasValue("archivedAt", nil)
	admin.POST("/api/v1/projects/TC/dimensions").WithJSON(map[string]any{"key": "browser", "name": "Again"}).Expect().Status(http.StatusConflict)
	admin.POST("/api/v1/projects/TC/dimensions").WithJSON(map[string]any{"key": "Browser", "name": "x"}).Expect().Status(http.StatusBadRequest)
	admin.POST("/api/v1/projects/TC/dimensions").WithText(`{"key":"os","name":"OS"}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.POST("/api/v1/projects/NOPE/dimensions").WithJSON(map[string]any{"key": "os", "name": "OS"}).Expect().Status(http.StatusNotFound)

	admin.POST("/api/v1/projects/TC/dimensions/browser/values").WithJSON(map[string]any{"key": "chrome", "name": "Chrome"}).Expect().
		Status(http.StatusCreated).JSON().Object().Value("values").Array().Value(0).Object().HasValue("key", "chrome")
	admin.POST("/api/v1/projects/TC/dimensions/browser/values").WithJSON(map[string]any{"key": "chrome", "name": "x"}).Expect().Status(http.StatusConflict)
	admin.POST("/api/v1/projects/TC/dimensions/browser/values").WithJSON(map[string]any{"key": "-x", "name": "x"}).Expect().Status(http.StatusBadRequest)
	admin.POST("/api/v1/projects/TC/dimensions/os/values").WithJSON(map[string]any{"key": "x", "name": "x"}).Expect().Status(http.StatusNotFound)
	admin.POST("/api/v1/projects/TC/dimensions/browser/values").WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)

	admin.PATCH("/api/v1/projects/TC/dimensions/browser").WithJSON(map[string]any{"archived": true}).Expect().
		Status(http.StatusOK).JSON().Object().Value("archivedAt").String().NotEmpty()
	admin.PATCH("/api/v1/projects/TC/dimensions/browser").WithJSON(map[string]any{}).Expect().Status(http.StatusBadRequest)
	admin.PATCH("/api/v1/projects/TC/dimensions/os").WithJSON(map[string]any{"name": "OS"}).Expect().Status(http.StatusNotFound)
	admin.PATCH("/api/v1/projects/TC/dimensions/browser").WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.PATCH("/api/v1/projects/TC/dimensions/risk/values/low").WithJSON(map[string]any{"name": "Minor"}).Expect().
		Status(http.StatusOK).JSON().Object().Value("values").Array().Value(3).Object().HasValue("name", "Minor")
	admin.PATCH("/api/v1/projects/TC/dimensions/risk/values/Low").WithJSON(map[string]any{"name": "x"}).Expect().Status(http.StatusBadRequest)
	admin.PATCH("/api/v1/projects/TC/dimensions/risk/values/none").WithJSON(map[string]any{"name": "x"}).Expect().Status(http.StatusNotFound)
	admin.PATCH("/api/v1/projects/TC/dimensions/risk/values/low").WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)

	// Test cases carry tags and classification; the list filters by them.
	created := admin.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "pay", "tags": []string{"Smoke"}, "classification": map[string]any{"risk": "critical"}}).
		Expect().Status(http.StatusCreated).JSON().Object()
	created.Value("tags").Array().IsEqual([]string{"smoke"})
	created.Value("classification").Object().IsEqual(map[string]any{"risk": "critical"})
	id := strconv.FormatInt(int64(created.Value("id").Number().Raw()), 10)
	admin.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "x", "classification": map[string]any{"browser": "chrome"}}).Expect().
		Status(http.StatusBadRequest).JSON(problemOpts).Object().Value("errors").Array().Value(0).Object().HasValue("field", "classification.browser")
	admin.PATCH("/api/v1/test-cases/"+id).WithJSON(map[string]any{"classification": map[string]any{"risk": nil}, "tags": []string{"api"}}).Expect().
		Status(http.StatusOK).JSON().Object().HasValue("classification", map[string]any{}).HasValue("tags", []string{"api"})
	admin.PATCH("/api/v1/test-cases/" + id).WithJSON(map[string]any{"tags": []string{"a b"}}).Expect().Status(http.StatusBadRequest)
	admin.GET("/api/v1/test-cases").WithQuery("tag", "api").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 1)
	admin.GET("/api/v1/test-cases").WithQuery("classification", "risk:critical").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 0)
	admin.GET("/api/v1/test-cases").WithQuery("classification", "risk").Expect().Status(http.StatusBadRequest)
	admin.GET("/api/v1/test-cases").WithQuery("tag", "").Expect().Status(http.StatusBadRequest)

	// A viewer reads dimensions; writes need a maintainer (403).
	inv := admin.POST("/api/v1/invitations").WithJSON(map[string]any{"project": "TC", "role": "viewer"}).Expect().Status(http.StatusCreated).JSON().Object()
	viewer := as(e, e.POST("/api/v1/invitations/accept").WithJSON(map[string]any{"token": inv.Value("token").String().Raw(), "username": "vic",
		"displayName": "Vic", "password": "vic's password"}).Expect().Status(http.StatusCreated).JSON().Object().Value("token").String().Raw())
	viewer.GET("/api/v1/projects/TC/dimensions").Expect().Status(http.StatusOK)
	viewer.POST("/api/v1/projects/TC/dimensions").WithJSON(map[string]any{"key": "os", "name": "OS"}).Expect().Status(http.StatusForbidden)
	viewer.PATCH("/api/v1/projects/TC/dimensions/risk").WithJSON(map[string]any{"name": "R"}).Expect().Status(http.StatusForbidden)
	viewer.POST("/api/v1/projects/TC/dimensions/risk/values").WithJSON(map[string]any{"key": "x", "name": "X"}).Expect().Status(http.StatusForbidden)
	viewer.PATCH("/api/v1/projects/TC/dimensions/risk/values/low").WithJSON(map[string]any{"name": "L"}).Expect().Status(http.StatusForbidden)
}

// TestSuites: suites per project (maintainers write, anyone in the project reads), the list and run filters, and
// runs reported for a suite (partial runs, MVP D2).
func TestSuites(t *testing.T) {
	s := fresh(t)
	admin := api(t, s, 1<<20)
	e := anon(t, s, 1<<20)
	tc := admin.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "login", "automated": true, "tags": []string{"smoke"}}).
		Expect().Status(http.StatusCreated).JSON().Object()
	id := int64(tc.Value("id").Number().Raw())
	admin.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "pay", "automated": true}).Expect().Status(http.StatusCreated)

	suites := "/api/v1/projects/TC/suites"
	admin.POST(suites).WithJSON(map[string]any{"key": "release", "name": "Release", "kind": "static", "testCaseIds": []int64{id}}).Expect().
		Status(http.StatusCreated).JSON().Object().HasValue("testCaseIds", []int64{id}).HasValue("query", nil).HasValue("caseCount", 1)
	admin.POST(suites).WithJSON(map[string]any{"key": "smoke", "name": "Smoke", "kind": "query", "query": map[string]any{"tag": "smoke"}}).Expect().
		Status(http.StatusCreated).JSON().Object().Value("query").Object().HasValue("tag", "smoke").HasValue("classification", []string{})
	admin.POST(suites).WithJSON(map[string]any{"key": "smoke", "name": "x", "kind": "query", "query": map[string]any{"tag": "x"}}).Expect().Status(http.StatusConflict)
	admin.POST(suites).WithJSON(map[string]any{"key": "x", "name": "x", "kind": "query"}).Expect().Status(http.StatusBadRequest)
	admin.POST(suites).WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.POST("/api/v1/projects/NOPE/suites").WithJSON(map[string]any{"key": "x", "name": "x", "kind": "static"}).Expect().Status(http.StatusNotFound)
	admin.GET(suites).Expect().Status(http.StatusOK).JSON().Object().Value("items").Array().Length().IsEqual(2)
	admin.GET("/api/v1/projects/tc/suites").Expect().Status(http.StatusBadRequest)
	admin.GET("/api/v1/projects/NOPE/suites").Expect().Status(http.StatusNotFound)
	admin.GET(suites+"/release").Expect().Status(http.StatusOK).JSON().Object().HasValue("kind", "static")
	admin.GET(suites + "/Bad").Expect().Status(http.StatusBadRequest)
	admin.GET(suites + "/nope").Expect().Status(http.StatusNotFound)
	admin.PATCH(suites+"/smoke").WithJSON(map[string]any{"description": "fast checks"}).Expect().Status(http.StatusOK).JSON().Object().HasValue("description", "fast checks")
	admin.PATCH(suites + "/release").WithJSON(map[string]any{"query": map[string]any{"tag": "x"}}).Expect().Status(http.StatusBadRequest)
	admin.PATCH(suites + "/nope").WithJSON(map[string]any{"name": "x"}).Expect().Status(http.StatusNotFound)
	admin.PATCH(suites + "/smoke").WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.PUT(suites+"/release/cases").WithJSON(map[string]any{"testCaseIds": []int64{}}).Expect().Status(http.StatusOK).JSON().Object().HasValue("caseCount", 0)
	admin.PUT(suites + "/release/cases").WithJSON(map[string]any{"testCaseIds": []int64{987654}}).Expect().Status(http.StatusBadRequest)
	admin.PUT(suites + "/nope/cases").WithJSON(map[string]any{"testCaseIds": []int64{}}).Expect().Status(http.StatusNotFound)
	admin.PUT(suites + "/release/cases").WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)

	// Lists narrowed to a suite.
	admin.GET("/api/v1/test-cases").WithQuery("project", "TC").WithQuery("suite", "smoke").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 1)
	admin.GET("/api/v1/test-cases").WithQuery("project", "TC").WithQuery("suite", "nope").Expect().Status(http.StatusNotFound)

	// A run for a suite: only the suite's test cases are expected; the run names its suite.
	run := ingest(admin, "70", 1, `<testsuite><testcase name="login TC-`+strconv.FormatInt(id, 10)+`"/></testsuite>`).WithQuery("suite", "smoke").
		Expect().Status(http.StatusCreated).JSON().Object().Value("testRun").Object()
	run.HasValue("expectedCount", 1).Value("suite").Object().IsEqual(map[string]any{"key": "smoke", "name": "Smoke"})
	ingest(admin, "71", 1, `<testsuite/>`).Expect().Status(http.StatusCreated).JSON().Object().Value("testRun").Object().HasValue("suite", nil)
	admin.GET("/api/v1/test-runs").WithQuery("suite", "smoke").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 1)
	admin.GET("/api/v1/test-runs").WithQuery("suite", "Smoke").Expect().Status(http.StatusBadRequest)
	ingest(admin, "72", 1, `<testsuite/>`).WithQuery("suite", "nope").Expect().Status(http.StatusNotFound)
	admin.PATCH(suites + "/smoke").WithJSON(map[string]any{"archived": true}).Expect().Status(http.StatusOK)
	ingest(admin, "73", 1, `<testsuite/>`).WithQuery("suite", "smoke").Expect().Status(http.StatusConflict).JSON(problemOpts).Object().HasValue("code", "conflict")

	// A viewer reads suites; writes need a maintainer (403).
	inv := admin.POST("/api/v1/invitations").WithJSON(map[string]any{"project": "TC", "role": "viewer"}).Expect().Status(http.StatusCreated).JSON().Object()
	viewer := as(e, e.POST("/api/v1/invitations/accept").WithJSON(map[string]any{"token": inv.Value("token").String().Raw(), "username": "vic",
		"displayName": "Vic", "password": "vic's password"}).Expect().Status(http.StatusCreated).JSON().Object().Value("token").String().Raw())
	viewer.GET(suites).Expect().Status(http.StatusOK)
	viewer.GET(suites + "/release").Expect().Status(http.StatusOK)
	viewer.POST(suites).WithJSON(map[string]any{"key": "v", "name": "V", "kind": "static"}).Expect().Status(http.StatusForbidden)
	viewer.PATCH(suites + "/release").WithJSON(map[string]any{"name": "R"}).Expect().Status(http.StatusForbidden)
	viewer.PUT(suites + "/release/cases").WithJSON(map[string]any{"testCaseIds": []int64{}}).Expect().Status(http.StatusForbidden)
}

// TestManualExecution: members start manual runs, record results while they run and finish them.
func TestManualExecution(t *testing.T) {
	s := fresh(t)
	admin := api(t, s, 1<<20)
	e := anon(t, s, 1<<20)
	tc := admin.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "checkout"}).Expect().Status(http.StatusCreated).JSON().Object()
	id := int64(tc.Value("id").Number().Raw())

	run := admin.POST("/api/v1/test-runs/manual").WithJSON(map[string]any{"project": "TC", "name": "Sign-off", "scope": "manual", "branch": "main"}).
		Expect().Status(http.StatusCreated).JSON().Object()
	run.HasValue("mode", "manual").HasValue("executionStatus", "running").HasValue("startedBy", "admin").HasValue("expectedCount", 1).HasValue("completedAt", nil)
	runID := strconv.FormatInt(int64(run.Value("id").Number().Raw()), 10)
	admin.POST("/api/v1/test-runs/manual").WithJSON(map[string]any{"project": "TC", "name": " "}).Expect().Status(http.StatusBadRequest)
	admin.POST("/api/v1/test-runs/manual").WithJSON(map[string]any{"project": "NOPE", "name": "x"}).Expect().Status(http.StatusNotFound)
	admin.POST("/api/v1/test-runs/manual").WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.POST("/api/v1/projects/TC/suites").WithJSON(map[string]any{"key": "old", "name": "Old", "kind": "static"}).Expect().Status(http.StatusCreated)
	admin.PATCH("/api/v1/projects/TC/suites/old").WithJSON(map[string]any{"archived": true}).Expect().Status(http.StatusOK)
	admin.POST("/api/v1/test-runs/manual").WithJSON(map[string]any{"project": "TC", "name": "x", "suite": "old"}).Expect().Status(http.StatusConflict)

	results := "/api/v1/test-runs/" + runID + "/manual-results"
	admin.POST(results).WithJSON(map[string]any{"testCaseId": id, "status": "failed", "note": "Pay button missing", "failedStep": 2}).Expect().
		Status(http.StatusCreated).JSON().Object().HasValue("recordedBy", "admin").HasValue("failedStep", 2).HasValue("attempt", 1).HasValue("testCaseKey", "TC-"+strconv.FormatInt(id, 10))
	admin.POST(results).WithJSON(map[string]any{"testCaseId": id, "status": "passed"}).Expect().Status(http.StatusCreated).JSON().Object().HasValue("attempt", 2)
	admin.POST(results).WithJSON(map[string]any{"testCaseId": id, "status": "blocked"}).Expect().Status(http.StatusBadRequest)
	admin.POST(results).WithJSON(map[string]any{"testCaseId": 987654, "status": "passed"}).Expect().Status(http.StatusConflict)
	admin.POST(results).WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.POST("/api/v1/test-runs/987654/manual-results").WithJSON(map[string]any{"testCaseId": id, "status": "passed"}).Expect().Status(http.StatusNotFound)

	finish := "/api/v1/test-runs/" + runID + "/finish"
	admin.POST(finish).WithJSON(map[string]any{"status": "interrupted"}).Expect().Status(http.StatusBadRequest)
	admin.POST(finish).WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.POST("/api/v1/test-runs/987654/finish").WithJSON(map[string]any{"status": "completed"}).Expect().Status(http.StatusNotFound)
	admin.POST(finish).WithJSON(map[string]any{"status": "completed"}).Expect().Status(http.StatusOK).JSON().Object().
		HasValue("executionStatus", "completed").Value("outcome").Object().HasValue("verdict", "passed")
	admin.POST(finish).WithJSON(map[string]any{"status": "cancelled"}).Expect().Status(http.StatusConflict)
	admin.GET("/api/v1/test-runs/"+runID+"/results").Expect().Status(http.StatusOK).JSON().Object().
		Value("items").Array().Value(0).Object().HasValue("recordedBy", "admin").HasValue("errorMessage", "Pay button missing")

	// A viewer reads the run but cannot run it (403).
	inv := admin.POST("/api/v1/invitations").WithJSON(map[string]any{"project": "TC", "role": "viewer"}).Expect().Status(http.StatusCreated).JSON().Object()
	viewer := as(e, e.POST("/api/v1/invitations/accept").WithJSON(map[string]any{"token": inv.Value("token").String().Raw(), "username": "vic",
		"displayName": "Vic", "password": "vic's password"}).Expect().Status(http.StatusCreated).JSON().Object().Value("token").String().Raw())
	viewer.GET("/api/v1/test-runs/" + runID).Expect().Status(http.StatusOK)
	viewer.POST("/api/v1/test-runs/manual").WithJSON(map[string]any{"project": "TC", "name": "x"}).Expect().Status(http.StatusForbidden)
	viewer.POST(results).WithJSON(map[string]any{"testCaseId": id, "status": "passed"}).Expect().Status(http.StatusForbidden)
	viewer.POST(finish).WithJSON(map[string]any{"status": "completed"}).Expect().Status(http.StatusForbidden)

	// A CI API key reports runs; it never starts, records or finishes a manual run (401: not a session route).
	key := as(e, admin.POST("/api/v1/projects/TC/api-keys").WithJSON(map[string]any{"name": "ci"}).Expect().
		Status(http.StatusCreated).JSON().Object().Value("token").String().Raw())
	key.POST("/api/v1/test-runs/manual").WithJSON(map[string]any{"project": "TC", "name": "x"}).Expect().Status(http.StatusUnauthorized)
	key.POST(results).WithJSON(map[string]any{"testCaseId": id, "status": "passed"}).Expect().Status(http.StatusUnauthorized)
	key.POST(finish).WithJSON(map[string]any{"status": "completed"}).Expect().Status(http.StatusUnauthorized)
}

// TestRequirements: requirements per project (members write, maintainers import, anyone in the project reads) and
// their coverage from the latest results.
func TestRequirements(t *testing.T) {
	s := fresh(t)
	admin := api(t, s, 1<<20)
	e := anon(t, s, 1<<20)
	tc := admin.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "refund", "automated": true}).Expect().Status(http.StatusCreated).JSON().Object()
	id := int64(tc.Value("id").Number().Raw())
	reqs := "/api/v1/projects/TC/requirements"

	native := admin.POST(reqs).WithJSON(map[string]any{"title": "Sign in", "url": "https://x.test"}).Expect().Status(http.StatusCreated).JSON().Object()
	native.HasValue("externalId", "R-1").HasValue("provider", "provenly").HasValue("lastSyncedAt", nil).Value("coverage").Object().HasValue("status", "uncovered")
	jira := admin.POST(reqs).WithJSON(map[string]any{"title": "Refunds", "provider": "jira", "externalId": "PAY-12"}).Expect().Status(http.StatusCreated).JSON().Object()
	jiraID := strconv.FormatInt(int64(jira.Value("id").Number().Raw()), 10)
	admin.POST(reqs).WithJSON(map[string]any{"title": "x", "provider": "jira", "externalId": "PAY-12"}).Expect().Status(http.StatusConflict)
	admin.POST(reqs).WithJSON(map[string]any{"title": " "}).Expect().Status(http.StatusBadRequest)
	admin.POST(reqs).WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.POST("/api/v1/projects/NOPE/requirements").WithJSON(map[string]any{"title": "x"}).Expect().Status(http.StatusNotFound)

	admin.POST(reqs + "/import").WithJSON(map[string]any{"provider": "jira", "items": []map[string]any{{"externalId": "PAY-12", "title": "Refunds v2", "providerStatus": "Done"},
		{"externalId": "PAY-13", "title": "Chargebacks"}}}).Expect().Status(http.StatusOK).JSON().Object().IsEqual(map[string]any{"created": 1, "updated": 1})
	admin.POST(reqs + "/import").WithJSON(map[string]any{"provider": "provenly", "items": []map[string]any{{"externalId": "1", "title": "x"}}}).Expect().Status(http.StatusBadRequest)
	admin.POST(reqs + "/import").WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.POST("/api/v1/projects/NOPE/requirements/import").WithJSON(map[string]any{"provider": "jira", "items": []map[string]any{{"externalId": "1", "title": "x"}}}).Expect().Status(http.StatusNotFound)

	admin.PUT(reqs+"/"+jiraID+"/test-cases").WithJSON(map[string]any{"testCaseIds": []int64{id}}).Expect().Status(http.StatusOK).
		JSON().Object().Value("coverage").Object().HasValue("status", "not_run").Value("testCases").Array().Value(0).Object().HasValue("status", nil)
	ingest(admin, "90", 1, `<testsuite><testcase name="refund TC-`+strconv.FormatInt(id, 10)+`"/></testsuite>`).Expect().Status(http.StatusCreated)
	admin.GET(reqs+"/"+jiraID).Expect().Status(http.StatusOK).JSON().Object().HasValue("title", "Refunds v2").
		Value("coverage").Object().HasValue("status", "passing").HasValue("passed", 1)
	admin.GET(reqs).WithQuery("testCase", id).Expect().Status(http.StatusOK).JSON().Object().Value("items").Array().Length().IsEqual(1)
	admin.GET(reqs).WithQuery("testCase", "x").Expect().Status(http.StatusBadRequest)
	admin.GET("/api/v1/projects/NOPE/requirements").Expect().Status(http.StatusNotFound)
	admin.GET(reqs + "/0").Expect().Status(http.StatusBadRequest)
	admin.GET(reqs + "/987654").Expect().Status(http.StatusNotFound)
	admin.PATCH(reqs + "/" + jiraID).WithJSON(map[string]any{"archived": true}).Expect().Status(http.StatusOK).JSON().Object().Value("archivedAt").String().NotEmpty()
	admin.PATCH(reqs + "/" + jiraID).WithJSON(map[string]any{}).Expect().Status(http.StatusBadRequest)
	admin.PATCH(reqs + "/987654").WithJSON(map[string]any{"title": "x"}).Expect().Status(http.StatusNotFound)
	admin.PATCH(reqs + "/" + jiraID).WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.PUT(reqs + "/" + jiraID + "/test-cases").WithJSON(map[string]any{"testCaseIds": []int64{987654}}).Expect().Status(http.StatusBadRequest)
	admin.PUT(reqs + "/987654/test-cases").WithJSON(map[string]any{"testCaseIds": []int64{}}).Expect().Status(http.StatusNotFound)
	admin.PUT(reqs + "/" + jiraID + "/test-cases").WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)

	// Viewers read; members (and up) write; only maintainers import.
	inv := admin.POST("/api/v1/invitations").WithJSON(map[string]any{"project": "TC", "role": "viewer"}).Expect().Status(http.StatusCreated).JSON().Object()
	viewer := as(e, e.POST("/api/v1/invitations/accept").WithJSON(map[string]any{"token": inv.Value("token").String().Raw(), "username": "vic",
		"displayName": "Vic", "password": "vic's password"}).Expect().Status(http.StatusCreated).JSON().Object().Value("token").String().Raw())
	viewer.GET(reqs).Expect().Status(http.StatusOK)
	viewer.GET(reqs + "/" + jiraID).Expect().Status(http.StatusOK)
	viewer.POST(reqs).WithJSON(map[string]any{"title": "x"}).Expect().Status(http.StatusForbidden)
	viewer.POST(reqs + "/import").WithJSON(map[string]any{"provider": "jira", "items": []map[string]any{{"externalId": "1", "title": "x"}}}).Expect().Status(http.StatusForbidden)
	viewer.PATCH(reqs + "/" + jiraID).WithJSON(map[string]any{"title": "x"}).Expect().Status(http.StatusForbidden)
	viewer.PUT(reqs + "/" + jiraID + "/test-cases").WithJSON(map[string]any{"testCaseIds": []int64{}}).Expect().Status(http.StatusForbidden)
}

func TestIssues(t *testing.T) {
	s := fresh(t)
	admin := api(t, s, 1<<20)
	e := anon(t, s, 1<<20)
	tc := admin.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "refund", "automated": true}).Expect().Status(http.StatusCreated).JSON().Object()
	id := int64(tc.Value("id").Number().Raw())
	issues := "/api/v1/projects/TC/issues"

	native := admin.POST(issues).WithJSON(map[string]any{"title": "Double refund", "url": "https://x.test"}).Expect().Status(http.StatusCreated).JSON().Object()
	native.HasValue("externalId", "I-1").HasValue("provider", "provenly").HasValue("state", "open").HasValue("closedAt", nil).
		Value("verification").Object().HasValue("status", "unlinked")
	jira := admin.POST(issues).WithJSON(map[string]any{"title": "Refund fails", "provider": "jira", "externalId": "PAY-7", "state": "closed"}).Expect().Status(http.StatusCreated).JSON().Object()
	jira.Value("closedAt").String().NotEmpty()
	jiraID := strconv.FormatInt(int64(jira.Value("id").Number().Raw()), 10)
	admin.POST(issues).WithJSON(map[string]any{"title": "x", "provider": "jira", "externalId": "PAY-7"}).Expect().Status(http.StatusConflict)
	admin.POST(issues).WithJSON(map[string]any{"title": "x", "state": "done"}).Expect().Status(http.StatusBadRequest)
	admin.POST(issues).WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.POST("/api/v1/projects/NOPE/issues").WithJSON(map[string]any{"title": "x"}).Expect().Status(http.StatusNotFound)

	admin.POST(issues + "/import").WithJSON(map[string]any{"provider": "jira", "items": []map[string]any{{"externalId": "PAY-7", "title": "Refund fails v2", "state": "closed", "providerStatus": "Done"},
		{"externalId": "PAY-8", "title": "Slow", "state": "open"}}}).Expect().Status(http.StatusOK).JSON().Object().IsEqual(map[string]any{"created": 1, "updated": 1})
	admin.POST(issues + "/import").WithJSON(map[string]any{"provider": "jira", "items": []map[string]any{{"externalId": "1", "title": "x"}}}).Expect().Status(http.StatusBadRequest)
	admin.POST(issues + "/import").WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.POST("/api/v1/projects/NOPE/issues/import").WithJSON(map[string]any{"provider": "jira", "items": []map[string]any{{"externalId": "1", "title": "x", "state": "open"}}}).Expect().Status(http.StatusNotFound)

	admin.PUT(issues+"/"+jiraID+"/test-cases").WithJSON(map[string]any{"testCaseIds": []int64{id}}).Expect().Status(http.StatusOK).
		JSON().Object().Value("verification").Object().HasValue("status", "unverified").Value("testCases").Array().Value(0).Object().HasValue("evidence", nil)
	ingest(admin, "91", 1, `<testsuite><testcase name="refund TC-`+strconv.FormatInt(id, 10)+`"><failure message="x"/></testcase></testsuite>`).Expect().Status(http.StatusCreated)
	ingest(admin, "92", 1, `<testsuite><testcase name="refund TC-`+strconv.FormatInt(id, 10)+`"><skipped/></testcase></testsuite>`).Expect().Status(http.StatusCreated)
	link := admin.GET(issues+"/"+jiraID).Expect().Status(http.StatusOK).JSON().Object().HasValue("title", "Refund fails v2").
		Value("verification").Object().HasValue("status", "reopen").Value("testCases").Array().Value(0).Object()
	link.HasValue("evidence", "failed").HasValue("latestInconclusive", true).Value("evidenceRunId").Number().Gt(0)
	admin.GET(issues).WithQuery("testCase", id).Expect().Status(http.StatusOK).JSON().Object().Value("items").Array().Length().IsEqual(1)
	admin.GET(issues).WithQuery("state", "open").Expect().Status(http.StatusOK).JSON().Object().Value("items").Array().Length().IsEqual(2)
	admin.GET(issues).WithQuery("state", "done").Expect().Status(http.StatusBadRequest)
	admin.GET(issues).WithQuery("testCase", "x").Expect().Status(http.StatusBadRequest)
	admin.GET("/api/v1/projects/NOPE/issues").Expect().Status(http.StatusNotFound)
	admin.GET(issues + "/0").Expect().Status(http.StatusBadRequest)
	admin.GET(issues + "/987654").Expect().Status(http.StatusNotFound)
	admin.PATCH(issues+"/"+jiraID).WithJSON(map[string]any{"state": "open"}).Expect().Status(http.StatusOK).JSON().Object().
		HasValue("closedAt", nil).Value("verification").Object().HasValue("status", "known_issue")
	admin.PATCH(issues + "/" + jiraID).WithJSON(map[string]any{}).Expect().Status(http.StatusBadRequest)
	admin.PATCH(issues + "/987654").WithJSON(map[string]any{"title": "x"}).Expect().Status(http.StatusNotFound)
	admin.PATCH(issues + "/" + jiraID).WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.PUT(issues + "/" + jiraID + "/test-cases").WithJSON(map[string]any{"testCaseIds": []int64{987654}}).Expect().Status(http.StatusBadRequest)
	admin.PUT(issues + "/987654/test-cases").WithJSON(map[string]any{"testCaseIds": []int64{}}).Expect().Status(http.StatusNotFound)
	admin.PUT(issues + "/" + jiraID + "/test-cases").WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)

	// Viewers read; members (and up) write; only maintainers import.
	inv := admin.POST("/api/v1/invitations").WithJSON(map[string]any{"project": "TC", "role": "viewer"}).Expect().Status(http.StatusCreated).JSON().Object()
	viewer := as(e, e.POST("/api/v1/invitations/accept").WithJSON(map[string]any{"token": inv.Value("token").String().Raw(), "username": "vic",
		"displayName": "Vic", "password": "vic's password"}).Expect().Status(http.StatusCreated).JSON().Object().Value("token").String().Raw())
	viewer.GET(issues).Expect().Status(http.StatusOK)
	viewer.GET(issues + "/" + jiraID).Expect().Status(http.StatusOK)
	viewer.POST(issues).WithJSON(map[string]any{"title": "x"}).Expect().Status(http.StatusForbidden)
	viewer.POST(issues + "/import").WithJSON(map[string]any{"provider": "jira", "items": []map[string]any{{"externalId": "1", "title": "x", "state": "open"}}}).Expect().Status(http.StatusForbidden)
	viewer.PATCH(issues + "/" + jiraID).WithJSON(map[string]any{"title": "x"}).Expect().Status(http.StatusForbidden)
	viewer.PUT(issues + "/" + jiraID + "/test-cases").WithJSON(map[string]any{"testCaseIds": []int64{}}).Expect().Status(http.StatusForbidden)
}

func TestQuality(t *testing.T) {
	s := fresh(t)
	admin := api(t, s, 1<<20)
	e := anon(t, s, 1<<20)
	login := admin.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "login", "automated": true}).Expect().Status(http.StatusCreated).JSON().Object()
	admin.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "receipt"}).Expect().Status(http.StatusCreated)
	key := login.Value("key").String().Raw()
	ingest(admin, "93", 1, `<testsuite><testcase name="login"><properties><property name="tc-id" value="`+key+`"/></properties><flakyFailure message="x"/></testcase></testsuite>`).
		Expect().Status(http.StatusCreated)

	q := admin.GET("/api/v1/projects/TC/quality").Expect().Status(http.StatusOK).JSON().Object()
	q.Value("testCases").Object().IsEqual(map[string]any{"active": 2, "automated": 1, "manual": 1, "automationRate": 50})
	ex := q.Value("execution").Object().HasValue("staleDays", 14).HasValue("neverExecuted", 1).HasValue("stale", 0)
	ex.Value("testCases").Array().Value(0).Object().HasValue("lastExecutedAt", nil)
	q.Value("flaky").Object().HasValue("window", 20).Value("testCases").Array().Value(0).Object().HasValue("testCaseKey", key).HasValue("runs", 1)
	admin.GET("/api/v1/projects/TC/quality").WithQuery("staleDays", 7).WithQuery("window", 5).Expect().Status(http.StatusOK).
		JSON().Object().Value("flaky").Object().HasValue("window", 5)
	admin.GET("/api/v1/projects/TC/quality").WithQuery("staleDays", 366).Expect().Status(http.StatusBadRequest)
	admin.GET("/api/v1/projects/TC/quality").WithQuery("window", "x").Expect().Status(http.StatusBadRequest)
	admin.GET("/api/v1/projects/tc/quality").Expect().Status(http.StatusBadRequest)
	admin.GET("/api/v1/projects/NOPE/quality").Expect().Status(http.StatusNotFound)

	// Viewers of a project read it; other projects stay invisible.
	inv := admin.POST("/api/v1/invitations").WithJSON(map[string]any{"project": "TC", "role": "viewer"}).Expect().Status(http.StatusCreated).JSON().Object()
	viewer := as(e, e.POST("/api/v1/invitations/accept").WithJSON(map[string]any{"token": inv.Value("token").String().Raw(), "username": "vic",
		"displayName": "Vic", "password": "vic's password"}).Expect().Status(http.StatusCreated).JSON().Object().Value("token").String().Raw())
	viewer.GET("/api/v1/projects/TC/quality").Expect().Status(http.StatusOK)
	admin.POST("/api/v1/projects").WithJSON(map[string]any{"key": "CHK", "name": "Checkout"}).Expect().Status(http.StatusCreated)
	viewer.GET("/api/v1/projects/CHK/quality").Expect().Status(http.StatusNotFound)
}

func TestLiveRuns(t *testing.T) {
	s := fresh(t)
	admin := api(t, s, 1<<20)
	e := anon(t, s, 1<<20)
	tcs := make([]string, 3)
	for i := range tcs {
		tcs[i] = admin.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "t" + strconv.Itoa(i), "automated": true}).
			Expect().Status(http.StatusCreated).JSON().Object().Value("key").String().Raw()
	}
	admin.POST("/api/v1/projects").WithJSON(map[string]any{"key": "CHK", "name": "Checkout"}).Expect().Status(http.StatusCreated)
	key := as(e, admin.POST("/api/v1/projects/CHK/api-keys").WithJSON(map[string]any{"name": "CI"}).Expect().Status(http.StatusCreated).
		JSON().Object().Value("token").String().Raw())

	start := map[string]any{"project": "TC", "provider": "github", "runId": "300", "runAttempt": 1, "pipeline": "ci"}
	run := admin.POST("/api/v1/test-runs/live").WithJSON(start).Expect().Status(http.StatusCreated).JSON().Object()
	run.HasValue("mode", "live").HasValue("executionStatus", "running").HasValue("expectedCount", 3)
	id := strconv.FormatInt(int64(run.Value("id").Number().Raw()), 10)
	admin.POST("/api/v1/test-runs/live").WithJSON(start).Expect().Status(http.StatusCreated).JSON().Object().Value("id").Number().IsEqual(run.Value("id").Number().Raw())
	admin.POST("/api/v1/test-runs/live").WithJSON(map[string]any{"provider": "github", "runId": "300"}).Expect().Status(http.StatusBadRequest)
	admin.POST("/api/v1/test-runs/live").WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.POST("/api/v1/test-runs/live").WithJSON(map[string]any{"project": "NOPE", "provider": "github", "runId": "1", "runAttempt": 1}).Expect().Status(http.StatusNotFound)
	key.POST("/api/v1/test-runs/live").WithJSON(start).Expect().Status(http.StatusNotFound)
	ingest(admin, "301", 1, `<testsuite/>`).Expect().Status(http.StatusCreated)
	admin.POST("/api/v1/test-runs/live").WithJSON(map[string]any{"provider": "github", "runId": "301", "runAttempt": 1}).Expect().Status(http.StatusConflict)

	events := admin.POST("/api/v1/test-runs/" + id + "/events")
	events.WithJSON(map[string]any{"events": []map[string]any{
		{"eventId": "e1", "sequence": 1, "type": "test.started", "testName": "t0", "testCase": tcs[0]},
		{"eventId": "e2", "sequence": 2, "type": "test.finished", "testName": "t0", "testCase": tcs[0], "status": "passed", "occurredAt": "2026-10-05T12:00:00Z"},
		{"eventId": "e3", "sequence": 3, "type": "test.started", "testName": "t1", "testCase": tcs[1]},
		{"eventId": "e4", "sequence": 4, "type": "test.started", "testName": "x", "testCase": "TC-999"},
	}}).Expect().Status(http.StatusOK).JSON().Object().IsEqual(map[string]any{"accepted": 4, "duplicates": 0})
	admin.POST("/api/v1/test-runs/" + id + "/events").WithJSON(map[string]any{"events": []map[string]any{{"eventId": "e1", "sequence": 1, "type": "test.started"}}}).
		Expect().Status(http.StatusOK).JSON().Object().IsEqual(map[string]any{"accepted": 0, "duplicates": 1})
	admin.POST("/api/v1/test-runs/" + id + "/events").WithJSON(map[string]any{"events": []map[string]any{{"eventId": "x", "sequence": 1, "type": "test.finished"}}}).
		Expect().Status(http.StatusBadRequest)
	admin.POST("/api/v1/test-runs/" + id + "/events").WithJSON(map[string]any{"events": []map[string]any{{"eventId": "x", "sequence": 1, "type": "run.finished", "attempt": 101}}}).
		Expect().Status(http.StatusBadRequest)
	admin.POST("/api/v1/test-runs/" + id + "/events").WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.POST("/api/v1/test-runs/987654/events").WithJSON(map[string]any{"events": []map[string]any{{"eventId": "x", "sequence": 1, "type": "run.finished"}}}).Expect().Status(http.StatusNotFound)
	key.POST("/api/v1/test-runs/" + id + "/events").WithJSON(map[string]any{"events": []map[string]any{{"eventId": "x", "sequence": 1, "type": "run.finished"}}}).Expect().Status(http.StatusNotFound)

	live := admin.GET("/api/v1/test-runs/" + id + "/live").Expect().Status(http.StatusOK).JSON().Object()
	live.HasValue("reconciliation", "pending").HasValue("events", 4).HasValue("waiting", 1).HasValue("running", 1).HasValue("finished", 1)
	admin.GET("/api/v1/test-runs/0/live").Expect().Status(http.StatusBadRequest)
	admin.GET("/api/v1/test-runs/987654/live").Expect().Status(http.StatusNotFound)

	// The final report completes the run; live events and results are reconciled.
	ingest(admin, "300", 1, `<testsuite><testcase name="t0"><properties><property name="tc-id" value="`+tcs[0]+`"/></properties><failure/></testcase>`+
		`<testcase name="t2"><properties><property name="tc-id" value="`+tcs[2]+`"/></properties></testcase></testsuite>`).
		Expect().Status(http.StatusCreated).JSON().Object().HasValue("created", true).Value("testRun").Object().HasValue("executionStatus", "completed")
	rec := admin.GET("/api/v1/test-runs/"+id+"/live").Expect().Status(http.StatusOK).JSON().Object().HasValue("reconciliation", "mismatch")
	rec.Value("mismatches").Array().Length().IsEqual(4)
	admin.POST("/api/v1/test-runs/" + id + "/events").WithJSON(map[string]any{"events": []map[string]any{{"eventId": "late", "sequence": 9, "type": "run.finished"}}}).
		Expect().Status(http.StatusConflict)
	// Viewers read live runs but neither start them nor stream events.
	inv := admin.POST("/api/v1/invitations").WithJSON(map[string]any{"project": "TC", "role": "viewer"}).Expect().Status(http.StatusCreated).JSON().Object()
	viewer := as(e, e.POST("/api/v1/invitations/accept").WithJSON(map[string]any{"token": inv.Value("token").String().Raw(), "username": "vic",
		"displayName": "Vic", "password": "vic's password"}).Expect().Status(http.StatusCreated).JSON().Object().Value("token").String().Raw())
	viewer.GET("/api/v1/test-runs/" + id + "/live").Expect().Status(http.StatusOK)
	viewer.POST("/api/v1/test-runs/live").WithJSON(map[string]any{"provider": "github", "runId": "303", "runAttempt": 1}).Expect().Status(http.StatusForbidden)
	open := admin.POST("/api/v1/test-runs/live").WithJSON(map[string]any{"provider": "github", "runId": "304", "runAttempt": 1}).Expect().Status(http.StatusCreated).
		JSON().Object().Value("id").Number().Raw()
	viewer.POST("/api/v1/test-runs/" + strconv.FormatInt(int64(open), 10) + "/events").WithJSON(map[string]any{"events": []map[string]any{{"eventId": "v", "sequence": 1, "type": "run.finished"}}}).
		Expect().Status(http.StatusForbidden)
	batchID := strconv.FormatInt(int64(ingest(admin, "302", 1, `<testsuite/>`).Expect().Status(http.StatusCreated).JSON().Object().Value("testRun").Object().Value("id").Number().Raw()), 10)
	admin.GET("/api/v1/test-runs/" + batchID + "/live").Expect().Status(http.StatusConflict)
}

// TestTraceIDs: with OpenTelemetry set up every response names its trace in X-Trace-Id, continuing a W3C
// traceparent sent by the caller (CI, a proxy or the browser).
func TestTraceIDs(t *testing.T) {
	shutdown, err := telemetry.Setup(context.Background(), "test", false, nil)
	require.NoError(t, err)
	defer func() { _ = shutdown(context.Background()) }()
	s := fresh(t)
	e := anon(t, s, 1<<20)
	e.GET("/healthz").WithHeader("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01").
		Expect().Status(http.StatusOK).Header(telemetry.TraceHeader).IsEqual("4bf92f3577b34da6a3ce929d0e0e4736")
	e.GET("/api/v1/test-cases").Expect().Status(http.StatusUnauthorized).Header(telemetry.TraceHeader).Length().IsEqual(32)
}
