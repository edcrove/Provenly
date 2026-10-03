package junit

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStatusesAndFields(t *testing.T) {
	doc := `<?xml version="1.0"?>
<testsuites>
  <testsuite name="auth" timestamp="2026-09-28T10:00:00">
    <testcase name="passes" classname="pkg.Auth" time="1.2345">
      <properties><property name="tc-id" value="153"/></properties>
    </testcase>
    <testcase name="fails TC-7" time="0.5"><failure message="expected 1" type="AssertionError">trace
</failure></testcase>
    <testcase name="errors TC-8"><error type="NullPointer">npe</error></testcase>
    <testcase name="skips TC-9"><skipped message="not today"/></testcase>
  </testsuite>
  <testsuite name="outer" timestamp="2026-09-28T09:00:00Z">
    <testsuite name="inner"><testcase name="nested TC-10" time="1,000.5"/></testsuite>
  </testsuite>
</testsuites>`
	rep, err := Parse(strings.NewReader(doc))
	require.NoError(t, err)
	assert.Equal(t, 5, rep.Received)
	assert.Empty(t, rep.Errors)
	require.Len(t, rep.Results, 5)

	assert.Equal(t, Result{Index: 0, TestName: "passes", ClassName: "pkg.Auth", SuiteName: "auth", Status: Passed, DurationMs: ms(1235),
		Ref: TCRef{Kind: RefFound, Source: SourceProperty, Raw: "153", ID: 153}}, rep.Results[0])
	assert.Equal(t, Failed, rep.Results[1].Status)
	assert.Equal(t, "expected 1", rep.Results[1].ErrorMessage)
	assert.Equal(t, "trace", rep.Results[1].ErrorDetails)
	assert.Equal(t, Error, rep.Results[2].Status)
	assert.Equal(t, "NullPointer", rep.Results[2].ErrorMessage, "falls back to type when message is empty")
	assert.Equal(t, Skipped, rep.Results[3].Status)
	assert.Equal(t, "not today", rep.Results[3].ErrorMessage)
	assert.Equal(t, "inner", rep.Results[4].SuiteName)
	assert.Equal(t, int64(1000500), *rep.Results[4].DurationMs)
	assert.Nil(t, rep.Results[2].DurationMs, "absent time is unknown, not 0")
	assert.Equal(t, time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC), *rep.StartedAt)
}

func TestParseSuiteTimestampFormats(t *testing.T) {
	want := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	for _, ts := range []string{
		"2026-09-28T10:00:00",           // no zone (Surefire, pytest): UTC
		"2026-09-28T10:00:00Z",          // UTC designator
		"2026-09-28T10:00:00.000Z",      // Playwright: Date.toISOString()
		"2026-09-28T07:00:00-03:00",     // explicit offset
		"2026-09-28T12:00:00.000+02:00", // offset with fraction
		" 2026-09-28T10:00:00 ",         // surrounding spaces
	} {
		rep, err := Parse(strings.NewReader(`<testsuite name="s" timestamp="` + ts + `"><testcase name="TC-1 ok"/></testsuite>`))
		require.NoError(t, err)
		if assert.NotNil(t, rep.StartedAt, ts) {
			assert.Equal(t, want, *rep.StartedAt, ts)
		}
	}
	rep, err := Parse(strings.NewReader(`<testsuites><testsuite name="a" timestamp="2026-09-28T10:00:00.500Z"/><testsuite name="b" timestamp="2026-09-28T12:00:00+03:00"/></testsuites>`))
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC), *rep.StartedAt, "the earliest instant wins across zones")
}

func TestParseSingleSuiteRoot(t *testing.T) {
	rep, err := Parse(strings.NewReader(`<testsuite name="s" timestamp="bad"><testcase name="TC-1 ok"/></testsuite>`))
	require.NoError(t, err)
	assert.Nil(t, rep.StartedAt)
	require.Len(t, rep.Results, 1)
	assert.Equal(t, "s", rep.Results[0].SuiteName)
	assert.Equal(t, int64(1), rep.Results[0].Ref.ID)
}

func TestParseInvalidCasesDoNotStopParsing(t *testing.T) {
	rep, err := Parse(strings.NewReader(`<testsuites><testsuite name="s">
<testcase name=""/>
<testcase name="bad time" time="soon"><failure/></testcase>
<testcase name="negative" time="-1"/>
<testcase name="tiny" time="0.0004"/>
<testcase name="zero fail" time="0"><failure/></testcase>
<testcase name="zero skip" time="0"><skipped/></testcase>
<testcase name="zero error" time="0"><error/></testcase>
<testcase name="good"/>
</testsuite></testsuites>`))
	require.NoError(t, err)
	assert.Equal(t, 8, rep.Received)
	names := make([]string, len(rep.Results))
	for i, r := range rep.Results {
		names[i] = r.TestName
	}
	assert.Equal(t, []string{"bad time", "negative", "tiny", "zero fail", "zero skip", "zero error", "good"}, names, "only the nameless testcase is discarded")
	assert.Nil(t, rep.Results[0].DurationMs)
	assert.Equal(t, Failed, rep.Results[0].Status, "an invalid time never hides a failure")
	assert.Equal(t, int64(0), *rep.Results[2].DurationMs, "sub-millisecond durations round to 0")
	assert.Equal(t, []CaseError{
		{Index: 0, TestName: "", Message: "testcase has no name; result discarded", Severity: SeverityError},
		{Index: 1, TestName: "bad time", Message: `invalid time attribute "soon"; result kept without duration`, Persisted: true, Severity: SeverityError},
		{Index: 2, TestName: "negative", Message: `invalid time attribute "-1"; result kept without duration`, Persisted: true, Severity: SeverityError},
		{Index: 3, TestName: "tiny", Message: "passed test reported a 0 ms duration (0 or under 0.5 ms); review the reporter", Persisted: true, Severity: SeverityWarning},
		{Index: 4, TestName: "zero fail", Message: "failed test reported a 0 ms duration (0 or under 0.5 ms); review the reporter", Persisted: true, Severity: SeverityWarning},
	}, rep.Errors, "skipped and error results with 0 ms are not flagged")
}

func ms(v int64) *int64 { return &v }

func TestParseKeepsEveryFailureOfATestcase(t *testing.T) {
	rep, err := Parse(strings.NewReader(`<testsuite name="s">
<testcase name="two failures"><failure message="first">d1</failure><failure message="second">d2</failure></testcase>
<testcase name="failure and error"><failure message="F" type="AssertionError">trace</failure><error message="E">npe</error></testcase>
<testcase name="one failure"><failure message="only">trace</failure></testcase>
</testsuite>`))
	require.NoError(t, err)
	require.Len(t, rep.Results, 3)
	assert.Equal(t, Failed, rep.Results[0].Status)
	assert.Equal(t, "first", rep.Results[0].ErrorMessage)
	assert.Equal(t, "failure: first\nd1\n\nfailure: second\nd2", rep.Results[0].ErrorDetails)
	assert.Equal(t, Failed, rep.Results[1].Status, "failure wins over error")
	assert.Equal(t, "F", rep.Results[1].ErrorMessage)
	assert.Equal(t, "failure: F\ntrace\n\nerror: E\nnpe", rep.Results[1].ErrorDetails)
	assert.Equal(t, "trace", rep.Results[2].ErrorDetails, "a single outcome keeps its text as is")
	assert.Empty(t, rep.Errors)
}

func TestParseWarnsAboutIgnoredStructure(t *testing.T) {
	rep, err := Parse(strings.NewReader(`<testsuites>
<testsuite name="declared on suite"><properties><property name="tc-id" value="7"/></properties>
  <testcase name="inherits nothing"/>
  <testcase name="own id"><properties><property name="tc-id" value="8"/></properties></testcase>
</testsuite>
<testsuite name="nested"><testcase name="outer TC-9"><testsuite name="in"><testcase name="inner TC-9"/></testsuite></testcase></testsuite>
</testsuites>`))
	require.NoError(t, err)
	assert.Equal(t, 3, rep.Received)
	assert.Equal(t, []CaseError{
		{Index: 0, TestName: "inherits nothing", Persisted: true, Severity: SeverityWarning,
			Message: `testsuite "declared on suite" declares a tc-id property, which is ignored: declare it on each testcase`},
		{Index: 2, TestName: "outer TC-9", Persisted: true, Severity: SeverityWarning,
			Message: "testcase contains nested <testsuite>/<testcase> elements, which are not read"},
	}, rep.Errors)
}

func TestParseSuiteTimestampNotices(t *testing.T) {
	for _, c := range []struct{ raw, want string }{
		{"2026-10-03T10:00:00+0530", "2026-10-03T04:30:00Z"},
		{"2026-10-03 10:00:00", "2026-10-03T10:00:00Z"},
		{"2026-10-03 10:00:00.5-03:00", "2026-10-03T13:00:00.5Z"},
	} {
		rep, err := Parse(strings.NewReader(`<testsuite name="s" timestamp="` + c.raw + `"><testcase name="t"/></testsuite>`))
		require.NoError(t, err)
		require.NotNil(t, rep.StartedAt, c.raw)
		assert.Equal(t, c.want, rep.StartedAt.Format(time.RFC3339Nano), c.raw)
		assert.Empty(t, rep.Notices, c.raw)
	}
	rep, err := Parse(strings.NewReader(`<testsuites><testsuite name="a" timestamp="2026-02-30T10:00:00"/><testsuite name="b" timestamp="soon"/><testsuite name="c" timestamp="soon"/><testsuite name="d" timestamp=""/></testsuites>`))
	require.NoError(t, err)
	assert.Nil(t, rep.StartedAt)
	assert.Equal(t, []string{
		`suite timestamp "2026-02-30T10:00:00" could not be read; it does not set startedAt`,
		`suite timestamp "soon" could not be read; it does not set startedAt`,
	}, rep.Notices, "each unreadable value once; an absent timestamp is not reported")
}

func TestParseDurationFormats(t *testing.T) {
	cases := []struct {
		raw  string
		want *int64
	}{
		{"1.5", ms(1500)}, {"+2", ms(2000)}, {".25", ms(250)}, {"1e-3", ms(1)}, {"1.5E2", ms(150000)},
		{"1,000.5", ms(1000500)}, {"1,234,567", ms(1234567000)}, // grouping commas (en-US)
		{"0,123", ms(123)}, {"1,5", ms(1500)}, {"1.234,5", ms(1234500)}, {"1.234.567", ms(1234567000)}, // decimal comma (es/de locales)
		{"9007199254740", ms(9007199254740000)}, // just under the largest duration a JSON client reads exactly
	}
	for _, c := range cases {
		got, err := parseDuration(c.raw)
		require.NoError(t, err, c.raw)
		assert.Equal(t, c.want, got, c.raw)
	}
	for _, raw := range []string{"9007199254741", "1e300", "1e400", "9300000000000000", "0x1p3", "NaN", "Inf", "-1", "1,2,3.4,5", "1.2.3,4.5", "soon", "1 000"} {
		got, err := parseDuration(raw)
		assert.Error(t, err, raw)
		assert.Nil(t, got, raw)
	}
}

func TestParseDeclaredEncodings(t *testing.T) {
	body := `<testsuite name="s"><testcase name="café € TC-1" time="1"/></testsuite>`
	latin1 := append([]byte(`<?xml version="1.0" encoding="ISO-8859-1"?><testsuite name="s"><testcase name="caf`), 0xE9)
	latin1 = append(latin1, []byte(` TC-1" time="1"/></testsuite>`)...)
	cp1252 := append([]byte(`<?xml version="1.0" encoding="windows-1252"?><testsuite name="s"><testcase name="caf`), 0xE9, ' ', 0x80)
	cp1252 = append(cp1252, []byte(` TC-1" time="1"/></testsuite>`)...)
	utf16 := func(bigEndian, bom bool) []byte {
		doc := `<?xml version="1.0" encoding="UTF-16"?>` + body
		var out []byte
		if bom {
			if bigEndian {
				out = append(out, 0xFE, 0xFF)
			} else {
				out = append(out, 0xFF, 0xFE)
			}
		}
		for _, u := range utf16Encode(doc) {
			if bigEndian {
				out = append(out, byte(u>>8), byte(u))
			} else {
				out = append(out, byte(u), byte(u>>8))
			}
		}
		return out
	}
	for name, c := range map[string]struct {
		doc  []byte
		want string
	}{
		"utf-8":        {[]byte(`<?xml version="1.0" encoding="utf-8"?>` + body), "caf\u00e9 \u20ac TC-1"},
		"us-ascii":     {[]byte(`<?xml version="1.0" encoding="US-ASCII"?><testsuite name="s"><testcase name="plain TC-1"/></testsuite>`), "plain TC-1"},
		"iso-8859-1":   {latin1, "caf\u00e9 TC-1"},
		"windows-1252": {cp1252, "caf\u00e9 \u20ac TC-1"},
		"utf-16le bom": {utf16(false, true), "caf\u00e9 \u20ac TC-1"},
		"utf-16be bom": {utf16(true, true), "caf\u00e9 \u20ac TC-1"},
		"utf-16le":     {utf16(false, false), "caf\u00e9 \u20ac TC-1"},
		"utf-16be":     {utf16(true, false), "caf\u00e9 \u20ac TC-1"},
	} {
		rep, err := Parse(strings.NewReader(string(c.doc)))
		require.NoError(t, err, name)
		require.Len(t, rep.Results, 1, name)
		assert.Equal(t, c.want, rep.Results[0].TestName, name)
		assert.Equal(t, int64(1), rep.Results[0].Ref.ID, name)
	}
	_, err := Parse(strings.NewReader(`<?xml version="1.0" encoding="Shift_JIS"?><testsuite/>`))
	assert.ErrorContains(t, err, `unsupported encoding "Shift_JIS"`)
	_, err = Parse(strings.NewReader(string([]byte{0xFF, 0xFE, '<'})))
	assert.ErrorContains(t, err, "invalid JUnit XML", "odd-length UTF-16")
}

func utf16Encode(s string) []uint16 {
	var out []uint16
	for _, r := range s {
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
			continue
		}
		out = append(out, uint16(r))
	}
	return out
}

func TestParseRejectsContentAfterTheRoot(t *testing.T) {
	for _, doc := range []string{
		`<testsuite name="a"><testcase name="r1"/></testsuite><testsuite name="b"><testcase name="r2"/></testsuite>`,
		`<testsuite name="a"><testcase name="r1"/></testsuite>trailing text`,
		`<testsuite name="a"><testcase name="r1"/></testsuite><junk`,
	} {
		_, err := Parse(strings.NewReader(doc))
		require.Error(t, err, doc)
		assert.Contains(t, err.Error(), "invalid JUnit XML", doc)
	}
	rep, err := Parse(strings.NewReader("<testsuite name=\"a\"><testcase name=\"r1\"/></testsuite>\n<!-- generated -->\n<?pi x?>\n"))
	require.NoError(t, err, "whitespace, comments and processing instructions may follow the root")
	assert.Len(t, rep.Results, 1)
}

func TestParseDocumentErrors(t *testing.T) {
	for _, doc := range []string{"", "not xml", "<html></html>", "<testsuites><testsuite>"} {
		_, err := Parse(strings.NewReader(doc))
		require.Error(t, err, doc)
		assert.Contains(t, err.Error(), "invalid JUnit XML")
	}
	readErr := errors.New("read failed")
	_, err := Parse(failingReader{readErr})
	assert.ErrorIs(t, err, readErr)
	_, err = Parse(io.MultiReader(strings.NewReader("\xff\xfe<\x00"), failingReader{readErr}))
	assert.ErrorIs(t, err, readErr, "read failure while transcoding UTF-16")
	_, err = charsetReader("ISO-8859-1", failingReader{readErr})
	assert.ErrorIs(t, err, readErr, "read failure while transcoding Latin-1")
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

// A data provider / parameterized test reports one <testcase> per data row, so
// each invocation declares its own TC-ID independently.
func TestParameterizedTestsDeclareOneIDPerInvocation(t *testing.T) {
	rep, err := Parse(strings.NewReader(`<testsuite name="login">
<testcase name="login [1] TC-153"/>
<testcase name="login [2] TC-154"><failure/></testcase>
<testcase name="login[chrome]"><properties><property name="tc-id" value="155"/></properties></testcase>
<testcase name="login[firefox]"><properties><property name="tc-id" value="156"/></properties></testcase>
</testsuite>`))
	require.NoError(t, err)
	ids := make([]int64, len(rep.Results))
	for i, r := range rep.Results {
		require.Equal(t, RefFound, r.Ref.Kind)
		ids[i] = r.Ref.ID
	}
	assert.Equal(t, []int64{153, 154, 155, 156}, ids)
	assert.Equal(t, Failed, rep.Results[1].Status)
}

func TestExtractRef(t *testing.T) {
	prop := func(values ...string) []xmlProperty {
		var out []xmlProperty
		for _, v := range values {
			out = append(out, xmlProperty{Name: "tc-id", Value: v})
		}
		return out
	}
	cases := []struct {
		name  string
		props []xmlProperty
		want  TCRef
	}{
		{"plain", nil, TCRef{Kind: RefMissing, Source: SourceNone}},
		{"etc-5 is not a ref", nil, TCRef{Kind: RefMissing, Source: SourceNone}},
		{"TC-153 login", nil, TCRef{Kind: RefFound, Source: SourceName, Raw: "TC-153", ID: 153}},
		{"TC-153 again TC-153", nil, TCRef{Kind: RefFound, Source: SourceName, Raw: "TC-153", ID: 153}},
		{"TC-1 and TC-2", nil, TCRef{Kind: RefMalformed, Source: SourceName, Raw: "TC-1,TC-2"}},
		{"TC-abc", nil, TCRef{Kind: RefMalformed, Source: SourceName, Raw: "TC-abc"}},
		{"TC-0", nil, TCRef{Kind: RefMalformed, Source: SourceName, Raw: "TC-0"}},
		{"TC-000", nil, TCRef{Kind: RefMalformed, Source: SourceName, Raw: "TC-000"}},
		{"TC-0153 leading zeros", nil, TCRef{Kind: RefFound, Source: SourceName, Raw: "TC-0153", ID: 153}},
		{"TC-0153 and TC-153 are the same id", nil, TCRef{Kind: RefFound, Source: SourceName, Raw: "TC-0153,TC-153", ID: 153}},
		{"tc-153 lowercase is not a reference", nil, TCRef{Kind: RefMissing, Source: SourceNone}},
		{"TC- empty", nil, TCRef{Kind: RefMalformed, Source: SourceName, Raw: "TC-"}},
		{"TC-\uff11\uff15\uff13 full-width digits", nil, TCRef{Kind: RefMalformed, Source: SourceName, Raw: "TC-\uff11\uff15\uff13"}},
		{"TC-\u0661\u0665 arabic-indic digits", nil, TCRef{Kind: RefMalformed, Source: SourceName, Raw: "TC-\u0661\u0665"}},
		{"TC-12\u00f1 accented letter", nil, TCRef{Kind: RefMalformed, Source: SourceName, Raw: "TC-12\u00f1"}},
		{"TC-153-login dash ends the id", nil, TCRef{Kind: RefFound, Source: SourceName, Raw: "TC-153", ID: 153}},
		{"TC-9 in name ignored", prop("153"), TCRef{Kind: RefFound, Source: SourceProperty, Raw: "153", ID: 153}},
		{"x", prop(" TC-42 "), TCRef{Kind: RefFound, Source: SourceProperty, Raw: "TC-42", ID: 42}},
		{"x", []xmlProperty{{Name: "TC-ID", Value: "5"}}, TCRef{Kind: RefFound, Source: SourceProperty, Raw: "5", ID: 5}},
		{"x", prop("5", "5"), TCRef{Kind: RefFound, Source: SourceProperty, Raw: "5", ID: 5}},
		{"x", prop("5", "6"), TCRef{Kind: RefMalformed, Source: SourceProperty, Raw: "5,6"}},
		{"x", prop("0153"), TCRef{Kind: RefFound, Source: SourceProperty, Raw: "0153", ID: 153}},
		{"x", prop("153", "TC-0153"), TCRef{Kind: RefFound, Source: SourceProperty, Raw: "153,TC-0153", ID: 153}},
		{"x", prop("0"), TCRef{Kind: RefMalformed, Source: SourceProperty, Raw: "0"}},
		{"x", prop("TC-0"), TCRef{Kind: RefMalformed, Source: SourceProperty, Raw: "TC-0"}},
		{"x", prop("abc"), TCRef{Kind: RefMalformed, Source: SourceProperty, Raw: "abc"}},
		{"x", prop(""), TCRef{Kind: RefMalformed, Source: SourceProperty, Raw: ""}},
		{"x", prop("1234567890123456789"), TCRef{Kind: RefMalformed, Source: SourceProperty, Raw: "1234567890123456789"}},
		{"TC-3", []xmlProperty{{Name: "browser", Value: "chrome"}}, TCRef{Kind: RefFound, Source: SourceName, Raw: "TC-3", ID: 3}},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, extractRef(c.name, c.props), "%s %v", c.name, c.props)
	}
}

// FuzzParse: arbitrary input never panics; a parsed report only holds known
// statuses and positive TC-IDs for found references.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		`<testsuites><testsuite name="s"><testcase name="a TC-1" time="1.5"/></testsuite></testsuites>`,
		`<testsuite><testcase name="b"><properties><property name="tc-id" value="007"/></properties><failure/></testcase></testsuite>`,
		`<testsuite><testcase name="c" time="abc"><skipped/></testcase><testcase/></testsuite>`,
		`<testsuites><testsuite><testsuite><testcase name="TC-99999999999999999999"/></testsuite></testsuite></testsuites>`,
		`<testsuite><testcase name="d" time="1e300"/><testcase name="e" time="1.234,5"/></testsuite>`,
		`<?xml version="1.0" encoding="ISO-8859-1"?><testsuite><testcase name="f"/></testsuite>`,
		`<testsuite/><testsuite/>`,
		`<html/>`, ``, `<`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, doc string) {
		rep, err := Parse(strings.NewReader(doc))
		if err != nil {
			return
		}
		for _, r := range rep.Results {
			require.Contains(t, []Status{Passed, Failed, Error, Skipped}, r.Status)
			if r.Ref.Kind == RefFound {
				require.Positive(t, r.Ref.ID)
			}
			if r.DurationMs != nil {
				require.GreaterOrEqual(t, *r.DurationMs, int64(0))
				require.LessOrEqual(t, *r.DurationMs, maxDurationMs)
			}
		}
	})
}
