// Package app composes the modules of the modular monolith into one HTTP handler.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcrove/provenly/backend/internal/audit"
	auditpg "github.com/edcrove/provenly/backend/internal/audit/postgres"
	"github.com/edcrove/provenly/backend/internal/catalog"
	catalogpg "github.com/edcrove/provenly/backend/internal/catalog/postgres"
	"github.com/edcrove/provenly/backend/internal/execution"
	executionpg "github.com/edcrove/provenly/backend/internal/execution/postgres"
	"github.com/edcrove/provenly/backend/internal/identity"
	identitypg "github.com/edcrove/provenly/backend/internal/identity/postgres"
	"github.com/edcrove/provenly/backend/internal/ingestion"
	"github.com/edcrove/provenly/backend/internal/insights"
	"github.com/edcrove/provenly/backend/internal/integrations"
	integrationspg "github.com/edcrove/provenly/backend/internal/integrations/postgres"
	"github.com/edcrove/provenly/backend/internal/mcp"
	"github.com/edcrove/provenly/backend/internal/platform/httpx"
	"github.com/edcrove/provenly/backend/internal/platform/secrets"
	"github.com/edcrove/provenly/backend/internal/platform/telemetry"
)

// Services exposes the application layer of every module, so REST and future
// interfaces (e.g. MCP) share the same use cases.
type Services struct {
	Catalog   *catalog.Service
	Execution *execution.Service
	Ingestion *ingestion.Service
	Manual    *ingestion.Manual
	Live      *ingestion.Live
	Insights  *insights.Service
	// Integrations are the webhooks and connectors; its Run sends the queued webhook deliveries.
	Integrations *integrations.Service
	// Audit records every authenticated change made through the API.
	Audit    *audit.Service
	Identity *identity.Service
	// Now is the clock of the services (session and invitation expiry).
	Now func() time.Time
	// Ready reports whether the dependencies needed to serve requests (the
	// database) are reachable.
	Ready func(context.Context) error
}

// NewServices wires the modules on a PostgreSQL pool with a random session secret
// (sessions do not survive a restart) and the production identity lifetimes.
func NewServices(pool *pgxpool.Pool, now func() time.Time) Services {
	return NewServicesWith(pool, now, identity.DefaultConfig(identity.RandomSecret()))
}

// Config configures the modules.
type Config struct {
	Identity identity.Config
	// SecretsKey encrypts webhook secrets and connector tokens: 32 bytes, or nil for a random key (what is stored
	// cannot be read after a restart).
	SecretsKey   []byte
	Integrations integrations.Config
}

// DefaultGitHubAPIURL is the public GitHub REST API.
const DefaultGitHubAPIURL = "https://api.github.com"

// NewServicesWith wires the modules with an explicit identity configuration, a random secrets key and outbound
// requests restricted to public addresses.
func NewServicesWith(pool *pgxpool.Pool, now func() time.Time, idcfg identity.Config) Services {
	return NewServicesConfig(pool, now, Config{Identity: idcfg, Integrations: integrations.Config{GitHubAPIURL: DefaultGitHubAPIURL}})
}

// NewServicesConfig wires the modules with an explicit configuration.
func NewServicesConfig(pool *pgxpool.Pool, now func() time.Time, cfg Config) Services {
	idcfg := cfg.Identity
	cat := catalog.NewService(catalogpg.NewStore(pool))
	ids := identity.NewService(identitypg.NewStore(pool), now, idcfg)
	exe := execution.NewService(executionpg.NewStore(pool), now)
	// Requirement coverage reads the latest results through the catalog's port.
	cat.SetResults(exe)
	ing := ingestion.NewService(cat, exe, ids)
	manual := ingestion.NewManual(cat, exe, ids)
	box, _ := secrets.New(cfg.SecretsKey) // config.Load only accepts a 32-byte key (or none)
	integ := integrations.NewService(integrationspg.NewStore(pool), cat, ids, box, cfg.Integrations, now)
	// Completed runs (reports, live runs, manual runs) are exported to the project's webhooks.
	ing.SetNotifier(integ)
	manual.SetNotifier(integ)
	return Services{
		Catalog: cat, Execution: exe, Ingestion: ing, Live: ingestion.NewLive(ing, exe, now), Manual: manual,
		Insights:     insights.NewService(cat, exe, ids, now),
		Integrations: integ,
		Audit:        audit.NewService(auditpg.NewStore(pool), ids),
		Identity:     ids, Now: now, Ready: pool.Ping,
	}
}

// Version is the version of the Provenly server, set at build time by release images
// (-ldflags "-X github.com/edcrove/provenly/backend/internal/app.Version=v1.2.3"); "dev" otherwise.
var Version = "dev"

// readyTimeout bounds the readiness probe so a hung database fails it quickly.
const readyTimeout = 2 * time.Second

// NewHandler builds the REST API handler.
func NewHandler(s Services, maxIngestBytes int64) http.Handler {
	mux := http.NewServeMux()
	agents := mcp.NewHandler(Version)
	register(mux, s, maxIngestBytes, agents)
	// MCP tools call the API in-process, as the caller (same routing, authorization and problem answers).
	agents.Bind(httpx.Routes(mux))
	return httpx.Recover(telemetry.Middleware(httpx.AccessLog(httpx.Routes(mux))))
}

// RoutePatterns lists every route the API registers ("METHOD /path"), so tests
// can check the router against the OpenAPI contract.
func RoutePatterns() []string {
	var rec patternRecorder
	register(&rec, Services{}, 0, mcp.NewHandler(Version))
	return rec
}

func register(r httpx.Router, s Services, maxIngestBytes int64, agents *mcp.Handler) {
	r.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.HandleFunc("GET /readyz", func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), readyTimeout)
		defer cancel()
		if err := s.Ready(ctx); err != nil {
			slog.WarnContext(ctx, "not ready", "error", err)
			httpx.WriteProblem(w, http.StatusServiceUnavailable, httpx.CodeServiceUnavailable, "database is unreachable")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	ids := identity.NewHandler(s.Identity, s.Catalog, s.Now)
	ids.RegisterPublic(r)
	// CI reports with a project API key; people with a session.
	// Every authenticated change is audited (the audit router wraps the handlers, inside authentication).
	keys := audit.Wrap(identity.ProtectWithKeys(r, s.Identity), s.Audit)
	ingestion.NewHandler(s.Ingestion, maxIngestBytes).Register(keys)
	ingestion.NewLiveHandler(s.Live).Register(keys)
	// Every other API route needs a session.
	p := audit.Wrap(identity.Protect(r, s.Identity), s.Audit)
	ids.RegisterProtected(p)
	catalog.NewHandler(s.Catalog, s.Identity).Register(p)
	execution.NewHandler(s.Execution, s.Catalog, s.Identity).Register(p)
	ingestion.NewManualHandler(s.Manual).Register(p)
	insights.NewHandler(s.Insights).Register(p)
	integrations.NewHandler(s.Integrations).Register(p)
	agents.Register(p)
	audit.NewHandler(s.Audit).Register(p)
}

type patternRecorder []string

func (p *patternRecorder) HandleFunc(pattern string, _ func(http.ResponseWriter, *http.Request)) {
	*p = append(*p, pattern)
}
