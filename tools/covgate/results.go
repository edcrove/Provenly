package main

import (
	"bufio"
	"encoding/json"
	"os"
	"regexp"
	"strings"
)

var targetID = regexp.MustCompile(`((?:BE|FE)-(?:INT|E2E)-\d{3})(?:\D|$)`)

// passedIDs collects target ids from names of passed tests, and ids seen in failed tests.
type testIDs struct {
	passed map[string]bool
	failed map[string]bool
}

func newTestIDs() testIDs { return testIDs{passed: map[string]bool{}, failed: map[string]bool{}} }

func (t testIDs) record(name string, passed bool) {
	for _, m := range targetID.FindAllStringSubmatch(name, -1) {
		if passed {
			t.passed[m[1]] = true
		} else {
			t.failed[m[1]] = true
		}
	}
}

// covered is true when the id has passing tests and no failing ones.
func (t testIDs) covered(id string) bool { return t.passed[id] && !t.failed[id] }

// goTestJSON reads `go test -json` output.
func goTestJSON(path string) (testIDs, error) {
	ids := newTestIDs()
	f, err := os.Open(path)
	if err != nil {
		return ids, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4<<20), 4<<20)
	for sc.Scan() {
		var ev struct{ Action, Test string }
		if json.Unmarshal(sc.Bytes(), &ev) != nil || ev.Test == "" {
			continue
		}
		switch ev.Action {
		case "pass":
			ids.record(ev.Test, true)
		case "fail":
			ids.record(ev.Test, false)
		}
	}
	return ids, sc.Err()
}

// vitestJSON reads the vitest JSON reporter output.
func vitestJSON(path string) (testIDs, error) {
	ids := newTestIDs()
	raw, err := os.ReadFile(path)
	if err != nil {
		return ids, err
	}
	var doc struct {
		TestResults []struct {
			AssertionResults []struct {
				FullName string `json:"fullName"`
				Status   string `json:"status"`
			} `json:"assertionResults"`
		} `json:"testResults"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ids, err
	}
	for _, f := range doc.TestResults {
		for _, a := range f.AssertionResults {
			ids.record(a.FullName, a.Status == "passed")
		}
	}
	return ids, nil
}

// playwrightJSON reads the Playwright JSON reporter output.
func playwrightJSON(path string) (testIDs, error) {
	ids := newTestIDs()
	raw, err := os.ReadFile(path)
	if err != nil {
		return ids, err
	}
	type spec struct {
		Title string `json:"title"`
		OK    bool   `json:"ok"`
	}
	type suite struct {
		Title  string  `json:"title"`
		Specs  []spec  `json:"specs"`
		Suites []suite `json:"suites"`
	}
	var doc struct {
		Suites []suite `json:"suites"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ids, err
	}
	var walk func(s suite)
	walk = func(s suite) {
		for _, sp := range s.Specs {
			ids.record(sp.Title, sp.OK)
		}
		for _, c := range s.Suites {
			walk(c)
		}
	}
	for _, s := range doc.Suites {
		walk(s)
	}
	return ids, nil
}

// contractEvidence reads the "operationId status" pairs validated by a contract suite.
func contractEvidence(path string) (map[string]bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Validated []string `json:"validated"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, v := range doc.Validated {
		out[strings.TrimSpace(v)] = true
	}
	return out, nil
}
