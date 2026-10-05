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
		require.NoError(t, postgres.Migrate(ctx, db.Pool, "reset"))
		var n int
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_name LIKE 'test_%'`).Scan(&n))
		assert.Equal(t, 0, n)
		require.NoError(t, postgres.Migrate(ctx, db.Pool, "up"))
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_name LIKE 'test_%'`).Scan(&n))
		assert.Equal(t, 11, n)
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
