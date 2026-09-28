package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// CodeCoverage is instrumented code coverage published as evidence (never a gate for Integration/E2E).
type CodeCoverage struct {
	Side    string  `json:"side"`
	Source  string  `json:"source"`
	Metric  string  `json:"metric"`
	Covered int     `json:"covered"`
	Total   int     `json:"total"`
	Percent float64 `json:"percent"`
}

func statementTotals(blocks map[string]*goBlock) (int, int) {
	covered, total := 0, 0
	for _, b := range blocks {
		total += b.Statements
		if b.Count > 0 {
			covered += b.Statements
		}
	}
	return covered, total
}

// backendEvidence converts each GOCOVERDIR (and their merge) to a profile with go tool covdata.
func backendEvidence(root string) ([]CodeCoverage, []string) {
	var out []CodeCoverage
	var notes []string
	var dirs []string
	layers := []string{"unit", "integration", "contract", "e2e"}
	tmp, err := os.MkdirTemp("", "covgate")
	if err != nil {
		return nil, []string{err.Error()}
	}
	defer os.RemoveAll(tmp)
	profile := func(name string, inputs []string) {
		dst := filepath.Join(tmp, name+".out")
		cmd := exec.Command("go", "tool", "covdata", "textfmt", "-i="+strings.Join(inputs, ","), "-o="+dst)
		cmd.Dir = filepath.Join(root, "backend")
		if b, err := cmd.CombinedOutput(); err != nil {
			notes = append(notes, fmt.Sprintf("backend %s evidence unavailable: %v %s", name, err, b))
			return
		}
		blocks, err := parseGoProfile(dst, backendModule)
		if err != nil {
			notes = append(notes, err.Error())
			return
		}
		c, t := statementTotals(blocks)
		out = append(out, CodeCoverage{Side: "backend", Source: name, Metric: "statements", Covered: c, Total: t, Percent: pct(c, t)})
	}
	for _, l := range layers {
		dir := filepath.Join(root, paths.GoCovDirs[l])
		if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
			dirs = append(dirs, dir)
			profile(l, []string{dir})
		} else {
			notes = append(notes, fmt.Sprintf("backend %s evidence not collected (%s)", l, paths.GoCovDirs[l]))
		}
	}
	if len(dirs) > 1 {
		profile("consolidated", dirs)
	}
	return out, notes
}

// frontendEvidence reports statement-line coverage per source and their
// consolidation. The line universe is the one of the v8 maps (unit and
// integration share it); a line counts as covered when any source (including
// the istanbul-instrumented E2E bundle) executed a statement on it.
func frontendEvidence(root string) ([]CodeCoverage, []string) {
	var out []CodeCoverage
	var notes []string
	universe := map[string]map[int]bool{}
	var sources []map[string]map[int]bool
	add := func(name string, m istanbulMap, defines bool) {
		lines := istanbulLines(m, "src/")
		sources = append(sources, lines)
		c, t := 0, 0
		for file, ls := range lines {
			if defines && universe[file] == nil {
				universe[file] = map[int]bool{}
			}
			for l, hit := range ls {
				t++
				if hit {
					c++
				}
				if defines {
					universe[file][l] = true
				}
			}
		}
		out = append(out, CodeCoverage{Side: "frontend", Source: name, Metric: "lines", Covered: c, Total: t, Percent: pct(c, t)})
	}
	for _, src := range []struct{ name, path string }{
		{"unit", paths.FrontendUnit}, {"integration", paths.FrontendIntCoverage},
	} {
		if m, err := readIstanbul(filepath.Join(root, src.path)); err == nil {
			add(src.name, m, true)
		} else {
			notes = append(notes, fmt.Sprintf("frontend %s evidence not collected (%s)", src.name, src.path))
		}
	}
	if m, err := readIstanbulDir(filepath.Join(root, paths.FrontendE2ECoverage)); err == nil {
		add("e2e", m, false)
	} else {
		notes = append(notes, "frontend e2e evidence not collected ("+paths.FrontendE2ECoverage+")")
	}
	if len(out) > 1 && len(universe) > 0 {
		c, t := 0, 0
		files := make([]string, 0, len(universe))
		for f := range universe {
			files = append(files, f)
		}
		sort.Strings(files)
		for _, f := range files {
			for l := range universe[f] {
				t++
				for _, src := range sources {
					if src[f][l] {
						c++
						break
					}
				}
			}
		}
		out = append(out, CodeCoverage{Side: "frontend", Source: "consolidated (any source, v8 line universe)", Metric: "lines", Covered: c, Total: t, Percent: pct(c, t)})
	}
	return out, notes
}
