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
	"time"
	"unicode/utf8"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/ingestion/junit"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
)

// Catalog is what ingestion needs from the catalog module.
type Catalog interface {
	ProjectByKey(ctx context.Context, key string) (catalog.Project, error)
	ProjectByID(ctx context.Context, id int64) (catalog.Project, error)
	IngestionView(ctx context.Context, projectID int64, numbers []int64) (catalog.IngestionView, error)
}

// Recorder is what ingestion needs from the execution module.
type Recorder interface {
	RecordRun(ctx context.Context, run execution.NewRun, expected []int64, results []execution.NewResult, parseErrors []execution.ParseError) (execution.TestRun, bool, error)
	Diagnostics(ctx context.Context, runID int64) ([]execution.Diagnostic, error)
	ParseErrors(ctx context.Context, runID int64) ([]execution.ParseError, error)
}

// Access authorizes reports (the identity module, through authz).
type Access interface {
	// Require checks the caller holds at least minRole in the project (an API key: its own project).
	Require(ctx context.Context, projectID int64, minRole authz.Role, notFound error) error
	// KeyProject returns the project of the API key that authenticated the request, if any.
	KeyProject(ctx context.Context) (int64, bool)
}

// RunMeta is the CI metadata sent with a report.
type RunMeta struct {
	// ProjectKey is the project the run belongs to (default: the API key's project, else TC).
	ProjectKey    string
	Provider      string
	ProviderRunID string
	RunAttempt    int32
	Pipeline      string
	Branch        string
	Commit        string
	// Status is how the CI execution ended (completed when empty).
	Status execution.RunStatus
	// Charset is the Content-Type charset of the report, if any: it overrides the XML declaration.
	Charset string
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

// MetadataDiffersWarning is returned when a replay reports another pipeline,
// branch or commit than the ones recorded for the attempt; they are never applied.
const MetadataDiffersWarning = "%s %q differs from %q, recorded for this attempt; it was not applied"

// FutureStartWarning is returned when the report's suite timestamp is later than
// the ingestion, so the run's start is left unknown.
const FutureStartWarning = "the report's suite timestamp %s is later than the ingestion; startedAt is left unknown (clock skew, or a local time written without a zone)"

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
	access   Access
}

// NewService builds a Service.
func NewService(c Catalog, r Recorder, a Access) *Service {
	return &Service{catalog: c, recorder: r, access: a}
}

// project resolves the run's project and checks the caller may report into it: members (and
// administrators) with a session, or an API key of that project. A project the caller cannot
// see is "not found", like an unknown one.
func (s *Service) project(ctx context.Context, key string) (catalog.Project, error) {
	if id, ok := s.access.KeyProject(ctx); ok && key == "" {
		return s.catalog.ProjectByID(ctx, id)
	}
	key = cmp.Or(key, catalog.DefaultProjectKey)
	p, err := s.catalog.ProjectByKey(ctx, key)
	if err != nil {
		return p, err
	}
	return p, s.access.Require(ctx, p.ID, authz.RoleMember, apperr.NotFound("project %s not found", key))
}

// ValidateMeta checks the run metadata that forms the externalRunId.
func ValidateMeta(m RunMeta) error {
	var v apperr.Validator
	v.Check(providerPattern.MatchString(m.Provider), "provider", "must match "+providerPattern.String())
	v.Check(runIDPattern.MatchString(m.ProviderRunID), "runId", "must match "+runIDPattern.String())
	v.Check(m.RunAttempt >= 1, "runAttempt", "must be an integer >= 1")
	v.Check(utf8.RuneCountInString(m.Pipeline) <= 200, "pipeline", "must be at most 200 characters")
	v.Check(utf8.RuneCountInString(m.Branch) <= 255, "branch", "must be at most 255 characters")
	v.Check(utf8.RuneCountInString(m.Commit) <= 64, "commit", "must be at most 64 characters")
	v.CheckText("pipeline", m.Pipeline)
	v.CheckText("branch", m.Branch)
	v.CheckText("commit", m.Commit)
	v.Check(m.Status == "" || slices.Contains(execution.ExecutionStatuses, m.Status), "status", "must be one of completed, interrupted, cancelled")
	v.Check(m.ProjectKey == "" || catalog.ProjectKeyPattern.MatchString(m.ProjectKey), "project", catalog.ProjectKeyMessage)
	return v.Err()
}

// IngestJUnit ingests one complete JUnit XML report as a batch.
func (s *Service) IngestJUnit(ctx context.Context, meta RunMeta, body io.Reader) (Outcome, error) {
	if err := ValidateMeta(meta); err != nil {
		return Outcome{}, err
	}
	project, err := s.project(ctx, meta.ProjectKey)
	if err != nil {
		return Outcome{}, err
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		return Outcome{}, err
	}
	digest := sha256.Sum256(raw)
	reportSHA := hex.EncodeToString(digest[:])
	report, err := junit.ParseWith(bytes.NewReader(raw), junit.Options{Charset: meta.Charset, ProjectKey: project.Key})
	if err != nil {
		return Outcome{}, apperr.InvalidDocument("%s", err.Error())
	}
	var numbers []int64
	for _, r := range report.Results {
		if r.Ref.Kind == junit.RefFound && ownProject(r.Ref, project.Key) {
			numbers = append(numbers, r.Ref.ID)
		}
	}
	// One catalog read: the snapshot and the correlation agree even if a test
	// case is deprecated while the report is being ingested.
	view, err := s.catalog.IngestionView(ctx, project.ID, numbers)
	if err != nil {
		return Outcome{}, err
	}
	results := correlate(report.Results, project.Key, view.Entries)
	expected := view.Expected
	parseErrors := make([]execution.ParseError, len(report.Errors))
	for i, e := range report.Errors {
		parseErrors[i] = execution.ParseError{Index: int32(e.Index), TestName: e.TestName, Message: e.Message, Persisted: e.Persisted, Severity: string(e.Severity)}
	}
	run, created, err := s.recorder.RecordRun(ctx, execution.NewRun{
		ProjectID: project.ID, Provider: meta.Provider, ProviderRunID: meta.ProviderRunID, RunAttempt: meta.RunAttempt,
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
	if !created {
		for _, f := range [][3]string{{"pipeline", meta.Pipeline, run.Pipeline}, {"branch", meta.Branch, run.Branch}, {"commit", meta.Commit, run.Commit}} {
			if f[1] != f[2] {
				out.Warnings = append(out.Warnings, fmt.Sprintf(MetadataDiffersWarning, f[0], f[1], f[2]))
			}
		}
	}
	if created {
		out.Warnings = append(out.Warnings, report.Notices...)
	}
	if created && report.StartedAt != nil && run.StartedAt == nil {
		out.Warnings = append(out.Warnings, fmt.Sprintf(FutureStartWarning, report.StartedAt.Format(time.RFC3339)))
	}
	return out, nil
}

// ownProject tells whether a reference points into the run's project: a bare
// number does, <KEY>-<n> does when KEY is the project's key.
func ownProject(ref junit.TCRef, key string) bool { return ref.Prefix == "" || ref.Prefix == key }

func correlate(parsed []junit.Result, key string, entries map[int64]catalog.IngestionEntry) []execution.NewResult {
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
			entry, exists := entries[r.Ref.ID]
			switch {
			case !ownProject(r.Ref, key):
				nr.Correlation = execution.CorrelationWrongProject
			case !exists:
				nr.Correlation = execution.CorrelationUnknown
			case entry.Status == catalog.StatusActive:
				id := entry.ID
				nr.TestCaseID = &id
				nr.Correlation = execution.CorrelationValid
			default:
				// Deprecated: kept linked for history; excluded from summaries as a diagnostic.
				id := entry.ID
				nr.TestCaseID = &id
				nr.Correlation = execution.CorrelationDeprecated
			}
		}
		out[i] = nr
	}
	return out
}

func diagnosticMessage(d execution.Diagnostic) string {
	ref := ""
	if d.RequestedTestCaseID != nil {
		ref = *d.RequestedTestCaseID
	}
	switch d.Correlation {
	case execution.CorrelationMissing:
		return `no TC-ID declared: add <property name="tc-id" value="<KEY>-<n>"/> or <KEY>-<n> in the testcase name`
	case execution.CorrelationMalformed:
		return fmt.Sprintf("TC-ID reference %q is malformed: expected a single <KEY>-<n> or positive integer", ref)
	case execution.CorrelationUnknown:
		return fmt.Sprintf("TC-ID reference %q does not exist; test cases are never created automatically", ref)
	case execution.CorrelationWrongProject:
		return fmt.Sprintf("TC-ID reference %q belongs to another project than the run's; it is not correlated", ref)
	default:
		return fmt.Sprintf("TC-ID reference %q points to a deprecated test case", ref)
	}
}
