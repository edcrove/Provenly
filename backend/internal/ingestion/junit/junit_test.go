package junit

import (
	"errors"
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
<testcase name="good"/>
</testsuite></testsuites>`))
	require.NoError(t, err)
	assert.Equal(t, 5, rep.Received)
	names := make([]string, len(rep.Results))
	for i, r := range rep.Results {
		names[i] = r.TestName
	}
	assert.Equal(t, []string{"bad time", "negative", "tiny", "good"}, names, "only the nameless testcase is discarded")
	assert.Nil(t, rep.Results[0].DurationMs)
	assert.Equal(t, Failed, rep.Results[0].Status, "an invalid time never hides a failure")
	assert.Equal(t, int64(0), *rep.Results[2].DurationMs, "sub-millisecond durations round to 0")
	assert.Equal(t, []CaseError{
		{Index: 0, TestName: "", Message: "testcase has no name; result discarded"},
		{Index: 1, TestName: "bad time", Message: `invalid time attribute "soon"; result kept without duration`, Persisted: true},
		{Index: 2, TestName: "negative", Message: `invalid time attribute "-1"; result kept without duration`, Persisted: true},
	}, rep.Errors)
}

func ms(v int64) *int64 { return &v }

func TestParseDocumentErrors(t *testing.T) {
	for _, doc := range []string{"", "not xml", "<html></html>", "<testsuites><testsuite>"} {
		_, err := Parse(strings.NewReader(doc))
		require.Error(t, err, doc)
		assert.Contains(t, err.Error(), "invalid JUnit XML")
	}
	readErr := errors.New("read failed")
	_, err := Parse(failingReader{readErr})
	assert.ErrorIs(t, err, readErr)
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
