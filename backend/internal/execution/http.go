package execution

import (
	"context"
	"net/http"
	"regexp"
	"time"

	"github.com/edcrove/provenly/backend/internal/platform/httpx"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

// API is the set of execution use cases exposed over REST.
type API interface {
	GetRun(ctx context.Context, id int64) (TestRun, error)
	ListRuns(ctx context.Context, projectID *int64, page pagination.Page) (pagination.Result[TestRun], error)
	ListRunResults(ctx context.Context, runID int64, f ResultFilter, page pagination.Page) (pagination.Result[TestResult], error)
	Summary(ctx context.Context, runID int64) (Summary, error)
	ListParseErrors(ctx context.Context, runID int64, page pagination.Page) (pagination.Result[ParseError], error)
	History(ctx context.Context, testCaseID int64, page pagination.Page) (pagination.Result[HistoryEntry], error)
}

// TestCaseChecker is what the execution REST adapter needs from the catalog
// module's public interface: 404s for unknown test cases, the ?project=<KEY> filter
// and the display keys (<KEY>-<n>) of the test cases a response mentions.
type TestCaseChecker interface {
	EnsureExists(ctx context.Context, id int64) error
	ProjectIDByKey(ctx context.Context, key string) (int64, error)
	Keys(ctx context.Context, ids []int64) (map[int64]string, error)
}

// ProjectKeyPattern mirrors the catalog's project key format (validated before any lookup).
var ProjectKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

// TestRunDTO is the wire form of TestRun.
type TestRunDTO struct {
	ID            int64      `json:"id"`
	ProjectID     int64      `json:"projectId"`
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
		ID: r.ID, ProjectID: r.ProjectID, ExternalRunID: r.ExternalRunID, Provider: r.Provider, ProviderRunID: r.ProviderRunID,
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
	TestCaseKey         *string      `json:"testCaseKey"`
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

func resultDTO(r TestResult, keys map[int64]string) TestResultDTO {
	var key *string
	if r.TestCaseID != nil {
		if k, ok := keys[*r.TestCaseID]; ok {
			key = &k
		}
	}
	return TestResultDTO{
		ID: r.ID, TestRunID: r.TestRunID, TestCaseID: r.TestCaseID, TestCaseKey: key, RequestedTestCaseID: r.RequestedTestCaseID,
		Correlation: r.Correlation, TestName: r.TestName, ClassName: r.ClassName, SuiteName: r.SuiteName, Status: r.Status,
		DurationMs: r.DurationMs, ErrorMessage: r.ErrorMessage, ErrorDetails: r.ErrorDetails, CreatedAt: r.CreatedAt,
	}
}

// resultKeys resolves the display keys of the test cases the results link to.
func (h *Handler) resultKeys(ctx context.Context, results []TestResult) (map[int64]string, error) {
	var ids []int64
	for _, r := range results {
		if r.TestCaseID != nil {
			ids = append(ids, *r.TestCaseID)
		}
	}
	return h.catalog.Keys(ctx, ids)
}

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

func historyEntryDTO(h HistoryEntry, keys map[int64]string) historyDTO {
	return historyDTO{Result: resultDTO(h.Result, keys), Run: RunDTO(h.Run)}
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
	Missing      int32 `json:"missing"`
	Malformed    int32 `json:"malformed"`
	Unknown      int32 `json:"unknown"`
	Deprecated   int32 `json:"deprecated"`
	WrongProject int32 `json:"wrongProject"`
	Total        int32 `json:"total"`
}

type testCaseOutcomeDTO struct {
	TestCaseID  int64         `json:"testCaseId"`
	TestCaseKey string        `json:"testCaseKey"`
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

func toSummaryDTO(s Summary, keys map[int64]string) summaryDTO {
	cases := make([]testCaseOutcomeDTO, len(s.TestCases))
	for i, c := range s.TestCases {
		cases[i] = testCaseOutcomeDTO{TestCaseID: c.TestCaseID, TestCaseKey: keys[c.TestCaseID], Status: c.Status, ResultCount: c.ResultCount}
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
	var projectID *int64
	key, err := httpx.PatternQuery(r, "project", ProjectKeyPattern, "must be a project key: 2 to 10 upper-case letters or digits, starting with a letter")
	if err == nil && key != nil {
		var id int64
		id, err = h.catalog.ProjectIDByKey(r.Context(), *key)
		projectID = &id
	}
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.api.ListRuns(r.Context(), projectID, page)
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
	keys, err := h.resultKeys(r.Context(), res.Items)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(res, func(t TestResult) TestResultDTO { return resultDTO(t, keys) }))
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
	ids := make([]int64, len(s.TestCases))
	for i, c := range s.TestCases {
		ids[i] = c.TestCaseID
	}
	keys, err := h.catalog.Keys(r.Context(), ids)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toSummaryDTO(s, keys))
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
	keys, err := h.catalog.Keys(r.Context(), []int64{id})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(res, func(e HistoryEntry) historyDTO { return historyEntryDTO(e, keys) }))
}
