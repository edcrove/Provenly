// Package junit parses JUnit XML reports (encoding/xml) into normalized,
// format-independent test results and extracts the declared TC-ID of each
// testcase: first from <property name="tc-id" value="..."/>, falling back to
// the TC-<id> pattern in the testcase name.
package junit

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Status is the normalized outcome of a testcase.
type Status string

// Normalized statuses.
const (
	Passed  Status = "passed"
	Failed  Status = "failed"
	Error   Status = "error"
	Skipped Status = "skipped"
)

// RefKind classifies the TC-ID declared by a testcase.
type RefKind string

// Reference kinds. Existence/deprecation are checked later against the catalog.
const (
	RefFound     RefKind = "found"
	RefMissing   RefKind = "missing"
	RefMalformed RefKind = "malformed"
)

// RefSource tells where the TC-ID reference was read from.
type RefSource string

// Reference sources.
const (
	SourceNone     RefSource = "none"
	SourceProperty RefSource = "property"
	SourceName     RefSource = "name"
)

// TCRef is the TC-ID reference declared by a testcase.
type TCRef struct {
	Kind   RefKind
	Source RefSource
	// Raw is the reference exactly as declared (empty when missing).
	Raw string
	// ID is the numeric TC-ID when Kind is RefFound.
	ID int64
}

// Result is a NormalizedTestResult: independent of the input format.
type Result struct {
	Index        int
	TestName     string
	ClassName    string
	SuiteName    string
	Status       Status
	DurationMs   int64
	ErrorMessage string
	ErrorDetails string
	Ref          TCRef
}

// CaseError reports a testcase that could not be normalized. It does not stop parsing.
type CaseError struct {
	Index    int
	TestName string
	Message  string
}

// Report is the outcome of parsing one document.
type Report struct {
	Results []Result
	Errors  []CaseError
	// Received counts every <testcase> element in document order.
	Received int
	// StartedAt is the earliest parseable suite timestamp, if any.
	StartedAt *time.Time
}

// PropertyName is the testcase property that declares the TC-ID.
const PropertyName = "tc-id"

type xmlProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type xmlOutcome struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

type xmlCase struct {
	Name       string        `xml:"name,attr"`
	ClassName  string        `xml:"classname,attr"`
	Time       string        `xml:"time,attr"`
	Properties []xmlProperty `xml:"properties>property"`
	Failure    *xmlOutcome   `xml:"failure"`
	Error      *xmlOutcome   `xml:"error"`
	Skipped    *xmlOutcome   `xml:"skipped"`
}

type xmlSuite struct {
	XMLName   xml.Name
	Name      string     `xml:"name,attr"`
	Timestamp string     `xml:"timestamp,attr"`
	Suites    []xmlSuite `xml:"testsuite"`
	Cases     []xmlCase  `xml:"testcase"`
}

var (
	propertyValue = regexp.MustCompile(`^(?:TC-)?([1-9][0-9]{0,17})$`)
	nameRef       = regexp.MustCompile(`\bTC-([0-9A-Za-z_]*)`)
	validNameID   = regexp.MustCompile(`^[1-9][0-9]{0,17}$`)
)

// Parse reads a JUnit XML document with a <testsuites> or <testsuite> root.
// An unreadable document is an error; an invalid testcase is reported in
// Report.Errors and the rest of the document is still parsed.
func Parse(r io.Reader) (Report, error) {
	var root xmlSuite
	if err := xml.NewDecoder(r).Decode(&root); err != nil {
		return Report{}, fmt.Errorf("invalid JUnit XML: %w", err)
	}
	if root.XMLName.Local != "testsuites" && root.XMLName.Local != "testsuite" {
		return Report{}, fmt.Errorf("invalid JUnit XML: unsupported root element <%s>", root.XMLName.Local)
	}
	var rep Report
	walk(&rep, root, "")
	return rep, nil
}

func walk(rep *Report, s xmlSuite, parent string) {
	suite := parent
	if s.XMLName.Local == "testsuite" {
		suite = s.Name
		if ts, err := time.Parse("2006-01-02T15:04:05", strings.TrimSuffix(s.Timestamp, "Z")); err == nil {
			if rep.StartedAt == nil || ts.Before(*rep.StartedAt) {
				rep.StartedAt = &ts
			}
		}
	}
	for _, c := range s.Cases {
		index := rep.Received
		rep.Received++
		res, err := normalize(c, suite, index)
		if err != nil {
			rep.Errors = append(rep.Errors, CaseError{Index: index, TestName: c.Name, Message: err.Error()})
			continue
		}
		rep.Results = append(rep.Results, res)
	}
	for _, child := range s.Suites {
		walk(rep, child, suite)
	}
}

func normalize(c xmlCase, suite string, index int) (Result, error) {
	name := strings.TrimSpace(c.Name)
	if name == "" {
		return Result{}, fmt.Errorf("testcase has no name")
	}
	duration, err := parseDuration(c.Time)
	if err != nil {
		return Result{}, err
	}
	res := Result{
		Index: index, TestName: name, ClassName: c.ClassName, SuiteName: suite,
		Status: Passed, DurationMs: duration, Ref: extractRef(name, c.Properties),
	}
	switch {
	case c.Failure != nil:
		res.Status = Failed
		res.ErrorMessage, res.ErrorDetails = outcomeText(c.Failure)
	case c.Error != nil:
		res.Status = Error
		res.ErrorMessage, res.ErrorDetails = outcomeText(c.Error)
	case c.Skipped != nil:
		res.Status = Skipped
		res.ErrorMessage, res.ErrorDetails = outcomeText(c.Skipped)
	}
	return res, nil
}

func outcomeText(o *xmlOutcome) (string, string) {
	msg := o.Message
	if msg == "" {
		msg = o.Type
	}
	return msg, strings.TrimSpace(o.Text)
}

func parseDuration(raw string) (int64, error) {
	raw = strings.ReplaceAll(strings.TrimSpace(raw), ",", "")
	if raw == "" {
		return 0, nil
	}
	secs, err := strconv.ParseFloat(raw, 64)
	if err != nil || secs < 0 || math.IsInf(secs, 0) || math.IsNaN(secs) {
		return 0, fmt.Errorf("invalid time attribute %q", raw)
	}
	return int64(math.Round(secs * 1000)), nil
}

// extractRef resolves the TC-ID reference of a testcase: the tc-id property
// wins; otherwise the TC-<id> pattern in the name is used.
func extractRef(name string, props []xmlProperty) TCRef {
	var values []string
	for _, p := range props {
		if strings.EqualFold(strings.TrimSpace(p.Name), PropertyName) {
			values = append(values, strings.TrimSpace(p.Value))
		}
	}
	if len(values) > 0 {
		return fromProperty(values)
	}
	return fromName(name)
}

func fromProperty(values []string) TCRef {
	raw := strings.Join(uniq(values), ",")
	if len(uniq(values)) > 1 {
		return TCRef{Kind: RefMalformed, Source: SourceProperty, Raw: raw}
	}
	m := propertyValue.FindStringSubmatch(values[0])
	if m == nil {
		return TCRef{Kind: RefMalformed, Source: SourceProperty, Raw: raw}
	}
	id, _ := strconv.ParseInt(m[1], 10, 64)
	return TCRef{Kind: RefFound, Source: SourceProperty, Raw: raw, ID: id}
}

func fromName(name string) TCRef {
	matches := nameRef.FindAllStringSubmatch(name, -1)
	if len(matches) == 0 {
		return TCRef{Kind: RefMissing, Source: SourceNone}
	}
	var refs, ids []string
	malformed := false
	for _, m := range matches {
		refs = append(refs, m[0])
		ids = append(ids, m[1])
		if !validNameID.MatchString(m[1]) {
			malformed = true
		}
	}
	raw := strings.Join(uniq(refs), ",")
	if malformed || len(uniq(ids)) > 1 {
		return TCRef{Kind: RefMalformed, Source: SourceName, Raw: raw}
	}
	id, _ := strconv.ParseInt(ids[0], 10, 64)
	return TCRef{Kind: RefFound, Source: SourceName, Raw: raw, ID: id}
}

func uniq(values []string) []string {
	seen := make(map[string]bool, len(values))
	var out []string
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
