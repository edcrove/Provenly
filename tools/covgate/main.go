// Command covgate evaluates the 8 independent POC coverage gates (Unit,
// Integration, Contract and E2E x backend and frontend) and publishes
// actual/target/gap per gate plus consolidated code-coverage evidence.
//
// Every gate must reach 100% of its own required denominator. Exceptions come
// only from coverage/exceptions.yaml, are validated (fields, category, expiry,
// staleness) and are always reported separately.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	root := flag.String("root", ".", "repository root")
	only := flag.String("gates", "", "comma-separated subset of gates to evaluate (default: all 8)")
	out := flag.String("out", "coverage/out", "report output directory (relative to root)")
	today := flag.String("today", time.Now().UTC().Format("2006-01-02"), "date used to expire exceptions (YYYY-MM-DD)")
	evidence := flag.Bool("evidence", true, "also compute consolidated code-coverage evidence")
	flag.Parse()

	day, err := time.Parse("2006-01-02", *today)
	if err != nil {
		fatal(err)
	}
	excs, excErrs, err := loadExceptions(filepath.Join(*root, paths.Exceptions), day)
	if err != nil {
		fatal(err)
	}
	unitExceptions = map[string][]*Exception{"backend-unit": excs["backend-unit"], "frontend-unit": excs["frontend-unit"]}
	consolidatedExceptions = map[string][]*Exception{"backend-consolidated": excs["backend-consolidated"], "frontend-consolidated": excs["frontend-consolidated"]}
	selected := map[string]bool{}
	for _, g := range strings.Split(*only, ",") {
		if g = strings.TrimSpace(g); g != "" {
			selected[g] = true
		}
	}
	report := Report{GeneratedAt: time.Now().UTC().Format(time.RFC3339)}
	for g := range selected {
		if !knownGate(g) {
			fatal(fmt.Errorf("unknown gate %q", g))
		}
	}
	for _, g := range gates {
		if !isSelected(selected, g.name) {
			continue
		}
		r := &GateResult{Gate: g.name, Side: g.side, Layer: g.layer, Denominator: g.denominator, Target: 100,
			GapDetails: []string{}, Exceptions: []AppliedExcept{}, Errors: []string{}}
		elements := g.build(*root, r)
		if len(r.Errors) == 0 {
			evaluate(r, elements, excs[g.name])
			r.Errors = append(r.Errors, unusedExceptions(excs[g.name])...)
		}
		r.Errors = append(r.Errors, excErrs[g.name]...)
		r.Pass = len(r.Errors) == 0
		for _, m := range r.Metrics {
			if m.Gaps > 0 {
				r.Pass = false
			}
		}
		report.Gates = append(report.Gates, *r)
	}
	dir := filepath.Join(*root, *out)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fatal(err)
	}
	if *evidence {
		b, bf, bn := backendEvidence(*root, dir)
		f, ff, fn := frontendEvidence(*root)
		report.CodeCoverage = append(b, f...)
		report.Files = append(bf, ff...)
		report.EvidenceNotes = append(bn, fn...)
		_ = os.WriteFile(filepath.Join(dir, "consolidated-coverage.md"), []byte(report.ConsolidatedMarkdown()), 0o644)
	}
	report.Pass = true
	for _, g := range report.Gates {
		report.Pass = report.Pass && g.Pass
	}

	js, _ := json.MarshalIndent(report, "", "  ")
	md := report.Markdown()
	name := "coverage-report"
	if *only != "" {
		name += "-" + strings.ReplaceAll(*only, ",", "-")
	}
	_ = os.WriteFile(filepath.Join(dir, name+".json"), js, 0o644)
	_ = os.WriteFile(filepath.Join(dir, name+".md"), []byte(md), 0o644)
	fmt.Print(md)
	if summary := os.Getenv("GITHUB_STEP_SUMMARY"); summary != "" {
		if f, err := os.OpenFile(summary, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644); err == nil {
			_, _ = f.WriteString(md)
			_ = f.Close()
		}
	}
	if !report.Pass {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "covgate:", err)
	os.Exit(2)
}

func knownGate(name string) bool {
	for _, g := range gates {
		if g.name == name {
			return true
		}
	}
	return false
}

// isSelected reports whether a gate runs: all gates when none were selected.
func isSelected(selected map[string]bool, name string) bool {
	return len(selected) == 0 || selected[name]
}
