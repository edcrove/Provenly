// Package cli implements the provenly command line: `serve` (default) and
// `migrate <up|down|status|...>`.
package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcrove/provenly/backend/internal/app"
	"github.com/edcrove/provenly/backend/internal/identity"
	"github.com/edcrove/provenly/backend/internal/platform/config"
	"github.com/edcrove/provenly/backend/internal/platform/postgres"
	"github.com/edcrove/provenly/backend/internal/platform/server"
	"github.com/edcrove/provenly/backend/internal/platform/telemetry"
)

// Deps are the side-effecting operations of the CLI, injectable for tests.
type Deps struct {
	Getenv  func(string) string
	Stderr  io.Writer
	OpenDB  func(ctx context.Context, url string) (*pgxpool.Pool, error)
	Migrate func(ctx context.Context, pool *pgxpool.Pool, command string) error
	Listen  func(addr string) (net.Listener, error)
	Serve   func(ctx context.Context, l net.Listener, h http.Handler) error
	// Exporter builds the OpenTelemetry span exporter when an OTLP endpoint is configured.
	Exporter telemetry.Exporter
}

// DefaultDeps are the production dependencies.
func DefaultDeps(getenv func(string) string, stderr io.Writer) Deps {
	return Deps{
		Getenv: getenv, Stderr: stderr,
		OpenDB: postgres.Open, Migrate: postgres.Migrate, Exporter: telemetry.OTLP,
		Listen: func(addr string) (net.Listener, error) { return net.Listen("tcp", addr) },
		Serve: func(ctx context.Context, l net.Listener, h http.Handler) error {
			return server.Run(ctx, l, h, server.ShutdownTimeout)
		},
	}
}

const usage = "usage: provenly [serve | migrate <up|down|status|reset|version>]"

// Run executes the command in args and returns the process exit code.
func Run(ctx context.Context, args []string, d Deps) int {
	if err := run(ctx, args, d); err != nil {
		fmt.Fprintln(d.Stderr, "error:", err)
		return 1
	}
	return 0
}

func run(ctx context.Context, args []string, d Deps) error {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	if cmd != "serve" && (cmd != "migrate" || len(args) != 2) {
		return fmt.Errorf("%s", usage)
	}
	cfg, err := config.Load(d.Getenv)
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(telemetry.LogHandler{Handler: slog.NewJSONHandler(d.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel})}))
	shutdown, err := telemetry.Setup(ctx, cfg.Env, cfg.OTLPExport, d.Exporter)
	if err != nil {
		return fmt.Errorf("opentelemetry: %w", err)
	}
	defer func() { _ = shutdown(context.WithoutCancel(ctx)) }()
	pool, err := d.OpenDB(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if cmd == "migrate" {
		return d.Migrate(ctx, pool, args[1])
	}
	if cfg.AutoMigrate {
		if err := d.Migrate(ctx, pool, "up"); err != nil {
			return err
		}
	}
	l, err := d.Listen(cfg.HTTPAddr)
	if err != nil {
		return err
	}
	secret := cfg.JWTSecret
	if secret == nil {
		slog.WarnContext(ctx, "PROVENLY_JWT_SECRET is not set: using a random secret, sessions end when the API restarts")
		secret = identity.RandomSecret()
	}
	services := app.NewServicesWith(pool, time.Now, identity.DefaultConfig(secret))
	if cfg.AdminUsername != "" {
		if err := services.Identity.Bootstrap(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
			return err
		}
	}
	return d.Serve(ctx, l, app.NewHandler(services, cfg.MaxIngestBytes))
}
