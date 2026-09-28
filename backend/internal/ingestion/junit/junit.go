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
	Index     int
	TestName  string
	ClassName string
	SuiteName string
	Status    Status
	// DurationMs is the rounded `time` in milliseconds; nil when absent or invalid.
	DurationMs   *int64
	ErrorMessage string
	ErrorDetails string
	Ref          TCRef
}

// Severity of a CaseError.
type Severity string

// Severities: an error means data was lost or is unknown; a warning flags a
// suspicious but stored value that deserves review.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// CaseError reports a testcase that could not be fully normalized, or looks
// suspicious. It never stops parsing: Persisted tells whether the result was
// still kept (e.g. with an unknown duration) or discarded (e.g. no name).
type CaseError struct {
	Index     int
	TestName  string
	Message   string
	Persisted bool
	Severity  Severity
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
	propertyValue = regexp.MustCompile(`^(?:TC-)?([0-9]+)$`)
	nameRef       = regexp.MustCompile(`\bTC-([0-9A-Za-z_]*)`)
	numericID     = regexp.MustCompile(`^0*([1-9][0-9]{0,17})$`)
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
		if ts, ok := parseTimestamp(s.Timestamp); ok {
			if rep.StartedAt == nil || ts.Before(*rep.StartedAt) {
				rep.StartedAt = &ts
			}
		}
	}
	for _, c := range s.Cases {
		index := rep.Received
		rep.Received++
		res, issue, err := normalize(c, suite, index)
		if err != nil {
			rep.Errors = append(rep.Errors, CaseError{Index: index, TestName: c.Name, Message: err.Error(), Severity: SeverityError})
			continue
		}
		if issue != nil {
			rep.Errors = append(rep.Errors, *issue)
		}
		rep.Results = append(rep.Results, res)
	}
	for _, child := range s.Suites {
		walk(rep, child, suite)
	}
}

// timestampLayouts are the suite timestamp forms reporters emit: ISO 8601 with
// an offset or Z (e.g. Playwright's toISOString(), with milliseconds) or without
// a zone (Maven Surefire, pytest), read as UTC.
var timestampLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999"}

// parseTimestamp reads a suite timestamp and normalizes it to UTC.
func parseTimestamp(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	for _, layout := range timestampLayouts {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts.UTC(), true
		}
	}
	return time.Time{}, false
}

// normalize returns the result plus an optional issue on a kept result, or an
// error when the testcase cannot be kept at all.
func normalize(c xmlCase, suite string, index int) (Result, *CaseError, error) {
	name := strings.TrimSpace(c.Name)
	if name == "" {
		return Result{}, nil, fmt.Errorf("testcase has no name; result discarded")
	}
	var issue *CaseError
	duration, err := parseDuration(c.Time)
	if err != nil {
		issue = &CaseError{Index: index, TestName: name, Message: err.Error() + "; result kept without duration", Persisted: true, Severity: SeverityError}
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
	if duration != nil && *duration == 0 && (res.Status == Passed || res.Status == Failed) {
		issue = &CaseError{Index: index, TestName: name, Persisted: true, Severity: SeverityWarning,
			Message: fmt.Sprintf("%s test reported a 0 ms duration (0 or under 0.5 ms); review the reporter", res.Status)}
	}
	return res, issue, nil
}

func outcomeText(o *xmlOutcome) (string, string) {
	msg := o.Message
	if msg == "" {
		msg = o.Type
	}
	return msg, strings.TrimSpace(o.Text)
}

// parseDuration converts the JUnit `time` (seconds) to rounded milliseconds;
// nil when the attribute is absent or invalid.
func parseDuration(raw string) (*int64, error) {
	raw = strings.ReplaceAll(strings.TrimSpace(raw), ",", "")
	if raw == "" {
		return nil, nil
	}
	secs, err := strconv.ParseFloat(raw, 64)
	if err != nil || secs < 0 || math.IsInf(secs, 0) || math.IsNaN(secs) {
		return nil, fmt.Errorf("invalid time attribute %q", raw)
	}
	ms := int64(math.Round(secs * 1000))
	return &ms, nil
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

// parseID accepts a positive id of up to 18 significant digits; leading zeros are ignored (TC-0153 == TC-153).
func parseID(digits string) (int64, bool) {
	m := numericID.FindStringSubmatch(digits)
	if m == nil {
		return 0, false
	}
	id, _ := strconv.ParseInt(m[1], 10, 64)
	return id, true
}

// resolve returns the single id declared by all refs, or false when a ref is
// invalid or refs declare different ids (e.g. TC-1 and TC-2 in one testcase).
func resolve(digits []string) (int64, bool) {
	var found int64
	for _, d := range digits {
		id, ok := parseID(d)
		if !ok || (found != 0 && id != found) {
			return 0, false
		}
		found = id
	}
	return found, true
}

func fromProperty(values []string) TCRef {
	raw := strings.Join(uniq(values), ",")
	digits := make([]string, len(values))
	for i, v := range values {
		m := propertyValue.FindStringSubmatch(v)
		if m == nil {
			return TCRef{Kind: RefMalformed, Source: SourceProperty, Raw: raw}
		}
		digits[i] = m[1]
	}
	id, ok := resolve(digits)
	if !ok {
		return TCRef{Kind: RefMalformed, Source: SourceProperty, Raw: raw}
	}
	return TCRef{Kind: RefFound, Source: SourceProperty, Raw: raw, ID: id}
}

func fromName(name string) TCRef {
	matches := nameRef.FindAllStringSubmatch(name, -1)
	if len(matches) == 0 {
		return TCRef{Kind: RefMissing, Source: SourceNone}
	}
	refs := make([]string, len(matches))
	digits := make([]string, len(matches))
	for i, m := range matches {
		refs[i], digits[i] = m[0], m[1]
	}
	raw := strings.Join(uniq(refs), ",")
	id, ok := resolve(digits)
	if !ok {
		return TCRef{Kind: RefMalformed, Source: SourceName, Raw: raw}
	}
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
