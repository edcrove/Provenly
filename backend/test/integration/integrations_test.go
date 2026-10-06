//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/app"
	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/identity"
	"github.com/edcrove/provenly/backend/internal/ingestion"
	"github.com/edcrove/provenly/backend/internal/integrations"
	integrationspg "github.com/edcrove/provenly/backend/internal/integrations/postgres"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

// freshIntegrations is fresh with outbound requests allowed to loopback (the test endpoints) and GitHub at gh.
func freshIntegrations(t *testing.T, gh string) (app.Services, context.Context) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, db.Reset(ctx))
	s := app.NewServicesConfig(db.Pool, time.Now, app.Config{
		Identity: testIdentity(), SecretsKey: []byte(strings.Repeat("s", 32)),
		Integrations: integrations.Config{AllowPrivate: true, GitHubAPIURL: gh},
	})
	return s, identity.WithUser(ctx, identity.User{Username: "integration", IsAdmin: true})
}

// endpoint is a webhook receiver that verifies signatures and answers code.
type endpoint struct {
	mu     sync.Mutex
	secret string
	code   int
	events []string
	bodies []map[string]any
	bad    int
}

func (e *endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	e.mu.Lock()
	defer e.mu.Unlock()
	if integrations.Sign(e.secret, r.Header.Get("X-Provenly-Timestamp"), body) != r.Header.Get("X-Provenly-Signature") {
		e.bad++
	}
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	e.events, e.bodies = append(e.events, r.Header.Get("X-Provenly-Event")), append(e.bodies, m)
	w.WriteHeader(e.code)
}

func TestIntegrations(t *testing.T) {
	t.Run("BE-INT-055_webhooks_export_completed_runs_signed_and_retried_once_per_delivery", func(t *testing.T) {
		s, ctx := freshIntegrations(t, "http://127.0.0.1:1")
		ep := &endpoint{code: http.StatusOK}
		srv := httptest.NewServer(ep)
		defer srv.Close()
		hook, secret, err := s.Integrations.CreateWebhook(ctx, "TC", srv.URL+"/hook", []string{integrations.EventRunCompleted})
		require.NoError(t, err)
		ep.secret = secret

		// The signing secret is stored encrypted, never in clear.
		var stored string
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT secret FROM webhooks WHERE id = $1`, hook.ID).Scan(&stored))
		assert.True(t, strings.HasPrefix(stored, "v1:"))
		assert.NotContains(t, stored, secret)

		// A created run is queued once (a replay is not); manual runs that finish and live runs completed by their report too.
		tc, err := s.Catalog.Create(ctx, catalog.CreateInput{Title: "login", Automated: true})
		require.NoError(t, err)
		report := junitFor(tcProp("login", tc.Key(), ""))
		for range 2 {
			_, err = s.Ingestion.IngestJUnit(ctx, meta("700", 1), strings.NewReader(report))
			require.NoError(t, err)
		}
		manual, err := s.Manual.Start(ctx, ingestion.ManualRunInput{ProjectKey: "TC", Name: "sign-off", Scope: ingestion.ScopeAll})
		require.NoError(t, err)
		_, err = s.Manual.Finish(ctx, manual.ID, execution.RunCancelled)
		require.NoError(t, err)
		live, err := s.Live.Start(ctx, meta("701", 1))
		require.NoError(t, err)
		for range 2 {
			_, err = s.Ingestion.IngestJUnit(ctx, meta("701", 1), strings.NewReader(report))
			require.NoError(t, err)
		}
		page, err := s.Integrations.Deliveries(ctx, "TC", hook.ID, pagination.Default())
		require.NoError(t, err)
		require.Equal(t, int64(3), page.Total)

		n, err := s.Integrations.DeliverDue(ctx)
		require.NoError(t, err)
		assert.Equal(t, 3, n)
		assert.Zero(t, ep.bad, "every delivery verifies with the secret")
		require.Len(t, ep.bodies, 3)
		var runIDs []any
		for _, b := range ep.bodies {
			assert.Equal(t, "run.completed", b["event"])
			assert.Equal(t, map[string]any{"key": "TC", "name": "Default"}, b["project"])
			runIDs = append(runIDs, b["run"].(map[string]any)["id"])
		}
		assert.ElementsMatch(t, []any{1.0, float64(manual.ID), float64(live.ID)}, runIDs)
		page, _ = s.Integrations.Deliveries(ctx, "TC", hook.ID, pagination.Default())
		for _, d := range page.Items {
			assert.Equal(t, integrations.DeliverySucceeded, d.Status)
			assert.NotNil(t, d.CompletedAt)
			assert.Equal(t, int32(200), *d.LastStatusCode)
		}
		n, err = s.Integrations.DeliverDue(ctx)
		require.NoError(t, err)
		assert.Zero(t, n, "nothing is sent twice")

		// A failing endpoint keeps the delivery pending with the backoff; the lease keeps it from other workers.
		ep.code = http.StatusServiceUnavailable
		_, err = s.Integrations.Ping(ctx, "TC", hook.ID)
		require.NoError(t, err)
		_, err = s.Integrations.DeliverDue(ctx)
		require.NoError(t, err)
		page, _ = s.Integrations.Deliveries(ctx, "TC", hook.ID, pagination.Page{Number: 1, Size: 1})
		d := page.Items[0]
		assert.Equal(t, integrations.DeliveryPending, d.Status)
		assert.Equal(t, int32(1), d.Attempts)
		assert.Equal(t, int32(503), *d.LastStatusCode)
		assert.WithinDuration(t, time.Now().Add(10*time.Second), d.NextAttemptAt, 5*time.Second)
		assert.Nil(t, d.CompletedAt)

		// Concurrent workers send each due delivery exactly once (FOR UPDATE SKIP LOCKED + lease).
		ep.code, ep.events = http.StatusNoContent, nil
		for range 30 {
			_, err = s.Integrations.Ping(ctx, "TC", hook.ID)
			require.NoError(t, err)
		}
		var wg sync.WaitGroup
		for range 5 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					n, err := s.Integrations.DeliverDue(ctx)
					assert.NoError(t, err)
					if n == 0 {
						return
					}
				}
			}()
		}
		wg.Wait()
		assert.Len(t, ep.events, 30)

		// A claimed delivery is leased for a minute: a second claim does not see it.
		_, err = s.Integrations.Ping(ctx, "TC", hook.ID)
		require.NoError(t, err)
		store := integrationspg.NewStore(db.Pool)
		first, err := store.ClaimDueDeliveries(ctx, 10)
		require.NoError(t, err)
		require.Len(t, first, 1)
		second, err := store.ClaimDueDeliveries(ctx, 10)
		require.NoError(t, err)
		assert.Empty(t, second)

		// Paused webhooks get nothing new; lookups of other projects' webhooks are not found.
		_, err = s.Integrations.UpdateWebhook(ctx, "TC", hook.ID, integrations.UpdateWebhookInput{Active: ptr(false), URL: ptr(srv.URL + "/v2"), Events: []string{integrations.EventRunCompleted}})
		require.NoError(t, err)
		before, _ := s.Integrations.Deliveries(ctx, "TC", hook.ID, pagination.Default())
		_, err = s.Ingestion.IngestJUnit(ctx, meta("702", 1), strings.NewReader(report))
		require.NoError(t, err)
		after, _ := s.Integrations.Deliveries(ctx, "TC", hook.ID, pagination.Default())
		assert.Equal(t, before.Total, after.Total)
		_, err = store.GetWebhook(ctx, 999, hook.ID)
		assert.ErrorIs(t, err, integrations.ErrNotFound)
		_, err = store.GetWebhookByID(ctx, 999)
		assert.ErrorIs(t, err, integrations.ErrNotFound)
		assert.ErrorIs(t, store.UpdateWebhook(ctx, 1, 999, integrations.UpdateWebhookInput{Active: ptr(true)}), integrations.ErrNotFound)

		// The database refuses unknown events, secrets in clear and inconsistent deliveries.
		for name, q := range map[string]string{
			"event":     `INSERT INTO webhooks (project_id, url, events, secret, created_by) VALUES (1, 'https://x', '{run.started}', 'v1:x', 'a')`,
			"no events": `INSERT INTO webhooks (project_id, url, events, secret, created_by) VALUES (1, 'https://x', '{}', 'v1:x', 'a')`,
			"clear":     `INSERT INTO webhooks (project_id, url, events, secret, created_by) VALUES (1, 'https://x', '{run.completed}', 'whsec_x', 'a')`,
			"scheme":    `INSERT INTO webhooks (project_id, url, events, secret, created_by) VALUES (1, 'ftp://x', '{run.completed}', 'v1:x', 'a')`,
			"completed": fmt.Sprintf(`UPDATE webhook_deliveries SET status = 'succeeded' WHERE webhook_id = %d AND status = 'pending'`, hook.ID),
			"pending":   fmt.Sprintf(`UPDATE webhook_deliveries SET status = 'pending' WHERE webhook_id = %d AND status = 'succeeded'`, hook.ID),
			"token":     `INSERT INTO github_connections (project_id, repository, token) VALUES (1, 'acme/shop', 'ghp_clear')`,
			"repo":      `INSERT INTO github_connections (project_id, repository, token) VALUES (1, 'acme', 'v1:x')`,
			"dot dot":   `INSERT INTO github_connections (project_id, repository, token) VALUES (1, '../..', 'v1:x')`,
			"climbs":    `INSERT INTO github_connections (project_id, repository, token) VALUES (1, 'acme/..', 'v1:x')`,
		} {
			_, err := db.Pool.Exec(ctx, q)
			assert.Error(t, err, name)
		}
	})

	t.Run("BE-INT-056_github_connector_mirrors_issues_with_an_encrypted_token", func(t *testing.T) {
		var auth string
		gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth = r.Header.Get("Authorization")
			if r.URL.Query().Get("page") != "1" {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_, _ = w.Write([]byte(`[
				{"number": 12, "title": "Checkout fails", "body": "b", "html_url": "https://github.com/acme/shop/issues/12", "state": "open"},
				{"number": 13, "title": "Fix", "html_url": "https://github.com/acme/shop/pull/13", "state": "open", "pull_request": {}},
				{"number": 9, "title": "Old", "html_url": "https://github.com/acme/shop/issues/9", "state": "closed", "state_reason": "completed"}]`))
		}))
		defer gh.Close()
		s, ctx := freshIntegrations(t, gh.URL)
		v, err := s.Integrations.ConnectGitHub(ctx, "TC", integrations.GitHubInput{Repository: "acme/shop", Token: ptr("ghp_integration42")})
		require.NoError(t, err)
		assert.Equal(t, "…on42", v.TokenHint)
		var stored string
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT token FROM github_connections WHERE project_id = 1`).Scan(&stored))
		assert.True(t, strings.HasPrefix(stored, "v1:"))
		assert.NotContains(t, stored, "ghp_integration42")

		res, err := s.Integrations.SyncGitHub(ctx, "TC")
		require.NoError(t, err)
		assert.Equal(t, catalog.ImportResult{Created: 2}, res)
		assert.Equal(t, "Bearer ghp_integration42", auth)
		res, err = s.Integrations.SyncGitHub(ctx, "TC")
		require.NoError(t, err)
		assert.Equal(t, catalog.ImportResult{Updated: 2}, res, "a sync updates by number")
		issues, err := s.Catalog.Issues(ctx, catalog.DefaultProjectID, catalog.IssueFilter{})
		require.NoError(t, err)
		require.Len(t, issues, 2)
		states := map[string]string{}
		for _, is := range issues {
			assert.Equal(t, catalog.ProviderGitHub, is.Provider)
			states[is.ExternalID] = is.State
		}
		assert.Equal(t, map[string]string{"12": catalog.IssueOpen, "9": catalog.IssueClosed}, states)
		v, err = s.Integrations.GitHub(ctx, "TC")
		require.NoError(t, err)
		require.NotNil(t, v.LastSyncedAt)
		assert.Empty(t, v.LastError)

		// A failed sync is recorded and keeps the last successful one; disconnecting keeps the mirrored issues.
		gh.Close()
		_, err = s.Integrations.SyncGitHub(ctx, "TC")
		require.Error(t, err)
		failed, err := s.Integrations.GitHub(ctx, "TC")
		require.NoError(t, err)
		assert.Contains(t, failed.LastError, "GitHub is unreachable")
		assert.Equal(t, v.LastSyncedAt.Unix(), failed.LastSyncedAt.Unix())
		require.NoError(t, s.Integrations.DisconnectGitHub(ctx, "TC"))
		_, err = s.Integrations.GitHub(ctx, "TC")
		assert.Error(t, err)
		issues, _ = s.Catalog.Issues(ctx, catalog.DefaultProjectID, catalog.IssueFilter{})
		assert.Len(t, issues, 2)

		// Another key cannot read what was stored (PROVENLY_SECRETS_KEY changed): connect again with a token.
		_, err = s.Integrations.ConnectGitHub(ctx, "TC", integrations.GitHubInput{Repository: "acme/shop", Token: ptr("ghp_integration42")})
		require.NoError(t, err)
		other := app.NewServicesConfig(db.Pool, time.Now, app.Config{Identity: testIdentity(), Integrations: integrations.Config{AllowPrivate: true, GitHubAPIURL: gh.URL}})
		v, err = other.Integrations.GitHub(ctx, "TC")
		require.NoError(t, err)
		assert.Empty(t, v.TokenHint)
		_, err = other.Integrations.SyncGitHub(ctx, "TC")
		assert.ErrorContains(t, err, "connect again")
	})
}
