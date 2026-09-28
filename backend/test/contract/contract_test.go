//go:build contract

package contract

import (
	"context"
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

func TestSystem(t *testing.T) {
	e := api(t, fresh(t), 1<<20)
	e.GET("/healthz").Expect().Status(http.StatusOK).JSON().Object().HasValue("status", "ok")
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

	e.GET("/api/v1/test-cases/xyz/results").Expect().Status(http.StatusBadRequest)
	e.GET("/api/v1/test-cases/987654/results").Expect().Status(http.StatusNotFound)
	e.GET(path+"/results").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 0)
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
	ingest(e, "1", 1, "<not-xml").Expect().Status(http.StatusBadRequest).JSON(problemOpts).Object().HasValue("code", "invalid_junit")
	e.POST("/api/v1/ingestion/junit").WithQuery("provider", "github").WithQuery("runId", "1").WithQuery("runAttempt", 1).
		WithJSON(map[string]any{}).Expect().Status(http.StatusUnsupportedMediaType)
	small := api(t, app.NewServices(db.Pool, time.Now), 64)
	ingest(small, "9", 1, report(id)).Expect().Status(http.StatusRequestEntityTooLarge).JSON(problemOpts).Object().HasValue("code", "payload_too_large")

	e.GET("/api/v1/test-runs").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 2)
	e.GET("/api/v1/test-runs").WithQuery("pageSize", 0).Expect().Status(http.StatusBadRequest)

	e.GET("/api/v1/test-runs/"+runID).Expect().Status(http.StatusOK).JSON().Object().HasValue("externalRunId", "github:1:1")
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
		JSON().Object().HasValue("totalItems", 2)
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
	problem(e.GET("/api/v1/test-cases").Expect())
	problem(e.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "x"}).Expect())
	problem(e.GET("/api/v1/test-cases/1").Expect())
	problem(e.PATCH("/api/v1/test-cases/1").WithJSON(map[string]any{"title": "x"}).Expect())
	problem(e.POST("/api/v1/test-cases/1/deprecate").Expect())
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
