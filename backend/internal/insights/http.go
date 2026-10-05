package insights

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/httpx"
)

// API is the insights use cases exposed over REST.
type API interface {
	Quality(ctx context.Context, q Query) (Quality, error)
}

// Handler is the REST adapter of insights (session routes).
type Handler struct{ api API }

// NewHandler builds a Handler.
func NewHandler(api API) *Handler { return &Handler{api: api} }

// Register mounts the insights routes.
func (h *Handler) Register(mux httpx.Router) {
	mux.HandleFunc("GET /api/v1/projects/{projectKey}/quality", h.quality)
}

// StaleCaseDTO is the wire form of StaleCase.
type StaleCaseDTO struct {
	TestCaseID     int64      `json:"testCaseId"`
	TestCaseKey    string     `json:"testCaseKey"`
	LastExecutedAt *time.Time `json:"lastExecutedAt"`
}

// FlakyCaseDTO is the wire form of FlakyCase.
type FlakyCaseDTO struct {
	TestCaseID  int64  `json:"testCaseId"`
	TestCaseKey string `json:"testCaseKey"`
	Runs        int32  `json:"runs"`
}

// QualityDTO is the wire form of Quality.
type QualityDTO struct {
	TestCases struct {
		Active         int32   `json:"active"`
		Automated      int32   `json:"automated"`
		Manual         int32   `json:"manual"`
		AutomationRate float64 `json:"automationRate"`
	} `json:"testCases"`
	Execution struct {
		StaleDays     int32          `json:"staleDays"`
		NeverExecuted int32          `json:"neverExecuted"`
		Stale         int32          `json:"stale"`
		TestCases     []StaleCaseDTO `json:"testCases"`
	} `json:"execution"`
	Flaky struct {
		Window    int32          `json:"window"`
		TestCases []FlakyCaseDTO `json:"testCases"`
	} `json:"flaky"`
}

func qualityDTO(q Quality) QualityDTO {
	var dto QualityDTO
	dto.TestCases.Active, dto.TestCases.Automated, dto.TestCases.Manual, dto.TestCases.AutomationRate = q.Active, q.Automated, q.Manual, q.AutomationRate
	dto.Execution.StaleDays, dto.Execution.NeverExecuted, dto.Execution.Stale = q.StaleDays, q.NeverExecuted, q.Stale
	dto.Execution.TestCases = make([]StaleCaseDTO, len(q.StaleCases))
	for i, c := range q.StaleCases {
		dto.Execution.TestCases[i] = StaleCaseDTO{TestCaseID: c.TestCaseID, TestCaseKey: c.Key, LastExecutedAt: c.LastExecutedAt}
	}
	dto.Flaky.Window = q.Window
	dto.Flaky.TestCases = make([]FlakyCaseDTO, len(q.Flaky))
	for i, f := range q.Flaky {
		dto.Flaky.TestCases[i] = FlakyCaseDTO{TestCaseID: f.TestCaseID, TestCaseKey: f.Key, Runs: f.Runs}
	}
	return dto
}

// intParam reads an optional integer query parameter (present but not an integer is a 400).
func intParam(r *http.Request, name string) (int32, error) {
	q := r.URL.Query()
	if !q.Has(name) {
		return 0, nil
	}
	n, err := strconv.ParseInt(q.Get(name), 10, 32)
	if err != nil || n < 1 {
		return 0, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: name, Message: "must be a positive integer"})
	}
	return int32(n), nil
}

func (h *Handler) quality(w http.ResponseWriter, r *http.Request) {
	staleDays, err := intParam(r, "staleDays")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	window, err := intParam(r, "window")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	q, err := h.api.Quality(r.Context(), Query{ProjectKey: r.PathValue("projectKey"), StaleDays: staleDays, Window: window})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, qualityDTO(q))
}
