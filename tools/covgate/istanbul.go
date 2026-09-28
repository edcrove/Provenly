package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// istanbulFile is the per-file istanbul coverage format (also emitted by
// @vitest/coverage-v8 and by vite-plugin-istanbul's window.__coverage__).
type istanbulFile struct {
	Path         string `json:"path"`
	StatementMap map[string]struct {
		Start struct{ Line int } `json:"start"`
	} `json:"statementMap"`
	S         map[string]int   `json:"s"`
	B         map[string][]int `json:"b"`
	BranchMap map[string]struct {
		Line int `json:"line"`
		Loc  struct {
			Start struct{ Line int } `json:"start"`
		} `json:"loc"`
	} `json:"branchMap"`
}

type istanbulMap map[string]*istanbulFile

func readIstanbul(path string) (istanbulMap, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m istanbulMap
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// readIstanbulDir merges every istanbul JSON of a directory (same build, so the maps are identical).
func readIstanbulDir(dir string) (istanbulMap, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("no istanbul coverage files in %s", dir)
	}
	merged := istanbulMap{}
	for _, f := range files {
		m, err := readIstanbul(f)
		if err != nil {
			return nil, err
		}
		for k, fc := range m {
			cur, ok := merged[k]
			if !ok {
				merged[k] = fc
				continue
			}
			for id, n := range fc.S {
				cur.S[id] += n
			}
			for id, ns := range fc.B {
				for i, n := range ns {
					if i < len(cur.B[id]) {
						cur.B[id][i] += n
					}
				}
			}
		}
	}
	return merged, nil
}

func relPath(p, base string) string {
	if i := strings.Index(p, base); i >= 0 {
		return p[i:]
	}
	return p
}

// istanbulElements expands statements and branches (per branch path) into elements.
func istanbulElements(m istanbulMap, base string) []Element {
	files := make([]string, 0, len(m))
	for k := range m {
		files = append(files, k)
	}
	sort.Strings(files)
	var out []Element
	for _, k := range files {
		f := m[k]
		rel := relPath(f.Path, base)
		for _, id := range sortedKeys(f.S) {
			out = append(out, Element{Metric: "statements", Key: rel, Label: fmt.Sprintf("%s:%d", rel, f.StatementMap[id].Start.Line), Covered: f.S[id] > 0})
		}
		for _, id := range sortedKeys(f.B) {
			line := f.BranchMap[id].Loc.Start.Line
			for i, n := range f.B[id] {
				out = append(out, Element{Metric: "branches", Key: rel, Label: fmt.Sprintf("%s:%d (branch %s path %d)", rel, line, id, i), Covered: n > 0})
			}
		}
	}
	return out
}

// istanbulLines returns covered lines per file (for consolidated line-union evidence).
func istanbulLines(m istanbulMap, base string) map[string]map[int]bool {
	out := map[string]map[int]bool{}
	for _, f := range m {
		rel := relPath(f.Path, base)
		lines, ok := out[rel]
		if !ok {
			lines = map[int]bool{}
			out[rel] = lines
		}
		for id, n := range f.S {
			line := f.StatementMap[id].Start.Line
			lines[line] = lines[line] || n > 0
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) < len(keys[j])
		}
		return keys[i] < keys[j]
	})
	return keys
}
