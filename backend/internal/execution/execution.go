// Package execution is the TestRun/Execution module: idempotent test runs with
// their immutable expected-universe snapshot, persisted results, the run
// summary and the per-TC-ID result history. TC-IDs are held by value; the
// module never reads catalog tables.
package execution

import (
	"context"
	"errors"
	"strconv"
	"time"
)

// RunStatus is how the CI execution of a run ended, as reported by CI. It
// says nothing about test outcomes: see RunOutcome.Verdict.
type RunStatus string

// Execution statuses. The POC creates runs synchronously on ingestion of the
// final report: completed by default, or interrupted/cancelled when CI reports
// that the pipeline broke or was stopped (the report may be incomplete).
const (
	RunCompleted   RunStatus = "completed"
	RunInterrupted RunStatus = "interrupted"
	RunCancelled   RunStatus = "cancelled"
)

// ResultStatus is the observed outcome of one result. "untested" is never a
// persisted result: it is derived in the summary.
type ResultStatus string

// Result statuses.
const (
	Passed  ResultStatus = "passed"
	Failed  ResultStatus = "failed"
	Error   ResultStatus = "error"
	Skipped ResultStatus = "skipped"
)

// ResultStatuses lists every persisted result status.
var ResultStatuses = []ResultStatus{Passed, Failed, Error, Skipped}

// Correlation is the outcome of TC-ID extraction/validation for a result.
type Correlation string

// Correlations.
const (
	CorrelationValid      Correlation = "valid"
	CorrelationMissing    Correlation = "missing"
	CorrelationMalformed  Correlation = "malformed"
	CorrelationUnknown    Correlation = "unknown"
	CorrelationDeprecated Correlation = "deprecated"
	// CorrelationWrongProject is a reference with another project's key (e.g. WEB-12 in a CHK run).
	CorrelationWrongProject Correlation = "wrong_project"
)

// Correlations lists every correlation.
var Correlations = []Correlation{CorrelationValid, CorrelationMissing, CorrelationMalformed, CorrelationUnknown, CorrelationDeprecated, CorrelationWrongProject}

// ErrNotFound is returned by a Repository when a row does not exist.
var ErrNotFound = errors.New("not found")

// ExternalRunID builds the idempotency key {provider}:{run_id}:{run_attempt}.
func ExternalRunID(provider, providerRunID string, attempt int32) string {
	return provider + ":" + providerRunID + ":" + strconv.Itoa(int(attempt))
}

// TestRun is a logical execution identified by its ExternalRunID.
type TestRun struct {
	ID            int64
	ProjectID     int64
	ExternalRunID string
	Provider      string
	ProviderRunID string
	RunAttempt    int32
	Pipeline      string
	Branch        string
	Commit        string
	Status        RunStatus
	Outcome       RunOutcome
	ExpectedCount int32
	ResultCount   int32
	CreatedAt     time.Time
	StartedAt     *time.Time
	CompletedAt   *time.Time
	// ReportSHA256 is the digest of the report that created the run (internal; not exposed).
	ReportSHA256 string
}

// TestResult is one persisted result. TestCaseID is set when the correlation
// is valid or deprecated (so the result belongs to the test case's history);
// RequestedTestCaseID keeps the raw declared reference.
type TestResult struct {
	ID                  int64
	TestRunID           int64
	TestCaseID          *int64
	RequestedTestCaseID *string
	Correlation         Correlation
	TestName            string
	ClassName           string
	SuiteName           string
	Status              ResultStatus
	DurationMs          *int64
	ErrorMessage        string
	ErrorDetails        string
	CreatedAt           time.Time
}

// NewRun is the metadata of a run to record.
type NewRun struct {
	ProjectID     int64
	Provider      string
	ProviderRunID string
	RunAttempt    int32
	Pipeline      string
	Branch        string
	Commit        string
	StartedAt     *time.Time
	// ReportSHA256 is the digest of the ingested report.
	ReportSHA256 string
	// Status is how the execution ended; empty means completed.
	Status RunStatus
}

// ExecutionStatuses are the statuses CI may report with a final report.
var ExecutionStatuses = []RunStatus{RunCompleted, RunInterrupted, RunCancelled}

// NewResult is a result to persist within a new run.
type NewResult struct {
	TestCaseID          *int64
	RequestedTestCaseID *string
	Correlation         Correlation
	TestName            string
	ClassName           string
	SuiteName           string
	Status              ResultStatus
	DurationMs          *int64
	ErrorMessage        string
	ErrorDetails        string
}

// ParseError is a testcase of the ingested report that could not be fully
// normalized or looks suspicious, stored with its run. Persisted tells whether
// its result was kept; Severity is "error" or "warning".
type ParseError struct {
	Index     int32
	TestName  string
	Message   string
	Persisted bool
	Severity  string
}

// Diagnostic is a stored result whose TC-ID is not valid.
type Diagnostic struct {
	TestName            string
	Correlation         Correlation
	RequestedTestCaseID *string
}

// ResultFilter narrows the results of a run.
type ResultFilter struct {
	Status      *ResultStatus
	Correlation *Correlation
}

// ValidResult is the (TC-ID, status) pair of a result with a valid correlation.
type ValidResult struct {
	TestCaseID int64
	Status     ResultStatus
}

// SummaryInputs are the immutable inputs of a run's summary: its snapshot
// TC-IDs (ascending) and its valid results.
type SummaryInputs struct {
	Expected []int64
	Valid    []ValidResult
}

// HistoryEntry is a historical result of a TC-ID plus the run it was observed in.
type HistoryEntry struct {
	Result TestResult
	Run    TestRun
}

// InsertRunParams is the storage form of a run to create.
type InsertRunParams struct {
	NewRun
	ExternalRunID string
	Status        RunStatus
	CompletedAt   time.Time
}

// Repository is the persistence port of the execution module.
type Repository interface {
	// InsertTestRun inserts a run unless its external id exists; ok is false on conflict.
	InsertTestRun(ctx context.Context, p InsertRunParams) (id int64, ok bool, err error)
	GetTestRunIDByExternalID(ctx context.Context, projectID int64, externalRunID string) (int64, error)
	InsertExpectedCases(ctx context.Context, runID int64, testCaseIDs []int64) error
	InsertTestResults(ctx context.Context, runID int64, results []NewResult) error
	InsertParseErrors(ctx context.Context, runID int64, errs []ParseError) error
	ListParseErrors(ctx context.Context, runID int64, limit, offset int32) ([]ParseError, error)
	CountParseErrors(ctx context.Context, runID int64) (int64, error)
	GetTestRun(ctx context.Context, id int64) (TestRun, error)
	ListTestRuns(ctx context.Context, projectIDs []int64, limit, offset int32) ([]TestRun, error)
	CountTestRuns(ctx context.Context, projectIDs []int64) (int64, error)
	ListRunResults(ctx context.Context, runID int64, f ResultFilter, limit, offset int32) ([]TestResult, error)
	CountRunResults(ctx context.Context, runID int64, f ResultFilter) (int64, error)
	// ListSummaryInputs returns the snapshot TC-IDs and valid results of each given run.
	ListSummaryInputs(ctx context.Context, runIDs []int64) (map[int64]SummaryInputs, error)
	ListDiagnostics(ctx context.Context, runID int64) ([]Diagnostic, error)
	ListResultsForTestCase(ctx context.Context, testCaseID int64, limit, offset int32) ([]HistoryEntry, error)
	CountResultsForTestCase(ctx context.Context, testCaseID int64) (int64, error)

	// InTx runs fn inside one database transaction.
	InTx(ctx context.Context, fn func(Repository) error) error
}
