package ingestion

import (
	"context"
	"net/http"

	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/platform/httpx"
)

// ManualAPI is the manual execution use cases exposed over REST.
type ManualAPI interface {
	Start(ctx context.Context, in ManualRunInput) (execution.TestRun, error)
	Record(ctx context.Context, runID int64, in ManualResultInput) (execution.TestResult, error)
	Finish(ctx context.Context, runID int64, status execution.RunStatus) (execution.TestRun, error)
}

// ManualHandler is the REST adapter of manual execution (session routes).
type ManualHandler struct{ api ManualAPI }

// NewManualHandler builds a ManualHandler.
func NewManualHandler(api ManualAPI) *ManualHandler { return &ManualHandler{api: api} }

// Register mounts the manual execution routes.
func (h *ManualHandler) Register(mux httpx.Router) {
	mux.HandleFunc("POST /api/v1/test-runs/manual", h.start)
	mux.HandleFunc("POST /api/v1/test-runs/{testRunId}/manual-results", h.record)
	mux.HandleFunc("POST /api/v1/test-runs/{testRunId}/finish", h.finish)
}

type startRequest struct {
	Project string      `json:"project"`
	Suite   string      `json:"suite"`
	Name    string      `json:"name"`
	Scope   ManualScope `json:"scope"`
	Branch  string      `json:"branch"`
	Commit  string      `json:"commit"`
}

type recordRequest struct {
	TestCaseID int64                  `json:"testCaseId"`
	Status     execution.ResultStatus `json:"status"`
	Note       string                 `json:"note"`
	FailedStep *int32                 `json:"failedStep"`
	DurationMs *int64                 `json:"durationMs"`
}

type finishRequest struct {
	Status execution.RunStatus `json:"status"`
}

func (h *ManualHandler) start(w http.ResponseWriter, r *http.Request) {
	var req startRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	run, err := h.api.Start(r.Context(), ManualRunInput{
		ProjectKey: req.Project, SuiteKey: req.Suite, Name: req.Name, Scope: req.Scope, Branch: req.Branch, Commit: req.Commit,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, execution.RunDTO(run))
}

func (h *ManualHandler) record(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "testRunId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var req recordRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.api.Record(r.Context(), id, ManualResultInput(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, execution.ResultDTO(res, map[int64]string{*res.TestCaseID: res.TestName}))
}

func (h *ManualHandler) finish(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "testRunId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var req finishRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	run, err := h.api.Finish(r.Context(), id, req.Status)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, execution.RunDTO(run))
}
