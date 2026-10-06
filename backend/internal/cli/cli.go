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
	"github.com/edcrove/provenly/backend/internal/integrations"
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
	// BreakGlass makes a password reset link for a user without an administrator (reset-password).
	BreakGlass func(ctx context.Context, pool *pgxpool.Pool, username string) (identity.PasswordReset, string, error)
}

// DefaultDeps are the production dependencies.
func DefaultDeps(getenv func(string) string, stderr io.Writer) Deps {
	return Deps{
		Getenv: getenv, Stderr: stderr,
		OpenDB: postgres.Open, Migrate: postgres.Migrate, Exporter: telemetry.OTLP, BreakGlass: breakGlass,
		Listen: func(addr string) (net.Listener, error) { return net.Listen("tcp", addr) },
		Serve: func(ctx context.Context, l net.Listener, h http.Handler) error {
			return server.Run(ctx, l, h, server.ShutdownTimeout)
		},
	}
}

const usage = "usage: provenly [serve | migrate <up|down|status|reset|version> | reset-password <username>]"

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
	if cmd != "serve" && ((cmd != "migrate" && cmd != "reset-password") || len(args) != 2) {
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
	if cmd == "reset-password" {
		return resetPassword(ctx, pool, args[1], d)
	}
	if cfg.AutoMigrate {
		if err := d.Migrate(ctx, pool, "up"); err != nil {
			return err
		}
	}
	slog.InfoContext(ctx, "starting provenly", "version", app.Version, "env", cfg.Env)
	l, err := d.Listen(cfg.HTTPAddr)
	if err != nil {
		return err
	}
	secret := cfg.JWTSecret
	if secret == nil {
		slog.WarnContext(ctx, "PROVENLY_JWT_SECRET is not set: using a random secret, sessions end when the API restarts")
		secret = identity.RandomSecret()
	}
	if cfg.SecretsKey == nil {
		slog.WarnContext(ctx, "PROVENLY_SECRETS_KEY is not set: using a random key, webhook secrets and connector tokens cannot be read after the API restarts")
	}
	services := app.NewServicesConfig(pool, time.Now, app.Config{
		Identity: identity.DefaultConfig(secret), SecretsKey: cfg.SecretsKey, TrustedProxies: cfg.TrustedProxies,
		Integrations: integrations.Config{AllowPrivate: cfg.WebhooksAllowPrivate, GitHubAPIURL: cfg.GitHubAPIURL,
			DeliveryRetention: time.Duration(cfg.WebhookDeliveryRetentionDays) * 24 * time.Hour},
	})
	if cfg.AdminUsername != "" {
		if err := services.Identity.Bootstrap(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
			return err
		}
	}
	// The webhook deliveries are sent while the API serves; the worker stops with it.
	workerCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		services.Integrations.Run(workerCtx, deliveryInterval)
		close(done)
	}()
	defer func() {
		stop()
		<-done
	}()
	return d.Serve(ctx, l, app.NewHandler(services, cfg.MaxIngestBytes))
}

// deliveryInterval is how often due webhook deliveries are sent.
const deliveryInterval = 2 * time.Second

// breakGlass makes the reset link through the identity service on the given database.
func breakGlass(ctx context.Context, pool *pgxpool.Pool, username string) (identity.PasswordReset, string, error) {
	services := app.NewServicesConfig(pool, time.Now, app.Config{Identity: identity.DefaultConfig(identity.RandomSecret())})
	return services.Identity.BreakGlassReset(ctx, username)
}

// resetPassword is the break-glass way back in (card #61): run on the server, it prints a single-use link token to set
// a new password for the user (reactivating them), without an administrator session.
func resetPassword(ctx context.Context, pool *pgxpool.Pool, username string, d Deps) error {
	reset, token, err := d.BreakGlass(ctx, pool, username)
	if err != nil {
		return err
	}
	fmt.Fprintf(d.Stderr, "password reset link for %s (single use, until %s):\n  <web address>/reset-password?token=%s\n",
		username, reset.ExpiresAt.UTC().Format(time.RFC3339), token)
	return nil
}
