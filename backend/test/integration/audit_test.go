//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/app"
	"github.com/edcrove/provenly/backend/internal/audit"
	auditpg "github.com/edcrove/provenly/backend/internal/audit/postgres"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

func TestAudit(t *testing.T) {
	t.Run("BE-INT-057_every_authenticated_change_through_the_api_is_audited_append_only", func(t *testing.T) {
		s, ctx := fresh(t)
		require.NoError(t, s.Identity.Bootstrap(context.Background(), "admin", "correct horse"))
		sess, err := s.Identity.Login(context.Background(), "admin", "correct horse")
		require.NoError(t, err)
		srv := httptest.NewServer(app.NewHandler(s, 1<<20))
		defer srv.Close()
		do := func(method, path, body string) int {
			req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+sess.Token)
			req.Header.Set("Content-Type", "application/json")
			res, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			_ = res.Body.Close()
			return res.StatusCode
		}
		require.Equal(t, 201, do("POST", "/api/v1/projects", `{"key":"AUD","name":"Audited"}`))
		require.Equal(t, 201, do("POST", "/api/v1/test-cases", `{"title":"pay","project":"AUD"}`))
		require.Equal(t, 400, do("POST", "/api/v1/test-cases", `{"title":""}`), "refused changes are not audited")
		require.Equal(t, 200, do("GET", "/api/v1/test-cases", ""), "reads are not audited")
		require.Equal(t, 201, do("POST", "/api/v1/projects/AUD/webhooks", `{"url":"https://hooks.example.com/x","events":["run.completed"]}`))

		// Concurrent changes are all recorded.
		var wg sync.WaitGroup
		for range 10 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				assert.Equal(t, 201, do("POST", "/api/v1/test-cases", `{"title":"race","project":"AUD"}`))
			}()
		}
		wg.Wait()

		page, err := s.Audit.Events(ctx, audit.Filter{}, pagination.Page{Number: 1, Size: 100})
		require.NoError(t, err)
		require.Equal(t, int64(13), page.Total)
		last := page.Items[len(page.Items)-1]
		assert.Equal(t, "admin", last.Actor)
		assert.Equal(t, "POST /api/v1/projects", last.Action)
		hook := page.Items[10]
		assert.Equal(t, "POST /api/v1/projects/{projectKey}/webhooks", hook.Action)
		assert.Equal(t, "AUD", hook.ProjectKey)
		assert.Equal(t, int32(201), hook.Status)
		byProject, err := s.Audit.Events(ctx, audit.Filter{ProjectKey: "AUD"}, pagination.Default())
		require.NoError(t, err)
		assert.Equal(t, int64(1), byProject.Total, "only routes that name the project carry it")
		byActor, err := s.Audit.Events(ctx, audit.Filter{Actor: "nobody"}, pagination.Default())
		require.NoError(t, err)
		assert.Zero(t, byActor.Total)

		// Append-only: the database refuses edits and deletes, and malformed rows.
		_, err = db.Pool.Exec(ctx, `UPDATE audit_events SET actor = 'mallory'`)
		assert.ErrorContains(t, err, "append-only")
		_, err = db.Pool.Exec(ctx, `DELETE FROM audit_events`)
		assert.ErrorContains(t, err, "append-only")
		store := auditpg.NewStore(db.Pool)
		for _, e := range []audit.Event{
			{Actor: "", Action: "POST /x", Path: "/x", Status: 201},
			{Actor: "a", Action: "GET /x", Path: "/x", Status: 200},
			{Actor: "a", Action: "POST /x", Path: "/x", Status: 404},
			{Actor: "a", Action: "POST /x", Path: "", Status: 201},
		} {
			assert.Error(t, store.Insert(ctx, e), "%+v", e)
		}
	})
}
