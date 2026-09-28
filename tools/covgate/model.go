package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Element is one unit of a gate denominator: a statement, a branch, an
// integration surface, an OpenAPI variant or an E2E journey.
type Element struct {
	// Metric groups elements (e.g. "statements", "branches", "targets").
	Metric string
	// Key is what exceptions match against (a file path or a target id).
	Key string
	// Label is shown in gap listings.
	Label   string
	Covered bool
}

// Metric is the coverage accounting of one metric of a gate.
type Metric struct {
	Name              string  `json:"name"`
	ReachableTotal    int     `json:"reachableTotal"`
	Covered           int     `json:"covered"`
	Exceptions        int     `json:"exceptions"`
	RequiredTotal     int     `json:"requiredTotal"`
	Gaps              int     `json:"gaps"`
	EffectiveCoverage float64 `json:"effectiveCoverage"`
	RawCoverage       float64 `json:"rawCoverage"`
}

// GateResult is the outcome of one of the 8 gates.
type GateResult struct {
	Gate        string          `json:"gate"`
	Side        string          `json:"side"`
	Layer       string          `json:"layer"`
	Denominator string          `json:"denominator"`
	Target      float64         `json:"target"`
	Metrics     []Metric        `json:"metrics"`
	GapDetails  []string        `json:"gapDetails"`
	Exceptions  []AppliedExcept `json:"exceptions"`
	Errors      []string        `json:"errors"`
	Notes       []string        `json:"notes,omitempty"`
	Pass        bool            `json:"pass"`
}

// AppliedExcept reports how many elements an exception removed from a gate.
type AppliedExcept struct {
	ID       string `json:"id"`
	Target   string `json:"target"`
	Category string `json:"category"`
	Elements int    `json:"elements"`
	Reason   string `json:"reason"`
}

func pct(n, d int) float64 {
	if d == 0 {
		return 100
	}
	return float64(int(float64(n)*10000/float64(d))) / 100
}

// evaluate turns elements into metrics, applying the gate's exceptions.
// Exceptions never hide elements: they are counted and listed separately.
func evaluate(r *GateResult, elements []Element, excs []*Exception) {
	order := []string{}
	metrics := map[string]*Metric{}
	applied := map[string]*AppliedExcept{}
	for _, e := range elements {
		m, ok := metrics[e.Metric]
		if !ok {
			m = &Metric{Name: e.Metric}
			metrics[e.Metric] = m
			order = append(order, e.Metric)
		}
		m.ReachableTotal++
		raw := e.Covered
		var exc *Exception
		for _, x := range excs {
			if x.matches(e.Key) || x.matches(e.Label) {
				exc = x
				break
			}
		}
		if raw {
			m.RawCoverage++
		}
		switch {
		case exc != nil:
			m.Exceptions++
			exc.used = true
			a, ok := applied[exc.ID]
			if !ok {
				a = &AppliedExcept{ID: exc.ID, Target: exc.Target, Category: exc.Category, Reason: exc.Reason}
				applied[exc.ID] = a
			}
			a.Elements++
		case raw:
			m.Covered++
		default:
			m.Gaps++
			if len(r.GapDetails) < 200 {
				r.GapDetails = append(r.GapDetails, fmt.Sprintf("[%s] %s", e.Metric, e.Label))
			}
		}
	}
	for _, name := range order {
		m := metrics[name]
		m.RequiredTotal = m.ReachableTotal - m.Exceptions
		m.EffectiveCoverage = pct(m.Covered, m.RequiredTotal)
		m.RawCoverage = pct(int(m.RawCoverage), m.ReachableTotal)
		r.Metrics = append(r.Metrics, *m)
	}
	for _, x := range excs {
		if a, ok := applied[x.ID]; ok {
			r.Exceptions = append(r.Exceptions, *a)
		}
	}
	if len(elements) == 0 {
		r.Errors = append(r.Errors, "empty denominator: no reachable targets were found (an empty layer is explicit debt)")
	}
}

// globRegexp converts a glob (with ** and {a,b} support) into a regexp.
func globRegexp(glob string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch {
		case c == '*' && i+1 < len(glob) && glob[i+1] == '*':
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '{':
			b.WriteString("(")
		case c == '}':
			b.WriteString(")")
		case c == ',':
			b.WriteString("|")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}
