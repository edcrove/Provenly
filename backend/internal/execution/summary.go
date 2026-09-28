package execution

import (
	"math"
	"sort"
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
	Missing, Malformed, Unknown, Deprecated, Total int32
}

// TestCaseOutcome is the aggregated outcome of one snapshot TC-ID.
type TestCaseOutcome struct {
	TestCaseID  int64
	Status      SummaryStatus
	ResultCount int32
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
	// OutsideUniverseIDs are the distinct TC-IDs behind OutsideUniverse, ascending.
	OutsideUniverseIDs []int64
	TestCases          []TestCaseOutcome
}

func percent(part, total int32) float64 {
	if total == 0 {
		return 0
	}
	return math.Round(float64(part)*10000/float64(total)) / 100
}

// ComputeSummary derives the summary of a run from its immutable snapshot
// (expected), its valid results and its diagnostic results. Valid results for
// TC-IDs outside the snapshot are counted in OutsideUniverse and excluded.
func ComputeSummary(runID int64, expected []int64, valid []ValidResult, diagnostics []Diagnostic) Summary {
	byCase := make(map[int64][]ResultStatus, len(expected))
	for _, id := range expected {
		byCase[id] = nil
	}
	s := Summary{TestRunID: runID, ExpectedTotal: int32(len(expected)), OutsideUniverseIDs: []int64{}}
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
		byCase[r.TestCaseID] = append(byCase[r.TestCaseID], r.Status)
	}
	sort.Slice(s.OutsideUniverseIDs, func(i, j int) bool { return s.OutsideUniverseIDs[i] < s.OutsideUniverseIDs[j] })
	ids := append([]int64(nil), expected...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	s.TestCases = make([]TestCaseOutcome, 0, len(ids))
	for _, id := range ids {
		agg := Aggregate(byCase[id])
		s.TestCases = append(s.TestCases, TestCaseOutcome{TestCaseID: id, Status: agg, ResultCount: int32(len(byCase[id]))})
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
		}
	}
	s.Diagnostics.Total = int32(len(diagnostics))
	return s
}
