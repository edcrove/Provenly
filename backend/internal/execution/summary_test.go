package execution

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAggregatePrecedence(t *testing.T) {
	cases := []struct {
		in   []ResultStatus
		want SummaryStatus
	}{
		{nil, Untested},
		{[]ResultStatus{Passed}, "passed"},
		{[]ResultStatus{Passed, Failed}, "failed"},
		{[]ResultStatus{Failed, Error}, "failed"},
		{[]ResultStatus{Error, Skipped, Passed}, "error"},
		{[]ResultStatus{Passed, Skipped}, "skipped"},
		{[]ResultStatus{Skipped, Passed}, "skipped"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, Aggregate(c.in), "%v", c.in)
	}
}

func TestComputeSummary(t *testing.T) {
	expected := []int64{5, 1, 2, 3, 4}
	valid := []ValidResult{
		{TestCaseID: 1, Status: Passed}, {TestCaseID: 1, Status: Failed}, // Chrome=PASS + Firefox=FAIL => failed
		{TestCaseID: 2, Status: Passed},
		{TestCaseID: 3, Status: Error},
		{TestCaseID: 4, Status: Skipped},
		{TestCaseID: 99, Status: Passed}, {TestCaseID: 99, Status: Failed}, {TestCaseID: 98, Status: Passed}, // valid but outside the snapshot
	}
	diags := []Diagnostic{
		{Correlation: CorrelationMissing}, {Correlation: CorrelationMalformed},
		{Correlation: CorrelationUnknown}, {Correlation: CorrelationUnknown}, {Correlation: CorrelationDeprecated},
		{Correlation: CorrelationWrongProject},
	}
	s := ComputeSummary(7, expected, valid, diags)
	assert.Equal(t, int64(7), s.TestRunID)
	assert.Equal(t, int32(5), s.ExpectedTotal)
	assert.Equal(t, int32(4), s.ExecutedTotal)
	assert.Equal(t, StatusCounts{Untested: 1, Passed: 1, Failed: 1, Error: 1, Skipped: 1}, s.Counts)
	assert.Equal(t, StatusPercentages{Untested: 20, Passed: 20, Failed: 20, Error: 20, Skipped: 20}, s.PercentOfExpected)
	assert.Equal(t, ExecutedPercentages{Passed: 25, Failed: 25, Error: 25, Skipped: 25}, s.PercentOfExecuted)
	assert.Equal(t, 80.0, s.ExecutionPercent)
	assert.Equal(t, DiagnosticCounts{Missing: 1, Malformed: 1, Unknown: 2, Deprecated: 1, WrongProject: 1, Total: 6}, s.Diagnostics)
	assert.Equal(t, int32(3), s.OutsideUniverse)
	assert.Equal(t, []int64{98, 99}, s.OutsideUniverseIDs)
	assert.Equal(t, []TestCaseOutcome{
		{TestCaseID: 1, Status: "failed", ResultCount: 2}, {TestCaseID: 2, Status: "passed", ResultCount: 1}, {TestCaseID: 3, Status: "error", ResultCount: 1}, {TestCaseID: 4, Status: "skipped", ResultCount: 1}, {TestCaseID: 5, Status: Untested, ResultCount: 0},
	}, s.TestCases)
}

func TestComputeSummaryRoundingAndEmpty(t *testing.T) {
	s := ComputeSummary(1, []int64{1, 2, 3}, []ValidResult{{TestCaseID: 1, Status: Passed}}, nil)
	assert.Equal(t, 33.333333, s.PercentOfExpected.Passed)
	assert.Equal(t, 66.666667, s.PercentOfExpected.Untested)
	assert.Equal(t, 100.0, s.PercentOfExecuted.Passed)
	assert.Equal(t, 33.333333, s.ExecutionPercent)

	thirds := ComputeSummary(3, []int64{1, 2, 3}, []ValidResult{{TestCaseID: 1, Status: Passed}, {TestCaseID: 2, Status: Failed}, {TestCaseID: 3, Status: Error}}, nil)
	sum := thirds.PercentOfExecuted.Passed + thirds.PercentOfExecuted.Failed + thirds.PercentOfExecuted.Error
	assert.InDelta(t, 100, sum, 0.00001, "precise values add up to 100 once rounded for display")

	empty := ComputeSummary(2, nil, []ValidResult{{TestCaseID: 1, Status: Passed}}, nil)
	assert.Equal(t, int32(0), empty.ExpectedTotal)
	assert.Equal(t, 0.0, empty.ExecutionPercent)
	assert.Equal(t, 0.0, empty.PercentOfExecuted.Passed)
	assert.Equal(t, int32(1), empty.OutsideUniverse)
	assert.Equal(t, []int64{1}, empty.OutsideUniverseIDs)
	assert.Equal(t, []int64{}, s.OutsideUniverseIDs)
	assert.Empty(t, empty.TestCases)
}

func TestExternalRunID(t *testing.T) {
	assert.Equal(t, "github:123:2", ExternalRunID("github", "123", 2))
}

func TestOutcomeVerdict(t *testing.T) {
	p, f, e, s := Passed, Failed, Error, Skipped
	cases := []struct {
		name     string
		expected []int64
		results  []ResultStatus // one per expected TC-ID, in order; "" leaves it untested
		verdict  Verdict
		passRate float64
	}{
		{"no tests", nil, nil, VerdictNoTests, 0},
		{"all passed", []int64{1, 2}, []ResultStatus{p, p}, VerdictPassed, 100},
		{"a failure wins over untested", []int64{1, 2, 3}, []ResultStatus{p, f, ""}, VerdictFailed, 50},
		{"an error is a failure", []int64{1, 2}, []ResultStatus{p, e}, VerdictFailed, 50},
		{"untested is incomplete", []int64{1, 2}, []ResultStatus{p, ""}, VerdictIncomplete, 100},
		{"skipped is incomplete", []int64{1, 2}, []ResultStatus{p, s}, VerdictIncomplete, 50},
		{"nothing executed", []int64{1}, []ResultStatus{""}, VerdictIncomplete, 0},
	}
	for _, c := range cases {
		var valid []ValidResult
		for i, st := range c.results {
			if st != "" {
				valid = append(valid, ValidResult{TestCaseID: c.expected[i], Status: st})
			}
		}
		o := ComputeSummary(1, c.expected, valid, nil).Outcome()
		assert.Equal(t, c.verdict, o.Verdict, c.name)
		assert.Equal(t, c.passRate, o.PassRate, c.name)
	}
	o := ComputeSummary(1, []int64{1, 2, 3, 4, 5}, []ValidResult{{TestCaseID: 1, Status: Passed}, {TestCaseID: 2, Status: Failed}, {TestCaseID: 3, Status: Error}, {TestCaseID: 4, Status: Skipped}, {TestCaseID: 9, Status: Failed}}, nil).Outcome()
	assert.Equal(t, RunOutcome{Verdict: VerdictFailed, Executed: 4, Passed: 1, Failed: 1, Error: 1, Skipped: 1, Untested: 1, PassRate: 25}, o,
		"results outside the snapshot do not count")
}

// D1: the last attempt of each test is its logical result; a pass after failed attempts is passed and flaky;
// variants (other tests of the TC-ID, or repeated names that share an attempt) still aggregate
// failed > error > skipped > passed, and a TC-ID that failed or errored is never flaky.
func TestRetriesAndFlaky(t *testing.T) {
	r := func(tc int64, exec string, attempt int32, st ResultStatus) ValidResult {
		return ValidResult{TestCaseID: tc, Execution: exec, Attempt: attempt, Status: st}
	}
	s := ComputeSummary(1, []int64{1, 2, 3, 4, 5, 6, 7, 8}, []ValidResult{
		r(1, "login", 1, Failed), r(1, "login", 2, Passed), // flaky pass
		r(2, "pay", 2, Failed), r(2, "pay", 1, Passed), // attempts out of order: attempt 2 (failed) is last
		r(3, "chrome", 1, Error), r(3, "chrome", 2, Passed), r(3, "firefox", 1, Failed), // flaky variant, failing variant: failed, not flaky
		r(4, "x", 1, Skipped), r(4, "x", 1, Passed), // same attempt twice: two variants
		r(5, "", 1, Passed), r(5, "", 1, Failed), // no execution identity: separate variants
		r(6, "same", 1, Failed), r(6, "same", 1, Passed), // same name without an attempt signal: failed wins, not flaky
		r(7, "dup", 1, Failed), r(7, "dup", 2, Passed), r(7, "dup", 2, Error), // last attempt has a variant that errored
		r(8, "both", 1, Failed), r(8, "both", 2, Passed), r(8, "both", 2, Passed), // every variant of the last attempt passed
	}, nil)
	got := map[int64]TestCaseOutcome{}
	for _, c := range s.TestCases {
		got[c.TestCaseID] = c
	}
	assert.Equal(t, TestCaseOutcome{TestCaseID: 1, Status: "passed", ResultCount: 2, Flaky: true}, got[1])
	assert.Equal(t, TestCaseOutcome{TestCaseID: 2, Status: "failed", ResultCount: 2}, got[2])
	assert.Equal(t, TestCaseOutcome{TestCaseID: 3, Status: "failed", ResultCount: 3}, got[3])
	assert.Equal(t, TestCaseOutcome{TestCaseID: 4, Status: "skipped", ResultCount: 2}, got[4])
	assert.Equal(t, TestCaseOutcome{TestCaseID: 5, Status: "failed", ResultCount: 2}, got[5])
	assert.Equal(t, TestCaseOutcome{TestCaseID: 6, Status: "failed", ResultCount: 2}, got[6])
	assert.Equal(t, TestCaseOutcome{TestCaseID: 7, Status: "error", ResultCount: 3}, got[7])
	assert.Equal(t, TestCaseOutcome{TestCaseID: 8, Status: "passed", ResultCount: 3, Flaky: true}, got[8])
	assert.Equal(t, int32(2), s.Flaky)
	assert.Equal(t, int32(2), s.Outcome().Flaky)
	assert.Equal(t, int32(2), s.Counts.Passed)
	assert.Equal(t, int32(4), s.Counts.Failed)
}
