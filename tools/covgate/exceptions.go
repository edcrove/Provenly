package main

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

// Exception is a versioned, justified removal of elements from a gate's
// required denominator. It stays visible in every report.
type Exception struct {
	ID        string `yaml:"id"`
	Gate      string `yaml:"gate"`
	Layer     string `yaml:"layer"`
	Side      string `yaml:"side"`
	Target    string `yaml:"target"`
	Reason    string `yaml:"reason"`
	Category  string `yaml:"category"`
	Evidence  string `yaml:"evidence"`
	Owner     string `yaml:"owner"`
	CreatedAt string `yaml:"createdAt"`
	ReviewBy  string `yaml:"reviewBy"`
	Link      string `yaml:"link"`
	Status    string `yaml:"status"`

	re   *regexp.Regexp
	used bool
}

var categories = map[string]bool{
	"generated-code":     true,
	"other-layer":        true,
	"vendored-ui":        true,
	"entrypoint":         true,
	"not-reachable":      true,
	"tooling-limitation": true,
	"test-support":       true,
}

func (e *Exception) matches(key string) bool { return e.re != nil && e.re.MatchString(key) }

// loadExceptions reads the registry and returns exceptions by gate plus validation errors by gate.
func loadExceptions(path string, today time.Time) (map[string][]*Exception, map[string][]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var doc struct {
		Exceptions []*Exception `yaml:"exceptions"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	byGate := map[string][]*Exception{}
	errs := map[string][]string{}
	seen := map[string]bool{}
	for _, e := range doc.Exceptions {
		var problems []string
		for field, v := range map[string]string{
			"id": e.ID, "gate": e.Gate, "layer": e.Layer, "side": e.Side, "target": e.Target, "reason": e.Reason,
			"category": e.Category, "evidence": e.Evidence, "owner": e.Owner, "createdAt": e.CreatedAt,
			"reviewBy": e.ReviewBy, "link": e.Link, "status": e.Status,
		} {
			if v == "" {
				problems = append(problems, "missing "+field)
			}
		}
		if seen[e.ID] {
			problems = append(problems, "duplicate id")
		}
		seen[e.ID] = true
		if e.Gate != e.Side+"-"+e.Layer {
			problems = append(problems, fmt.Sprintf("gate %q does not match side %q and layer %q", e.Gate, e.Side, e.Layer))
		}
		if !categories[e.Category] {
			problems = append(problems, fmt.Sprintf("unknown category %q", e.Category))
		}
		if e.Status != "approved" {
			problems = append(problems, fmt.Sprintf("status is %q (only approved exceptions apply)", e.Status))
		}
		if _, err := time.Parse("2006-01-02", e.CreatedAt); err != nil && e.CreatedAt != "" {
			problems = append(problems, "createdAt must be YYYY-MM-DD")
		}
		if review, err := time.Parse("2006-01-02", e.ReviewBy); err != nil {
			problems = append(problems, "reviewBy must be YYYY-MM-DD")
		} else if review.Before(today) {
			problems = append(problems, "expired on "+e.ReviewBy+": review it or remove it")
		}
		if len(problems) > 0 {
			errs[e.Gate] = append(errs[e.Gate], fmt.Sprintf("invalid exception %s: %v", e.ID, problems))
			continue
		}
		e.re = globRegexp(e.Target)
		byGate[e.Gate] = append(byGate[e.Gate], e)
	}
	return byGate, errs, nil
}

// unusedExceptions reports exceptions that matched nothing (stale entries are invalid).
func unusedExceptions(excs []*Exception) []string {
	var out []string
	for _, e := range excs {
		if !e.used {
			out = append(out, fmt.Sprintf("stale exception %s: target %q matches no reachable element", e.ID, e.Target))
		}
	}
	return out
}
