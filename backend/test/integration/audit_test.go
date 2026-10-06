//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
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
		assert.Equal(t, int64(12), byProject.Total, "the webhook and the 11 test cases (their project is in the body, card #48)")
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

	t.Run("BE-INT-069_changes_without_a_project_in_their_path_are_filed_under_it_and_read_in_words", func(t *testing.T) {
		s, ctx := fresh(t)
		require.NoError(t, s.Identity.Bootstrap(context.Background(), "admin", "correct horse"))
		sess, err := s.Identity.Login(context.Background(), "admin", "correct horse")
		require.NoError(t, err)
		srv := httptest.NewServer(app.NewHandler(s, 1<<20))
		defer srv.Close()
		do := func(method, path, body string) (int, map[string]any) {
			req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+sess.Token)
			req.Header.Set("Content-Type", "application/json")
			res, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer res.Body.Close()
			var out map[string]any
			_ = json.NewDecoder(res.Body).Decode(&out)
			return res.StatusCode, out
		}
		id := func(m map[string]any) string { return fmt.Sprint(int64(m["id"].(float64))) }
		code, _ := do("POST", "/api/v1/projects", `{"key":"CHK","name":"Checkout"}`)
		require.Equal(t, 201, code)
		code, tc := do("POST", "/api/v1/test-cases", `{"title":"pay","project":"CHK","automated":false}`)
		require.Equal(t, 201, code)
		key := tc["key"].(string)
		tcPath := "/api/v1/test-cases/" + id(tc)
		code, _ = do("PATCH", tcPath, `{"title":"pay by card"}`)
		require.Equal(t, 200, code)
		var steps []string
		for _, a := range []string{"open", "pay", "check"} {
			code, st := do("POST", tcPath+"/steps", `{"action":"`+a+`"}`)
			require.Equal(t, 201, code)
			steps = append(steps, id(st))
		}
		code, _ = do("PATCH", tcPath+"/steps/"+steps[2], `{"action":"check the receipt"}`)
		require.Equal(t, 200, code)
		code, _ = do("DELETE", tcPath+"/steps/"+steps[1], "")
		require.Equal(t, 204, code)
		code, _ = do("POST", "/api/v1/projects/CHK/suites", `{"key":"smoke","name":"Smoke","kind":"static"}`)
		require.Equal(t, 201, code)
		code, _ = do("PUT", "/api/v1/projects/CHK/suites/smoke/cases", `{"testCaseIds":[`+id(tc)+`]}`)
		require.Equal(t, 200, code)
		code, run := do("POST", "/api/v1/test-runs/manual", `{"project":"CHK","name":"sign-off"}`)
		require.Equal(t, 201, code)
		code, _ = do("POST", "/api/v1/test-runs/"+id(run)+"/finish", `{"status":"cancelled"}`)
		require.Equal(t, 200, code)

		page, err := s.Audit.Events(ctx, audit.Filter{ProjectKey: "CHK"}, pagination.Page{Number: 1, Size: 100})
		require.NoError(t, err)
		var got []string
		for _, e := range page.Items {
			got = append(got, e.Summary)
		}
		assert.Equal(t, []string{
			"finished run #" + id(run), "started a manual run", "set the test cases of suite smoke", "created a suite",
			"deleted " + key + " step 2", "edited " + key + " step 3", "added a step to " + key, "added a step to " + key,
			"added a step to " + key, "edited " + key, "created a test case",
		}, got, "every change of the project is filed under it, newest first; the project itself was created before it existed")

		byCase, err := s.Audit.Events(ctx, audit.Filter{TestCaseKey: key}, pagination.Default())
		require.NoError(t, err)
		assert.Equal(t, int64(6), byCase.Total, "the edit and the step changes of the test case")
		for _, e := range byCase.Items {
			assert.Equal(t, key, e.TestCaseKey)
			assert.Equal(t, "CHK", e.ProjectKey)
		}
	})
}
