package ingestion

import (
	"context"
	"net/http"
	"time"

	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/platform/httpx"
)

// LiveAPI is the live run use cases exposed over REST.
type LiveAPI interface {
	Start(ctx context.Context, meta RunMeta) (execution.TestRun, error)
	RecordEvents(ctx context.Context, runID int64, events []EventInput) (execution.EventsResult, error)
}

// LiveHandler is the REST adapter of live runs (API keys or sessions, like reports).
type LiveHandler struct{ api LiveAPI }

// NewLiveHandler builds a LiveHandler.
func NewLiveHandler(api LiveAPI) *LiveHandler { return &LiveHandler{api: api} }

// Register mounts the live run routes.
func (h *LiveHandler) Register(mux httpx.Router) {
	mux.HandleFunc("POST /api/v1/test-runs/live", h.start)
	mux.HandleFunc("POST /api/v1/test-runs/{testRunId}/events", h.events)
}

type liveStartRequest struct {
	Project    string `json:"project"`
	Suite      string `json:"suite"`
	Provider   string `json:"provider"`
	RunID      string `json:"runId"`
	RunAttempt int32  `json:"runAttempt"`
	Pipeline   string `json:"pipeline"`
	Branch     string `json:"branch"`
	Commit     string `json:"commit"`
}

type eventRequest struct {
	EventID    string                 `json:"eventId"`
	Sequence   int64                  `json:"sequence"`
	Type       execution.EventType    `json:"type"`
	TestName   string                 `json:"testName"`
	TestCase   string                 `json:"testCase"`
	Status     execution.ResultStatus `json:"status"`
	OccurredAt *time.Time             `json:"occurredAt"`
}

type eventsRequest struct {
	Events []eventRequest `json:"events"`
}

type eventsResponse struct {
	Accepted   int `json:"accepted"`
	Duplicates int `json:"duplicates"`
}

func (h *LiveHandler) start(w http.ResponseWriter, r *http.Request) {
	var req liveStartRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	run, err := h.api.Start(r.Context(), RunMeta{
		ProjectKey: req.Project, SuiteKey: req.Suite, Provider: req.Provider, ProviderRunID: req.RunID, RunAttempt: req.RunAttempt,
		Pipeline: req.Pipeline, Branch: req.Branch, Commit: req.Commit,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, execution.RunDTO(run))
}

func (h *LiveHandler) events(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "testRunId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var req eventsRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	events := make([]EventInput, len(req.Events))
	for i, e := range req.Events {
		events[i] = EventInput(e)
	}
	res, err := h.api.RecordEvents(r.Context(), id, events)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, eventsResponse(res))
}
