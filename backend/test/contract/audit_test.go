//go:build contract

package contract

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestAuditLog: administrators read who changed what; other users are refused; filters are validated.
func TestAuditLog(t *testing.T) {
	s := fresh(t)
	admin := api(t, s, 1<<20)
	e := anon(t, s, 1<<20)
	admin.POST("/api/v1/projects").WithJSON(map[string]any{"key": "AUD", "name": "Audited"}).Expect().Status(http.StatusCreated)
	admin.PATCH("/api/v1/projects/AUD").WithJSON(map[string]any{"name": "Audited 2"}).Expect().Status(http.StatusOK)
	ingest(admin, "audit", 1, report(1)).Expect().Status(http.StatusCreated)

	page := admin.GET("/api/v1/audit").Expect().Status(http.StatusOK).JSON().Object()
	page.HasValue("totalItems", 4) // the 3 changes and the administrator's sign-in (card #49)
	first := page.Value("items").Array().Value(0).Object()
	first.HasValue("actor", "admin").HasValue("action", "POST /api/v1/ingestion/junit").HasValue("project", "TC").HasValue("status", 201).
		HasValue("summary", "uploaded a JUnit report").HasValue("testCase", nil)
	admin.GET("/api/v1/audit").WithQuery("project", "AUD").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 1).
		Value("items").Array().Value(0).Object().HasValue("action", "PATCH /api/v1/projects/{projectKey}").HasValue("path", "/api/v1/projects/AUD")
	admin.GET("/api/v1/audit").WithQuery("actor", "nobody").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 0)
	for _, q := range []string{"page=0", "project=aud", "actor=", "actor=" + strings.Repeat("a", 201), "testCase=", "testCase=tc-1", "testCase=TC-0", "testCase=TC"} {
		admin.GET("/api/v1/audit").WithQueryString(q).Expect().Status(http.StatusBadRequest)
	}

	// Sign-in events are audited with the client; an unknown username reads "unknown" (card #49).
	e.POST("/api/v1/auth/login").WithHeader("User-Agent", "contract-agent").WithJSON(map[string]any{"username": "nobody", "password": "wrong password"}).
		Expect().Status(http.StatusUnauthorized)
	failed := admin.GET("/api/v1/audit").WithQuery("actor", "unknown").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 1).
		Value("items").Array().Value(0).Object()
	failed.HasValue("status", 401).HasValue("summary", "failed to sign in").HasValue("action", "POST /api/v1/auth/login").HasValue("userAgent", "contract-agent")
	failed.Value("ip").String().NotEmpty()

	// A change without a project in its path is filed under the project it changed, in words, with its test case
	// (card #48).
	id := admin.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "pay", "project": "AUD"}).Expect().Status(http.StatusCreated).
		JSON().Object().Value("id").Number().Raw()
	admin.PATCH(fmt.Sprintf("/api/v1/test-cases/%d", int64(id))).WithJSON(map[string]any{"title": "pay by card"}).Expect().Status(http.StatusOK)
	// Its creation names it and is found by its key too (deployed audit).
	history := admin.GET("/api/v1/audit").WithQuery("testCase", "AUD-1").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 2).
		Value("items").Array()
	history.Value(1).Object().HasValue("summary", "created a test case AUD-1").HasValue("testCase", "AUD-1")
	edit := history.Value(0).Object()
	edit.HasValue("summary", "edited AUD-1").HasValue("testCase", "AUD-1").HasValue("project", "AUD").HasValue("action", "PATCH /api/v1/test-cases/{testCaseId}")
	admin.GET("/api/v1/audit").WithQuery("project", "AUD").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 3)

	inv := admin.POST("/api/v1/invitations").WithJSON(map[string]any{}).Expect().Status(http.StatusCreated).JSON().Object()
	ana := as(e, e.POST("/api/v1/invitations/accept").WithJSON(map[string]any{"token": inv.Value("token").String().Raw(), "username": "ana",
		"displayName": "Ana", "password": "ana's password"}).Expect().Status(http.StatusCreated).JSON().Object().Value("token").String().Raw())
	ana.GET("/api/v1/audit").Expect().Status(http.StatusForbidden)
}
