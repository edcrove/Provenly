// Package app composes the modules of the modular monolith into one HTTP handler.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcrove/provenly/backend/internal/catalog"
	catalogpg "github.com/edcrove/provenly/backend/internal/catalog/postgres"
	"github.com/edcrove/provenly/backend/internal/execution"
	executionpg "github.com/edcrove/provenly/backend/internal/execution/postgres"
	"github.com/edcrove/provenly/backend/internal/identity"
	identitypg "github.com/edcrove/provenly/backend/internal/identity/postgres"
	"github.com/edcrove/provenly/backend/internal/ingestion"
	"github.com/edcrove/provenly/backend/internal/insights"
	"github.com/edcrove/provenly/backend/internal/platform/httpx"
)

// Services exposes the application layer of every module, so REST and future
// interfaces (e.g. MCP) share the same use cases.
type Services struct {
	Catalog   *catalog.Service
	Execution *execution.Service
	Ingestion *ingestion.Service
	Manual    *ingestion.Manual
	Insights  *insights.Service
	Identity  *identity.Service
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

// NewServicesWith wires the modules with an explicit identity configuration.
func NewServicesWith(pool *pgxpool.Pool, now func() time.Time, idcfg identity.Config) Services {
	cat := catalog.NewService(catalogpg.NewStore(pool))
	ids := identity.NewService(identitypg.NewStore(pool), now, idcfg)
	exe := execution.NewService(executionpg.NewStore(pool), now)
	// Requirement coverage reads the latest results through the catalog's port.
	cat.SetResults(exe)
	return Services{
		Catalog: cat, Execution: exe, Ingestion: ingestion.NewService(cat, exe, ids),
		Manual:   ingestion.NewManual(cat, exe, ids),
		Insights: insights.NewService(cat, exe, ids, now),
		Identity: ids, Now: now, Ready: pool.Ping,
	}
}

// readyTimeout bounds the readiness probe so a hung database fails it quickly.
const readyTimeout = 2 * time.Second

// NewHandler builds the REST API handler.
func NewHandler(s Services, maxIngestBytes int64) http.Handler {
	mux := http.NewServeMux()
	register(mux, s, maxIngestBytes)
	return httpx.Recover(httpx.AccessLog(httpx.Routes(mux)))
}

// RoutePatterns lists every route the API registers ("METHOD /path"), so tests
// can check the router against the OpenAPI contract.
func RoutePatterns() []string {
	var rec patternRecorder
	register(&rec, Services{}, 0)
	return rec
}

func register(r httpx.Router, s Services, maxIngestBytes int64) {
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
	ingestion.NewHandler(s.Ingestion, maxIngestBytes).Register(identity.ProtectWithKeys(r, s.Identity))
	// Every other API route needs a session.
	p := identity.Protect(r, s.Identity)
	ids.RegisterProtected(p)
	catalog.NewHandler(s.Catalog, s.Identity).Register(p)
	execution.NewHandler(s.Execution, s.Catalog, s.Identity).Register(p)
	ingestion.NewManualHandler(s.Manual).Register(p)
	insights.NewHandler(s.Insights).Register(p)
}

type patternRecorder []string

func (p *patternRecorder) HandleFunc(pattern string, _ func(http.ResponseWriter, *http.Request)) {
	*p = append(*p, pattern)
}
