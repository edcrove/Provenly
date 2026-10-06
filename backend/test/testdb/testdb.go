// Package testdb starts a disposable PostgreSQL (testcontainers-go) with the
// schema migrated, for Integration and Contract tests.
package testdb

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/edcrove/provenly/backend/internal/platform/postgres"
)

// DB is a running, migrated database.
type DB struct {
	URL       string
	Pool      *pgxpool.Pool
	container testcontainers.Container
}

// Start launches PostgreSQL 16 and applies every goose migration.
func Start(ctx context.Context) (*DB, error) {
	c, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("provenly"),
		tcpostgres.WithUsername("provenly"),
		tcpostgres.WithPassword("provenly"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		return nil, fmt.Errorf("start postgres container: %w", err)
	}
	url, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, err
	}
	pool, err := postgres.Open(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := postgres.Migrate(ctx, pool, "up"); err != nil {
		return nil, err
	}
	return &DB{URL: url, Pool: pool, container: c}, nil
}

// Reset empties every table (TRUNCATE does not fire the row-level protection triggers).
func (d *DB) Reset(ctx context.Context) error {
	// Identities restart so the default project is id 1 again and numbers start at 1.
	_, err := d.Pool.Exec(ctx, `TRUNCATE audit_events, password_resets, personal_access_token_projects, personal_access_tokens, webhook_deliveries, webhooks, github_connections, test_run_events, test_run_shards, issue_test_cases, issues, requirement_test_cases, requirements, test_suite_cases, test_suites, test_case_tags, test_case_classifications, classification_values, classification_dimensions, api_keys, test_run_amendments, test_run_parse_errors, test_results, test_run_expected_cases, test_runs, test_steps, test_cases, projects, project_members, invitations, users RESTART IDENTITY;
INSERT INTO projects (key, name, description) VALUES ('TC', 'Default', 'Test cases created before projects existed.')`)
	return err
}

// Stop closes the pool and removes the container.
func (d *DB) Stop(ctx context.Context) {
	d.Pool.Close()
	_ = d.container.Terminate(ctx)
}

// Main wraps a package TestMain: it starts the database, runs the tests and cleans up.
func Main(run func(*DB) int) {
	ctx := context.Background()
	db, err := Start(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := run(db)
	db.Stop(ctx)
	os.Exit(code)
}
