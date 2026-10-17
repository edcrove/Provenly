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

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/app"
	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
	"github.com/edcrove/provenly/backend/internal/platform/postgres"
)

func TestProjects(t *testing.T) {
	t.Run("BE-INT-031_projects_are_created_listed_updated_and_their_identity_is_protected", func(t *testing.T) {
		s, ctx := fresh(t)
		chk, err := s.Catalog.CreateProject(ctx, catalog.CreateProjectInput{Key: " chk ", Name: " Checkout ", Description: "cart"})
		require.NoError(t, err)
		assert.Equal(t, "CHK", chk.Key, "keys are trimmed and upper-cased")
		assert.Equal(t, "Checkout", chk.Name)

		_, err = s.Catalog.CreateProject(ctx, catalog.CreateProjectInput{Key: "CHK", Name: "again"})
		e, ok := apperr.As(err)
		require.True(t, ok)
		assert.Equal(t, apperr.KindConflict, e.Kind)

		got, err := s.Catalog.ProjectByKey(ctx, "CHK")
		require.NoError(t, err)
		assert.Equal(t, chk.ID, got.ID)
		byID, err := s.Catalog.ProjectByID(ctx, chk.ID)
		require.NoError(t, err)
		assert.Equal(t, "CHK", byID.Key)
		_, err = s.Catalog.ProjectByKey(ctx, "NOPE")
		e, _ = apperr.As(err)
		assert.Equal(t, apperr.KindNotFound, e.Kind)
		_, err = s.Catalog.ProjectByID(ctx, 999)
		e, _ = apperr.As(err)
		assert.Equal(t, apperr.KindNotFound, e.Kind)

		page, err := s.Catalog.ListProjects(ctx, catalog.ProjectFilter{}, pagination.Page{Number: 1, Size: 1})
		require.NoError(t, err)
		assert.Equal(t, int64(2), page.Total)
		assert.Equal(t, "CHK", page.Items[0].Key, "ordered by key")

		name := "Checkout v2"
		up, err := s.Catalog.UpdateProject(ctx, "CHK", catalog.UpdateProjectInput{Name: &name})
		require.NoError(t, err)
		assert.Equal(t, name, up.Name)
		assert.Equal(t, "cart", up.Description, "fields not sent are kept")
		_, err = s.Catalog.UpdateProject(ctx, "NOPE", catalog.UpdateProjectInput{Name: &name})
		e, _ = apperr.As(err)
		assert.Equal(t, apperr.KindNotFound, e.Kind)

		_, err = db.Pool.Exec(ctx, `DELETE FROM projects WHERE key = 'CHK'`)
		assert.ErrorContains(t, err, "projects cannot be deleted")
		_, err = db.Pool.Exec(ctx, `UPDATE projects SET key = 'WEB' WHERE key = 'CHK'`)
		assert.ErrorContains(t, err, "the key of a project is immutable (CHK)")
		_, err = s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: chk.ID, Title: "a"})
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `UPDATE projects SET next_number = 1 WHERE key = 'CHK'`)
		assert.ErrorContains(t, err, "test case numbers are never reused (CHK)")
		for _, stmt := range []string{
			`INSERT INTO projects (key, name) VALUES ('chk2', 'lower')`,
			`INSERT INTO projects (key, name) VALUES ('A', 'short')`,
			`INSERT INTO projects (key, name) VALUES ('ABCDEFGHIJK', 'long')`,
			`INSERT INTO projects (key, name) VALUES ('OK', '')`,
		} {
			_, err := db.Pool.Exec(ctx, stmt)
			assert.Error(t, err, stmt)
		}
	})

	t.Run("BE-INT-077_projects_are_found_by_key_or_name_beyond_the_first_page", func(t *testing.T) {
		s, ctx := fresh(t)
		for i := range 120 {
			_, err := s.Catalog.CreateProject(ctx, catalog.CreateProjectInput{Key: fmt.Sprintf("A%03d", i), Name: fmt.Sprintf("Bulk %d", i)})
			require.NoError(t, err)
		}
		pay, err := s.Catalog.CreateProject(ctx, catalog.CreateProjectInput{Key: "PAY", Name: "Payments 100%_done"})
		require.NoError(t, err)
		find := func(ids []int64, q string) (int64, []string) {
			p, err := s.Catalog.ListProjects(ctx, catalog.ProjectFilter{IDs: ids, Search: &q}, pagination.Page{Number: 1, Size: 100})
			require.NoError(t, err)
			keys := make([]string, len(p.Items))
			for i, it := range p.Items {
				keys[i] = it.Key
			}
			return p.Total, keys
		}
		all, err := s.Catalog.ListProjects(ctx, catalog.ProjectFilter{}, pagination.Page{Number: 1, Size: 100})
		require.NoError(t, err)
		assert.Equal(t, int64(122), all.Total, "the default project, 120 and PAY")
		n, keys := find(nil, "pay")
		assert.Equal(t, int64(1), n, "by key, any case: past the first page")
		assert.Equal(t, []string{"PAY"}, keys)
		n, _ = find(nil, "PAYMENTS")
		assert.Equal(t, int64(1), n, "by name, any case")
		n, _ = find(nil, `100\%\_`)
		assert.Equal(t, int64(1), n, "escaped wildcards are literal")
		n, _ = find(nil, `1\_`)
		assert.Equal(t, int64(0), n)
		n, _ = find(nil, "bulk 11")
		assert.Equal(t, int64(11), n, "Bulk 11 and Bulk 110..119")
		n, _ = find([]int64{pay.ID}, "bulk")
		assert.Equal(t, int64(0), n, "only within the caller's projects")
	})

	t.Run("BE-INT-078_an_archived_project_leaves_the_lists_and_is_read_only_until_unarchived", func(t *testing.T) {
		s, ctx := fresh(t)
		require.NoError(t, s.Identity.Bootstrap(context.Background(), "admin", "correct horse"))
		sess, err := s.Identity.Login(context.Background(), "admin", "correct horse")
		require.NoError(t, err)
		srv := httptest.NewServer(app.NewHandler(s, 1<<20))
		defer srv.Close()
		do := func(method, path, body string) (int, string) {
			req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+sess.Token)
			req.Header.Set("Content-Type", "application/json")
			res, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer res.Body.Close()
			b, _ := io.ReadAll(res.Body)
			return res.StatusCode, string(b)
		}
		code, body := do("POST", "/api/v1/projects", `{"key":"OLD","name":"Old product"}`)
		require.Equal(t, 201, code)
		var created struct {
			ID int64 `json:"id"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &created))
		code, _ = do("POST", "/api/v1/test-cases", `{"title":"kept","project":"OLD","automated":true}`)
		require.Equal(t, 201, code)
		code, body = do("POST", "/api/v1/projects/OLD/api-keys", `{"name":"ci"}`)
		require.Equal(t, 201, code)
		var issued struct {
			Token  string `json:"token"`
			APIKey struct {
				ID int64 `json:"id"`
			} `json:"apiKey"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &issued))
		report := func(headers map[string]string, query string) (int, string) {
			req, _ := http.NewRequest("POST", srv.URL+"/api/v1/ingestion/junit?provider=github&runAttempt=1&"+query,
				strings.NewReader(`<testsuite name="s"><testcase name="t"/></testsuite>`))
			req.Header.Set("Content-Type", "application/xml")
			for k, v := range headers {
				req.Header.Set(k, v)
			}
			res, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer res.Body.Close()
			b, _ := io.ReadAll(res.Body)
			return res.StatusCode, string(b)
		}
		session := map[string]string{"Authorization": "Bearer " + sess.Token}
		apiKey := map[string]string{"Authorization": "Bearer " + issued.Token}
		code, _ = report(session, "project=OLD&runId=before")
		require.Equal(t, 201, code)
		code, _ = report(apiKey, "runId=key-before")
		require.Equal(t, 201, code, "the key reports into its project")

		code, body = do("POST", "/api/v1/projects/OLD/archive", "")
		require.Equal(t, 200, code)
		assert.Contains(t, body, `"archivedAt":"`)
		_, body = do("GET", "/api/v1/projects", "")
		var listed struct {
			Items []struct {
				Key string `json:"key"`
			} `json:"items"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &listed))
		keys := make([]string, 0, len(listed.Items))
		for _, p := range listed.Items {
			keys = append(keys, p.Key)
		}
		assert.Equal(t, []string{"TC"}, keys, "not in the projects list")
		_, body = do("GET", "/api/v1/test-runs", "")
		assert.NotContains(t, body, fmt.Sprintf(`"projectId":%d`, created.ID), "its runs leave the runs list")
		_, body = do("GET", "/api/v1/test-runs?project=OLD", "")
		assert.Contains(t, body, fmt.Sprintf(`"projectId":%d`, created.ID), "read by key")
		assert.Contains(t, body, `"projectKey":"OLD"`, "a run names its project (audit 2026-10-17, D5)")
		code, body = report(apiKey, "runId=key-after")
		assert.Equal(t, 409, code, "its CI key cannot report (audit 2026-10-17, D1)")
		assert.Contains(t, body, "project OLD is archived")
		code, body = do("POST", fmt.Sprintf("/api/v1/projects/OLD/api-keys/%d/revoke", issued.APIKey.ID), "")
		assert.Equal(t, 200, code, "revoking (reducing access) stays allowed")
		assert.Contains(t, body, `"status":"revoked"`)
		_, body = do("GET", "/api/v1/projects?archived=true", "")
		assert.Contains(t, body, `"key":"OLD"`)
		_, body = do("GET", "/api/v1/test-cases", "")
		assert.NotContains(t, body, "OLD-1", "nor in lists that span projects")
		_, body = do("GET", "/api/v1/test-cases?project=OLD", "")
		assert.Contains(t, body, "OLD-1", "read by key")
		code, _ = do("GET", "/api/v1/projects/OLD/webhooks", "")
		assert.Equal(t, 200, code, "reads that need a maintainer stay allowed")

		// Read-only: changes, reports and settings are a 409 that says why.
		for _, c := range [][3]string{
			{"POST", "/api/v1/test-cases", `{"title":"x","project":"OLD"}`},
			{"PATCH", "/api/v1/projects/OLD", `{"name":"x"}`},
			{"POST", "/api/v1/projects/OLD/suites", `{"key":"smoke","name":"Smoke","kind":"static"}`},
			{"POST", "/api/v1/ingestion/junit?project=OLD&provider=github&runId=1&runAttempt=1", `<testsuite name="s"><testcase name="t"/></testsuite>`},
			{"POST", "/api/v1/test-runs/manual", `{"project":"OLD","name":"sign-off","scope":"manual"}`},
			{"POST", "/api/v1/test-runs/live", `{"project":"OLD","provider":"github","runId":"2","runAttempt":1}`},
			{"PUT", "/api/v1/projects/OLD/members/nobody", `{"role":"member"}`},
			{"POST", "/api/v1/projects/OLD/api-keys", `{"name":"late"}`},
			{"POST", "/api/v1/invitations", `{"project":"OLD","role":"viewer"}`},
		} {
			req, _ := http.NewRequest(c[0], srv.URL+c[1], strings.NewReader(c[2]))
			req.Header.Set("Authorization", "Bearer "+sess.Token)
			req.Header.Set("Content-Type", map[bool]string{true: "application/xml", false: "application/json"}[strings.Contains(c[1], "junit")])
			res, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			b, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()
			assert.Equal(t, 409, res.StatusCode, c[0]+" "+c[1])
			assert.Contains(t, string(b), "project OLD is archived", c[0]+" "+c[1])
		}

		// Archiving and unarchiving race: every call answers 200 and the project ends in one of the two states.
		var wg sync.WaitGroup
		codes := make(chan int, 10)
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				action := map[bool]string{true: "unarchive", false: "archive"}[i%2 == 1]
				c, _ := do("POST", "/api/v1/projects/OLD/"+action, "")
				codes <- c
			}(i)
		}
		wg.Wait()
		close(codes)
		for c := range codes {
			assert.Equal(t, 200, c)
		}
		_, body = do("GET", "/api/v1/projects/OLD", "")
		assert.Regexp(t, `"archivedAt":(null|"20)`, body)

		// TC never: the database refuses it too; nor can a project be archived before it was created.
		code, _ = do("POST", "/api/v1/projects/TC/archive", "")
		assert.Equal(t, 409, code)
		_, err = db.Pool.Exec(ctx, `UPDATE projects SET archived_at = now() WHERE key = 'TC'`)
		assert.ErrorContains(t, err, "projects_default_never_archived")
		_, err = db.Pool.Exec(ctx, `UPDATE projects SET archived_at = created_at - interval '1 day' WHERE key = 'OLD'`)
		assert.ErrorContains(t, err, "projects_archived_after_creation")

		code, body = do("POST", "/api/v1/projects/OLD/unarchive", "")
		require.Equal(t, 200, code)
		assert.Contains(t, body, `"archivedAt":null`)
		code, _ = do("POST", "/api/v1/test-cases", `{"title":"again","project":"OLD"}`)
		assert.Equal(t, 201, code, "writable again")
		ids, err := s.Catalog.ActiveProjectIDs(ctx, nil)
		require.NoError(t, err)
		assert.Nil(t, ids, "nothing archived: nil keeps the lists' every-project fast path")
	})

	t.Run("BE-INT-032_numbers_are_per_project_contiguous_and_unique_under_concurrency", func(t *testing.T) {
		s, ctx := fresh(t)
		chk, err := s.Catalog.CreateProject(ctx, catalog.CreateProjectInput{Key: "CHK", Name: "Checkout"})
		require.NoError(t, err)
		tc, err := s.Catalog.Create(ctx, catalog.CreateInput{Title: "default"})
		require.NoError(t, err)
		assert.Equal(t, "TC-1", tc.Key())

		const n = 20
		var wg sync.WaitGroup
		numbers := make(chan int64, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: chk.ID, Title: "c", Automated: true})
				assert.NoError(t, err)
				assert.Equal(t, "CHK", c.ProjectKey)
				numbers <- c.Number
			}()
		}
		wg.Wait()
		close(numbers)
		seen := map[int64]bool{}
		for num := range numbers {
			assert.False(t, seen[num], "number %d assigned twice", num)
			seen[num] = true
		}
		for i := int64(1); i <= n; i++ {
			assert.True(t, seen[i], "CHK-%d missing: numbers are contiguous", i)
		}

		list, err := s.Catalog.List(ctx, catalog.ListFilter{ProjectIDs: []int64{chk.ID}}, pagination.Default())
		require.NoError(t, err)
		assert.Equal(t, int64(n), list.Total)
		got, err := s.Catalog.Get(ctx, list.Items[0].ID)
		require.NoError(t, err)
		assert.Equal(t, "CHK", got.ProjectKey, "keys resolve on reads too")

		view, err := s.Catalog.IngestionView(ctx, chk.ID, []int64{1, n + 1})
		require.NoError(t, err)
		assert.Len(t, view.Expected, n, "the expected universe is the project's")
		assert.Contains(t, view.Entries, int64(1))
		assert.NotContains(t, view.Entries, int64(n+1))

		keys, err := s.Catalog.Keys(ctx, []int64{tc.ID, got.ID, 987654})
		require.NoError(t, err)
		assert.Equal(t, map[int64]string{tc.ID: "TC-1", got.ID: got.Key()}, keys, "display keys for other modules; unknown ids absent")

		_, err = s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: 999, Title: "x"})
		e, _ := apperr.As(err)
		assert.Equal(t, apperr.KindNotFound, e.Kind)
	})

	t.Run("BE-INT-033_ingestion_correlates_within_the_run_project", func(t *testing.T) {
		s, ctx := fresh(t)
		chk, err := s.Catalog.CreateProject(ctx, catalog.CreateProjectInput{Key: "CHK", Name: "Checkout"})
		require.NoError(t, err)
		tc1, _ := s.Catalog.Create(ctx, catalog.CreateInput{Title: "default", Automated: true})
		chk1, _ := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: chk.ID, Title: "pay", Automated: true})
		_, _ = s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: chk.ID, Title: "refund", Automated: true})

		doc := junitFor(
			`<testcase name="pay CHK-1"/>`,
			`<testcase name="login TC-1"/>`,
			tcProp("cross", "TC-1", ""),
			tcProp("bare", "2", "<failure/>"),
		)
		m := meta("700", 1)
		m.ProjectKey = "CHK"
		out, err := s.Ingestion.IngestJUnit(ctx, m, strings.NewReader(doc))
		require.NoError(t, err)
		assert.Equal(t, chk.ID, out.Run.ProjectID)
		corr := map[string]execution.Correlation{}
		for _, d := range out.Diagnostics {
			corr[d.TestName] = d.Correlation
		}
		assert.Equal(t, map[string]execution.Correlation{
			"login TC-1": execution.CorrelationMissing,
			"cross":      execution.CorrelationWrongProject,
		}, corr, "the name fallback reads CHK only; a TC property in a CHK run is another project's")

		sum, err := s.Execution.Summary(ctx, out.Run.ID)
		require.NoError(t, err)
		assert.Equal(t, int32(2), sum.ExpectedTotal, "the snapshot is the CHK universe")
		assert.Equal(t, int32(1), sum.Diagnostics.WrongProject)

		// The same CI run id in the default project is another run.
		def, err := s.Ingestion.IngestJUnit(ctx, meta("700", 1), strings.NewReader(junitFor(`<testcase name="login TC-1"/>`)))
		require.NoError(t, err)
		assert.True(t, def.Created)
		assert.NotEqual(t, out.Run.ID, def.Run.ID)
		assert.Equal(t, catalog.DefaultProjectID, def.Run.ProjectID)
		replay, err := s.Ingestion.IngestJUnit(ctx, m, strings.NewReader(doc))
		require.NoError(t, err)
		assert.False(t, replay.Created, "externalRunId is unique per project")
		assert.Equal(t, out.Run.ID, replay.Run.ID)

		runs, err := s.Execution.ListRuns(ctx, execution.RunFilter{ProjectIDs: []int64{chk.ID}}, pagination.Default())
		require.NoError(t, err)
		assert.Equal(t, int64(1), runs.Total)
		all, err := s.Execution.ListRuns(ctx, execution.RunFilter{}, pagination.Default())
		require.NoError(t, err)
		assert.Equal(t, int64(2), all.Total)

		h, err := s.Execution.History(ctx, chk1.ID, pagination.Default())
		require.NoError(t, err)
		require.Len(t, h.Items, 1)
		assert.Equal(t, chk.ID, h.Items[0].Run.ProjectID)
		h, err = s.Execution.History(ctx, tc1.ID, pagination.Default())
		require.NoError(t, err)
		require.Len(t, h.Items, 1, "only the default run's TC-1 result is linked")
		assert.Equal(t, catalog.DefaultProjectID, h.Items[0].Run.ProjectID)

		m.ProjectKey = "NOPE"
		_, err = s.Ingestion.IngestJUnit(ctx, m, strings.NewReader(doc))
		e, _ := apperr.As(err)
		assert.Equal(t, apperr.KindNotFound, e.Kind)
	})

	t.Run("BE-INT-034_projects_migration_keeps_existing_ids_as_TC_keys", func(t *testing.T) {
		ctx := context.Background()
		require.NoError(t, db.Reset(ctx))
		sqlDB := stdlib.OpenDBFromPool(db.Pool)
		defer func() { _ = sqlDB.Close() }()
		require.NoError(t, goose.DownToContext(ctx, sqlDB, ".", 11))
		_, err := db.Pool.Exec(ctx, `INSERT INTO test_cases (title, automated) VALUES ('a', true), ('b', false), ('c', true)`)
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `INSERT INTO test_runs (external_run_id, provider, provider_run_id, run_attempt, status)
			VALUES ('github:1:1', 'github', '1', 1, 'completed')`)
		require.NoError(t, err)
		require.NoError(t, postgres.Migrate(ctx, db.Pool, "up"))

		var keys []string
		rows, err := db.Pool.Query(ctx, `SELECT p.key || '-' || t.number FROM test_cases t JOIN projects p ON p.id = t.project_id WHERE t.number = t.id ORDER BY t.id`)
		require.NoError(t, err)
		for rows.Next() {
			var k string
			require.NoError(t, rows.Scan(&k))
			keys = append(keys, k)
		}
		require.NoError(t, rows.Err())
		assert.Len(t, keys, 3, "every existing test case keeps number = id")
		var next, runProject int64
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT next_number FROM projects WHERE key = 'TC'`).Scan(&next))
		assert.Equal(t, int64(4), next, "the counter continues after the highest id")
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT project_id FROM test_runs`).Scan(&runProject))
		assert.Equal(t, catalog.DefaultProjectID, runProject)
		require.NoError(t, db.Reset(ctx))
	})
}
