// Package postgres opens the connection pool and applies goose migrations.
package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/edcrove/provenly/backend/migrations"
)

// Open creates a pgx pool and verifies connectivity.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// Every query is an OpenTelemetry span of the request or job that ran it.
	cfg.ConnConfig.Tracer = otelpgx.NewTracer()
	// NewWithConfig only fails on a config ParseConfig rejects; connecting is checked by the ping.
	pool, _ := pgxpool.NewWithConfig(ctx, cfg)
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// Migrate runs a goose command ("up", "down", "status", ...) with the embedded migrations.
func Migrate(ctx context.Context, pool *pgxpool.Pool, command string) error {
	db := stdlib.OpenDBFromPool(pool)
	defer func(db *sql.DB) { _ = db.Close() }(db)
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	if err := goose.RunContext(ctx, command, db, "."); err != nil {
		return fmt.Errorf("migrate %s: %w", command, err)
	}
	return nil
}
