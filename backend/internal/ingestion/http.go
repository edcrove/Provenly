package ingestion

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/httpx"
)

// API is the set of ingestion use cases exposed over REST.
type API interface {
	IngestJUnit(ctx context.Context, meta RunMeta, body io.Reader) (Outcome, error)
}

type diagnosticDTO struct {
	TestName            string                `json:"testName"`
	Correlation         execution.Correlation `json:"correlation"`
	RequestedTestCaseID *string               `json:"requestedTestCaseId"`
	Message             string                `json:"message"`
}

type ingestionResponse struct {
	Created     bool                      `json:"created"`
	TestRun     execution.TestRunDTO      `json:"testRun"`
	Received    int                       `json:"received"`
	Persisted   int                       `json:"persisted"`
	Diagnostics []diagnosticDTO           `json:"diagnostics"`
	ParseErrors []execution.ParseErrorDTO `json:"parseErrors"`
	Warnings    []string                  `json:"warnings"`
}

// Handler is the REST adapter of the ingestion module.
type Handler struct {
	api      API
	maxBytes int64
}

// NewHandler builds a Handler accepting reports up to maxBytes.
func NewHandler(api API, maxBytes int64) *Handler { return &Handler{api: api, maxBytes: maxBytes} }

// Register mounts the ingestion routes.
func (h *Handler) Register(mux httpx.Router) {
	mux.HandleFunc("POST /api/v1/ingestion/junit", h.ingestJUnit)
}

func isXML(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && (mt == "application/xml" || mt == "text/xml")
}

func (h *Handler) ingestJUnit(w http.ResponseWriter, r *http.Request) {
	if !isXML(r.Header.Get("Content-Type")) {
		httpx.WriteProblem(w, http.StatusUnsupportedMediaType, httpx.CodeUnsupportedMediaType, "Content-Type must be application/xml")
		return
	}
	q := r.URL.Query()
	meta := RunMeta{
		Provider: q.Get("provider"), ProviderRunID: q.Get("runId"),
		Pipeline: q.Get("pipeline"), Branch: q.Get("branch"), Commit: q.Get("commit"),
		Status: execution.RunStatus(q.Get("status")),
	}
	if q.Has("status") && meta.Status == "" {
		httpx.WriteError(w, r, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "status", Message: "must be one of completed, interrupted, cancelled"}))
		return
	}
	attempt, err := strconv.ParseInt(q.Get("runAttempt"), 10, 32)
	if err != nil {
		httpx.WriteError(w, r, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "runAttempt", Message: "must be an integer >= 1"}))
		return
	}
	meta.RunAttempt = int32(attempt)
	out, err := h.api.IngestJUnit(r.Context(), meta, http.MaxBytesReader(w, r.Body, h.maxBytes))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	resp := ingestionResponse{
		Created: out.Created, TestRun: execution.RunDTO(out.Run), Received: out.Received, Persisted: out.Persisted,
		Diagnostics: make([]diagnosticDTO, len(out.Diagnostics)), ParseErrors: make([]execution.ParseErrorDTO, len(out.ParseErrors)),
		Warnings: out.Warnings,
	}
	for i, d := range out.Diagnostics {
		resp.Diagnostics[i] = diagnosticDTO(d)
	}
	for i, e := range out.ParseErrors {
		resp.ParseErrors[i] = execution.ToParseErrorDTO(e)
	}
	status := http.StatusOK
	if out.Created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, resp)
}
