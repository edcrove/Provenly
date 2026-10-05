//go:build contract

package contract

import (
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
	page.HasValue("totalItems", 3)
	first := page.Value("items").Array().Value(0).Object()
	first.HasValue("actor", "admin").HasValue("action", "POST /api/v1/ingestion/junit").HasValue("project", nil).HasValue("status", 201)
	admin.GET("/api/v1/audit").WithQuery("project", "AUD").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 1).
		Value("items").Array().Value(0).Object().HasValue("action", "PATCH /api/v1/projects/{projectKey}").HasValue("path", "/api/v1/projects/AUD")
	admin.GET("/api/v1/audit").WithQuery("actor", "nobody").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 0)
	for _, q := range []string{"page=0", "project=aud", "actor=", "actor=" + strings.Repeat("a", 201)} {
		admin.GET("/api/v1/audit").WithQueryString(q).Expect().Status(http.StatusBadRequest)
	}

	inv := admin.POST("/api/v1/invitations").WithJSON(map[string]any{}).Expect().Status(http.StatusCreated).JSON().Object()
	ana := as(e, e.POST("/api/v1/invitations/accept").WithJSON(map[string]any{"token": inv.Value("token").String().Raw(), "username": "ana",
		"displayName": "Ana", "password": "ana's password"}).Expect().Status(http.StatusCreated).JSON().Object().Value("token").String().Raw())
	ana.GET("/api/v1/audit").Expect().Status(http.StatusForbidden)
}
