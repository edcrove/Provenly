//go:build integration

// Package integration exercises application, persistence and infrastructure
// together against a real PostgreSQL (testcontainers-go). Each subtest name
// starts with the id of the integration target it covers (see
// coverage/inventories/backend-integration.yaml).
package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/edcrove/provenly/backend/internal/app"
	"github.com/edcrove/provenly/backend/internal/identity"
	"github.com/edcrove/provenly/backend/test/testdb"
)

var db *testdb.DB

func TestMain(m *testing.M) {
	testdb.Main(func(d *testdb.DB) int {
		db = d
		return m.Run()
	})
}

// fresh empties the database and returns services wired on it, with a context signed in as an
// administrator (ingestion authorizes its caller; administrators may report into any project).
func fresh(t *testing.T) (app.Services, context.Context) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, db.Reset(ctx))
	return app.NewServicesWith(db.Pool, time.Now, testIdentity()), identity.WithUser(ctx, identity.User{Username: "integration", IsAdmin: true})
}

// testIdentity is the production identity configuration with the cheapest bcrypt cost.
func testIdentity() identity.Config {
	cfg := identity.DefaultConfig([]byte(strings.Repeat("k", 32)))
	cfg.BcryptCost = bcrypt.MinCost
	return cfg
}

func ptr[T any](v T) *T { return &v }
