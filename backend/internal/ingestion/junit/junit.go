// Package junit parses JUnit XML reports (encoding/xml) into normalized,
// format-independent test results and extracts the declared TC-ID of each
// testcase: first from <property name="tc-id" value="..."/>, falling back to
// the TC-<id> pattern in the testcase name.
package junit

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
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
	// Prefix is the project key declared with the number (CHK in CHK-12), upper-cased;
	// empty for a bare number, which refers to the run's project.
	Prefix string
	// ID is the test case number in its project when Kind is RefFound.
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
	// Attempt numbers the executions of one test in the run (1 = first). Retries are read only when the report
	// says so: Surefire <flakyFailure>/<rerunFailure> elements or an attempt/retry property.
	Attempt int
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
	// Notices are document-level remarks (e.g. an unreadable suite timestamp), each once.
	Notices []string

	nameRef *regexp.Regexp
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
	// StackTrace is where Surefire puts the details of flaky and rerun attempts.
	StackTrace string `xml:"stackTrace"`
}

type xmlCase struct {
	Name       string        `xml:"name,attr"`
	ClassName  string        `xml:"classname,attr"`
	Time       string        `xml:"time,attr"`
	Properties []xmlProperty `xml:"properties>property"`
	Failures   []xmlOutcome  `xml:"failure"`
	Errors     []xmlOutcome  `xml:"error"`
	Skipped    *xmlOutcome   `xml:"skipped"`
	// Maven Surefire retries: failed attempts before a pass, and failed reruns after a failure.
	FlakyFailures []xmlOutcome `xml:"flakyFailure"`
	FlakyErrors   []xmlOutcome `xml:"flakyError"`
	RerunFailures []xmlOutcome `xml:"rerunFailure"`
	RerunErrors   []xmlOutcome `xml:"rerunError"`
	// Nested elements are not valid JUnit: they are reported, not read.
	NestedSuites []struct{} `xml:"testsuite"`
	NestedCases  []struct{} `xml:"testcase"`
}

type xmlSuite struct {
	XMLName    xml.Name
	Name       string        `xml:"name,attr"`
	Timestamp  string        `xml:"timestamp,attr"`
	Properties []xmlProperty `xml:"properties>property"`
	Suites     []xmlSuite    `xml:"testsuite"`
	Cases      []xmlCase     `xml:"testcase"`
}

var (
	// A tc-id property is a bare number or <KEY>-<number> with any project key.
	propertyValue = regexp.MustCompile(`^(?:([A-Za-z][A-Za-z0-9]{1,9})-)?([0-9]+)$`)
	numericID     = regexp.MustCompile(`^0*([1-9][0-9]{0,17})$`)
)

// AttemptProperty (1-based) and RetryProperty (0-based, as Playwright counts) number the attempt of a testcase.
const (
	AttemptProperty = "attempt"
	RetryProperty   = "retry"
	// MaxAttempts bounds the attempts of one test (a larger number is a broken reporter).
	MaxAttempts = 100
)

// DefaultProjectKey is the key the name fallback looks for when none is given.
const DefaultProjectKey = "TC"

// nameRefFor matches <KEY>-<id> references of one project in testcase names.
// Any letters or digits after the dash are captured, so a non-ASCII id (TC-１５３)
// is reported as declared (malformed) instead of being cut. Only the run's own
// key is looked for: other upper-case words with a dash (HTTP-200, UTF-8) in
// names are not references.
func nameRefFor(key string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(key) + `-([\p{L}\p{N}_]*)`)
}

var (
	defaultNameRef = nameRefFor(DefaultProjectKey)
)

// Parse reads a JUnit XML document with a <testsuites> or <testsuite> root.
// An unreadable document is an error; an invalid testcase is reported in
// Report.Errors and the rest of the document is still parsed.
func Parse(r io.Reader) (Report, error) {
	return ParseWithCharset(r, "")
}

// Options configure ParseWith.
type Options struct {
	// Charset is the Content-Type charset; empty defers to the document.
	Charset string
	// ProjectKey is the key the name fallback looks for (default TC).
	ProjectKey string
}

// ParseWith is Parse with a declared charset and the run's project key.
func ParseWith(r io.Reader, o Options) (Report, error) {
	nameRef := defaultNameRef
	if o.ProjectKey != "" && o.ProjectKey != DefaultProjectKey {
		nameRef = nameRefFor(o.ProjectKey)
	}
	return parse(r, o.Charset, nameRef)
}

// ParseWithCharset is Parse for a body whose charset was declared out of band
// (the Content-Type charset parameter): it overrides the document's XML
// declaration (RFC 7303). An empty charset defers to the document.
func ParseWithCharset(r io.Reader, charset string) (Report, error) {
	return parse(r, charset, defaultNameRef)
}

func parse(r io.Reader, charset string, nameRef *regexp.Regexp) (Report, error) {
	var err error
	if charset != "" {
		if r, err = charsetReader(charset, r); err != nil {
			return Report{}, fmt.Errorf("invalid JUnit XML: %w", err)
		}
	} else if r, err = fromUTF16(r); err != nil {
		return Report{}, fmt.Errorf("invalid JUnit XML: %w", err)
	}
	dec := xml.NewDecoder(r)
	dec.CharsetReader = charsetReader
	if charset != "" {
		// Already UTF-8: the declared encoding must not transcode it again.
		dec.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) { return input, nil }
	}
	var root xmlSuite
	if err := dec.Decode(&root); err != nil {
		return Report{}, fmt.Errorf("invalid JUnit XML: %w", err)
	}
	if root.XMLName.Local != "testsuites" && root.XMLName.Local != "testsuite" {
		return Report{}, fmt.Errorf("invalid JUnit XML: unsupported root element <%s>", root.XMLName.Local)
	}
	if err := checkNothingAfterRoot(dec); err != nil {
		return Report{}, fmt.Errorf("invalid JUnit XML: %w", err)
	}
	rep := Report{nameRef: nameRef}
	walk(&rep, root, "")
	rep.noticeVariants()
	return rep, nil
}

// checkNothingAfterRoot rejects a second root (e.g. two concatenated reports) or
// stray text instead of silently ignoring it; whitespace, comments and processing
// instructions may follow the root.
func checkNothingAfterRoot(dec *xml.Decoder) error {
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.Comment, xml.ProcInst, xml.Directive:
		case xml.CharData:
			if len(bytes.TrimSpace(t)) > 0 {
				return errors.New("text after the root element")
			}
		default:
			return errors.New("content after the root element (a report has a single <testsuites> or <testsuite> root)")
		}
	}
}

// fromUTF16 transcodes a UTF-16 document (detected by its byte order mark or by
// "<?" encoded in UTF-16, per the XML spec) to UTF-8; any other input is returned as is.
func fromUTF16(r io.Reader) (io.Reader, error) {
	br := bufio.NewReader(r)
	head, _ := br.Peek(4)
	var bigEndian bool
	switch {
	case bytes.HasPrefix(head, []byte{0xFE, 0xFF}), bytes.HasPrefix(head, []byte{0, '<', 0, '?'}):
		bigEndian = true
	case bytes.HasPrefix(head, []byte{0xFF, 0xFE}), bytes.HasPrefix(head, []byte{'<', 0, '?', 0}):
	default:
		return br, nil
	}
	raw, err := io.ReadAll(br)
	if err != nil {
		return nil, err
	}
	if len(raw)%2 != 0 {
		return nil, errors.New("truncated UTF-16 document")
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		hi, lo := raw[2*i], raw[2*i+1]
		if !bigEndian {
			hi, lo = lo, hi
		}
		units[i] = uint16(hi)<<8 | uint16(lo)
	}
	if len(units) > 0 && units[0] == 0xFEFF {
		units = units[1:]
	}
	return strings.NewReader(string(utf16.Decode(units))), nil
}

// windows1252 maps the bytes 0x80-0x9F that differ from ISO-8859-1 (undefined ones map to themselves).
var windows1252 = [32]rune{
	0x20AC, 0x81, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021, 0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x8D, 0x017D, 0x8F,
	0x90, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x9D, 0x017E, 0x0178,
}

// SupportedCharset reports whether a document or Content-Type charset can be read.
func SupportedCharset(label string) bool {
	switch strings.ToLower(label) {
	case "utf-8", "utf8", "us-ascii", "ascii", "utf-16", "utf-16le", "utf-16be",
		"iso-8859-1", "iso8859-1", "latin1", "latin-1", "l1", "windows-1252", "cp1252":
		return true
	}
	return false
}

// charsetReader supports the non-UTF-8 encodings reporters declare in practice.
func charsetReader(label string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(label) {
	case "utf-8", "utf8", "us-ascii", "ascii": // ASCII is UTF-8
		return input, nil
	case "utf-16", "utf-16le", "utf-16be": // transcoded from its byte order mark or "<?" pattern
		return fromUTF16(input)
	case "iso-8859-1", "iso8859-1", "latin1", "latin-1", "l1", "windows-1252", "cp1252":
		raw, err := io.ReadAll(input)
		if err != nil {
			return nil, err
		}
		cp1252 := strings.Contains(strings.ToLower(label), "1252")
		out := make([]byte, 0, len(raw))
		for _, b := range raw {
			r := rune(b)
			if cp1252 && b >= 0x80 && b <= 0x9F {
				r = windows1252[b-0x80]
			}
			out = utf8.AppendRune(out, r)
		}
		return bytes.NewReader(out), nil
	}
	return nil, fmt.Errorf("unsupported encoding %q (use UTF-8)", label)
}

func walk(rep *Report, s xmlSuite, parent string) {
	suite := parent
	if s.XMLName.Local == "testsuite" {
		suite = s.Name
		if ts, ok := parseTimestamp(s.Timestamp); ok {
			if rep.StartedAt == nil || ts.Before(*rep.StartedAt) {
				rep.StartedAt = &ts
			}
		} else if strings.TrimSpace(s.Timestamp) != "" {
			rep.notice(fmt.Sprintf("suite timestamp %q could not be read; it does not set startedAt", s.Timestamp))
		}
	}
	suiteDeclaresID := len(propertyValues(s.Properties)) > 0
	for _, c := range s.Cases {
		index := rep.Received
		rep.Received++
		res, issues, err := normalize(c, suite, index, rep.nameRef)
		if err != nil {
			rep.Errors = append(rep.Errors, CaseError{Index: index, TestName: c.Name, Message: err.Error(), Severity: SeverityError})
			continue
		}
		if suiteDeclaresID && res.Ref.Kind == RefMissing {
			issues = append(issues, warning(index, res.TestName,
				fmt.Sprintf("testsuite %q declares a tc-id property, which is ignored: declare it on each testcase", s.Name)))
		}
		attempts, attemptIssues := expandAttempts(c, res)
		rep.Errors = append(rep.Errors, issues...)
		rep.Errors = append(rep.Errors, attemptIssues...)
		rep.Received += len(attempts) - 1
		rep.Results = append(rep.Results, attempts...)
	}
	for _, child := range s.Suites {
		walk(rep, child, suite)
	}
}

// VariantsNotice tells that testcases repeat another's suite, class and name without an attempt or retry signal:
// they are kept as variants of one test, and a failed one fails the test case (never read as a flaky retry).
const VariantsNotice = "%d testcase(s) repeat the suite, class and name of another without an attempt or retry signal: " +
	"counted as variants, a failure among them fails the test case"

// noticeVariants counts the results that share suite, class, name and attempt with an earlier one.
func (rep *Report) noticeVariants() {
	type key struct {
		suite, class, name string
		attempt            int
	}
	seen := make(map[key]bool, len(rep.Results))
	n := 0
	for _, r := range rep.Results {
		k := key{r.SuiteName, r.ClassName, r.TestName, r.Attempt}
		if seen[k] {
			n++
		}
		seen[k] = true
	}
	if n > 0 {
		rep.notice(fmt.Sprintf(VariantsNotice, n))
	}
}

func (rep *Report) notice(msg string) {
	if !slices.Contains(rep.Notices, msg) {
		rep.Notices = append(rep.Notices, msg)
	}
}

func warning(index int, name, msg string) CaseError {
	return CaseError{Index: index, TestName: name, Message: msg, Persisted: true, Severity: SeverityWarning}
}

// timestampLayouts are the suite timestamp forms reporters emit: ISO 8601 with
// an offset or Z (e.g. Playwright's toISOString(), with milliseconds), an offset
// without colon (+0530), a space instead of T, or no zone (Maven Surefire,
// pytest), read as UTC.
var timestampLayouts = []string{
	time.RFC3339Nano, "2006-01-02T15:04:05.999999999-0700", "2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05.999999999-0700", "2006-01-02 15:04:05.999999999",
}

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

// normalize returns the result plus the issues on a kept result, or an error
// when the testcase cannot be kept at all.
func normalize(c xmlCase, suite string, index int, nameRef *regexp.Regexp) (Result, []CaseError, error) {
	name := strings.TrimSpace(c.Name)
	if name == "" {
		return Result{}, nil, fmt.Errorf("testcase has no name; result discarded")
	}
	var issues []CaseError
	duration, err := parseDuration(c.Time)
	if err != nil {
		issues = append(issues, CaseError{Index: index, TestName: name, Message: err.Error() + "; result kept without duration", Persisted: true, Severity: SeverityError})
	}
	res := Result{
		Index: index, TestName: name, ClassName: c.ClassName, SuiteName: suite,
		Status: Passed, DurationMs: duration, Ref: extractRef(name, c.Properties, nameRef),
	}
	switch {
	case len(c.Failures) > 0:
		res.Status = Failed
	case len(c.Errors) > 0:
		res.Status = Error
	case c.Skipped != nil:
		res.Status = Skipped
		res.ErrorMessage, res.ErrorDetails = outcomeText(*c.Skipped)
	}
	if res.Status == Failed || res.Status == Error {
		res.ErrorMessage, res.ErrorDetails = outcomesText(c.Failures, c.Errors)
	}
	if duration != nil && *duration == 0 && (res.Status == Passed || res.Status == Failed) {
		issues = append(issues, warning(index, name,
			fmt.Sprintf("%s test reported a 0 ms duration (0 or under 0.5 ms); review the reporter", res.Status)))
	}
	if len(c.NestedSuites)+len(c.NestedCases) > 0 {
		issues = append(issues, warning(index, name, "testcase contains nested <testsuite>/<testcase> elements, which are not read"))
	}
	return res, issues, nil
}

func outcomeText(o xmlOutcome) (string, string) {
	msg := o.Message
	if msg == "" {
		msg = o.Type
	}
	details := strings.TrimSpace(o.Text)
	if details == "" {
		details = strings.TrimSpace(o.StackTrace)
	}
	return msg, details
}

// declaredAttempt reads the attempt/retry property of a testcase (0 when absent).
func declaredAttempt(props []xmlProperty) (int, error) {
	for _, p := range props {
		name := strings.ToLower(strings.TrimSpace(p.Name))
		if name != AttemptProperty && name != RetryProperty {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(p.Value))
		if name == RetryProperty {
			n++
		}
		if err != nil || n < 1 || n > MaxAttempts {
			return 0, fmt.Errorf("%s property %q is not a valid attempt (attempt 1..%d, retry 0..%d); read as the first attempt",
				name, p.Value, MaxAttempts, MaxAttempts-1)
		}
		return n, nil
	}
	return 0, nil
}

// expandAttempts turns one testcase into the attempts it reports (D1): Surefire's flaky elements are failed
// attempts before the final pass; its rerun elements are failed attempts after the first failure. The last
// attempt is the test's logical result.
func expandAttempts(c xmlCase, res Result) ([]Result, []CaseError) {
	var issues []CaseError
	first, err := declaredAttempt(c.Properties)
	if err != nil {
		issues = append(issues, warning(res.Index, res.TestName, err.Error()))
	}
	first = max(first, 1)
	retried := func(status Status, o xmlOutcome) Result {
		r := res
		r.Status, r.DurationMs = status, nil
		r.ErrorMessage, r.ErrorDetails = outcomeText(o)
		return r
	}
	var out []Result
	for _, o := range c.FlakyFailures {
		out = append(out, retried(Failed, o))
	}
	for _, o := range c.FlakyErrors {
		out = append(out, retried(Error, o))
	}
	out = append(out, res)
	for _, o := range c.RerunFailures {
		out = append(out, retried(Failed, o))
	}
	for _, o := range c.RerunErrors {
		out = append(out, retried(Error, o))
	}
	if first+len(out)-1 > MaxAttempts {
		issues = append(issues, warning(res.Index, res.TestName, fmt.Sprintf("more than %d attempts; only the last %d are kept", MaxAttempts, MaxAttempts)))
		out = out[len(out)-(MaxAttempts-first+1):]
	}
	for i := range out {
		out[i].Attempt = first + i
	}
	return out, issues
}

// outcomesText keeps every <failure> and <error> of a testcase: the message is
// the first one's (failures first, as failure decides the status), and with more
// than one outcome the details list each of them, so none is lost.
func outcomesText(failures, errs []xmlOutcome) (string, string) {
	type labeled struct {
		kind string
		o    xmlOutcome
	}
	var all []labeled
	for _, f := range failures {
		all = append(all, labeled{"failure", f})
	}
	for _, e := range errs {
		all = append(all, labeled{"error", e})
	}
	msg, details := outcomeText(all[0].o)
	if len(all) == 1 {
		return msg, details
	}
	parts := make([]string, len(all))
	for i, l := range all {
		m, d := outcomeText(l.o)
		parts[i] = strings.TrimSpace(l.kind + ": " + m + "\n" + d)
	}
	return msg, strings.Join(parts, "\n\n")
}

// maxDurationMs is the largest duration a JSON client reads exactly (2^53-1 ms, ~285,000 years);
// anything larger is a broken reporter, not a real duration.
const maxDurationMs int64 = 1<<53 - 1

var decimalSeconds = regexp.MustCompile(`^\+?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][-+]?[0-9]+)?$`)

// parseDuration converts the JUnit `time` (seconds) to rounded milliseconds;
// nil when the attribute is absent or invalid.
func parseDuration(raw string) (*int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	plain := plainDecimal(raw)
	if !decimalSeconds.MatchString(plain) {
		return nil, fmt.Errorf("invalid time attribute %q", raw)
	}
	secs, err := strconv.ParseFloat(plain, 64)
	ms := math.Round(secs * 1000)
	if err != nil || ms > float64(maxDurationMs) {
		return nil, fmt.Errorf("invalid time attribute %q", raw)
	}
	v := int64(ms)
	return &v, nil
}

// plainDecimal undoes locale formatting of the seconds: with both separators the
// last one is the decimal point ("1,234.5", "1.234,5"); a single comma is a decimal
// comma ("0,123"); a separator repeated alone is digit grouping ("1,234,567").
func plainDecimal(s string) string {
	commas, dots := strings.Count(s, ","), strings.Count(s, ".")
	switch {
	case commas > 0 && dots > 0 && strings.LastIndex(s, ",") > strings.LastIndex(s, "."):
		return strings.Replace(strings.ReplaceAll(s, ".", ""), ",", ".", 1)
	case commas > 0 && dots > 0, commas > 1:
		return strings.ReplaceAll(s, ",", "")
	case commas == 1:
		return strings.Replace(s, ",", ".", 1)
	case dots > 1:
		return strings.ReplaceAll(s, ".", "")
	}
	return s
}

// extractRef resolves the TC-ID reference of a testcase: the tc-id property
// wins; otherwise the TC-<id> pattern in the name is used.
func extractRef(name string, props []xmlProperty, nameRef *regexp.Regexp) TCRef {
	if values := propertyValues(props); len(values) > 0 {
		return fromProperty(values)
	}
	return fromName(name, nameRef)
}

// propertyValues returns the trimmed values of the tc-id properties.
func propertyValues(props []xmlProperty) []string {
	var values []string
	for _, p := range props {
		if strings.EqualFold(strings.TrimSpace(p.Name), PropertyName) {
			values = append(values, strings.TrimSpace(p.Value))
		}
	}
	return values
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
	prefix := ""
	for i, v := range values {
		m := propertyValue.FindStringSubmatch(v)
		if m == nil {
			return TCRef{Kind: RefMalformed, Source: SourceProperty, Raw: raw}
		}
		// A bare number agrees with any key; CHK-12 and WEB-12 in one testcase are not one reference.
		switch p := strings.ToUpper(m[1]); {
		case p == "" || p == prefix:
		case prefix == "":
			prefix = p
		default:
			return TCRef{Kind: RefMalformed, Source: SourceProperty, Raw: raw}
		}
		digits[i] = m[2]
	}
	id, ok := resolve(digits)
	if !ok {
		return TCRef{Kind: RefMalformed, Source: SourceProperty, Raw: raw}
	}
	return TCRef{Kind: RefFound, Source: SourceProperty, Raw: raw, Prefix: prefix, ID: id}
}

func fromName(name string, nameRef *regexp.Regexp) TCRef {
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

// ParseRef reads a TC-ID reference given on its own (e.g. a live event's testCase): CHK-12, TC-12 or 12. An empty
// value is a missing reference.
func ParseRef(value string) TCRef {
	value = strings.TrimSpace(value)
	if value == "" {
		return TCRef{Kind: RefMissing, Source: SourceNone}
	}
	return fromProperty([]string{value})
}
