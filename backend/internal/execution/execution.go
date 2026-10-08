// Package execution is the TestRun/Execution module: idempotent test runs with
// their immutable expected-universe snapshot, persisted results, the run
// summary and the per-TC-ID result history. TC-IDs are held by value; the
// module never reads catalog tables.
package execution

import (
	"context"
	"errors"
	"slices"
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
	// RunRunning is a manual (or live) run still receiving results.
	RunRunning RunStatus = "running"
)

// RunMode tells how a run's results arrive.
type RunMode string

// Run modes.
const (
	// ModeBatch runs are created with every result of one CI report.
	ModeBatch RunMode = "batch"
	// ModeManual runs are started by a person, who records results one by one and finishes the run.
	ModeManual RunMode = "manual"
	// ModeLive runs receive results while CI executes them (reserved for streamed runs).
	ModeLive RunMode = "live"
	// ModeSharded runs are one logical CI run split into N reports (?shard=i/N): created by the first shard, completed
	// when every shard arrived, or finalized by CI as interrupted.
	ModeSharded RunMode = "sharded"
)

// MaxAttempts bounds the attempts of one test in a run (the JUnit parser's bound; a manual test re-tested more is
// refused).
const MaxAttempts = 100

// ManualClass is the class name of manually recorded results: a manual re-test is the next attempt of the same
// test, and passing after a failed attempt is a fix, not flakiness.
const ManualClass = "provenly-manual"

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

// ErrConflict is returned by repositories when a unique record already exists.
var ErrConflict = errors.New("conflict")

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
	// ExpectedCount is the size of the universe: the snapshot plus its amendments.
	ExpectedCount int32
	ResultCount   int32
	// AmendmentCount > 0 marks the run as edited after its creation (DEC-42).
	AmendmentCount int32
	CreatedAt      time.Time
	StartedAt      *time.Time
	CompletedAt    *time.Time
	// ReportSHA256 is the digest of the report that created the run (internal; not exposed).
	ReportSHA256 string
	// SuiteKey and SuiteName name the suite the run was reported for, as they were then (empty: none).
	SuiteKey  string
	SuiteName string
	Mode      RunMode
	// StartedBy is who started a manual run (empty otherwise).
	StartedBy string
	// ShardTotal is how many reports a sharded run is split into (0: not sharded); ShardsReceived the shards that
	// arrived, ascending.
	ShardTotal     int32
	ShardsReceived []int32
	// Shards are the received shards in detail (only when the run was just recorded).
	Shards []RunShard
}

// RunShard is one received report of a sharded run.
type RunShard struct {
	Shard        int32
	ReportSHA256 string
	Status       RunStatus
	// ResultCount is how many results the shard stored.
	ResultCount int32
}

// MissingShards returns the shards of a sharded run that have not arrived, ascending.
func (r TestRun) MissingShards() []int32 {
	var out []int32
	for i := int32(1); i <= r.ShardTotal; i++ {
		if !slices.Contains(r.ShardsReceived, i) {
			out = append(out, i)
		}
	}
	return out
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
	// Attempt numbers the executions of the same test in the run (1 = first, D1).
	Attempt int32
	// Retried tells that a later attempt of the same test exists: this result is not the test's logical result.
	Retried bool
	// RecordedBy is who recorded a manual result; FailedStep the step where it failed, if any.
	RecordedBy string
	FailedStep *int32
	// Shard is the shard of a sharded run that reported the result (nil otherwise).
	Shard *int32
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
	// SuiteKey and SuiteName name the suite the run was reported for (empty: the project's automated catalog).
	SuiteKey  string
	SuiteName string
	// Mode is how the results arrive (empty: batch); StartedBy is who started a manual run.
	Mode      RunMode
	StartedBy string
	// Shard and ShardTotal identify one report of a sharded run (?shard=i/N; 0: not sharded).
	Shard      int32
	ShardTotal int32
}

// RunFilter narrows a run list; nil fields do not filter.
type RunFilter struct {
	// ProjectIDs nil means every project.
	ProjectIDs []int64
	SuiteKey   *string
	// Branch matches exactly; Status is the execution status, Mode how results arrive; From and To bound the creation
	// time (inclusive). nil means no condition.
	Branch   *string
	Status   *string
	Mode     *string
	From, To *time.Time
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
	// Attempt numbers the executions of the same test in the run (1 = first, D1).
	Attempt int32
	// RecordedBy is who recorded a manual result; FailedStep the step where it failed, if any.
	RecordedBy string
	FailedStep *int32
	// Shard is the shard of a sharded run that reported it (0: not sharded).
	Shard int32
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
	// Shard is the shard of a sharded run whose report it belongs to (0: not sharded).
	Shard int32
}

// Diagnostic is a stored result whose TC-ID is not valid.
type Diagnostic struct {
	TestName            string
	Correlation         Correlation
	RequestedTestCaseID *string
	// Shard is the shard of a sharded run that reported it (0: not sharded).
	Shard int32
}

// ResultFilter narrows the results of a run.
type ResultFilter struct {
	Status      *ResultStatus
	Correlation *Correlation
	Shard       *int32
}

// ValidResult is the (TC-ID, status) pair of a result with a valid correlation.
type ValidResult struct {
	TestCaseID int64
	Status     ResultStatus
	// Execution identifies the test the result belongs to (suite, class and name); Attempt orders its attempts.
	// An empty Execution makes the result an execution of its own.
	Execution string
	Attempt   int32
}

// SummaryInputs are the immutable inputs of a run's summary: its snapshot
// TC-IDs (ascending) and its valid results.
type SummaryInputs struct {
	Expected []int64
	// Amended are TC-IDs added to the universe after the run was created (DEC-42), ascending.
	Amended []int64
	Valid   []ValidResult
}

// Universe is the snapshot plus its amendments.
func (in SummaryInputs) Universe() []int64 {
	return append(append([]int64(nil), in.Expected...), in.Amended...)
}

// Amendment adds a TC-ID with valid results to a run's universe after the run was created (DEC-42). Amendments
// are append-only and record who made them and why.
type Amendment struct {
	ID                int64
	TestRunID         int64
	TestCaseID        int64
	AmendedBy         int64
	AmendedByUsername string
	Reason            string
	CreatedAt         time.Time
}

// NewAmendment is the content of an amendment to record.
type NewAmendment struct {
	TestRunID         int64
	TestCaseID        int64
	AmendedBy         int64
	AmendedByUsername string
	Reason            string
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
	// CompletedAt is nil while the run is running.
	CompletedAt *time.Time
}

// Conclusive is the latest conclusive logical status (passed, failed or error) of a test case and the run it came from.
type Conclusive struct {
	TestCaseID int64
	RunID      int64
	Status     ResultStatus
}

// FlakyCount is how many of a project's latest runs a test case was flaky in.
type FlakyCount struct {
	TestCaseID int64
	Runs       int32
}

// Repository is the persistence port of the execution module.
type Repository interface {
	// InsertTestRun inserts a run unless its external id exists; ok is false on conflict.
	InsertTestRun(ctx context.Context, p InsertRunParams) (id int64, ok bool, err error)
	// LockTestRun locks a run until the transaction ends and returns its status and mode.
	LockTestRun(ctx context.Context, id int64) (RunStatus, RunMode, error)
	// IsInUniverse tells whether a test case is in a run's snapshot or amendments.
	IsInUniverse(ctx context.Context, runID, testCaseID int64) (bool, error)
	// InsertManualResult appends one recorded result (the next attempt of its test) to a running run.
	InsertManualResult(ctx context.Context, runID int64, r NewResult) (TestResult, error)
	// ListLatestResults returns the valid results of each test case in its latest run with results.
	ListLatestResults(ctx context.Context, testCaseIDs []int64) ([]ValidResult, error)
	// ListLatestConclusive returns, per test case, its latest run with a conclusive logical status and that status.
	ListLatestConclusive(ctx context.Context, testCaseIDs []int64) ([]Conclusive, error)
	// CountRunEvents counts a run's live events.
	CountRunEvents(ctx context.Context, runID int64) (int, error)
	// InsertRunEvent appends a live event; inserted is false when its event id was already received.
	InsertRunEvent(ctx context.Context, runID int64, e NewEvent) (inserted bool, err error)
	// ListRunEvents returns a run's live events ordered by sequence.
	ListRunEvents(ctx context.Context, runID int64) ([]Event, error)
	// CompleteLiveRun ends a running live run with its final report's execution status and digest.
	CompleteLiveRun(ctx context.Context, runID int64, status RunStatus, reportSHA256 string) error
	// ListLastExecuted returns when each test case last had a valid result (absent: never).
	ListLastExecuted(ctx context.Context, testCaseIDs []int64) (map[int64]time.Time, error)
	// ListFlakyCounts returns the test cases flaky in a project's latest window runs, most flaky first, at most limit.
	ListFlakyCounts(ctx context.Context, projectID int64, window, limit int32) ([]FlakyCount, error)
	// FinishTestRun ends a running run with a final status.
	FinishTestRun(ctx context.Context, id int64, status RunStatus) error
	// GetRunProject returns the project of a run (ErrNotFound when it does not exist).
	GetRunProject(ctx context.Context, id int64) (int64, error)
	// InsertRunShard records a shard of a running sharded run; false when it was already received.
	InsertRunShard(ctx context.Context, runID int64, shard RunShard) (bool, error)
	// ListRunShards returns the received shards of a run, ascending.
	ListRunShards(ctx context.Context, runID int64) ([]RunShard, error)
	// FinishShardedRun ends a running sharded run with its execution status.
	FinishShardedRun(ctx context.Context, runID int64, status RunStatus) error
	GetTestRunIDByExternalID(ctx context.Context, projectID int64, externalRunID string) (int64, error)
	InsertExpectedCases(ctx context.Context, runID int64, testCaseIDs []int64) error
	InsertTestResults(ctx context.Context, runID int64, results []NewResult) error
	InsertParseErrors(ctx context.Context, runID int64, errs []ParseError) error
	ListParseErrors(ctx context.Context, runID int64, limit, offset int32) ([]ParseError, error)
	CountParseErrors(ctx context.Context, runID int64) (int64, error)
	GetTestRun(ctx context.Context, id int64) (TestRun, error)
	ListTestRuns(ctx context.Context, f RunFilter, limit, offset int32) ([]TestRun, error)
	CountTestRuns(ctx context.Context, f RunFilter) (int64, error)
	ListRunResults(ctx context.Context, runID int64, f ResultFilter, limit, offset int32) ([]TestResult, error)
	CountRunResults(ctx context.Context, runID int64, f ResultFilter) (int64, error)
	// ListSummaryInputs returns the snapshot TC-IDs and valid results of each given run.
	ListSummaryInputs(ctx context.Context, runIDs []int64) (map[int64]SummaryInputs, error)
	ListDiagnostics(ctx context.Context, runID int64) ([]Diagnostic, error)
	ListResultsForTestCase(ctx context.Context, testCaseID int64, limit, offset int32) ([]HistoryEntry, error)
	CountResultsForTestCase(ctx context.Context, testCaseID int64) (int64, error)

	// InsertAmendment returns ErrConflict when the TC-ID is already amended into the run.
	InsertAmendment(ctx context.Context, a NewAmendment) (Amendment, error)
	ListAmendments(ctx context.Context, runID int64, limit, offset int32) ([]Amendment, error)
	CountAmendments(ctx context.Context, runID int64) (int64, error)

	// InTx runs fn inside one database transaction.
	InTx(ctx context.Context, fn func(Repository) error) error
}
