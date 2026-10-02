package execution

import (
	"context"
	"net/http"
	"time"

	"github.com/edcrove/provenly/backend/internal/platform/httpx"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

// API is the set of execution use cases exposed over REST.
type API interface {
	GetRun(ctx context.Context, id int64) (TestRun, error)
	ListRuns(ctx context.Context, page pagination.Page) (pagination.Result[TestRun], error)
	ListRunResults(ctx context.Context, runID int64, f ResultFilter, page pagination.Page) (pagination.Result[TestResult], error)
	Summary(ctx context.Context, runID int64) (Summary, error)
	ListParseErrors(ctx context.Context, runID int64, page pagination.Page) (pagination.Result[ParseError], error)
	History(ctx context.Context, testCaseID int64, page pagination.Page) (pagination.Result[HistoryEntry], error)
}

// TestCaseChecker lets the history endpoint answer 404 for unknown TC-IDs
// through the catalog module's public interface.
type TestCaseChecker interface {
	EnsureExists(ctx context.Context, id int64) error
}

// TestRunDTO is the wire form of TestRun.
type TestRunDTO struct {
	ID            int64      `json:"id"`
	ExternalRunID string     `json:"externalRunId"`
	Provider      string     `json:"provider"`
	ProviderRunID string     `json:"providerRunId"`
	RunAttempt    int32      `json:"runAttempt"`
	Pipeline      string     `json:"pipeline"`
	Branch        string     `json:"branch"`
	Commit        string     `json:"commit"`
	Status        RunStatus  `json:"executionStatus"`
	Outcome       outcomeDTO `json:"outcome"`
	ExpectedCount int32      `json:"expectedCount"`
	ResultCount   int32      `json:"resultCount"`
	CreatedAt     time.Time  `json:"createdAt"`
	StartedAt     *time.Time `json:"startedAt"`
	CompletedAt   *time.Time `json:"completedAt"`
}

type outcomeDTO struct {
	Verdict  Verdict `json:"verdict"`
	Executed int32   `json:"executed"`
	Passed   int32   `json:"passed"`
	Failed   int32   `json:"failed"`
	Error    int32   `json:"error"`
	Skipped  int32   `json:"skipped"`
	Untested int32   `json:"untested"`
	PassRate float64 `json:"passRate"`
}

// RunDTO converts a TestRun to its wire form.
func RunDTO(r TestRun) TestRunDTO {
	return TestRunDTO{
		ID: r.ID, ExternalRunID: r.ExternalRunID, Provider: r.Provider, ProviderRunID: r.ProviderRunID,
		RunAttempt: r.RunAttempt, Pipeline: r.Pipeline, Branch: r.Branch, Commit: r.Commit, Status: r.Status,
		Outcome:       outcomeDTO(r.Outcome),
		ExpectedCount: r.ExpectedCount, ResultCount: r.ResultCount, CreatedAt: r.CreatedAt,
		StartedAt: r.StartedAt, CompletedAt: r.CompletedAt,
	}
}

// TestResultDTO is the wire form of TestResult.
type TestResultDTO struct {
	ID                  int64        `json:"id"`
	TestRunID           int64        `json:"testRunId"`
	TestCaseID          *int64       `json:"testCaseId"`
	RequestedTestCaseID *string      `json:"requestedTestCaseId"`
	Correlation         Correlation  `json:"correlation"`
	TestName            string       `json:"testName"`
	ClassName           string       `json:"className"`
	SuiteName           string       `json:"suiteName"`
	Status              ResultStatus `json:"status"`
	DurationMs          *int64       `json:"durationMs"`
	ErrorMessage        string       `json:"errorMessage"`
	ErrorDetails        string       `json:"errorDetails"`
	CreatedAt           time.Time    `json:"createdAt"`
}

func resultDTO(r TestResult) TestResultDTO { return TestResultDTO(r) }

// ParseErrorDTO is the wire form of ParseError.
type ParseErrorDTO struct {
	Index     int32  `json:"index"`
	TestName  string `json:"testName"`
	Message   string `json:"message"`
	Persisted bool   `json:"persisted"`
	Severity  string `json:"severity"`
}

// ToParseErrorDTO converts a ParseError to its wire form.
func ToParseErrorDTO(p ParseError) ParseErrorDTO { return ParseErrorDTO(p) }

type historyDTO struct {
	Result TestResultDTO `json:"result"`
	Run    TestRunDTO    `json:"run"`
}

func historyEntryDTO(h HistoryEntry) historyDTO {
	return historyDTO{Result: resultDTO(h.Result), Run: RunDTO(h.Run)}
}

type statusCountsDTO struct {
	Untested int32 `json:"untested"`
	Passed   int32 `json:"passed"`
	Failed   int32 `json:"failed"`
	Error    int32 `json:"error"`
	Skipped  int32 `json:"skipped"`
}

type statusPercentagesDTO struct {
	Untested float64 `json:"untested"`
	Passed   float64 `json:"passed"`
	Failed   float64 `json:"failed"`
	Error    float64 `json:"error"`
	Skipped  float64 `json:"skipped"`
}

type executedPercentagesDTO struct {
	Passed  float64 `json:"passed"`
	Failed  float64 `json:"failed"`
	Error   float64 `json:"error"`
	Skipped float64 `json:"skipped"`
}

type diagnosticCountsDTO struct {
	Missing    int32 `json:"missing"`
	Malformed  int32 `json:"malformed"`
	Unknown    int32 `json:"unknown"`
	Deprecated int32 `json:"deprecated"`
	Total      int32 `json:"total"`
}

type testCaseOutcomeDTO struct {
	TestCaseID  int64         `json:"testCaseId"`
	Status      SummaryStatus `json:"status"`
	ResultCount int32         `json:"resultCount"`
}

type summaryDTO struct {
	TestRunID          int64                  `json:"testRunId"`
	ExpectedTotal      int32                  `json:"expectedTotal"`
	ExecutedTotal      int32                  `json:"executedTotal"`
	Counts             statusCountsDTO        `json:"counts"`
	PercentOfExpected  statusPercentagesDTO   `json:"percentOfExpected"`
	PercentOfExecuted  executedPercentagesDTO `json:"percentOfExecuted"`
	ExecutionPercent   float64                `json:"executionPercent"`
	Diagnostics        diagnosticCountsDTO    `json:"diagnostics"`
	OutsideUniverse    int32                  `json:"outsideUniverse"`
	OutsideUniverseIDs []int64                `json:"outsideUniverseTestCaseIds"`
	TestCases          []testCaseOutcomeDTO   `json:"testCases"`
}

func toSummaryDTO(s Summary) summaryDTO {
	cases := make([]testCaseOutcomeDTO, len(s.TestCases))
	for i, c := range s.TestCases {
		cases[i] = testCaseOutcomeDTO(c)
	}
	return summaryDTO{
		TestRunID: s.TestRunID, ExpectedTotal: s.ExpectedTotal, ExecutedTotal: s.ExecutedTotal,
		Counts:             statusCountsDTO(s.Counts),
		PercentOfExpected:  statusPercentagesDTO(s.PercentOfExpected),
		PercentOfExecuted:  executedPercentagesDTO(s.PercentOfExecuted),
		ExecutionPercent:   s.ExecutionPercent,
		Diagnostics:        diagnosticCountsDTO(s.Diagnostics),
		OutsideUniverse:    s.OutsideUniverse,
		OutsideUniverseIDs: s.OutsideUniverseIDs,
		TestCases:          cases,
	}
}

// Handler is the REST adapter of the execution module.
type Handler struct {
	api     API
	catalog TestCaseChecker
}

// NewHandler builds a Handler.
func NewHandler(api API, catalog TestCaseChecker) *Handler {
	return &Handler{api: api, catalog: catalog}
}

// Register mounts the execution routes.
func (h *Handler) Register(mux httpx.Router) {
	mux.HandleFunc("GET /api/v1/test-runs", h.listRuns)
	mux.HandleFunc("GET /api/v1/test-runs/{testRunId}", h.getRun)
	mux.HandleFunc("GET /api/v1/test-runs/{testRunId}/results", h.listResults)
	mux.HandleFunc("GET /api/v1/test-runs/{testRunId}/summary", h.summary)
	mux.HandleFunc("GET /api/v1/test-runs/{testRunId}/parse-errors", h.parseErrors)
	mux.HandleFunc("GET /api/v1/test-cases/{testCaseId}/results", h.history)
}

func (h *Handler) listRuns(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.api.ListRuns(r.Context(), page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(res, RunDTO))
}

func (h *Handler) getRun(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "testRunId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	run, err := h.api.GetRun(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, RunDTO(run))
}

func enumStrings[T ~string](values []T) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}

func parseResultFilter(r *http.Request) (ResultFilter, error) {
	var f ResultFilter
	status, err := httpx.EnumQuery(r, "status", enumStrings(ResultStatuses)...)
	if err != nil {
		return f, err
	}
	if status != nil {
		s := ResultStatus(*status)
		f.Status = &s
	}
	corr, err := httpx.EnumQuery(r, "correlation", enumStrings(Correlations)...)
	if err != nil {
		return f, err
	}
	if corr != nil {
		c := Correlation(*corr)
		f.Correlation = &c
	}
	return f, nil
}

func (h *Handler) listResults(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "testRunId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f, err := parseResultFilter(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.api.ListRunResults(r.Context(), id, f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(res, resultDTO))
}

func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "testRunId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	s, err := h.api.Summary(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toSummaryDTO(s))
}

func (h *Handler) parseErrors(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "testRunId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.api.ListParseErrors(r.Context(), id, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(res, ToParseErrorDTO))
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "testCaseId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.catalog.EnsureExists(r.Context(), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.api.History(r.Context(), id, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(res, historyEntryDTO))
}
