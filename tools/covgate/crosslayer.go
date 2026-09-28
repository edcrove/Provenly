package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// unitExceptions are the exceptions of the Unit gates, set by main before the
// gates run: the cross-layer gates verify their elements in the other layers.
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

// backendCrossLayer: every statement excepted from the backend Unit gate must
// be executed by some other layer (all layers merged).
func backendCrossLayer(root string, r *GateResult) []Element {
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
	for _, b := range blocks {
		label := fmt.Sprintf("%s:%d", b.File, b.StartLine)
		if !matchesAny(excs, b.File, label) {
			continue
		}
		for i := 0; i < b.Statements; i++ {
			out = append(out, Element{Metric: "statements", Key: b.File, Label: label, Covered: b.Count > 0})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	r.Notes = append(r.Notes, fmt.Sprintf("denominator: %d statements excepted from backend-unit, checked against all layers merged", len(out)))
	return out
}

// frontendCrossLayer: every line excepted from the frontend Unit gate must be
// executed by some other layer (unit, integration and e2e merged).
func frontendCrossLayer(root string, r *GateResult) []Element {
	universe, hits, err := frontendConsolidated(root)
	if err != nil {
		return fail(r, "%v", err)
	}
	excs := unitExceptions["frontend-unit"]
	var out []Element
	for file, lines := range universe {
		for l := range lines {
			label := fmt.Sprintf("%s:%d", file, l)
			if matchesAny(excs, file, label) {
				out = append(out, Element{Metric: "lines", Key: file, Label: label, Covered: anyHit(hits, file, l)})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	r.Notes = append(r.Notes, fmt.Sprintf("denominator: %d lines excepted from frontend-unit, checked against all layers merged", len(out)))
	return out
}
