//go:build contract

package contract

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gavv/httpexpect/v2"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/app"
	"github.com/edcrove/provenly/backend/internal/platform/postgres"
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

	down := app.NewServices(db.Pool, time.Now)
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
	small := api(t, app.NewServices(db.Pool, time.Now), 64)
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
	pool, err := postgres.Open(context.Background(), db.URL)
	require.NoError(t, err)
	pool.Close()
	e := api(t, app.NewServices(pool, time.Now), 1<<20)
	problem := func(r *httpexpect.Response) {
		r.Status(http.StatusInternalServerError).JSON(problemOpts).Object().HasValue("code", "internal_error")
	}
	problem(e.GET("/api/v1/projects").Expect())
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
// every query parameter error is reported at once (docs/review.md finding 24).
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
	send("gzip", "application/xml", []byte{0x1f, 0x8b}).WithHeader("Content-Encoding", "gzip").Expect().
		Status(http.StatusUnsupportedMediaType).JSON(problemOpts).Object().HasValue("code", "unsupported_media_type")

	errs := e.POST("/api/v1/ingestion/junit").WithHeader("Content-Type", xmlType).WithText(`<testsuite/>`).Expect().
		Status(http.StatusBadRequest).JSON(problemOpts).Object().Value("errors").Array()
	errs.Length().IsEqual(3)

	send("empty", xmlType, nil).Expect().Status(http.StatusBadRequest).JSON(problemOpts).Object().HasValue("code", "invalid_junit")

	raw := offContract(t, fresh(t))
	raw.POST("/api/v1/ingestion/junit").WithQuery("provider", "github").WithQuery("runId", "plusxml").WithQuery("runAttempt", 1).
		WithHeader("Content-Type", "application/junit+xml").WithText(`<testsuite name="s"/>`).Expect().Status(http.StatusCreated)
	raw.GET("/api/v1/ingestion/junit").Expect().Status(http.StatusMethodNotAllowed)
}
