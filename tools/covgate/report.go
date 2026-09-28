package main

import (
	"fmt"
	"strings"
)

// Report is the full output of one covgate run.
type Report struct {
	GeneratedAt   string         `json:"generatedAt"`
	Pass          bool           `json:"pass"`
	Gates         []GateResult   `json:"gates"`
	CodeCoverage  []CodeCoverage `json:"codeCoverageEvidence"`
	Files         []FileCoverage `json:"consolidatedFiles"`
	EvidenceNotes []string       `json:"evidenceNotes"`
}

func mark(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

// Markdown renders the report for humans (terminal, CI step summary, PRs).
func (r Report) Markdown() string {
	var b strings.Builder
	b.WriteString("## Provenly coverage gates\n\n")
	b.WriteString("Each gate must independently reach 100% of its own required denominator.\n\n")
	b.WriteString("| Gate | Metric | Reachable | Covered | Exceptions | Required | Gaps | Effective | Raw | Target | Status |\n")
	b.WriteString("|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---|\n")
	for _, g := range r.Gates {
		if len(g.Metrics) == 0 {
			fmt.Fprintf(&b, "| %s | — | — | — | — | — | — | — | — | 100%% | %s |\n", g.Gate, mark(g.Pass))
		}
		for _, m := range g.Metrics {
			fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %d | %d | %.2f%% | %.2f%% | 100%% | %s |\n",
				g.Gate, m.Name, m.ReachableTotal, m.Covered, m.Exceptions, m.RequiredTotal, m.Gaps,
				m.EffectiveCoverage, m.RawCoverage, mark(g.Pass))
		}
	}
	for _, g := range r.Gates {
		if len(g.Errors)+len(g.GapDetails)+len(g.Exceptions)+len(g.Notes) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n### %s — %s\n\nDenominator: %s\n\n", g.Gate, mark(g.Pass), g.Denominator)
		for _, e := range g.Errors {
			fmt.Fprintf(&b, "- ERROR: %s\n", e)
		}
		for _, gap := range g.GapDetails {
			fmt.Fprintf(&b, "- GAP: %s\n", gap)
		}
		for _, e := range g.Exceptions {
			fmt.Fprintf(&b, "- EXCEPTION %s (%s) `%s`: %d uncovered elements — %s\n", e.ID, e.Category, e.Target, e.Elements, e.Reason)
			if g.Layer == "cross-layer" {
				for _, d := range e.Details {
					fmt.Fprintf(&b, "  - %s\n", d)
				}
			}
		}
		for _, n := range g.Notes {
			fmt.Fprintf(&b, "- NOTE: %s\n", n)
		}
	}
	if len(r.CodeCoverage) > 0 || len(r.EvidenceNotes) > 0 {
		b.WriteString("\n### Code coverage evidence (not a gate for Integration/E2E)\n\n| Side | Source | Metric | Covered | Total | % |\n|---|---|---|---:|---:|---:|\n")
		for _, c := range r.CodeCoverage {
			fmt.Fprintf(&b, "| %s | %s | %s | %d | %d | %.2f%% |\n", c.Side, c.Source, c.Metric, c.Covered, c.Total, c.Percent)
		}
		for _, n := range r.EvidenceNotes {
			fmt.Fprintf(&b, "\n- %s", n)
		}
		b.WriteString("\n\nPer-file consolidated report: `coverage/out/consolidated-coverage.md`; backend HTML: `coverage/out/backend-consolidated.html`.\n")
	}
	fmt.Fprintf(&b, "\n**Overall: %s**\n", mark(r.Pass))
	return b.String()
}

// ConsolidatedMarkdown renders coverage per file with all layers merged.
func (r Report) ConsolidatedMarkdown() string {
	var b strings.Builder
	b.WriteString("## Consolidated coverage (all layers merged)\n\n")
	b.WriteString("Backend: unit + integration + contract + e2e raw coverage merged with `go tool covdata` (statements).\n")
	b.WriteString("Frontend: unit + integration (v8) + e2e (istanbul) merged by executed line ranges (lines).\n\n")
	for _, c := range r.CodeCoverage {
		if strings.HasPrefix(c.Source, "consolidated") {
			fmt.Fprintf(&b, "- **%s**: %d/%d %s (%.2f%%)\n", c.Side, c.Covered, c.Total, c.Metric, c.Percent)
		}
	}
	for _, side := range []string{"backend", "frontend"} {
		fmt.Fprintf(&b, "\n### %s\n\n| File | Covered | Total | %% | Uncovered lines |\n|---|---:|---:|---:|---|\n", side)
		for _, f := range r.Files {
			if f.Side != side {
				continue
			}
			lines := make([]string, len(f.Uncovered))
			for i, l := range f.Uncovered {
				lines[i] = fmt.Sprint(l)
			}
			fmt.Fprintf(&b, "| %s | %d | %d | %.2f%% | %s |\n", f.File, f.Covered, f.Total, f.Percent, strings.Join(lines, ", "))
		}
	}
	return b.String()
}
