package execution

import (
	"math"
	"slices"
	"sort"
	"strconv"
)

// SummaryStatus is the aggregated status of a TC-ID within a run.
type SummaryStatus string

// Untested is derived: a TC-ID of the snapshot without any result in the run.
const Untested SummaryStatus = "untested"

// precedence is the aggregation precedence used only for the summary. It is a
// product policy, not an objective severity ranking: failed > error > skipped > passed.
var precedence = map[ResultStatus]int{Failed: 4, Error: 3, Skipped: 2, Passed: 1}

// Aggregate returns the aggregated status of several results of one TC-ID.
func Aggregate(statuses []ResultStatus) SummaryStatus {
	if len(statuses) == 0 {
		return Untested
	}
	worst := statuses[0]
	for _, s := range statuses[1:] {
		if precedence[s] > precedence[worst] {
			worst = s
		}
	}
	return SummaryStatus(worst)
}

// StatusCounts counts TC-IDs per aggregated status.
type StatusCounts struct {
	Untested, Passed, Failed, Error, Skipped int32
}

// StatusPercentages are percentages over the expected universe.
type StatusPercentages struct {
	Untested, Passed, Failed, Error, Skipped float64
}

// ExecutedPercentages are percentages over executed TC-IDs only.
type ExecutedPercentages struct {
	Passed, Failed, Error, Skipped float64
}

// DiagnosticCounts counts results excluded because their TC-ID is not valid.
type DiagnosticCounts struct {
	Missing, Malformed, Unknown, Deprecated, WrongProject, Total int32
}

// TestCaseOutcome is the aggregated outcome of one snapshot TC-ID.
type TestCaseOutcome struct {
	TestCaseID  int64
	Status      SummaryStatus
	ResultCount int32
	// Flaky: one of its tests passed on a retry after failed attempts (D1).
	Flaky bool
}

// Verdict is the test outcome of a run, derived from its summary.
type Verdict string

// Verdicts, by precedence: no_tests when the expected universe is empty,
// failed when any TC-ID failed or errored, incomplete when any is untested or
// skipped, passed otherwise.
const (
	VerdictPassed     Verdict = "passed"
	VerdictFailed     Verdict = "failed"
	VerdictIncomplete Verdict = "incomplete"
	VerdictNoTests    Verdict = "no_tests"
)

// RunOutcome is the test outcome of a run: its verdict, the counts behind it
// and the pass rate over executed TC-IDs.
type RunOutcome struct {
	Verdict                                            Verdict
	Executed, Passed, Failed, Error, Skipped, Untested int32
	PassRate                                           float64
	// Flaky counts the TC-IDs that passed only on a retry.
	Flaky int32
}

// Outcome derives the run outcome from the summary.
func (s Summary) Outcome() RunOutcome {
	o := RunOutcome{
		Executed: s.ExecutedTotal, Passed: s.Counts.Passed, Failed: s.Counts.Failed, Error: s.Counts.Error,
		Skipped: s.Counts.Skipped, Untested: s.Counts.Untested, PassRate: s.PercentOfExecuted.Passed, Flaky: s.Flaky,
	}
	switch {
	case s.ExpectedTotal == 0:
		o.Verdict = VerdictNoTests
	case o.Failed+o.Error > 0:
		o.Verdict = VerdictFailed
	case o.Untested+o.Skipped > 0:
		o.Verdict = VerdictIncomplete
	default:
		o.Verdict = VerdictPassed
	}
	return o
}

// Summary is the snapshot-based summary of a run.
type Summary struct {
	TestRunID         int64
	ExpectedTotal     int32
	ExecutedTotal     int32
	Counts            StatusCounts
	PercentOfExpected StatusPercentages
	PercentOfExecuted ExecutedPercentages
	ExecutionPercent  float64
	Diagnostics       DiagnosticCounts
	OutsideUniverse   int32
	// Flaky counts the TC-IDs of the universe that passed only on a retry (D1).
	Flaky int32
	// OutsideUniverseIDs are the distinct TC-IDs behind OutsideUniverse, ascending.
	OutsideUniverseIDs []int64
	TestCases          []TestCaseOutcome
	// SnapshotTotal is the size of the snapshot frozen at creation; ExpectedTotal adds the amendments.
	SnapshotTotal int32
	// AmendedIDs are the TC-IDs added to the universe after creation (DEC-42), ascending.
	AmendedIDs []int64
}

// percentPrecision keeps 6 decimals: clients sum the precise values and round
// only for display, so e.g. 3 x 33.333333 is shown as a 100% total.
const percentPrecision = 1e6

func percent(part, total int32) float64 {
	if total == 0 {
		return 0
	}
	return math.Round(float64(part)*100*percentPrecision/float64(total)) / percentPrecision
}

// ComputeSummary derives the summary of a run from its immutable snapshot
// (expected), its valid results and its diagnostic results. Valid results for
// TC-IDs outside the snapshot are counted in OutsideUniverse and excluded.
func ComputeSummary(runID int64, expected []int64, valid []ValidResult, diagnostics []Diagnostic) Summary {
	byCase := make(map[int64][]ValidResult, len(expected))
	for _, id := range expected {
		byCase[id] = nil
	}
	s := Summary{TestRunID: runID, ExpectedTotal: int32(len(expected)), SnapshotTotal: int32(len(expected)), OutsideUniverseIDs: []int64{}, AmendedIDs: []int64{}}
	outside := map[int64]bool{}
	for _, r := range valid {
		if _, ok := byCase[r.TestCaseID]; !ok {
			s.OutsideUniverse++
			if !outside[r.TestCaseID] {
				outside[r.TestCaseID] = true
				s.OutsideUniverseIDs = append(s.OutsideUniverseIDs, r.TestCaseID)
			}
			continue
		}
		byCase[r.TestCaseID] = append(byCase[r.TestCaseID], r)
	}
	sort.Slice(s.OutsideUniverseIDs, func(i, j int) bool { return s.OutsideUniverseIDs[i] < s.OutsideUniverseIDs[j] })
	ids := append([]int64(nil), expected...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	s.TestCases = make([]TestCaseOutcome, 0, len(ids))
	for _, id := range ids {
		statuses, flaky := logical(byCase[id])
		agg := Aggregate(statuses)
		s.TestCases = append(s.TestCases, TestCaseOutcome{TestCaseID: id, Status: agg, ResultCount: int32(len(byCase[id])), Flaky: flaky})
		if flaky {
			s.Flaky++
		}
		switch agg {
		case Untested:
			s.Counts.Untested++
		case SummaryStatus(Passed):
			s.Counts.Passed++
		case SummaryStatus(Failed):
			s.Counts.Failed++
		case SummaryStatus(Error):
			s.Counts.Error++
		case SummaryStatus(Skipped):
			s.Counts.Skipped++
		}
	}
	s.ExecutedTotal = s.ExpectedTotal - s.Counts.Untested
	s.PercentOfExpected = StatusPercentages{
		Untested: percent(s.Counts.Untested, s.ExpectedTotal),
		Passed:   percent(s.Counts.Passed, s.ExpectedTotal),
		Failed:   percent(s.Counts.Failed, s.ExpectedTotal),
		Error:    percent(s.Counts.Error, s.ExpectedTotal),
		Skipped:  percent(s.Counts.Skipped, s.ExpectedTotal),
	}
	s.PercentOfExecuted = ExecutedPercentages{
		Passed:  percent(s.Counts.Passed, s.ExecutedTotal),
		Failed:  percent(s.Counts.Failed, s.ExecutedTotal),
		Error:   percent(s.Counts.Error, s.ExecutedTotal),
		Skipped: percent(s.Counts.Skipped, s.ExecutedTotal),
	}
	s.ExecutionPercent = percent(s.ExecutedTotal, s.ExpectedTotal)
	for _, d := range diagnostics {
		switch d.Correlation {
		case CorrelationMissing:
			s.Diagnostics.Missing++
		case CorrelationMalformed:
			s.Diagnostics.Malformed++
		case CorrelationUnknown:
			s.Diagnostics.Unknown++
		case CorrelationDeprecated:
			s.Diagnostics.Deprecated++
		case CorrelationWrongProject:
			s.Diagnostics.WrongProject++
		}
	}
	s.Diagnostics.Total = int32(len(diagnostics))
	return s
}

// Summarize computes a run's summary over its universe (snapshot plus amendments) and records which TC-IDs were
// amended in, so an amended summary is never mistaken for the original snapshot (DEC-42).
func Summarize(runID int64, in SummaryInputs, diagnostics []Diagnostic) Summary {
	s := ComputeSummary(runID, in.Universe(), in.Valid, diagnostics)
	s.SnapshotTotal = int32(len(in.Expected))
	s.AmendedIDs = append(s.AmendedIDs, in.Amended...)
	slices.Sort(s.AmendedIDs)
	return s
}

// logical reduces the results of one TC-ID to the logical result of each of its tests (D1): the last attempt
// wins, and a test that passed after a failed or errored attempt is flaky. Variants (other tests of the TC-ID,
// e.g. one per browser) stay separate and are aggregated by the caller.
func logical(results []ValidResult) ([]ResultStatus, bool) {
	type test struct {
		last   ValidResult
		failed bool // an attempt other than the last failed or errored
	}
	var order []string
	tests := map[string]*test{}
	for i, r := range results {
		key := r.Execution
		if key == "" {
			key = "#" + strconv.Itoa(i) // an execution of its own
		}
		t, ok := tests[key]
		switch {
		case !ok:
			tests[key] = &test{last: r}
			order = append(order, key)
			continue
		case r.Attempt >= t.last.Attempt:
			t.failed = t.failed || bad(t.last.Status)
			t.last = r
		default:
			t.failed = t.failed || bad(r.Status)
		}
	}
	statuses := make([]ResultStatus, len(order))
	flaky := false
	for i, key := range order {
		t := tests[key]
		statuses[i] = t.last.Status
		flaky = flaky || (t.last.Status == Passed && t.failed)
	}
	return statuses, flaky
}

func bad(s ResultStatus) bool { return s == Failed || s == Error }
