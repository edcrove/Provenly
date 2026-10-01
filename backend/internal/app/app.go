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
	"github.com/edcrove/provenly/backend/internal/ingestion"
	"github.com/edcrove/provenly/backend/internal/platform/httpx"
)

// Services exposes the application layer of every module, so REST and future
// interfaces (e.g. MCP) share the same use cases.
type Services struct {
	Catalog   *catalog.Service
	Execution *execution.Service
	Ingestion *ingestion.Service
	// Ready reports whether the dependencies needed to serve requests (the
	// database) are reachable.
	Ready func(context.Context) error
}

// NewServices wires the modules on a PostgreSQL pool.
func NewServices(pool *pgxpool.Pool, now func() time.Time) Services {
	cat := catalog.NewService(catalogpg.NewStore(pool))
	exe := execution.NewService(executionpg.NewStore(pool), now)
	return Services{Catalog: cat, Execution: exe, Ingestion: ingestion.NewService(cat, exe), Ready: pool.Ping}
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
	catalog.NewHandler(s.Catalog).Register(r)
	execution.NewHandler(s.Execution, s.Catalog).Register(r)
	ingestion.NewHandler(s.Ingestion, maxIngestBytes).Register(r)
}

type patternRecorder []string

func (p *patternRecorder) HandleFunc(pattern string, _ func(http.ResponseWriter, *http.Request)) {
	*p = append(*p, pattern)
}
