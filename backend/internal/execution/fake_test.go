package execution

import (
	"context"
	"slices"
	"sort"
	"time"
)

type fakeRepo struct {
	runs     map[int64]TestRun
	byExt    map[string]int64
	expected map[int64][]int64
	results  map[int64][]TestResult
	parseErr map[int64][]ParseError
	amended  map[int64][]Amendment
	nextRun  int64
	nextRes  int64
	errs     map[string]error
	// summaryReads records the run ids of every ListSummaryInputs call.
	summaryReads [][]int64
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{runs: map[int64]TestRun{}, byExt: map[string]int64{}, expected: map[int64][]int64{},
		results: map[int64][]TestResult{}, parseErr: map[int64][]ParseError{}, amended: map[int64][]Amendment{}, errs: map[string]error{}}
}

func (f *fakeRepo) InsertTestRun(_ context.Context, p InsertRunParams) (int64, bool, error) {
	if err := f.errs["InsertTestRun"]; err != nil {
		return 0, false, err
	}
	ext := extKey(p.ProjectID, p.ExternalRunID)
	if _, ok := f.byExt[ext]; ok {
		return 0, false, nil
	}
	f.nextRun++
	created := time.Now()
	if p.CompletedAt != nil {
		created = *p.CompletedAt
	}
	f.runs[f.nextRun] = TestRun{ID: f.nextRun, ProjectID: p.ProjectID, ExternalRunID: p.ExternalRunID, Provider: p.Provider, ProviderRunID: p.ProviderRunID,
		RunAttempt: p.RunAttempt, Pipeline: p.Pipeline, Branch: p.Branch, Commit: p.Commit, Status: p.Status,
		StartedAt: p.StartedAt, CompletedAt: p.CompletedAt, CreatedAt: created, SuiteKey: p.SuiteKey, SuiteName: p.SuiteName,
		Mode: p.Mode, StartedBy: p.StartedBy}
	f.byExt[ext] = f.nextRun
	return f.nextRun, true, nil
}

// extKey is the per-project uniqueness key of an externalRunId.
func extKey(projectID int64, ext string) string { return string(rune('0'+projectID)) + "|" + ext }

func (f *fakeRepo) GetTestRunIDByExternalID(_ context.Context, projectID int64, ext string) (int64, error) {
	if err := f.errs["GetTestRunIDByExternalID"]; err != nil {
		return 0, err
	}
	return f.byExt[extKey(projectID, ext)], nil
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

func (f *fakeRepo) InsertParseErrors(_ context.Context, runID int64, errs []ParseError) error {
	if err := f.errs["InsertParseErrors"]; err != nil {
		return err
	}
	f.parseErr[runID] = append(f.parseErr[runID], errs...)
	return nil
}

func (f *fakeRepo) ListParseErrors(_ context.Context, runID int64, limit, offset int32) ([]ParseError, error) {
	if err := f.errs["ListParseErrors"]; err != nil {
		return nil, err
	}
	all := f.parseErr[runID]
	if int(offset) >= len(all) {
		return []ParseError{}, nil
	}
	return all[offset:min(len(all), int(offset)+int(limit))], nil
}

func (f *fakeRepo) CountParseErrors(_ context.Context, runID int64) (int64, error) {
	if err := f.errs["CountParseErrors"]; err != nil {
		return 0, err
	}
	return int64(len(f.parseErr[runID])), nil
}

func (f *fakeRepo) GetTestRun(_ context.Context, id int64) (TestRun, error) {
	if err := f.errs["GetTestRun"]; err != nil {
		return TestRun{}, err
	}
	r, ok := f.runs[id]
	if !ok {
		return TestRun{}, ErrNotFound
	}
	r.ExpectedCount = int32(len(f.expected[id]) + len(f.amended[id]))
	r.ResultCount = int32(len(f.results[id]))
	r.AmendmentCount = int32(len(f.amended[id]))
	return r, nil
}

func (f *fakeRepo) ListTestRuns(ctx context.Context, flt RunFilter, limit, offset int32) ([]TestRun, error) {
	if err := f.errs["ListTestRuns"]; err != nil {
		return nil, err
	}
	var out []TestRun
	for id, run := range f.runs {
		if !runMatches(run, flt) {
			continue
		}
		r, _ := f.GetTestRun(ctx, id)
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if int(offset) >= len(out) {
		return []TestRun{}, nil
	}
	return out[offset:min(len(out), int(offset+limit))], nil
}

func runMatches(run TestRun, flt RunFilter) bool {
	return (flt.ProjectIDs == nil || slices.Contains(flt.ProjectIDs, run.ProjectID)) && (flt.SuiteKey == nil || run.SuiteKey == *flt.SuiteKey)
}

func (f *fakeRepo) CountTestRuns(_ context.Context, flt RunFilter) (int64, error) {
	if err := f.errs["CountTestRuns"]; err != nil {
		return 0, err
	}
	n := 0
	for _, run := range f.runs {
		if runMatches(run, flt) {
			n++
		}
	}
	return int64(n), nil
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

func (f *fakeRepo) ListSummaryInputs(_ context.Context, runIDs []int64) (map[int64]SummaryInputs, error) {
	if err := f.errs["ListSummaryInputs"]; err != nil {
		return nil, err
	}
	f.summaryReads = append(f.summaryReads, runIDs)
	out := map[int64]SummaryInputs{}
	for _, id := range runIDs {
		in := SummaryInputs{Expected: f.expected[id]}
		for _, a := range f.amended[id] {
			in.Amended = append(in.Amended, a.TestCaseID)
		}
		for _, r := range f.results[id] {
			if r.Correlation == CorrelationValid {
				in.Valid = append(in.Valid, ValidResult{TestCaseID: *r.TestCaseID, Status: r.Status})
			}
		}
		out[id] = in
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

func (f *fakeRepo) InsertAmendment(_ context.Context, a NewAmendment) (Amendment, error) {
	if err := f.errs["InsertAmendment"]; err != nil {
		return Amendment{}, err
	}
	for _, x := range f.amended[a.TestRunID] {
		if x.TestCaseID == a.TestCaseID {
			return Amendment{}, ErrConflict
		}
	}
	out := Amendment{ID: int64(len(f.amended[a.TestRunID]) + 1), TestRunID: a.TestRunID, TestCaseID: a.TestCaseID, AmendedBy: a.AmendedBy,
		AmendedByUsername: a.AmendedByUsername, Reason: a.Reason, CreatedAt: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)}
	f.amended[a.TestRunID] = append(f.amended[a.TestRunID], out)
	return out, nil
}

func (f *fakeRepo) ListAmendments(_ context.Context, runID int64, limit, offset int32) ([]Amendment, error) {
	if err := f.errs["ListAmendments"]; err != nil {
		return nil, err
	}
	all := f.amended[runID]
	start := min(int(offset), len(all))
	return all[start:min(start+int(limit), len(all))], nil
}

func (f *fakeRepo) CountAmendments(_ context.Context, runID int64) (int64, error) {
	if err := f.errs["CountAmendments"]; err != nil {
		return 0, err
	}
	return int64(len(f.amended[runID])), nil
}

func (f *fakeRepo) LockTestRun(_ context.Context, id int64) (RunStatus, RunMode, error) {
	if err := f.errs["LockTestRun"]; err != nil {
		return "", "", err
	}
	r, ok := f.runs[id]
	if !ok {
		return "", "", ErrNotFound
	}
	return r.Status, r.Mode, nil
}

func (f *fakeRepo) IsInUniverse(_ context.Context, runID, tcID int64) (bool, error) {
	if err := f.errs["IsInUniverse"]; err != nil {
		return false, err
	}
	for _, a := range f.amended[runID] {
		if a.TestCaseID == tcID {
			return true, nil
		}
	}
	return slices.Contains(f.expected[runID], tcID), nil
}

func (f *fakeRepo) InsertManualResult(_ context.Context, runID int64, r NewResult) (TestResult, error) {
	if err := f.errs["InsertManualResult"]; err != nil {
		return TestResult{}, err
	}
	attempt := int32(1)
	for _, x := range f.results[runID] {
		if x.TestName == r.TestName && x.Attempt >= attempt {
			attempt = x.Attempt + 1
		}
	}
	f.nextRes++
	res := TestResult{ID: f.nextRes, TestRunID: runID, TestCaseID: r.TestCaseID, RequestedTestCaseID: r.RequestedTestCaseID,
		Correlation: CorrelationValid, TestName: r.TestName, ClassName: ManualClass, Status: r.Status, Attempt: attempt,
		ErrorMessage: r.ErrorMessage, RecordedBy: r.RecordedBy, FailedStep: r.FailedStep}
	f.results[runID] = append(f.results[runID], res)
	return res, nil
}

func (f *fakeRepo) FinishTestRun(_ context.Context, id int64, status RunStatus) error {
	if err := f.errs["FinishTestRun"]; err != nil {
		return err
	}
	r := f.runs[id]
	now := time.Now()
	r.Status, r.CompletedAt = status, &now
	f.runs[id] = r
	return nil
}

func (f *fakeRepo) ListLatestResults(_ context.Context, ids []int64) ([]ValidResult, error) {
	if err := f.errs["ListLatestResults"]; err != nil {
		return nil, err
	}
	var out []ValidResult
	for _, id := range ids {
		latest := int64(0)
		for runID, rs := range f.results {
			for _, r := range rs {
				if r.TestCaseID != nil && *r.TestCaseID == id && r.Correlation == CorrelationValid && runID > latest {
					latest = runID
				}
			}
		}
		for _, r := range f.results[latest] {
			if r.TestCaseID != nil && *r.TestCaseID == id && r.Correlation == CorrelationValid {
				out = append(out, ValidResult{TestCaseID: id, Status: r.Status, Execution: r.TestName, Attempt: r.Attempt})
			}
		}
	}
	return out, nil
}

func (f *fakeRepo) ListLatestConclusive(_ context.Context, ids []int64) ([]Conclusive, error) {
	if err := f.errs["ListLatestConclusive"]; err != nil {
		return nil, err
	}
	var runIDs []int64
	for runID := range f.results {
		runIDs = append(runIDs, runID)
	}
	slices.Sort(runIDs)
	slices.Reverse(runIDs)
	var out []Conclusive
	for _, id := range ids {
		for _, runID := range runIDs {
			var vs []ValidResult
			for _, r := range f.results[runID] {
				if r.TestCaseID != nil && *r.TestCaseID == id && r.Correlation == CorrelationValid {
					vs = append(vs, ValidResult{TestCaseID: id, Status: r.Status, Execution: r.TestName, Attempt: r.Attempt})
				}
			}
			if statuses, _ := logical(vs); len(statuses) > 0 && Aggregate(statuses) != SummaryStatus(Skipped) {
				out = append(out, Conclusive{TestCaseID: id, RunID: runID, Status: ResultStatus(Aggregate(statuses))})
				break
			}
		}
	}
	return out, nil
}
