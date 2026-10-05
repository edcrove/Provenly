//go:build contract

package contract

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/app"
	"github.com/edcrove/provenly/backend/internal/integrations"
)

// TestIntegrations: maintainers manage webhooks (the secret is returned once) and the GitHub Issues connector; a
// GitHub failure is a 502, a token another secrets key sealed a 409.
func TestIntegrations(t *testing.T) {
	fresh(t)
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ghp_contract" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`[{"number": 4, "title": "Crash", "html_url": "https://github.com/acme/shop/issues/4", "state": "open"}]`))
	}))
	defer gh.Close()
	hooks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer hooks.Close()
	cfg := app.Config{Identity: identityConfig(), SecretsKey: []byte(strings.Repeat("c", 32)),
		Integrations: integrations.Config{AllowPrivate: true, GitHubAPIURL: gh.URL}}
	s := app.NewServicesConfig(db.Pool, time.Now, cfg)
	admin := api(t, s, 1<<20)
	e := anon(t, s, 1<<20)
	webhooks := "/api/v1/projects/TC/webhooks"

	created := admin.POST(webhooks).WithJSON(map[string]any{"url": hooks.URL + "/p", "events": []string{"run.completed"}}).
		Expect().Status(http.StatusCreated).JSON().Object()
	created.Value("secret").String().HasPrefix("whsec_")
	created.Value("webhook").Object().HasValue("active", true).HasValue("createdBy", "admin").HasValue("lastDelivery", nil)
	id := strconv.FormatInt(int64(created.Value("webhook").Object().Value("id").Number().Raw()), 10)
	admin.POST(webhooks).WithJSON(map[string]any{"url": "ftp://x", "events": []string{"run.completed"}}).Expect().Status(http.StatusBadRequest)
	admin.POST(webhooks).WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.POST("/api/v1/projects/NOPE/webhooks").WithJSON(map[string]any{"url": "https://x.test", "events": []string{"run.completed"}}).Expect().Status(http.StatusNotFound)

	admin.POST(webhooks + "/" + id + "/ping").Expect().Status(http.StatusAccepted).JSON().Object().HasValue("event", "ping").HasValue("status", "pending")
	admin.POST(webhooks + "/0/ping").Expect().Status(http.StatusBadRequest)
	admin.POST(webhooks + "/987654/ping").Expect().Status(http.StatusNotFound)
	_, err := s.Integrations.DeliverDue(context.Background())
	require.NoError(t, err)
	list := admin.GET(webhooks).Expect().Status(http.StatusOK).JSON().Object().Value("items").Array()
	list.Length().IsEqual(1)
	list.Value(0).Object().Value("lastDelivery").Object().HasValue("status", "succeeded").HasValue("lastStatusCode", 204).HasValue("nextAttemptAt", nil)
	admin.GET("/api/v1/projects/NOPE/webhooks").Expect().Status(http.StatusNotFound)
	admin.GET(webhooks+"/"+id+"/deliveries").WithQuery("pageSize", 5).Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 1)
	admin.GET(webhooks+"/"+id+"/deliveries").WithQuery("page", 0).Expect().Status(http.StatusBadRequest)
	admin.GET(webhooks + "/987654/deliveries").Expect().Status(http.StatusNotFound)

	admin.PATCH(webhooks+"/"+id).WithJSON(map[string]any{"active": false}).Expect().Status(http.StatusOK).JSON().Object().HasValue("active", false)
	admin.PATCH(webhooks + "/" + id).WithJSON(map[string]any{"events": []string{}}).Expect().Status(http.StatusBadRequest)
	admin.PATCH(webhooks + "/" + id).WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.PATCH(webhooks + "/987654").WithJSON(map[string]any{"active": true}).Expect().Status(http.StatusNotFound)

	github := "/api/v1/projects/TC/github"
	admin.GET(github).Expect().Status(http.StatusNotFound)
	admin.POST(github + "/sync").Expect().Status(http.StatusNotFound)
	admin.DELETE(github).Expect().Status(http.StatusNotFound)
	admin.PUT(github).WithJSON(map[string]any{"repository": "acme/shop"}).Expect().Status(http.StatusBadRequest)
	admin.PUT(github).WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.PUT("/api/v1/projects/NOPE/github").WithJSON(map[string]any{"repository": "acme/shop", "token": "t"}).Expect().Status(http.StatusNotFound)
	admin.PUT(github).WithJSON(map[string]any{"repository": "acme/shop", "token": "ghp_contract", "labels": ""}).Expect().Status(http.StatusOK).
		JSON().Object().HasValue("tokenHint", "…ract").HasValue("lastSyncedAt", nil).HasValue("lastError", nil).NotContainsKey("token")
	admin.POST(github+"/sync").Expect().Status(http.StatusOK).JSON().Object().IsEqual(map[string]any{"created": 1, "updated": 0})
	admin.GET(github).Expect().Status(http.StatusOK).JSON().Object().Value("lastSyncedAt").String().NotEmpty()
	admin.GET("/api/v1/projects/TC/issues").Expect().Status(http.StatusOK).JSON().Object().Value("items").Array().Value(0).Object().
		HasValue("provider", "github").HasValue("externalId", "4")
	admin.PUT(github).WithJSON(map[string]any{"repository": "acme/shop", "token": "ghp_wrong"}).Expect().Status(http.StatusOK)
	admin.POST(github+"/sync").Expect().Status(http.StatusBadGateway).JSON(problemOpts).Object().HasValue("code", "upstream_error")
	// Another secrets key cannot read the stored token: connect again.
	other := api(t, app.NewServicesConfig(db.Pool, time.Now, app.Config{Identity: identityConfig(), Integrations: cfg.Integrations}), 1<<20)
	other.POST(github + "/sync").Expect().Status(http.StatusConflict)
	admin.DELETE(github).Expect().Status(http.StatusNoContent)

	// Only maintainers manage integrations: viewers and members get 403.
	inv := admin.POST("/api/v1/invitations").WithJSON(map[string]any{"project": "TC", "role": "member"}).Expect().Status(http.StatusCreated).JSON().Object()
	member := as(e, e.POST("/api/v1/invitations/accept").WithJSON(map[string]any{"token": inv.Value("token").String().Raw(), "username": "mia",
		"displayName": "Mia", "password": "mia's password"}).Expect().Status(http.StatusCreated).JSON().Object().Value("token").String().Raw())
	member.GET(webhooks).Expect().Status(http.StatusForbidden)
	member.POST(webhooks).WithJSON(map[string]any{"url": "https://x.test", "events": []string{"run.completed"}}).Expect().Status(http.StatusForbidden)
	member.PATCH(webhooks + "/" + id).WithJSON(map[string]any{"active": true}).Expect().Status(http.StatusForbidden)
	member.POST(webhooks + "/" + id + "/ping").Expect().Status(http.StatusForbidden)
	member.GET(webhooks + "/" + id + "/deliveries").Expect().Status(http.StatusForbidden)
	member.GET(github).Expect().Status(http.StatusForbidden)
	member.PUT(github).WithJSON(map[string]any{"repository": "acme/shop", "token": "t"}).Expect().Status(http.StatusForbidden)
	member.DELETE(github).Expect().Status(http.StatusForbidden)
	member.POST(github + "/sync").Expect().Status(http.StatusForbidden)
}
