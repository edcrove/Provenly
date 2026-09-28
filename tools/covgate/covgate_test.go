package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGlob(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"internal/catalog/catalogdb/**", "internal/catalog/catalogdb/models.go", true},
		{"internal/{catalog,execution}/postgres/**", "internal/execution/postgres/store.go", true},
		{"internal/{catalog,execution}/postgres/**", "internal/platform/postgres/postgres.go", false},
		{"src/components/*.tsx", "src/components/Pagination.tsx", true},
		{"src/components/*.tsx", "src/components/ui/button.tsx", false},
		{"cmd/provenly/main.go:13", "cmd/provenly/main.go:13", true},
	}
	for _, c := range cases {
		if got := globRegexp(c.glob).MatchString(c.path); got != c.want {
			t.Errorf("%s ~ %s = %v, want %v", c.glob, c.path, got, c.want)
		}
	}
}

func TestEvaluateKeepsExceptionsVisible(t *testing.T) {
	exc := &Exception{ID: "E1", Target: "gen/**", Category: "generated-code", re: globRegexp("gen/**")}
	r := &GateResult{}
	evaluate(r, []Element{
		{Metric: "statements", Key: "a.go", Label: "a.go:1", Covered: true},
		{Metric: "statements", Key: "a.go", Label: "a.go:2", Covered: false},
		{Metric: "statements", Key: "gen/x.go", Label: "gen/x.go:1", Covered: false},
	}, []*Exception{exc})
	m := r.Metrics[0]
	if m.ReachableTotal != 3 || m.Exceptions != 1 || m.RequiredTotal != 2 || m.Covered != 1 || m.Gaps != 1 {
		t.Fatalf("unexpected metric %+v", m)
	}
	if m.EffectiveCoverage != 50 || m.RawCoverage != 33.33 {
		t.Fatalf("unexpected percentages %+v", m)
	}
	if len(r.Exceptions) != 1 || r.Exceptions[0].Elements != 1 || !exc.used {
		t.Fatalf("exception not reported: %+v", r.Exceptions)
	}
	if len(r.GapDetails) != 1 || !strings.Contains(r.GapDetails[0], "a.go:2") {
		t.Fatalf("gap not listed: %v", r.GapDetails)
	}
	empty := &GateResult{}
	evaluate(empty, nil, nil)
	if len(empty.Errors) != 1 {
		t.Fatal("an empty denominator must be reported")
	}
}

func TestExceptionValidation(t *testing.T) {
	dir := t.TempDir()
	valid := `
  - id: OK
    gate: backend-unit
    layer: unit
    side: backend
    target: x/**
    reason: r
    category: generated-code
    evidence: e
    owner: o
    createdAt: "2026-01-01"
    reviewBy: "2026-12-31"
    link: l
    status: approved`
	expired := strings.NewReplacer("id: OK", "id: OLD", `"2026-12-31"`, `"2026-02-01"`).Replace(valid)
	proposed := strings.NewReplacer("id: OK", "id: PROP", "approved", "proposed").Replace(valid)
	badGate := strings.NewReplacer("id: OK", "id: GATE", "gate: backend-unit", "gate: frontend-unit").Replace(valid)
	path := filepath.Join(dir, "exceptions.yaml")
	if err := os.WriteFile(path, []byte("exceptions:"+valid+expired+proposed+badGate+"\n  - id: EMPTY\n    gate: backend-unit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	byGate, errs, err := loadExceptions(path, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(byGate["backend-unit"]) != 1 || byGate["backend-unit"][0].ID != "OK" {
		t.Fatalf("only the valid exception applies: %+v", byGate)
	}
	joined := strings.Join(append(errs["backend-unit"], errs["frontend-unit"]...), "\n")
	for _, want := range []string{"OLD", "expired", "PROP", "status", "GATE", "EMPTY", "missing reason"} {
		if !strings.Contains(joined, want) {
			t.Errorf("validation errors should mention %q:\n%s", want, joined)
		}
	}
	if got := unusedExceptions(byGate["backend-unit"]); len(got) != 1 {
		t.Fatalf("unused exception must be stale: %v", got)
	}
}

func TestTargetIDs(t *testing.T) {
	ids := newTestIDs()
	ids.record("TestX/BE-INT-002_tc_ids_are_assigned", true)
	ids.record("[FE-E2E-001] journey", true)
	ids.record("FE-INT-003 fails", false)
	ids.record("FE-INT-003 passes", true)
	if !ids.covered("BE-INT-002") || !ids.covered("FE-E2E-001") {
		t.Fatal("ids in passing tests must be covered")
	}
	if ids.covered("FE-INT-003") {
		t.Fatal("an id with a failing test is not covered")
	}
}
