package execution

import (
	"context"
	"sort"
	"time"
)

type fakeRepo struct {
	runs     map[int64]TestRun
	byExt    map[string]int64
	expected map[int64][]int64
	results  map[int64][]TestResult
	nextRun  int64
	nextRes  int64
	errs     map[string]error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{runs: map[int64]TestRun{}, byExt: map[string]int64{}, expected: map[int64][]int64{},
		results: map[int64][]TestResult{}, errs: map[string]error{}}
}

func (f *fakeRepo) InsertTestRun(_ context.Context, p InsertRunParams) (int64, bool, error) {
	if err := f.errs["InsertTestRun"]; err != nil {
		return 0, false, err
	}
	if _, ok := f.byExt[p.ExternalRunID]; ok {
		return 0, false, nil
	}
	f.nextRun++
	completed := p.CompletedAt
	f.runs[f.nextRun] = TestRun{ID: f.nextRun, ExternalRunID: p.ExternalRunID, Provider: p.Provider, ProviderRunID: p.ProviderRunID,
		RunAttempt: p.RunAttempt, Pipeline: p.Pipeline, Branch: p.Branch, Commit: p.Commit, Status: p.Status,
		StartedAt: p.StartedAt, CompletedAt: &completed, CreatedAt: completed}
	f.byExt[p.ExternalRunID] = f.nextRun
	return f.nextRun, true, nil
}

func (f *fakeRepo) GetTestRunIDByExternalID(_ context.Context, ext string) (int64, error) {
	if err := f.errs["GetTestRunIDByExternalID"]; err != nil {
		return 0, err
	}
	return f.byExt[ext], nil
}

func (f *fakeRepo) InsertExpectedCases(_ context.Context, runID int64, ids []int64) error {
	if err := f.errs["InsertExpectedCases"]; err != nil {
		return err
	}
	f.expected[runID] = append([]int64(nil), ids...)
	return nil
}

func (f *fakeRepo) InsertTestResults(_ context.Context, runID int64, rs []NewResult) error {
	if err := f.errs["InsertTestResults"]; err != nil {
		return err
	}
	for _, r := range rs {
		f.nextRes++
		f.results[runID] = append(f.results[runID], TestResult{ID: f.nextRes, TestRunID: runID, TestCaseID: r.TestCaseID,
			RequestedTestCaseID: r.RequestedTestCaseID, Correlation: r.Correlation, TestName: r.TestName, Status: r.Status})
	}
	return nil
}

func (f *fakeRepo) GetTestRun(_ context.Context, id int64) (TestRun, error) {
	if err := f.errs["GetTestRun"]; err != nil {
		return TestRun{}, err
	}
	r, ok := f.runs[id]
	if !ok {
		return TestRun{}, ErrNotFound
	}
	r.ExpectedCount = int32(len(f.expected[id]))
	r.ResultCount = int32(len(f.results[id]))
	return r, nil
}

func (f *fakeRepo) ListTestRuns(ctx context.Context, limit, offset int32) ([]TestRun, error) {
	if err := f.errs["ListTestRuns"]; err != nil {
		return nil, err
	}
	var out []TestRun
	for id := range f.runs {
		r, _ := f.GetTestRun(ctx, id)
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if int(offset) >= len(out) {
		return []TestRun{}, nil
	}
	return out[offset:min(len(out), int(offset+limit))], nil
}

func (f *fakeRepo) CountTestRuns(context.Context) (int64, error) {
	if err := f.errs["CountTestRuns"]; err != nil {
		return 0, err
	}
	return int64(len(f.runs)), nil
}

func (f *fakeRepo) match(r TestResult, flt ResultFilter) bool {
	return (flt.Status == nil || r.Status == *flt.Status) && (flt.Correlation == nil || r.Correlation == *flt.Correlation)
}

func (f *fakeRepo) ListRunResults(_ context.Context, runID int64, flt ResultFilter, limit, offset int32) ([]TestResult, error) {
	if err := f.errs["ListRunResults"]; err != nil {
		return nil, err
	}
	var out []TestResult
	for _, r := range f.results[runID] {
		if f.match(r, flt) {
			out = append(out, r)
		}
	}
	if int(offset) >= len(out) {
		return []TestResult{}, nil
	}
	return out[offset:min(len(out), int(offset+limit))], nil
}

func (f *fakeRepo) CountRunResults(_ context.Context, runID int64, flt ResultFilter) (int64, error) {
	if err := f.errs["CountRunResults"]; err != nil {
		return 0, err
	}
	n := 0
	for _, r := range f.results[runID] {
		if f.match(r, flt) {
			n++
		}
	}
	return int64(n), nil
}

func (f *fakeRepo) ListExpectedCaseIDs(_ context.Context, runID int64) ([]int64, error) {
	if err := f.errs["ListExpectedCaseIDs"]; err != nil {
		return nil, err
	}
	return f.expected[runID], nil
}

func (f *fakeRepo) ListValidResults(_ context.Context, runID int64) ([]ValidResult, error) {
	if err := f.errs["ListValidResults"]; err != nil {
		return nil, err
	}
	var out []ValidResult
	for _, r := range f.results[runID] {
		if r.Correlation == CorrelationValid {
			out = append(out, ValidResult{TestCaseID: *r.TestCaseID, Status: r.Status})
		}
	}
	return out, nil
}

func (f *fakeRepo) ListDiagnostics(_ context.Context, runID int64) ([]Diagnostic, error) {
	if err := f.errs["ListDiagnostics"]; err != nil {
		return nil, err
	}
	var out []Diagnostic
	for _, r := range f.results[runID] {
		if r.Correlation != CorrelationValid {
			out = append(out, Diagnostic{TestName: r.TestName, Correlation: r.Correlation, RequestedTestCaseID: r.RequestedTestCaseID})
		}
	}
	return out, nil
}

func (f *fakeRepo) ListResultsForTestCase(ctx context.Context, tcID int64, limit, offset int32) ([]HistoryEntry, error) {
	if err := f.errs["ListResultsForTestCase"]; err != nil {
		return nil, err
	}
	var out []HistoryEntry
	for runID := f.nextRun; runID >= 1; runID-- {
		for _, r := range f.results[runID] {
			if r.TestCaseID != nil && *r.TestCaseID == tcID {
				run, _ := f.GetTestRun(ctx, runID)
				out = append(out, HistoryEntry{Result: r, Run: run})
			}
		}
	}
	if int(offset) >= len(out) {
		return []HistoryEntry{}, nil
	}
	return out[offset:min(len(out), int(offset+limit))], nil
}

func (f *fakeRepo) CountResultsForTestCase(ctx context.Context, tcID int64) (int64, error) {
	if err := f.errs["CountResultsForTestCase"]; err != nil {
		return 0, err
	}
	all, _ := f.ListResultsForTestCase(ctx, tcID, 1<<30, 0)
	return int64(len(all)), nil
}

func (f *fakeRepo) InTx(_ context.Context, fn func(Repository) error) error {
	if err := f.errs["InTx"]; err != nil {
		return err
	}
	return fn(f)
}

var fixedNow = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
