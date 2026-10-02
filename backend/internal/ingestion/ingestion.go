// Package ingestion is the Ingestion module: it receives complete CI reports,
// normalizes them (JUnit parser), correlates each result with a TC-ID through
// the catalog module's interface and records the run through the execution
// module's interface. It owns no tables.
package ingestion

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"slices"
	"unicode/utf8"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/ingestion/junit"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

// Catalog is what ingestion needs from the catalog module.
type Catalog interface {
	ExpectedUniverse(ctx context.Context) ([]int64, error)
	Statuses(ctx context.Context, ids []int64) (map[int64]catalog.Status, error)
}

// Recorder is what ingestion needs from the execution module.
type Recorder interface {
	RecordRun(ctx context.Context, run execution.NewRun, expected []int64, results []execution.NewResult, parseErrors []execution.ParseError) (execution.TestRun, bool, error)
	Diagnostics(ctx context.Context, runID int64) ([]execution.Diagnostic, error)
	ParseErrors(ctx context.Context, runID int64) ([]execution.ParseError, error)
}

// RunMeta is the CI metadata sent with a report.
type RunMeta struct {
	Provider      string
	ProviderRunID string
	RunAttempt    int32
	Pipeline      string
	Branch        string
	Commit        string
	// Status is how the CI execution ended (completed when empty).
	Status execution.RunStatus
}

// Diagnostic explains why a result has no valid TC-ID.
type Diagnostic struct {
	TestName            string
	Correlation         execution.Correlation
	RequestedTestCaseID *string
	Message             string
}

// Outcome is the result of one ingestion.
type Outcome struct {
	Created     bool
	Run         execution.TestRun
	Received    int
	Persisted   int
	Diagnostics []Diagnostic
	// ParseErrors are the ones stored with the run (on a replay: those of the original ingestion).
	ParseErrors []execution.ParseError
	// Warnings are non-blocking notices about the request (e.g. a replay with a different report).
	Warnings []string
}

// StatusDiffersWarning is returned when a replay reports another final status
// than the one recorded for the attempt; it is never applied.
const StatusDiffersWarning = "status %q differs from %q, recorded for this attempt; it was not applied"

// ReportDiffersWarning is returned when a replay of an attempt carries a report
// different from the one that created the run; it is never applied.
const ReportDiffersWarning = "report differs from the one already ingested for this attempt; it was not applied (send a new runAttempt to record it)"

var (
	providerPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,49}$`)
	runIDPattern    = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

// Service ingests reports.
type Service struct {
	catalog  Catalog
	recorder Recorder
}

// NewService builds a Service.
func NewService(c Catalog, r Recorder) *Service { return &Service{catalog: c, recorder: r} }

// ValidateMeta checks the run metadata that forms the externalRunId.
func ValidateMeta(m RunMeta) error {
	var v apperr.Validator
	v.Check(providerPattern.MatchString(m.Provider), "provider", "must match "+providerPattern.String())
	v.Check(runIDPattern.MatchString(m.ProviderRunID), "runId", "must match "+runIDPattern.String())
	v.Check(m.RunAttempt >= 1, "runAttempt", "must be an integer >= 1")
	v.Check(utf8.RuneCountInString(m.Pipeline) <= 200, "pipeline", "must be at most 200 characters")
	v.Check(utf8.RuneCountInString(m.Branch) <= 255, "branch", "must be at most 255 characters")
	v.Check(utf8.RuneCountInString(m.Commit) <= 64, "commit", "must be at most 64 characters")
	v.Check(m.Status == "" || slices.Contains(execution.ExecutionStatuses, m.Status), "status", "must be one of completed, interrupted, cancelled")
	return v.Err()
}

// IngestJUnit ingests one complete JUnit XML report as a batch.
func (s *Service) IngestJUnit(ctx context.Context, meta RunMeta, body io.Reader) (Outcome, error) {
	if err := ValidateMeta(meta); err != nil {
		return Outcome{}, err
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		return Outcome{}, err
	}
	digest := sha256.Sum256(raw)
	reportSHA := hex.EncodeToString(digest[:])
	report, err := junit.Parse(bytes.NewReader(raw))
	if err != nil {
		return Outcome{}, apperr.InvalidDocument("%s", err.Error())
	}
	results, err := s.correlate(ctx, report.Results)
	if err != nil {
		return Outcome{}, err
	}
	expected, err := s.catalog.ExpectedUniverse(ctx)
	if err != nil {
		return Outcome{}, err
	}
	parseErrors := make([]execution.ParseError, len(report.Errors))
	for i, e := range report.Errors {
		parseErrors[i] = execution.ParseError{Index: int32(e.Index), TestName: e.TestName, Message: e.Message, Persisted: e.Persisted, Severity: string(e.Severity)}
	}
	run, created, err := s.recorder.RecordRun(ctx, execution.NewRun{
		Provider: meta.Provider, ProviderRunID: meta.ProviderRunID, RunAttempt: meta.RunAttempt,
		Pipeline: meta.Pipeline, Branch: meta.Branch, Commit: meta.Commit, StartedAt: report.StartedAt,
		ReportSHA256: reportSHA, Status: meta.Status,
	}, expected, results, parseErrors)
	if err != nil {
		return Outcome{}, err
	}
	stored, err := s.recorder.Diagnostics(ctx, run.ID)
	if err != nil {
		return Outcome{}, err
	}
	storedParseErrors, err := s.recorder.ParseErrors(ctx, run.ID)
	if err != nil {
		return Outcome{}, err
	}
	out := Outcome{
		Created: created, Run: run, Received: report.Received, Persisted: int(run.ResultCount),
		Diagnostics: make([]Diagnostic, len(stored)), ParseErrors: storedParseErrors,
	}
	for i, d := range stored {
		out.Diagnostics[i] = Diagnostic{TestName: d.TestName, Correlation: d.Correlation, RequestedTestCaseID: d.RequestedTestCaseID, Message: diagnosticMessage(d)}
	}
	if out.ParseErrors == nil {
		out.ParseErrors = []execution.ParseError{}
	}
	out.Warnings = []string{}
	if !created && run.ReportSHA256 != reportSHA {
		out.Warnings = append(out.Warnings, ReportDiffersWarning)
	}
	if requested := cmp.Or(meta.Status, execution.RunCompleted); !created && run.Status != requested {
		out.Warnings = append(out.Warnings, fmt.Sprintf(StatusDiffersWarning, requested, run.Status))
	}
	return out, nil
}

func (s *Service) correlate(ctx context.Context, parsed []junit.Result) ([]execution.NewResult, error) {
	var ids []int64
	for _, r := range parsed {
		if r.Ref.Kind == junit.RefFound {
			ids = append(ids, r.Ref.ID)
		}
	}
	statuses, err := s.catalog.Statuses(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]execution.NewResult, len(parsed))
	for i, r := range parsed {
		nr := execution.NewResult{
			TestName: r.TestName, ClassName: r.ClassName, SuiteName: r.SuiteName,
			Status: execution.ResultStatus(r.Status), DurationMs: r.DurationMs,
			ErrorMessage: r.ErrorMessage, ErrorDetails: r.ErrorDetails,
		}
		if r.Ref.Raw != "" {
			raw := r.Ref.Raw
			nr.RequestedTestCaseID = &raw
		}
		switch r.Ref.Kind {
		case junit.RefMissing:
			nr.Correlation = execution.CorrelationMissing
		case junit.RefMalformed:
			nr.Correlation = execution.CorrelationMalformed
		default:
			switch statuses[r.Ref.ID] {
			case catalog.StatusActive:
				id := r.Ref.ID
				nr.TestCaseID = &id
				nr.Correlation = execution.CorrelationValid
			case catalog.StatusDeprecated:
				// Kept linked for history; excluded from summaries as a diagnostic.
				id := r.Ref.ID
				nr.TestCaseID = &id
				nr.Correlation = execution.CorrelationDeprecated
			default:
				nr.Correlation = execution.CorrelationUnknown
			}
		}
		out[i] = nr
	}
	return out, nil
}

func diagnosticMessage(d execution.Diagnostic) string {
	ref := ""
	if d.RequestedTestCaseID != nil {
		ref = *d.RequestedTestCaseID
	}
	switch d.Correlation {
	case execution.CorrelationMissing:
		return `no TC-ID declared: add <property name="tc-id" value="<id>"/> or TC-<id> in the testcase name`
	case execution.CorrelationMalformed:
		return fmt.Sprintf("TC-ID reference %q is malformed: expected a single positive integer id", ref)
	case execution.CorrelationUnknown:
		return fmt.Sprintf("TC-ID reference %q does not exist; test cases are never created automatically", ref)
	default:
		return fmt.Sprintf("TC-ID reference %q points to a deprecated test case", ref)
	}
}
