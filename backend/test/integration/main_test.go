//go:build integration

// Package integration exercises application, persistence and infrastructure
// together against a real PostgreSQL (testcontainers-go). Each subtest name
// starts with the id of the integration target it covers (see
// coverage/inventories/backend-integration.yaml).
package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/app"
	"github.com/edcrove/provenly/backend/test/testdb"
)

var db *testdb.DB

func TestMain(m *testing.M) {
	testdb.Main(func(d *testdb.DB) int {
		db = d
		return m.Run()
	})
}

// fresh empties the database and returns services wired on it.
func fresh(t *testing.T) (app.Services, context.Context) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, db.Reset(ctx))
	return app.NewServices(db.Pool, time.Now), ctx
}

func ptr[T any](v T) *T { return &v }
