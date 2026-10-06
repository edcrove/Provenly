package ingestion

import (
	"cmp"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
)

var errBoom = errors.New("boom")

type fakeCatalog struct {
	universe   []int64
	statuses   map[int64]catalog.Status
	viewErr    error
	projectErr error
	askedIDs   []int64
	gotProject int64
	// selection is what any suite selects; suiteErr fails the suite lookup.
	selection []int64
	suiteErr  error
	gotSuite  string
}

func (f *fakeCatalog) SuiteSelection(_ context.Context, _ int64, key string) (catalog.Suite, []int64, error) {
	f.gotSuite = key
	return catalog.Suite{Key: key, Name: "Suite " + key}, f.selection, f.suiteErr
}

// ProjectByKey answers CHK as project 2 and every other key as project 1.
func (f *fakeCatalog) ProjectByKey(_ context.Context, key string) (catalog.Project, error) {
	id := int64(1)
	if key == "CHK" {
		id = 2
	}
	return catalog.Project{ID: id, Key: key}, f.projectErr
}

// ProjectByID answers 2 as CHK and every other id as TC.
func (f *fakeCatalog) ProjectByID(_ context.Context, id int64) (catalog.Project, error) {
	if id == 2 {
		return catalog.Project{ID: 2, Key: "CHK"}, f.projectErr
	}
	return catalog.Project{ID: id, Key: "TC"}, f.projectErr
}

// fakeAccess allows everything unless denied lists the project; key is the API key's project (0: a session).
type fakeAccess struct {
	key    int64
	denied map[int64]error
}

func (a fakeAccess) Require(_ context.Context, projectID int64, _ authz.Role, notFound error) error {
	if err, ok := a.denied[projectID]; ok {
		if err == nil {
			return notFound
		}
		return err
	}
	return nil
}

func (a fakeAccess) KeyProject(context.Context) (int64, bool) { return a.key, a.key != 0 }

// IngestionView answers each known number with an id equal to the number (as in the default project).
func (f *fakeCatalog) IngestionView(_ context.Context, projectID int64, numbers []int64) (catalog.IngestionView, error) {
	f.askedIDs, f.gotProject = numbers, projectID
	entries := map[int64]catalog.IngestionEntry{}
	for n, st := range f.statuses {
		entries[n] = catalog.IngestionEntry{ID: n, Status: st}
	}
	return catalog.IngestionView{Expected: f.universe, Entries: entries}, f.viewErr
}

type fakeRecorder struct {
	created    bool
	gotRun     execution.NewRun
	gotExp     []int64
	gotResults []execution.NewResult
	diags      []execution.Diagnostic
	gotParse   []execution.ParseError
	storedPE   []execution.ParseError
	recordErr  error
	diagErr    error
	parseErr   error
	// storedDigest simulates the digest of the report that created an existing run.
	storedDigest string
	// storedStatus simulates the status recorded for an existing run.
	storedStatus execution.RunStatus
	// storedMeta simulates the pipeline, branch and commit recorded for an existing run.
	storedMeta *[3]string
	// storedStart simulates the startedAt the execution module kept.
	storedStart *time.Time
	// storedSuite simulates the suite recorded for an existing run.
	storedSuite *string
	// shardRun, when set, is the sharded run RecordRun returns; finalRun, finished and finalErr answer FinalizeShardedRun.
	shardRun *execution.TestRun
	finalRun execution.TestRun
	finished bool
	finalErr error
	gotFinal [4]any
}

func (f *fakeRecorder) FinalizeShardedRun(_ context.Context, projectID int64, provider, runID string, attempt int32) (execution.TestRun, bool, error) {
	f.gotFinal = [4]any{projectID, provider, runID, attempt}
	return f.finalRun, f.finished, f.finalErr
}

func (f *fakeRecorder) RecordRun(_ context.Context, run execution.NewRun, exp []int64, rs []execution.NewResult, pe []execution.ParseError) (execution.TestRun, bool, error) {
	f.gotRun, f.gotExp, f.gotResults, f.gotParse = run, exp, rs, pe
	if f.shardRun != nil {
		return *f.shardRun, f.created, f.recordErr
	}
	digest := run.ReportSHA256
	if f.storedDigest != "" {
		digest = f.storedDigest
	}
	status := cmp.Or(f.storedStatus, run.Status, execution.RunCompleted)
	recorded := [3]string{run.Pipeline, run.Branch, run.Commit}
	if f.storedMeta != nil {
		recorded = *f.storedMeta
	}
	suite := run.SuiteKey
	if f.storedSuite != nil {
		suite = *f.storedSuite
	}
	return execution.TestRun{ID: 1, ExternalRunID: execution.ExternalRunID(run.Provider, run.ProviderRunID, run.RunAttempt),
		Pipeline: recorded[0], Branch: recorded[1], Commit: recorded[2], StartedAt: f.storedStart,
		ResultCount: int32(len(rs)), ReportSHA256: digest, Status: status, SuiteKey: suite}, f.created, f.recordErr
}

func (f *fakeRecorder) Diagnostics(context.Context, int64) ([]execution.Diagnostic, error) {
	return f.diags, f.diagErr
}

func (f *fakeRecorder) ParseErrors(context.Context, int64) ([]execution.ParseError, error) {
	return f.storedPE, f.parseErr
}

var meta = RunMeta{Provider: "github", ProviderRunID: "99", RunAttempt: 1, Pipeline: "ci", Branch: "main", Commit: "abc"}

const report = `<testsuites><testsuite name="s" timestamp="2026-09-28T10:00:00">
<testcase name="valid"><properties><property name="tc-id" value="153"/></properties></testcase>
<testcase name="deprecated TC-2"/>
<testcase name="unknown TC-404"/>
<testcase name="no id"/>
<testcase name="bad TC-x"/>
<testcase name=""/>
<testcase name="slow TC-2" time="later"/>
</testsuite></testsuites>`

func TestIngestCorrelatesEveryResult(t *testing.T) {
	cat := &fakeCatalog{universe: []int64{153, 154}, statuses: map[int64]catalog.Status{153: catalog.StatusActive, 2: catalog.StatusDeprecated}}
	rec := &fakeRecorder{created: true, diags: []execution.Diagnostic{
		{TestName: "no id", Correlation: execution.CorrelationMissing},
		{TestName: "bad TC-x", Correlation: execution.CorrelationMalformed, RequestedTestCaseID: strPtr("TC-x")},
		{TestName: "unknown TC-404", Correlation: execution.CorrelationUnknown, RequestedTestCaseID: strPtr("TC-404")},
		{TestName: "deprecated TC-2", Correlation: execution.CorrelationDeprecated, RequestedTestCaseID: strPtr("TC-2")},
	}}
	out, err := NewService(cat, rec, fakeAccess{}).IngestJUnit(context.Background(), meta, strings.NewReader(report))
	require.NoError(t, err)

	assert.True(t, out.Created)
	assert.Equal(t, 7, out.Received)
	assert.Equal(t, 6, out.Persisted)
	assert.Equal(t, []execution.ParseError{
		{Index: 5, TestName: "", Message: "testcase has no name; result discarded", Severity: "error"},
		{Index: 6, TestName: "slow TC-2", Message: `invalid time attribute "later"; result kept without duration`, Persisted: true, Severity: "error"},
	}, rec.gotParse)
	assert.Equal(t, []execution.ParseError{}, out.ParseErrors, "the response returns what is stored")
	assert.Equal(t, []int64{153, 2, 404, 2}, cat.askedIDs)
	assert.Equal(t, []int64{153, 154}, rec.gotExp)
	assert.Equal(t, "github", rec.gotRun.Provider)
	require.NotNil(t, rec.gotRun.StartedAt)

	got := map[string]execution.NewResult{}
	for _, r := range rec.gotResults {
		got[r.TestName] = r
	}
	assert.Equal(t, execution.CorrelationValid, got["valid"].Correlation)
	assert.Equal(t, int64(153), *got["valid"].TestCaseID)
	assert.Equal(t, "153", *got["valid"].RequestedTestCaseID)
	assert.Equal(t, execution.CorrelationDeprecated, got["deprecated TC-2"].Correlation)
	assert.Equal(t, int64(2), *got["deprecated TC-2"].TestCaseID, "deprecated results stay linked for history")
	assert.Equal(t, execution.CorrelationUnknown, got["unknown TC-404"].Correlation)
	assert.Equal(t, execution.CorrelationMissing, got["no id"].Correlation)
	assert.Nil(t, got["no id"].RequestedTestCaseID)
	assert.Equal(t, execution.CorrelationMalformed, got["bad TC-x"].Correlation)

	require.Len(t, out.Diagnostics, 4)
	assert.Contains(t, out.Diagnostics[0].Message, "no TC-ID declared")
	assert.Contains(t, out.Diagnostics[1].Message, "malformed")
	assert.Contains(t, out.Diagnostics[2].Message, "never created automatically")
	assert.Contains(t, out.Diagnostics[3].Message, "deprecated")
}

func strPtr(s string) *string { return &s }

func TestIngestReplayAndEmptyParseErrors(t *testing.T) {
	rec := &fakeRecorder{created: false, storedPE: []execution.ParseError{{Index: 1, Message: "stored"}}}
	out, err := NewService(&fakeCatalog{}, rec, fakeAccess{}).IngestJUnit(context.Background(), meta, strings.NewReader(`<testsuite name="s"/>`))
	require.NoError(t, err)
	assert.False(t, out.Created)
	assert.Equal(t, rec.storedPE, out.ParseErrors, "a replay reports the parse errors stored at creation")
	assert.Empty(t, out.Diagnostics)
	assert.Equal(t, []string{}, out.Warnings, "same report: no warning")
	assert.Len(t, rec.gotRun.ReportSHA256, 64)

	rec.storedDigest = "digest-of-another-report"
	out, err = NewService(&fakeCatalog{}, rec, fakeAccess{}).IngestJUnit(context.Background(), meta, strings.NewReader(`<testsuite name="s"/>`))
	require.NoError(t, err)
	assert.Equal(t, []string{ReportDiffersWarning}, out.Warnings)

	rec.storedDigest, rec.storedStatus = "", execution.RunInterrupted
	out, err = NewService(&fakeCatalog{}, rec, fakeAccess{}).IngestJUnit(context.Background(), meta, strings.NewReader(`<testsuite name="s"/>`))
	require.NoError(t, err)
	assert.Equal(t, []string{`status "completed" differs from "interrupted", recorded for this attempt; it was not applied`}, out.Warnings)
}

func TestIngestWarnsWhenAReplayCarriesOtherMetadata(t *testing.T) {
	rec := &fakeRecorder{created: false, storedMeta: &[3]string{"ci", "release", "def"}}
	out, err := NewService(&fakeCatalog{}, rec, fakeAccess{}).IngestJUnit(context.Background(), meta, strings.NewReader(`<testsuite name="s"/>`))
	require.NoError(t, err)
	assert.Equal(t, []string{
		`branch "main" differs from "release", recorded for this attempt; it was not applied`,
		`commit "abc" differs from "def", recorded for this attempt; it was not applied`,
	}, out.Warnings)

	rec.created = true
	out, err = NewService(&fakeCatalog{}, rec, fakeAccess{}).IngestJUnit(context.Background(), meta, strings.NewReader(`<testsuite name="s"/>`))
	require.NoError(t, err)
	assert.Empty(t, out.Warnings, "a new run records the metadata it was sent")
}

func TestIngestWarnsWhenTheSuiteTimestampIsLaterThanIngestion(t *testing.T) {
	rec := &fakeRecorder{created: true} // the execution module left startedAt unknown
	out, err := NewService(&fakeCatalog{}, rec, fakeAccess{}).IngestJUnit(context.Background(), meta,
		strings.NewReader(`<testsuite name="s" timestamp="2099-01-01T00:00:00Z"/>`))
	require.NoError(t, err)
	assert.Equal(t, []string{"the report's suite timestamp 2099-01-01T00:00:00Z is later than the ingestion; startedAt is left unknown (clock skew, or a local time written without a zone)"}, out.Warnings)

	out, err = NewService(&fakeCatalog{}, rec, fakeAccess{}).IngestJUnit(context.Background(), meta,
		strings.NewReader(`<testsuite name="s" timestamp="2026-02-30T10:00:00"/>`))
	require.NoError(t, err)
	assert.Equal(t, []string{`suite timestamp "2026-02-30T10:00:00" could not be read; it does not set startedAt`}, out.Warnings)
	rec.created = false
	out, err = NewService(&fakeCatalog{}, rec, fakeAccess{}).IngestJUnit(context.Background(), meta,
		strings.NewReader(`<testsuite name="s" timestamp="2026-02-30T10:00:00"/>`))
	require.NoError(t, err)
	assert.Empty(t, out.Warnings, "a replay is not applied, so its report raises no notices")
	rec.created = true

	kept := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	rec.storedStart = &kept
	out, err = NewService(&fakeCatalog{}, rec, fakeAccess{}).IngestJUnit(context.Background(), meta,
		strings.NewReader(`<testsuite name="s" timestamp="2026-09-28T10:00:00Z"/>`))
	require.NoError(t, err)
	assert.Empty(t, out.Warnings)
}

func TestIngestPassesTheReportedRunStatus(t *testing.T) {
	rec := &fakeRecorder{created: true}
	m := meta
	m.Status = execution.RunCancelled
	out, err := NewService(&fakeCatalog{}, rec, fakeAccess{}).IngestJUnit(context.Background(), m, strings.NewReader(`<testsuite name="s"/>`))
	require.NoError(t, err)
	assert.Equal(t, execution.RunCancelled, rec.gotRun.Status)
	assert.Equal(t, execution.RunCancelled, out.Run.Status)
	assert.Empty(t, out.Warnings)
}

func TestValidateMeta(t *testing.T) {
	require.NoError(t, ValidateMeta(meta))
	bad := []RunMeta{
		{Provider: "", ProviderRunID: "1", RunAttempt: 1},
		{Provider: "Git Hub", ProviderRunID: "1", RunAttempt: 1},
		{Provider: "github", ProviderRunID: "a:b", RunAttempt: 1},
		{Provider: "github", ProviderRunID: "1", RunAttempt: 0},
		{Provider: "github", ProviderRunID: "1", RunAttempt: 1, Pipeline: strings.Repeat("p", 201)},
		{Provider: "github", ProviderRunID: "1", RunAttempt: 1, Branch: strings.Repeat("b", 256)},
		{Provider: "github", ProviderRunID: "1", RunAttempt: 1, Commit: strings.Repeat("c", 65)},
		{Provider: "github", ProviderRunID: "1", RunAttempt: 1, Pipeline: "p\x00"},
		{Provider: "github", ProviderRunID: "1", RunAttempt: 1, Branch: "\xff"},
		{Provider: "github", ProviderRunID: "1", RunAttempt: 1, Commit: "c\x00"},
		{Provider: "github", ProviderRunID: "1", RunAttempt: 1, Status: execution.RunStatus("running")},
		{Provider: "github", ProviderRunID: "1", RunAttempt: 1, Status: execution.RunStatus("failed")},
		{Provider: "GitHub", ProviderRunID: "1", RunAttempt: 1},
		{Provider: strings.Repeat("p", 51), ProviderRunID: "1", RunAttempt: 1},
		{Provider: "github", ProviderRunID: strings.Repeat("9", 101), RunAttempt: 1},
	}
	require.NoError(t, ValidateMeta(RunMeta{Provider: strings.Repeat("p", 50), ProviderRunID: strings.Repeat("9", 100), RunAttempt: 1}),
		"the limits themselves are valid")
	for _, m := range bad {
		e, ok := apperr.As(ValidateMeta(m))
		require.True(t, ok, "%+v", m)
		assert.Equal(t, apperr.KindValidation, e.Kind)
	}
}

func TestIngestErrors(t *testing.T) {
	ctx := context.Background()
	_, err := NewService(&fakeCatalog{}, &fakeRecorder{}, fakeAccess{}).IngestJUnit(ctx, RunMeta{}, strings.NewReader(report))
	assert.Error(t, err)

	_, err = NewService(&fakeCatalog{}, &fakeRecorder{}, fakeAccess{}).IngestJUnit(ctx, meta, strings.NewReader("<nope"))
	e, ok := apperr.As(err)
	require.True(t, ok)
	assert.Equal(t, apperr.KindInvalidDocument, e.Kind)

	tooLarge := &http.MaxBytesError{Limit: 1}
	_, err = NewService(&fakeCatalog{}, &fakeRecorder{}, fakeAccess{}).IngestJUnit(ctx, meta, errReader{tooLarge})
	assert.ErrorAs(t, err, &tooLarge, "read errors (e.g. body too large) are returned as is")

	_, err = NewService(&fakeCatalog{viewErr: errBoom}, &fakeRecorder{}, fakeAccess{}).IngestJUnit(ctx, meta, strings.NewReader(report))
	assert.ErrorIs(t, err, errBoom)
	_, err = NewService(&fakeCatalog{}, &fakeRecorder{recordErr: errBoom}, fakeAccess{}).IngestJUnit(ctx, meta, strings.NewReader(report))
	assert.ErrorIs(t, err, errBoom)
	_, err = NewService(&fakeCatalog{}, &fakeRecorder{diagErr: errBoom}, fakeAccess{}).IngestJUnit(ctx, meta, strings.NewReader(report))
	assert.ErrorIs(t, err, errBoom)
	_, err = NewService(&fakeCatalog{}, &fakeRecorder{parseErr: errBoom}, fakeAccess{}).IngestJUnit(ctx, meta, strings.NewReader(report))
	assert.ErrorIs(t, err, errBoom)
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

var _ io.Reader = errReader{}

// A run belongs to the project named by ?project=; references with its key or
// bare numbers correlate in it, another project's key is a wrong_project
// diagnostic, and the name fallback only looks for the run's own key.
func TestIngestIntoAProject(t *testing.T) {
	cat := &fakeCatalog{universe: []int64{5}, statuses: map[int64]catalog.Status{5: catalog.StatusActive, 7: catalog.StatusActive}}
	rec := &fakeRecorder{created: true}
	svc := NewService(cat, rec, fakeAccess{})
	m := meta
	m.ProjectKey = "CHK"
	xml := `<testsuite name="s">
<testcase name="a"><properties><property name="tc-id" value="CHK-5"/></properties></testcase>
<testcase name="b"><properties><property name="tc-id" value="chk-7"/></properties></testcase>
<testcase name="c"><properties><property name="tc-id" value="5"/></properties></testcase>
<testcase name="d"><properties><property name="tc-id" value="WEB-5"/></properties></testcase>
<testcase name="CHK-7 by name"/>
<testcase name="TC-7 is not this project's key"/>
</testsuite>`
	_, err := svc.IngestJUnit(context.Background(), m, strings.NewReader(xml))
	require.NoError(t, err)
	assert.Equal(t, int64(2), cat.gotProject)
	assert.Equal(t, int64(2), rec.gotRun.ProjectID)
	assert.Equal(t, []int64{5, 7, 5, 7}, cat.askedIDs, "another project's numbers are not looked up")
	var got []execution.Correlation
	for _, r := range rec.gotResults {
		got = append(got, r.Correlation)
	}
	assert.Equal(t, []execution.Correlation{
		execution.CorrelationValid, execution.CorrelationValid, execution.CorrelationValid,
		execution.CorrelationWrongProject, execution.CorrelationValid, execution.CorrelationMissing,
	}, got)

	// No project: the default project TC.
	_, err = svc.IngestJUnit(context.Background(), meta, strings.NewReader(`<testsuite><testcase name="TC-5"/></testsuite>`))
	require.NoError(t, err)
	assert.Equal(t, int64(1), cat.gotProject)

	cat.projectErr = apperr.NotFound("project NOPE not found")
	m.ProjectKey = "NOPE"
	_, err = svc.IngestJUnit(context.Background(), m, strings.NewReader(`<testsuite/>`))
	e, ok := apperr.As(err)
	require.True(t, ok)
	assert.Equal(t, apperr.KindNotFound, e.Kind)

	m.ProjectKey = "chk"
	assert.Error(t, ValidateMeta(m), "the key is validated before any lookup")
}

// Who may report: an API key reports into its own project (the default when ?project= is
// absent); people need the member role; a project the caller cannot see is "not found".
func TestIngestAccess(t *testing.T) {
	ctx := context.Background()
	report := `<testsuite><testcase name="t"/></testsuite>`
	cat := &fakeCatalog{}
	rec := &fakeRecorder{created: true}

	_, err := NewService(cat, rec, fakeAccess{key: 2}).IngestJUnit(ctx, meta, strings.NewReader(report))
	require.NoError(t, err)
	assert.Equal(t, int64(2), rec.gotRun.ProjectID, "a key's runs go to its project")

	m := meta
	m.ProjectKey = "CHK"
	_, err = NewService(cat, rec, fakeAccess{key: 2}).IngestJUnit(ctx, m, strings.NewReader(report))
	require.NoError(t, err, "naming the key's own project is fine")

	_, err = NewService(cat, rec, fakeAccess{key: 2, denied: map[int64]error{1: nil}}).IngestJUnit(ctx, func() RunMeta { m := meta; m.ProjectKey = "TC"; return m }(), strings.NewReader(report))
	e, ok := apperr.As(err)
	require.True(t, ok)
	assert.Equal(t, apperr.KindNotFound, e.Kind)
	assert.Equal(t, "project TC not found", e.Message, "another project looks unknown")

	forbidden := apperr.Forbidden("needs member")
	_, err = NewService(cat, rec, fakeAccess{denied: map[int64]error{1: forbidden}}).IngestJUnit(ctx, meta, strings.NewReader(report))
	assert.Equal(t, forbidden, err, "a viewer cannot report")

	cat.projectErr = errBoom
	_, err = NewService(cat, rec, fakeAccess{key: 2}).IngestJUnit(ctx, meta, strings.NewReader(report))
	assert.ErrorIs(t, err, errBoom)
}

func TestWrongProjectDiagnosticMessage(t *testing.T) {
	webRef := "WEB-5"
	msg := diagnosticMessage(execution.Diagnostic{Correlation: execution.CorrelationWrongProject, RequestedTestCaseID: &webRef})
	assert.Contains(t, msg, `"WEB-5" belongs to another project`)
}

// MVP D2: a run reported for a suite expects the suite's selection among the active automated test cases; results
// outside it stay valid but outside the universe. The run records the suite, and a replay naming another suite warns.
func TestIngestForSuite(t *testing.T) {
	cat := &fakeCatalog{universe: []int64{153, 154, 155}, selection: []int64{154, 155, 999}, statuses: map[int64]catalog.Status{153: catalog.StatusActive}}
	rec := &fakeRecorder{created: true}
	m := meta
	m.SuiteKey = "smoke"
	out, err := NewService(cat, rec, fakeAccess{}).IngestJUnit(context.Background(), m, strings.NewReader(report))
	require.NoError(t, err)
	assert.Equal(t, "smoke", cat.gotSuite)
	assert.Equal(t, []int64{154, 155}, rec.gotExp, "999 is not active and automated in the snapshot")
	assert.Equal(t, "smoke", rec.gotRun.SuiteKey)
	assert.Equal(t, "Suite smoke", rec.gotRun.SuiteName)
	for _, w := range out.Warnings {
		assert.NotContains(t, w, "suite \"", "the same suite does not warn")
	}

	rec = &fakeRecorder{storedSuite: ptrTo("")}
	out, err = NewService(cat, rec, fakeAccess{}).IngestJUnit(context.Background(), m, strings.NewReader(report))
	require.NoError(t, err)
	assert.Contains(t, out.Warnings, `suite "smoke" differs from "", recorded for this attempt; it was not applied`)

	cat.suiteErr = apperr.Conflict("suite smoke is archived")
	_, err = NewService(cat, &fakeRecorder{}, fakeAccess{}).IngestJUnit(context.Background(), m, strings.NewReader(report))
	assert.ErrorContains(t, err, "archived")

	m.SuiteKey = "Smoke"
	_, err = NewService(cat, &fakeRecorder{}, fakeAccess{}).IngestJUnit(context.Background(), m, strings.NewReader(report))
	e, ok := apperr.As(err)
	require.True(t, ok)
	assert.Equal(t, "suite", e.Fields[0].Field)
}

func ptrTo[T any](v T) *T { return &v }
