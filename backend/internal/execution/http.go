package execution

import (
	"cmp"
	"context"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/httpx"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

// API is the set of execution use cases exposed over REST.
type API interface {
	GetRun(ctx context.Context, id int64) (TestRun, error)
	ListRuns(ctx context.Context, f RunFilter, page pagination.Page) (pagination.Result[TestRun], error)
	ListRunResults(ctx context.Context, runID int64, f ResultFilter, page pagination.Page) (pagination.Result[TestResult], error)
	Summary(ctx context.Context, runID int64) (Summary, error)
	ListParseErrors(ctx context.Context, runID int64, page pagination.Page) (pagination.Result[ParseError], error)
	History(ctx context.Context, testCaseID int64, page pagination.Page) (pagination.Result[HistoryEntry], error)
	Amend(ctx context.Context, runID, testCaseID int64, reason string, by authz.Actor) (Amendment, error)
	ListAmendments(ctx context.Context, runID int64, page pagination.Page) (pagination.Result[Amendment], error)
	Live(ctx context.Context, runID int64) (Live, error)
}

// TestCaseChecker is what the execution REST adapter needs from the catalog
// module's public interface: the project of a test case, the ?project=<KEY> filter
// and the display keys (<KEY>-<n>) of the test cases a response mentions.
type TestCaseChecker interface {
	// ProjectOf returns the project of a test case (a not-found error for unknown ids).
	ProjectOf(ctx context.Context, id int64) (int64, error)
	ProjectIDByKey(ctx context.Context, key string) (int64, error)
	Keys(ctx context.Context, ids []int64) (map[int64]string, error)
}

// ProjectKeyPattern mirrors the catalog's project key format (validated before any lookup).
var ProjectKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

var suiteKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,29}$`)

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
	// AmendmentCount > 0 means the run's universe was edited after its creation (DEC-42).
	AmendmentCount int32      `json:"amendmentCount"`
	CreatedAt      time.Time  `json:"createdAt"`
	StartedAt      *time.Time `json:"startedAt"`
	CompletedAt    *time.Time `json:"completedAt"`
	// Suite is the suite the run was reported for (null: the project's automated catalog).
	Suite *SuiteRefDTO `json:"suite"`
	// Mode is how the results arrive (batch, manual, live); StartedBy who started a manual run.
	Mode      RunMode `json:"mode"`
	StartedBy *string `json:"startedBy"`
	// Shards tells how far a sharded run got (null: not sharded).
	Shards *ShardsDTO `json:"shards"`
}

// ShardsDTO is the progress of a sharded run: of Total reports, the shards Received and Missing (ascending).
type ShardsDTO struct {
	Total    int32   `json:"total"`
	Received []int32 `json:"received"`
	Missing  []int32 `json:"missing"`
}

// SuiteRefDTO names a suite as it was when the run was created.
type SuiteRefDTO struct {
	Key  string `json:"key"`
	Name string `json:"name"`
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
	Flaky    int32   `json:"flaky"`
}

// RunDTO converts a TestRun to its wire form.
func RunDTO(r TestRun) TestRunDTO {
	dto := TestRunDTO{
		ID: r.ID, ProjectID: r.ProjectID, ExternalRunID: r.ExternalRunID, Provider: r.Provider, ProviderRunID: r.ProviderRunID,
		RunAttempt: r.RunAttempt, Pipeline: r.Pipeline, Branch: r.Branch, Commit: r.Commit, Status: r.Status,
		Outcome:       outcomeDTO(r.Outcome),
		ExpectedCount: r.ExpectedCount, ResultCount: r.ResultCount, AmendmentCount: r.AmendmentCount, CreatedAt: r.CreatedAt,
		StartedAt: r.StartedAt, CompletedAt: r.CompletedAt,
	}
	if r.SuiteKey != "" {
		dto.Suite = &SuiteRefDTO{Key: r.SuiteKey, Name: r.SuiteName}
	}
	dto.Mode = cmp.Or(r.Mode, ModeBatch)
	if r.StartedBy != "" {
		dto.StartedBy = &r.StartedBy
	}
	if r.ShardTotal > 0 {
		dto.Shards = &ShardsDTO{Total: r.ShardTotal, Received: append([]int32{}, r.ShardsReceived...), Missing: append([]int32{}, r.MissingShards()...)}
	}
	return dto
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
	Attempt             int32        `json:"attempt"`
	Retried             bool         `json:"retried"`
	// RecordedBy is who recorded a manual result (null for CI results); FailedStep the step where it failed.
	RecordedBy *string `json:"recordedBy"`
	FailedStep *int32  `json:"failedStep"`
	// Shard is the shard of a sharded run that reported the result (null otherwise).
	Shard *int32 `json:"shard"`
}

// ResultDTO converts a TestResult to its wire form, with the display key of its test case when known.
func ResultDTO(r TestResult, keys map[int64]string) TestResultDTO { return resultDTO(r, keys) }

func resultDTO(r TestResult, keys map[int64]string) TestResultDTO {
	var key *string
	if r.TestCaseID != nil {
		if k, ok := keys[*r.TestCaseID]; ok {
			key = &k
		}
	}
	dto := TestResultDTO{
		ID: r.ID, TestRunID: r.TestRunID, TestCaseID: r.TestCaseID, TestCaseKey: key, RequestedTestCaseID: r.RequestedTestCaseID,
		Correlation: r.Correlation, TestName: r.TestName, ClassName: r.ClassName, SuiteName: r.SuiteName, Status: r.Status,
		DurationMs: r.DurationMs, ErrorMessage: r.ErrorMessage, ErrorDetails: r.ErrorDetails, CreatedAt: r.CreatedAt,
		Attempt: r.Attempt, Retried: r.Retried, FailedStep: r.FailedStep, Shard: r.Shard,
	}
	if r.RecordedBy != "" {
		dto.RecordedBy = &r.RecordedBy
	}
	return dto
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
	// Shard is the shard of a sharded run whose report it belongs to (null otherwise).
	Shard *int32 `json:"shard"`
}

// ToParseErrorDTO converts a ParseError to its wire form.
func ToParseErrorDTO(p ParseError) ParseErrorDTO {
	dto := ParseErrorDTO{Index: p.Index, TestName: p.TestName, Message: p.Message, Persisted: p.Persisted, Severity: p.Severity}
	if p.Shard != 0 {
		dto.Shard = &p.Shard
	}
	return dto
}

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
	Flaky       bool          `json:"flaky"`
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
	Flaky              int32                  `json:"flaky"`
	OutsideUniverseIDs []int64                `json:"outsideUniverseTestCaseIds"`
	TestCases          []testCaseOutcomeDTO   `json:"testCases"`
	SnapshotTotal      int32                  `json:"snapshotTotal"`
	AmendedIDs         []int64                `json:"amendedTestCaseIds"`
}

func toSummaryDTO(s Summary, keys map[int64]string) summaryDTO {
	cases := make([]testCaseOutcomeDTO, len(s.TestCases))
	for i, c := range s.TestCases {
		cases[i] = testCaseOutcomeDTO{TestCaseID: c.TestCaseID, TestCaseKey: keys[c.TestCaseID], Status: c.Status, ResultCount: c.ResultCount, Flaky: c.Flaky}
	}
	return summaryDTO{
		TestRunID: s.TestRunID, ExpectedTotal: s.ExpectedTotal, ExecutedTotal: s.ExecutedTotal,
		Counts:             statusCountsDTO(s.Counts),
		PercentOfExpected:  statusPercentagesDTO(s.PercentOfExpected),
		PercentOfExecuted:  executedPercentagesDTO(s.PercentOfExecuted),
		ExecutionPercent:   s.ExecutionPercent,
		Diagnostics:        diagnosticCountsDTO(s.Diagnostics),
		OutsideUniverse:    s.OutsideUniverse,
		Flaky:              s.Flaky,
		OutsideUniverseIDs: s.OutsideUniverseIDs,
		TestCases:          cases,
		SnapshotTotal:      s.SnapshotTotal,
		AmendedIDs:         s.AmendedIDs,
	}
}

type amendmentDTO struct {
	ID                int64     `json:"id"`
	TestRunID         int64     `json:"testRunId"`
	TestCaseID        int64     `json:"testCaseId"`
	TestCaseKey       string    `json:"testCaseKey"`
	AmendedBy         int64     `json:"amendedBy"`
	AmendedByUsername string    `json:"amendedByUsername"`
	Reason            string    `json:"reason"`
	CreatedAt         time.Time `json:"createdAt"`
}

func toAmendmentDTO(a Amendment, keys map[int64]string) amendmentDTO {
	return amendmentDTO{
		ID: a.ID, TestRunID: a.TestRunID, TestCaseID: a.TestCaseID, TestCaseKey: keys[a.TestCaseID], AmendedBy: a.AmendedBy,
		AmendedByUsername: a.AmendedByUsername, Reason: a.Reason, CreatedAt: a.CreatedAt,
	}
}

type amendRequest struct {
	TestCaseID int64  `json:"testCaseId"`
	Reason     string `json:"reason"`
}

// Handler is the REST adapter of the execution module.
type Handler struct {
	api     API
	catalog TestCaseChecker
	guard   authz.Guard
}

// NewHandler builds a Handler.
func NewHandler(api API, catalog TestCaseChecker, guard authz.Guard) *Handler {
	return &Handler{api: api, catalog: catalog, guard: guard}
}

type runKey struct{}

// onRun authorizes routes on /test-runs/{testRunId}: the run's project must be visible to the user (else 404)
// and give at least minRole (else 403). A malformed id is left to next.
func (h *Handler) onRun(minRole authz.Role, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if id, err := httpx.PathID(r, "testRunId"); err == nil {
			run, err := h.api.GetRun(r.Context(), id)
			if err == nil {
				err = h.guard.Require(r.Context(), run.ProjectID, minRole, runNotFound(id))
			}
			if err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), runKey{}, run))
		}
		next(w, r)
	}
}

// Register mounts the execution routes.
func (h *Handler) Register(mux httpx.Router) {
	mux.HandleFunc("GET /api/v1/test-runs", h.listRuns)
	mux.HandleFunc("GET /api/v1/test-runs/{testRunId}", h.onRun(authz.RoleViewer, h.getRun))
	mux.HandleFunc("GET /api/v1/test-runs/{testRunId}/results", h.onRun(authz.RoleViewer, h.listResults))
	mux.HandleFunc("GET /api/v1/test-runs/{testRunId}/summary", h.onRun(authz.RoleViewer, h.summary))
	mux.HandleFunc("GET /api/v1/test-runs/{testRunId}/parse-errors", h.onRun(authz.RoleViewer, h.parseErrors))
	mux.HandleFunc("GET /api/v1/test-runs/{testRunId}/live", h.onRun(authz.RoleViewer, h.live))
	mux.HandleFunc("GET /api/v1/test-runs/{testRunId}/amendments", h.onRun(authz.RoleViewer, h.listAmendments))
	mux.HandleFunc("POST /api/v1/test-runs/{testRunId}/amendments", h.onRun(authz.RoleMaintainer, h.amend))
	mux.HandleFunc("GET /api/v1/test-cases/{testCaseId}/results", h.history)
}

// visibleProjects narrows the run list to ?project=<KEY> (which the user must see) or to every project the
// user can see (nil: every project, for administrators).
func (h *Handler) visibleProjects(r *http.Request) ([]int64, error) {
	key, err := httpx.PatternQuery(r, "project", ProjectKeyPattern, "must be a project key: 2 to 10 upper-case letters or digits, starting with a letter")
	if err != nil {
		return nil, err
	}
	if key != nil {
		id, err := h.catalog.ProjectIDByKey(r.Context(), *key)
		if err == nil {
			err = h.guard.Require(r.Context(), id, authz.RoleViewer, apperr.NotFound("project %s not found", *key))
		}
		return []int64{id}, err
	}
	scope, err := h.guard.Scope(r.Context())
	return scope.ProjectIDs(), err
}

func (h *Handler) listRuns(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f RunFilter
	if f.SuiteKey, err = httpx.PatternQuery(r, "suite", suiteKeyPattern, "must be a suite key: 1 to 30 lower-case letters, digits or '-', starting with a letter"); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if f.ProjectIDs, err = h.visibleProjects(r); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.api.ListRuns(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(res, RunDTO))
}

func (h *Handler) getRun(w http.ResponseWriter, r *http.Request) {
	if _, err := httpx.PathID(r, "testRunId"); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	// Loaded and authorized by onRun.
	httpx.WriteJSON(w, http.StatusOK, RunDTO(r.Context().Value(runKey{}).(TestRun)))
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
	shard, err := httpx.PatternQuery(r, "shard", shardPattern, "must be an integer between 1 and 100")
	if err != nil {
		return f, err
	}
	if shard != nil {
		n, _ := strconv.Atoi(*shard)
		v := int32(n)
		f.Shard = &v
	}
	return f, nil
}

// shardPattern is a shard number, 1 to 100.
var shardPattern = regexp.MustCompile(`^([1-9]|[1-9][0-9]|100)$`)

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
	projectID, err := h.catalog.ProjectOf(r.Context(), id)
	if err == nil {
		err = h.guard.Require(r.Context(), projectID, authz.RoleViewer, apperr.NotFound("test case %d not found", id))
	}
	if err != nil {
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

// amendmentKeys resolves the display keys of the amended test cases.
func (h *Handler) amendmentKeys(ctx context.Context, items []Amendment) (map[int64]string, error) {
	ids := make([]int64, len(items))
	for i, a := range items {
		ids[i] = a.TestCaseID
	}
	return h.catalog.Keys(ctx, ids)
}

func (h *Handler) listAmendments(w http.ResponseWriter, r *http.Request) {
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
	res, err := h.api.ListAmendments(r.Context(), id, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	keys, err := h.amendmentKeys(r.Context(), res.Items)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(res, func(a Amendment) amendmentDTO { return toAmendmentDTO(a, keys) }))
}

func (h *Handler) amend(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "testRunId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var req amendRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	by, err := h.guard.Actor(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	a, err := h.api.Amend(r.Context(), id, req.TestCaseID, req.Reason, by)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	keys, err := h.amendmentKeys(r.Context(), []Amendment{a})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toAmendmentDTO(a, keys))
}
