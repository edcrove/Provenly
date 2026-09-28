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

// FileCoverage is the consolidated coverage of one source file across every layer.
type FileCoverage struct {
	Side      string  `json:"side"`
	File      string  `json:"file"`
	Metric    string  `json:"metric"`
	Covered   int     `json:"covered"`
	Total     int     `json:"total"`
	Percent   float64 `json:"percent"`
	Uncovered []int   `json:"uncoveredLines"`
}

var backendLayers = []string{"unit", "integration", "contract", "e2e"}

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

// goProfile converts GOCOVERDIRs to a merged text profile with go tool covdata.
func goProfile(root string, inputs []string, dst string) (map[string]*goBlock, error) {
	cmd := exec.Command("go", "tool", "covdata", "textfmt", "-i="+strings.Join(inputs, ","), "-o="+dst)
	cmd.Dir = filepath.Join(root, "backend")
	if b, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("go tool covdata: %v %s", err, b)
	}
	return parseGoProfile(dst, backendModule)
}

// backendLayerDirs returns the GOCOVERDIR of every backend layer, or an error naming the missing ones.
func backendLayerDirs(root string) ([]string, error) {
	var dirs, missing []string
	for _, l := range backendLayers {
		dir := filepath.Join(root, paths.GoCovDirs[l])
		if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
			dirs = append(dirs, dir)
		} else {
			missing = append(missing, paths.GoCovDirs[l])
		}
	}
	if len(missing) > 0 {
		return dirs, fmt.Errorf("missing evidence: backend raw coverage not collected in %s", strings.Join(missing, ", "))
	}
	return dirs, nil
}

// mergedBackend merges the raw coverage of every backend layer (unit, integration, contract, e2e).
func mergedBackend(root, dst string) (map[string]*goBlock, error) {
	dirs, err := backendLayerDirs(root)
	if err != nil {
		return nil, err
	}
	return goProfile(root, dirs, dst)
}

// frontendConsolidated returns the v8 statement-line universe and the lines
// executed by any frontend layer (unit, integration, e2e).
func frontendConsolidated(root string) (universe map[string]map[int]bool, hits []map[string]map[int]bool, err error) {
	universe = map[string]map[int]bool{}
	for _, p := range []string{paths.FrontendUnit, paths.FrontendIntCoverage} {
		m, err := readIstanbul(filepath.Join(root, p))
		if err != nil {
			return nil, nil, fmt.Errorf("missing evidence: %v", err)
		}
		for f, ls := range statementLines(m, "src/") {
			if universe[f] == nil {
				universe[f] = map[int]bool{}
			}
			for l := range ls {
				universe[f][l] = true
			}
		}
		hits = append(hits, hitLines(m, "src/"))
	}
	e2e, err := readIstanbulDir(filepath.Join(root, paths.FrontendE2ECoverage))
	if err != nil {
		return nil, nil, fmt.Errorf("missing evidence: %v", err)
	}
	return universe, append(hits, hitLines(e2e, "src/")), nil
}

func anyHit(hits []map[string]map[int]bool, file string, line int) bool {
	for _, h := range hits {
		if h[file][line] {
			return true
		}
	}
	return false
}

// backendEvidence reports statements covered per layer and merged, plus per-file consolidation.
func backendEvidence(root, outDir string) ([]CodeCoverage, []FileCoverage, []string) {
	var out []CodeCoverage
	var notes []string
	tmp, err := os.MkdirTemp("", "covgate")
	if err != nil {
		return nil, nil, []string{err.Error()}
	}
	defer os.RemoveAll(tmp)
	for _, l := range backendLayers {
		dir := filepath.Join(root, paths.GoCovDirs[l])
		if entries, err := os.ReadDir(dir); err != nil || len(entries) == 0 {
			notes = append(notes, fmt.Sprintf("backend %s evidence not collected (%s)", l, paths.GoCovDirs[l]))
			continue
		}
		blocks, err := goProfile(root, []string{dir}, filepath.Join(tmp, l+".out"))
		if err != nil {
			notes = append(notes, err.Error())
			continue
		}
		c, t := statementTotals(blocks)
		out = append(out, CodeCoverage{Side: "backend", Source: l, Metric: "statements", Covered: c, Total: t, Percent: pct(c, t)})
	}
	merged := filepath.Join(outDir, "backend-consolidated.out")
	blocks, err := mergedBackend(root, merged)
	if err != nil {
		return out, nil, append(notes, "backend consolidated evidence unavailable: "+err.Error())
	}
	c, t := statementTotals(blocks)
	out = append(out, CodeCoverage{Side: "backend", Source: "consolidated (all layers merged)", Metric: "statements", Covered: c, Total: t, Percent: pct(c, t)})
	html := exec.Command("go", "tool", "cover", "-html="+merged, "-o="+filepath.Join(outDir, "backend-consolidated.html"))
	html.Dir = filepath.Join(root, "backend")
	if b, err := html.CombinedOutput(); err != nil {
		notes = append(notes, fmt.Sprintf("backend HTML report not generated: %v %s", err, b))
	}
	byFile := map[string]*FileCoverage{}
	for _, b := range blocks {
		f := byFile[b.File]
		if f == nil {
			f = &FileCoverage{Side: "backend", File: b.File, Metric: "statements"}
			byFile[b.File] = f
		}
		f.Total += b.Statements
		if b.Count > 0 {
			f.Covered += b.Statements
		} else {
			f.Uncovered = append(f.Uncovered, b.StartLine)
		}
	}
	return out, sortedFiles(byFile), notes
}

// frontendEvidence reports lines covered per layer and consolidated, plus per-file consolidation.
func frontendEvidence(root string) ([]CodeCoverage, []FileCoverage, []string) {
	var out []CodeCoverage
	var notes []string
	for _, src := range []struct{ name, path string }{{"unit", paths.FrontendUnit}, {"integration", paths.FrontendIntCoverage}} {
		m, err := readIstanbul(filepath.Join(root, src.path))
		if err != nil {
			notes = append(notes, fmt.Sprintf("frontend %s evidence not collected (%s)", src.name, src.path))
			continue
		}
		c, t := lineTotals(statementLines(m, "src/"), []map[string]map[int]bool{hitLines(m, "src/")})
		out = append(out, CodeCoverage{Side: "frontend", Source: src.name, Metric: "lines", Covered: c, Total: t, Percent: pct(c, t)})
	}
	if m, err := readIstanbulDir(filepath.Join(root, paths.FrontendE2ECoverage)); err == nil {
		c, t := lineTotals(statementLines(m, "src/"), []map[string]map[int]bool{hitLines(m, "src/")})
		out = append(out, CodeCoverage{Side: "frontend", Source: "e2e", Metric: "lines", Covered: c, Total: t, Percent: pct(c, t)})
	} else {
		notes = append(notes, "frontend e2e evidence not collected ("+paths.FrontendE2ECoverage+")")
	}
	universe, hits, err := frontendConsolidated(root)
	if err != nil {
		return out, nil, append(notes, "frontend consolidated evidence unavailable: "+err.Error())
	}
	c, t := lineTotals(universe, hits)
	out = append(out, CodeCoverage{Side: "frontend", Source: "consolidated (all layers merged)", Metric: "lines", Covered: c, Total: t, Percent: pct(c, t)})
	byFile := map[string]*FileCoverage{}
	for file, lines := range universe {
		f := &FileCoverage{Side: "frontend", File: file, Metric: "lines"}
		for l := range lines {
			f.Total++
			if anyHit(hits, file, l) {
				f.Covered++
			} else {
				f.Uncovered = append(f.Uncovered, l)
			}
		}
		byFile[file] = f
	}
	return out, sortedFiles(byFile), notes
}

func lineTotals(universe map[string]map[int]bool, hits []map[string]map[int]bool) (int, int) {
	c, t := 0, 0
	for file, lines := range universe {
		for l := range lines {
			t++
			if anyHit(hits, file, l) {
				c++
			}
		}
	}
	return c, t
}

func sortedFiles(byFile map[string]*FileCoverage) []FileCoverage {
	out := make([]FileCoverage, 0, len(byFile))
	for _, f := range byFile {
		sort.Ints(f.Uncovered)
		f.Percent = pct(f.Covered, f.Total)
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out
}
