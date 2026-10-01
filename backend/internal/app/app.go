// Package app composes the modules of the modular monolith into one HTTP handler.
package app

import (
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
}

// NewServices wires the modules on a PostgreSQL pool.
func NewServices(pool *pgxpool.Pool, now func() time.Time) Services {
	cat := catalog.NewService(catalogpg.NewStore(pool))
	exe := execution.NewService(executionpg.NewStore(pool), now)
	return Services{Catalog: cat, Execution: exe, Ingestion: ingestion.NewService(cat, exe)}
}

// NewHandler builds the REST API handler.
func NewHandler(s Services, maxIngestBytes int64) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	catalog.NewHandler(s.Catalog).Register(mux)
	execution.NewHandler(s.Execution, s.Catalog).Register(mux)
	ingestion.NewHandler(s.Ingestion, maxIngestBytes).Register(mux)
	return httpx.Recover(httpx.AccessLog(httpx.Routes(mux)))
}
