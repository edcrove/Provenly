//go:build integration

package integration

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/app"
	"github.com/edcrove/provenly/backend/internal/cli"
	"github.com/edcrove/provenly/backend/internal/platform/postgres"
)

func TestPlatform(t *testing.T) {
	t.Run("BE-INT-001_migrations_apply_down_and_up_reproducibly", func(t *testing.T) {
		ctx := context.Background()
		require.NoError(t, postgres.Migrate(ctx, db.Pool, "status"))
		// fingerprint lists every object the migrations own in the public schema: tables and their columns, constraints,
		// indexes, functions, triggers, sequences and types.
		fingerprint := func() []string {
			rows, err := db.Pool.Query(ctx, `
				SELECT 'table ' || table_name || '.' || column_name || ' ' || data_type || ' ' || is_nullable || ' ' || coalesce(column_default, '')
				  FROM information_schema.columns WHERE table_schema = 'public' AND table_name <> 'goose_db_version'
				UNION ALL SELECT 'constraint ' || conrelid::regclass || ' ' || conname || ' ' || pg_get_constraintdef(oid)
				  FROM pg_constraint WHERE connamespace = 'public'::regnamespace AND conrelid::regclass::text <> 'goose_db_version'
				UNION ALL SELECT 'index ' || indexname || ' ' || indexdef FROM pg_indexes
				  WHERE schemaname = 'public' AND tablename <> 'goose_db_version'
				UNION ALL SELECT 'function ' || p.proname || ' ' || md5(pg_get_functiondef(p.oid)) FROM pg_proc p
				  WHERE p.pronamespace = 'public'::regnamespace
				UNION ALL SELECT 'trigger ' || tgrelid::regclass || ' ' || tgname FROM pg_trigger t
				  JOIN pg_class c ON c.oid = t.tgrelid WHERE NOT t.tgisinternal AND c.relnamespace = 'public'::regnamespace
				UNION ALL SELECT 'sequence ' || sequence_name FROM information_schema.sequences
				  WHERE sequence_schema = 'public' AND sequence_name NOT LIKE 'goose_db_version%'
				UNION ALL SELECT 'type ' || t.typname FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace
				  WHERE n.nspname = 'public' AND t.typtype IN ('e', 'd', 'c') AND NOT EXISTS (SELECT 1 FROM pg_class r WHERE r.reltype = t.oid)
				ORDER BY 1`)
			require.NoError(t, err)
			var out []string
			for rows.Next() {
				var line string
				require.NoError(t, rows.Scan(&line))
				out = append(out, line)
			}
			require.NoError(t, rows.Err())
			return out
		}
		tables := func() []string {
			rows, err := db.Pool.Query(ctx, `SELECT table_name FROM information_schema.tables
				WHERE table_schema = 'public' AND table_name <> 'goose_db_version' ORDER BY 1`)
			require.NoError(t, err)
			var out []string
			for rows.Next() {
				var name string
				require.NoError(t, rows.Scan(&name))
				out = append(out, name)
			}
			return out
		}
		before := fingerprint()
		require.Greater(t, len(before), 300, "the fingerprint sees the schema")

		// Every down migration removes what its up created: nothing of the schema is left behind.
		require.NoError(t, postgres.Migrate(ctx, db.Pool, "reset"))
		assert.Empty(t, fingerprint())

		// Up again: the exact tables, and the same schema object for object.
		require.NoError(t, postgres.Migrate(ctx, db.Pool, "up"))
		assert.Equal(t, []string{"api_keys", "audit_events", "classification_dimensions", "classification_values",
			"github_connections", "invitations", "issue_test_cases", "issues", "password_resets", "personal_access_token_projects",
			"personal_access_tokens", "project_members", "projects",
			"requirement_test_cases", "requirements", "test_case_classifications", "test_case_tags", "test_cases",
			"test_results", "test_run_amendments", "test_run_events", "test_run_expected_cases", "test_run_parse_errors", "test_run_shards",
			"test_runs", "test_steps", "test_suite_cases", "test_suites", "users", "webhook_deliveries", "webhooks"}, tables())
		assert.Equal(t, before, fingerprint())
		assert.ErrorContains(t, postgres.Migrate(ctx, db.Pool, "sideways"), "migrate sideways")
	})

	t.Run("BE-INT-022_readiness_reflects_database_reachability", func(t *testing.T) {
		ctx := context.Background()
		probe := func(s app.Services) int {
			rec := httptest.NewRecorder()
			app.NewHandler(s, 1024).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			return rec.Code
		}
		assert.Equal(t, http.StatusOK, probe(app.NewServices(db.Pool, time.Now)))

		pool, err := postgres.Open(ctx, db.URL)
		require.NoError(t, err)
		pool.Close()
		assert.Equal(t, http.StatusServiceUnavailable, probe(app.NewServices(pool, time.Now)))
	})

	t.Run("BE-INT-015_cli_migrates_and_serves_against_postgres", func(t *testing.T) {
		require.NoError(t, db.Reset(context.Background()))
		env := map[string]string{"PROVENLY_DATABASE_URL": db.URL, "PROVENLY_AUTO_MIGRATE": "true", "PROVENLY_HTTP_ADDR": "127.0.0.1:0",
			"PROVENLY_ADMIN_USERNAME": "admin", "PROVENLY_ADMIN_PASSWORD": "correct horse", "PROVENLY_JWT_SECRET": strings.Repeat("k", 32)}
		var stderr bytes.Buffer
		d := cli.DefaultDeps(func(k string) string { return env[k] }, &stderr)
		assert.Equal(t, 0, cli.Run(context.Background(), []string{"migrate", "up"}, d), stderr.String())

		addr := make(chan string, 1)
		listen := d.Listen
		d.Listen = func(a string) (net.Listener, error) {
			l, err := listen(a)
			if err == nil {
				addr <- l.Addr().String()
			}
			return l, err
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan int, 1)
		go func() { done <- cli.Run(ctx, []string{"serve"}, d) }()
		base := "http://" + <-addr
		require.Eventually(t, func() bool {
			resp, err := http.Get(base + "/readyz")
			if err != nil {
				return false
			}
			_ = resp.Body.Close()
			return resp.StatusCode == http.StatusOK
		}, 5*time.Second, 50*time.Millisecond)
		// The configured administrator was bootstrapped and signs in; the session opens the API.
		resp, err := http.Post(base+"/api/v1/auth/login", "application/json", strings.NewReader(`{"username":"admin","password":"correct horse"}`))
		require.NoError(t, err)
		_ = resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		req, _ := http.NewRequest(http.MethodGet, base+"/api/v1/test-cases", nil)
		req.AddCookie(resp.Cookies()[0])
		resp, err = http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		cancel()
		assert.Equal(t, 0, <-done, stderr.String())

		env["PROVENLY_DATABASE_URL"] = "postgres://nobody:wrong@127.0.0.1:1/none?sslmode=disable&connect_timeout=1"
		assert.Equal(t, 1, cli.Run(context.Background(), []string{"migrate", "up"}, d))
		assert.Contains(t, stderr.String(), "ping database")
		env["PROVENLY_DATABASE_URL"] = "::not a url::"
		assert.Equal(t, 1, cli.Run(context.Background(), []string{"migrate", "up"}, d))
		assert.Contains(t, stderr.String(), "open database")
	})
}
