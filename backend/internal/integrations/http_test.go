package integrations

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/authz"
)

type call struct {
	method, target, body string
}

func serve(t *testing.T, svc *Service, c call) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	NewHandler(svc).Register(mux)
	req := httptest.NewRequest(c.method, c.target, strings.NewReader(c.body))
	if c.body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestWebhookRoutes(t *testing.T) {
	f := newFixture(t, Config{}, maintainer())
	rec := serve(t, f.svc, call{"POST", "/api/v1/projects/SHOP/webhooks", `{"url":"https://hooks.example.com/p","events":["run.completed"]}`})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created struct {
		Webhook map[string]any `json:"webhook"`
		Secret  string         `json:"secret"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.True(t, strings.HasPrefix(created.Secret, "whsec_"))
	assert.Equal(t, map[string]any{"id": 1.0, "url": "https://hooks.example.com/p", "events": []any{"run.completed"}, "active": true,
		"createdBy": "maria", "createdAt": "2026-10-05T12:00:00Z", "updatedAt": "2026-10-05T12:00:00Z"}, created.Webhook)

	rec = serve(t, f.svc, call{"POST", "/api/v1/projects/SHOP/webhooks/1/ping", ""})
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.JSONEq(t, `{"id":1,"webhookId":1,"event":"ping","status":"pending","attempts":0,"nextAttemptAt":"2026-10-05T12:00:00Z",
		"lastStatusCode":null,"lastError":null,"createdAt":"2026-10-05T12:00:00Z","completedAt":null,
		"payload":{"event":"ping","sentAt":"2026-10-05T12:00:00Z","webhook":{"id":1,"url":"https://hooks.example.com/p"}}}`, rec.Body.String())

	code := int32(500)
	f.repo.deliveries[0].Status, f.repo.deliveries[0].Attempts, f.repo.deliveries[0].LastStatusCode, f.repo.deliveries[0].LastError = DeliveryFailed, 5, &code, "the endpoint answered 500"
	f.repo.deliveries[0].CompletedAt = &now
	rec = serve(t, f.svc, call{"GET", "/api/v1/projects/SHOP/webhooks", ""})
	assert.Equal(t, http.StatusOK, rec.Code)
	var list struct {
		Items []WebhookDTO `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list.Items, 1)
	last := list.Items[0].LastDelivery
	require.NotNil(t, last)
	assert.Nil(t, last.NextAttemptAt, "only pending deliveries have a next attempt")
	assert.Equal(t, "the endpoint answered 500", *last.LastError)
	assert.Equal(t, int32(500), *last.LastStatusCode)

	rec = serve(t, f.svc, call{"GET", "/api/v1/projects/SHOP/webhooks/1/deliveries?page=1&pageSize=10", ""})
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"totalItems":1`)

	rec = serve(t, f.svc, call{"PATCH", "/api/v1/projects/SHOP/webhooks/1", `{"active":false}`})
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"active":false`)

	for _, c := range []struct {
		call
		want int
	}{
		{call{"GET", "/api/v1/projects/NOPE/webhooks", ""}, 404},
		{call{"POST", "/api/v1/projects/SHOP/webhooks", ""}, 415},
		{call{"POST", "/api/v1/projects/SHOP/webhooks", `{"url":"http://x","events":[]}`}, 400},
		{call{"GET", "/api/v1/projects/SHOP/webhooks?page=0", ""}, 400},
		{call{"PATCH", "/api/v1/projects/SHOP/webhooks/x", `{"active":true}`}, 400},
		{call{"PATCH", "/api/v1/projects/SHOP/webhooks/1", ""}, 415},
		{call{"PATCH", "/api/v1/projects/SHOP/webhooks/9", `{"active":true}`}, 404},
		{call{"POST", "/api/v1/projects/SHOP/webhooks/0/ping", ""}, 400},
		{call{"POST", "/api/v1/projects/SHOP/webhooks/9/ping", ""}, 404},
		{call{"GET", "/api/v1/projects/SHOP/webhooks/x/deliveries", ""}, 400},
		{call{"GET", "/api/v1/projects/SHOP/webhooks/1/deliveries?page=0", ""}, 400},
		{call{"GET", "/api/v1/projects/SHOP/webhooks/9/deliveries", ""}, 404},
	} {
		assert.Equal(t, c.want, serve(t, f.svc, c.call).Code, c.method+" "+c.target)
	}
	viewer := newFixture(t, Config{}, fakeAccess{role: authz.RoleViewer})
	assert.Equal(t, http.StatusForbidden, serve(t, viewer.svc, call{"GET", "/api/v1/projects/SHOP/webhooks", ""}).Code)
}

func TestGitHubRoutes(t *testing.T) {
	gh := &fakeGitHub{total: 2}
	srv := httptest.NewServer(gh)
	defer srv.Close()
	f := newFixture(t, Config{AllowPrivate: true, GitHubAPIURL: srv.URL}, maintainer())

	notConnected := serve(t, f.svc, call{"GET", "/api/v1/projects/SHOP/github", ""})
	assert.Equal(t, http.StatusNoContent, notConnected.Code)
	assert.Empty(t, notConnected.Body.String())
	rec := serve(t, f.svc, call{"PUT", "/api/v1/projects/SHOP/github", `{"repository":"acme/shop","token":"ghp_abcd9876","labels":""}`})
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"repository":"acme/shop","labels":"","tokenHint":"…9876","lastSyncedAt":null,"lastError":null,"updatedAt":"2026-10-05T12:00:00Z"}`, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "ghp_abcd9876")

	rec = serve(t, f.svc, call{"POST", "/api/v1/projects/SHOP/github/sync", ""})
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"created":2,"updated":0}`, rec.Body.String())
	rec = serve(t, f.svc, call{"GET", "/api/v1/projects/SHOP/github", ""})
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"lastSyncedAt":"2026-10-05T12:00:00Z"`)

	gh.status = http.StatusNotFound
	rec = serve(t, f.svc, call{"POST", "/api/v1/projects/SHOP/github/sync", ""})
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"upstream_error"`)
	rec = serve(t, f.svc, call{"GET", "/api/v1/projects/SHOP/github", ""})
	assert.Contains(t, rec.Body.String(), `"lastError":"GitHub answered 404`)

	assert.Equal(t, http.StatusNoContent, serve(t, f.svc, call{"DELETE", "/api/v1/projects/SHOP/github", ""}).Code)
	assert.Equal(t, http.StatusNotFound, serve(t, f.svc, call{"DELETE", "/api/v1/projects/SHOP/github", ""}).Code)
	assert.Equal(t, http.StatusNotFound, serve(t, f.svc, call{"POST", "/api/v1/projects/SHOP/github/sync", ""}).Code)
	assert.Equal(t, http.StatusUnsupportedMediaType, serve(t, f.svc, call{"PUT", "/api/v1/projects/SHOP/github", ""}).Code)
	assert.Equal(t, http.StatusBadRequest, serve(t, f.svc, call{"PUT", "/api/v1/projects/SHOP/github", `{"repository":"x"}`}).Code)
}

func TestMalformedProjectKeysAreRejectedBeforeAnyLookup(t *testing.T) {
	f := newFixture(t, Config{}, maintainer())
	for _, target := range []string{"/api/v1/projects/%00/webhooks", "/api/v1/projects/%FF/github", "/api/v1/projects/shop/github"} {
		rec := serve(t, f.svc, call{"GET", target, ""})
		assert.Equal(t, http.StatusBadRequest, rec.Code, target)
		assert.Contains(t, rec.Body.String(), `"field":"projectKey"`, target)
	}
}
