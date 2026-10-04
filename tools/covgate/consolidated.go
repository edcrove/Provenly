package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// unitExceptions are the exceptions of the Unit gates, set by main before the
// gates run, to report how much of the consolidated denominator they represent.
var unitExceptions = map[string][]*Exception{}

func matchesAny(excs []*Exception, keys ...string) bool {
	for _, x := range excs {
		for _, k := range keys {
			if x.matches(k) {
				return true
			}
		}
	}
	return false
}

// backendConsolidated: every backend statement must be executed by some layer
// (unit, integration, contract and e2e merged). This includes everything the
// Unit gate excepts, which therefore has to be covered by another layer.
func backendConsolidated(root string, r *GateResult) []Element {
	tmp, err := os.MkdirTemp("", "covgate")
	if err != nil {
		return fail(r, "%v", err)
	}
	defer os.RemoveAll(tmp)
	blocks, err := mergedBackend(root, filepath.Join(tmp, "merged.out"))
	if err != nil {
		return fail(r, "%v", err)
	}
	excs := unitExceptions["backend-unit"]
	var out []Element
	fromUnit := 0
	for _, b := range blocks {
		label := fmt.Sprintf("%s:%d", b.File, b.StartLine)
		if matchesAny(excs, b.File, label) {
			fromUnit += b.Statements
		}
		for i := 0; i < b.Statements; i++ {
			out = append(out, Element{Metric: "statements", Key: b.File, Label: label, Covered: b.Count > 0})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	r.Notes = append(r.Notes, fmt.Sprintf("%d of %d statements are excepted from backend-unit and must be executed by another layer", fromUnit, len(out)))
	return out
}

// frontendConsolidated: every frontend line must be executed by some layer
// (unit, integration and e2e merged), including everything the Unit gate excepts.
func frontendConsolidatedGate(root string, r *GateResult) []Element {
	universe, hits, err := frontendConsolidated(root)
	if err != nil {
		return fail(r, "%v", err)
	}
	excs := unitExceptions["frontend-unit"]
	var out []Element
	fromUnit := 0
	for file, lines := range universe {
		for l := range lines {
			label := fmt.Sprintf("%s:%d", file, l)
			if matchesAny(excs, file, label) {
				fromUnit++
			}
			out = append(out, Element{Metric: "lines", Key: file, Label: label, Covered: anyHit(hits, file, l)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	r.Notes = append(r.Notes, fmt.Sprintf("%d of %d lines are excepted from frontend-unit and must be executed by another layer", fromUnit, len(out)))
	return out
}
