package ingestion

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

var errBoom = errors.New("boom")

type fakeCatalog struct {
	universe    []int64
	statuses    map[int64]catalog.Status
	universeErr error
	statusErr   error
	askedIDs    []int64
}

func (f *fakeCatalog) ExpectedUniverse(context.Context) ([]int64, error) {
	return f.universe, f.universeErr
}
func (f *fakeCatalog) Statuses(_ context.Context, ids []int64) (map[int64]catalog.Status, error) {
	f.askedIDs = ids
	return f.statuses, f.statusErr
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
}

func (f *fakeRecorder) RecordRun(_ context.Context, run execution.NewRun, exp []int64, rs []execution.NewResult, pe []execution.ParseError) (execution.TestRun, bool, error) {
	f.gotRun, f.gotExp, f.gotResults, f.gotParse = run, exp, rs, pe
	digest := run.ReportSHA256
	if f.storedDigest != "" {
		digest = f.storedDigest
	}
	return execution.TestRun{ID: 1, ExternalRunID: execution.ExternalRunID(run.Provider, run.ProviderRunID, run.RunAttempt),
		ResultCount: int32(len(rs)), ReportSHA256: digest}, f.created, f.recordErr
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
	out, err := NewService(cat, rec).IngestJUnit(context.Background(), meta, strings.NewReader(report))
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
	out, err := NewService(&fakeCatalog{}, rec).IngestJUnit(context.Background(), meta, strings.NewReader(`<testsuite name="s"/>`))
	require.NoError(t, err)
	assert.False(t, out.Created)
	assert.Equal(t, rec.storedPE, out.ParseErrors, "a replay reports the parse errors stored at creation")
	assert.Empty(t, out.Diagnostics)
	assert.Equal(t, []string{}, out.Warnings, "same report: no warning")
	assert.Len(t, rec.gotRun.ReportSHA256, 64)

	rec.storedDigest = "digest-of-another-report"
	out, err = NewService(&fakeCatalog{}, rec).IngestJUnit(context.Background(), meta, strings.NewReader(`<testsuite name="s"/>`))
	require.NoError(t, err)
	assert.Equal(t, []string{ReportDiffersWarning}, out.Warnings)
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
	}
	for _, m := range bad {
		e, ok := apperr.As(ValidateMeta(m))
		require.True(t, ok, "%+v", m)
		assert.Equal(t, apperr.KindValidation, e.Kind)
	}
}

func TestIngestErrors(t *testing.T) {
	ctx := context.Background()
	_, err := NewService(&fakeCatalog{}, &fakeRecorder{}).IngestJUnit(ctx, RunMeta{}, strings.NewReader(report))
	assert.Error(t, err)

	_, err = NewService(&fakeCatalog{}, &fakeRecorder{}).IngestJUnit(ctx, meta, strings.NewReader("<nope"))
	e, ok := apperr.As(err)
	require.True(t, ok)
	assert.Equal(t, apperr.KindInvalidDocument, e.Kind)

	tooLarge := &http.MaxBytesError{Limit: 1}
	_, err = NewService(&fakeCatalog{}, &fakeRecorder{}).IngestJUnit(ctx, meta, errReader{tooLarge})
	assert.ErrorAs(t, err, &tooLarge, "read errors (e.g. body too large) are returned as is")

	_, err = NewService(&fakeCatalog{statusErr: errBoom}, &fakeRecorder{}).IngestJUnit(ctx, meta, strings.NewReader(report))
	assert.ErrorIs(t, err, errBoom)
	_, err = NewService(&fakeCatalog{universeErr: errBoom}, &fakeRecorder{}).IngestJUnit(ctx, meta, strings.NewReader(report))
	assert.ErrorIs(t, err, errBoom)
	_, err = NewService(&fakeCatalog{}, &fakeRecorder{recordErr: errBoom}).IngestJUnit(ctx, meta, strings.NewReader(report))
	assert.ErrorIs(t, err, errBoom)
	_, err = NewService(&fakeCatalog{}, &fakeRecorder{diagErr: errBoom}).IngestJUnit(ctx, meta, strings.NewReader(report))
	assert.ErrorIs(t, err, errBoom)
	_, err = NewService(&fakeCatalog{}, &fakeRecorder{parseErr: errBoom}).IngestJUnit(ctx, meta, strings.NewReader(report))
	assert.ErrorIs(t, err, errBoom)
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

var _ io.Reader = errReader{}
