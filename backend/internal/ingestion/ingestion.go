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
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/ingestion/junit"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/projectkey"
	"github.com/edcrove/provenly/backend/internal/platform/telemetry"
)

// Catalog is what ingestion needs from the catalog module.
type Catalog interface {
	ProjectByKey(ctx context.Context, key string) (catalog.Project, error)
	ProjectByID(ctx context.Context, id int64) (catalog.Project, error)
	IngestionView(ctx context.Context, projectID int64, numbers []int64) (catalog.IngestionView, error)
	// SuiteSelection resolves a suite to the active automated test cases it selects (archived suites: 409).
	SuiteSelection(ctx context.Context, projectID int64, key string) (catalog.Suite, []int64, error)
}

// Recorder is what ingestion needs from the execution module.
type Recorder interface {
	RecordRun(ctx context.Context, run execution.NewRun, expected []int64, results []execution.NewResult, parseErrors []execution.ParseError) (execution.TestRun, bool, error)
	FinalizeShardedRun(ctx context.Context, projectID int64, provider, providerRunID string, attempt int32) (execution.TestRun, bool, error)
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

// RunNotifier hears of runs that completed (webhooks). It must not fail, nor slow down, the recording.
type RunNotifier interface {
	RunCompleted(ctx context.Context, run execution.TestRun)
}

type noNotifier struct{}

func (noNotifier) RunCompleted(context.Context, execution.TestRun) {}

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
	// SuiteKey is the suite the run executed (MVP D2): its selection is the expected universe. Empty: the project's
	// active automated test cases.
	SuiteKey string
	// Shard and ShardTotal identify one of the N reports of a sharded run (?shard=i/N); 0 when not sharded.
	Shard      int32
	ShardTotal int32
}

// MaxShards bounds how many reports one run may be split into.
const MaxShards = 100

// ShardsPendingWarning tells which shards a sharded run still waits for.
const ShardsPendingWarning = "shard %d/%d recorded; the run completes when shards %s arrive (or when CI finalizes it)"

// ShardsMissingWarning is the answer of a finalization that left shards missing.
const ShardsMissingWarning = "shards %s of %d never arrived: the run ended as interrupted"

// shardList renders shard numbers as "2, 4".
func shardList(shards []int32) string {
	parts := make([]string, len(shards))
	for i, n := range shards {
		parts[i] = strconv.Itoa(int(n))
	}
	return strings.Join(parts, ", ")
}

// Diagnostic explains why a result has no valid TC-ID.
type Diagnostic struct {
	TestName            string
	Correlation         execution.Correlation
	RequestedTestCaseID *string
	Message             string
	shard               int32
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
	notify   RunNotifier
}

// NewService builds a Service.
func NewService(c Catalog, r Recorder, a Access) *Service {
	return &Service{catalog: c, recorder: r, access: a, notify: noNotifier{}}
}

// SetNotifier sets who hears of runs a report completed (created, or a live run completed by its final report).
func (s *Service) SetNotifier(n RunNotifier) { s.notify = n }

// project resolves the run's project and checks the caller may report into it: members (and
// administrators) with a session, or an API key of that project. A project the caller cannot
// see is "not found", like an unknown one.
func (s *Service) project(ctx context.Context, key string) (catalog.Project, error) {
	if id, ok := s.access.KeyProject(ctx); ok && key == "" {
		return s.catalog.ProjectByID(ctx, id)
	}
	key = cmp.Or(key, catalog.DefaultProjectKey)
	return projectkey.Resolve(ctx, "project", key, s.catalog.ProjectByKey, catalog.ProjectID, s.access, authz.RoleMember)
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
	v.Check(m.ProjectKey == "" || projectkey.Valid(m.ProjectKey), "project", projectkey.Message)
	v.Check((m.Shard == 0 && m.ShardTotal == 0) || (m.ShardTotal >= 2 && m.ShardTotal <= MaxShards && m.Shard >= 1 && m.Shard <= m.ShardTotal),
		"shard", fmt.Sprintf("must be i/N with 2 <= N <= %d and 1 <= i <= N", MaxShards))
	v.Check(m.SuiteKey == "" || catalog.SuiteKeyPattern.MatchString(m.SuiteKey), "suite", catalog.SuiteKeyMessage)
	return v.Err()
}

// IngestJUnit ingests one complete JUnit XML report as a batch. It is one span with child spans for parsing,
// correlation and recording; the outcome is on the span (run id, results, whether it was created).
func (s *Service) IngestJUnit(ctx context.Context, meta RunMeta, body io.Reader) (out Outcome, err error) {
	ctx, span := telemetry.Tracer("ingestion").Start(ctx, "ingestion.junit", trace.WithAttributes(
		attribute.String("provenly.project", meta.ProjectKey), attribute.String("provenly.provider", meta.Provider),
		attribute.String("provenly.run_id", meta.ProviderRunID), attribute.Int("provenly.run_attempt", int(meta.RunAttempt))))
	defer func() {
		span.SetAttributes(attribute.Int64("provenly.test_run.id", out.Run.ID), attribute.Bool("provenly.created", out.Created),
			attribute.Int("provenly.results", out.Persisted))
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	return s.ingestJUnit(ctx, meta, body)
}

func (s *Service) ingestJUnit(ctx context.Context, meta RunMeta, body io.Reader) (Outcome, error) {
	if err := ValidateMeta(meta); err != nil {
		return Outcome{}, err
	}
	project, err := s.project(ctx, meta.ProjectKey)
	if err != nil {
		return Outcome{}, err
	}
	var suite catalog.Suite
	var selection []int64
	if meta.SuiteKey != "" {
		if suite, selection, err = s.catalog.SuiteSelection(ctx, project.ID, meta.SuiteKey); err != nil {
			return Outcome{}, err
		}
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		return Outcome{}, err
	}
	digest := sha256.Sum256(raw)
	reportSHA := hex.EncodeToString(digest[:])
	_, parse := telemetry.Tracer("ingestion").Start(ctx, "ingestion.junit.parse", trace.WithAttributes(attribute.Int("provenly.bytes", len(raw))))
	report, err := junit.ParseWith(bytes.NewReader(raw), junit.Options{Charset: meta.Charset, ProjectKey: project.Key})
	parse.End()
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
	if meta.SuiteKey != "" {
		// The suite's selection, among the test cases expected in the same snapshot as the correlation.
		expected = intersect(view.Expected, selection)
	}
	parseErrors := make([]execution.ParseError, len(report.Errors))
	for i, e := range report.Errors {
		parseErrors[i] = execution.ParseError{Index: int32(e.Index), TestName: e.TestName, Message: e.Message, Persisted: e.Persisted, Severity: string(e.Severity)}
	}
	run, created, err := s.recorder.RecordRun(ctx, execution.NewRun{
		ProjectID: project.ID, Provider: meta.Provider, ProviderRunID: meta.ProviderRunID, RunAttempt: meta.RunAttempt,
		Pipeline: meta.Pipeline, Branch: meta.Branch, Commit: meta.Commit, StartedAt: report.StartedAt,
		ReportSHA256: reportSHA, Status: meta.Status, SuiteKey: suite.Key, SuiteName: suite.Name,
		Shard: meta.Shard, ShardTotal: meta.ShardTotal,
	}, expected, results, parseErrors)
	if err != nil {
		return Outcome{}, err
	}
	if created && run.Status != execution.RunRunning {
		s.notify.RunCompleted(ctx, run) // a sharded run: once, when its last shard arrives
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
		out.Diagnostics[i] = Diagnostic{TestName: d.TestName, Correlation: d.Correlation, RequestedTestCaseID: d.RequestedTestCaseID, Message: diagnosticMessage(d), shard: d.Shard}
	}
	if out.ParseErrors == nil {
		out.ParseErrors = []execution.ParseError{}
	}
	out.Warnings = []string{}
	recordedSHA, recordedStatus := run.ReportSHA256, run.Status
	if meta.ShardTotal > 0 {
		// A shard answers for its own report: its results, diagnostics, parse errors, digest and status.
		for _, sh := range run.Shards {
			if sh.Shard == meta.Shard {
				recordedSHA, recordedStatus, out.Persisted = sh.ReportSHA256, sh.Status, int(sh.ResultCount)
			}
		}
		out.Diagnostics = slices.DeleteFunc(out.Diagnostics, func(d Diagnostic) bool { return d.shard != meta.Shard })
		out.ParseErrors = slices.DeleteFunc(out.ParseErrors, func(e execution.ParseError) bool { return e.Shard != meta.Shard })
	}
	if !created && recordedSHA != reportSHA {
		out.Warnings = append(out.Warnings, ReportDiffersWarning)
	}
	if requested := cmp.Or(meta.Status, execution.RunCompleted); !created && recordedStatus != requested {
		out.Warnings = append(out.Warnings, fmt.Sprintf(StatusDiffersWarning, requested, recordedStatus))
	}
	if !created {
		for _, f := range [][3]string{{"pipeline", meta.Pipeline, run.Pipeline}, {"branch", meta.Branch, run.Branch}, {"commit", meta.Commit, run.Commit}, {"suite", meta.SuiteKey, run.SuiteKey}} {
			if f[1] != f[2] {
				out.Warnings = append(out.Warnings, fmt.Sprintf(MetadataDiffersWarning, f[0], f[1], f[2]))
			}
		}
	}
	if created {
		out.Warnings = append(out.Warnings, report.Notices...)
	}
	if missing := run.MissingShards(); meta.ShardTotal > 0 && run.Status == execution.RunRunning {
		out.Warnings = append(out.Warnings, fmt.Sprintf(ShardsPendingWarning, meta.Shard, meta.ShardTotal, shardList(missing)))
	}
	if created && report.StartedAt != nil && run.StartedAt == nil {
		out.Warnings = append(out.Warnings, fmt.Sprintf(FutureStartWarning, report.StartedAt.Format(time.RFC3339)))
	}
	return out, nil
}

// FinalizeShards ends a sharded run whose shards will not all arrive (a CI job failed before reporting): it becomes
// interrupted with its missing shards named. Finalizing a run that already ended changes nothing.
func (s *Service) FinalizeShards(ctx context.Context, meta RunMeta) (Outcome, error) {
	if err := ValidateMeta(meta); err != nil {
		return Outcome{}, err
	}
	project, err := s.project(ctx, meta.ProjectKey)
	if err != nil {
		return Outcome{}, err
	}
	run, finished, err := s.recorder.FinalizeShardedRun(ctx, project.ID, meta.Provider, meta.ProviderRunID, meta.RunAttempt)
	if err != nil {
		return Outcome{}, err
	}
	out := Outcome{Created: finished, Run: run, Persisted: int(run.ResultCount), Diagnostics: []Diagnostic{},
		ParseErrors: []execution.ParseError{}, Warnings: []string{}}
	if finished {
		s.notify.RunCompleted(ctx, run)
	}
	if missing := run.MissingShards(); len(missing) > 0 && run.Status == execution.RunInterrupted {
		out.Warnings = append(out.Warnings, fmt.Sprintf(ShardsMissingWarning, shardList(missing), run.ShardTotal))
	}
	return out, nil
}

// intersect returns the ids of a (ascending) that are also in b, ascending.
func intersect(a, b []int64) []int64 {
	in := make(map[int64]bool, len(b))
	for _, id := range b {
		in[id] = true
	}
	out := []int64{}
	for _, id := range a {
		if in[id] {
			out = append(out, id)
		}
	}
	return out
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
			ErrorMessage: r.ErrorMessage, ErrorDetails: r.ErrorDetails, Attempt: int32(r.Attempt),
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
