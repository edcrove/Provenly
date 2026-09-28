//go:build integration

package integration

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
		assert.Equal(t, 5, n)
		assert.ErrorContains(t, postgres.Migrate(ctx, db.Pool, "sideways"), "migrate sideways")
	})

	t.Run("BE-INT-015_cli_migrates_and_serves_against_postgres", func(t *testing.T) {
		env := map[string]string{"PROVENLY_DATABASE_URL": db.URL, "PROVENLY_AUTO_MIGRATE": "true", "PROVENLY_HTTP_ADDR": "127.0.0.1:0"}
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
			resp, err := http.Get(base + "/api/v1/test-cases")
			if err != nil {
				return false
			}
			_ = resp.Body.Close()
			return resp.StatusCode == http.StatusOK
		}, 5*time.Second, 50*time.Millisecond)
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
