package report

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/coverage"
	"github.com/bitshepherds/json-schema-manager/internal/schema"
	"github.com/bitshepherds/json-schema-manager/internal/structdiff"
)

func TestTextReporter(t *testing.T) {
	t.Parallel()
	startTime := time.Now()
	endTime := startTime.Add(time.Second)

	r := schema.NewTestReport()
	r.StartTime = startTime
	r.EndTime = endTime

	k1 := schema.Key("d1_f1_1_0_0")
	k2 := schema.Key("d2_f1_1_0_0")
	specPass := schema.Spec{
		TestInfo:    schema.TestInfo{Path: "pass.json"},
		TestDocType: schema.TestDocTypePass,
	}
	specFail := schema.Spec{
		TestInfo:    schema.TestInfo{Path: "fail.json"},
		TestDocType: schema.TestDocTypePass,
		Err:         assert.AnError,
	}

	r.AddPassedTest(k1, &specPass)
	r.AddFailedTest(k1, &specFail)
	r.AddPassedTest(k2, &specPass)

	t.Run("Concise Mode", func(t *testing.T) {
		t.Parallel()
		tr := &TextReporter{Verbose: false}
		var buf bytes.Buffer
		err := tr.Write(&buf, r)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "[FAIL] d1_f1_1_0_0.schema.json")
		assert.Contains(t, output, "d2_f1_1_0_0.schema.json")
		assert.Contains(t, output, "✗ fail.json")
		assert.NotContains(t, output, "✓ pass.json")
		assert.Contains(t, output, "Test summary: 2 passed, 1 failed")
	})

	t.Run("Verbose Mode", func(t *testing.T) {
		t.Parallel()
		tr := &TextReporter{Verbose: true}
		var buf bytes.Buffer
		err := tr.Write(&buf, r)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "[FAIL] d1_f1_1_0_0.schema.json")
		assert.Contains(t, output, "[PASS] d2_f1_1_0_0.schema.json")
		assert.Contains(t, output, "✗ fail.json")
		assert.Contains(t, output, "✓ pass.json")
	})

	t.Run("Only Passed Tests", func(t *testing.T) {
		t.Parallel()
		r2 := schema.NewTestReport()
		r2.AddPassedTest(k1, &specPass)
		tr := &TextReporter{Verbose: false}
		var buf bytes.Buffer
		err := tr.Write(&buf, r2)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "d1_f1_1_0_0.schema.json")
	})

	t.Run("Colour Mode", func(t *testing.T) {
		t.Parallel()
		tr := &TextReporter{Verbose: true, UseColour: true}
		var buf bytes.Buffer
		err := tr.Write(&buf, r)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "\033[31m[FAIL]\033[0m")
		assert.Contains(t, output, "\033[32m[PASS]\033[0m")
		assert.Contains(t, output, "\033[32m✓\033[0m")
		assert.Contains(t, output, "\033[31m✗\033[0m")
		assert.Contains(t, output, "\033[90mpass.json\033[0m")
		assert.Contains(t, output, "\033[1;37mTest summary: \033[0m")
		assert.Contains(t, output, "\033[1;31m2 passed, 1 failed\033[0m")
	})

	t.Run("Fail as Expected", func(t *testing.T) {
		t.Parallel()
		r3 := schema.NewTestReport()
		specFailExpected := schema.Spec{
			TestInfo:    schema.TestInfo{Path: "fail.json"},
			TestDocType: schema.TestDocTypeFail,
		}
		r3.AddPassedTest(k1, &specFailExpected)
		tr := &TextReporter{Verbose: true}
		var buf bytes.Buffer
		err := tr.Write(&buf, r3)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "(failed, as expected)")
	})

	t.Run("Summary No Failures Colour", func(t *testing.T) {
		t.Parallel()
		r4 := schema.NewTestReport()
		r4.AddPassedTest(k1, &specPass)
		tr := &TextReporter{Verbose: true, UseColour: true}
		var buf bytes.Buffer
		err := tr.Write(&buf, r4)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "\033[1;37mTest summary: \033[0m")
		assert.Contains(t, output, "\033[1;32m1 passed, 0 failed\033[0m")
	})
}

func TestJSONReporter(t *testing.T) {
	t.Parallel()
	startTime := time.Time{}
	endTime := startTime.Add(time.Second)

	r := schema.NewTestReport()
	r.StartTime = startTime
	r.EndTime = endTime

	k := schema.Key("d1_f1_1_0_0")
	specPass := schema.Spec{
		TestInfo:    schema.TestInfo{Path: "pass.json"},
		TestDocType: schema.TestDocTypePass,
	}
	specFail := schema.Spec{
		TestInfo:    schema.TestInfo{Path: "fail.json"},
		TestDocType: schema.TestDocTypeFail,
		Err:         fmt.Errorf("boom"),
	}

	r.AddPassedTest(k, &specPass)
	r.AddFailedTest(k, &specFail)

	tr := &JSONReporter{}
	var buf bytes.Buffer
	err := tr.Write(&buf, r)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, `"duration": "1s"`)
	assert.Contains(t, output, `"totalPassed": 1`)
	assert.Contains(t, output, `"totalFailed": 1`)
	assert.Contains(t, output, `"path": "pass.json"`)
	assert.Contains(t, output, `"path": "fail.json"`)
	assert.Contains(t, output, `"error": "boom"`)
}

func TestJSONReporter_CompatibilityDetail(t *testing.T) {
	t.Parallel()
	r := schema.NewTestReport()

	earlier := schema.Key("shop_order_1_0_0")
	later := schema.Key("shop_order_1_1_0")
	forward, err := schema.NewSemVer("1", "1", "0")
	require.NoError(t, err)
	breaking := schema.Spec{
		Schema:         schema.New(earlier, nil),
		TestInfo:       schema.TestInfo{Path: "1/1/0/pass/refunded.json"},
		TestDocType:    schema.TestDocTypePass,
		ForwardVersion: &forward,
		Err:            fmt.Errorf("rejected"),
	}
	local := schema.Spec{
		Schema:      schema.New(later, nil),
		TestInfo:    schema.TestInfo{Path: "1/1/0/pass/refunded.json"},
		TestDocType: schema.TestDocTypePass,
	}
	r.AddFailedTest(earlier, &breaking)
	r.AddPassedTest(later, &local)

	var buf bytes.Buffer
	require.NoError(t, (&JSONReporter{}).Write(&buf, r))

	var out struct {
		Results map[schema.Key]struct {
			Passed []map[string]any `json:"passed"`
			Failed []map[string]any `json:"failed"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))

	failed := out.Results[earlier].Failed
	require.Len(t, failed, 1)
	assert.Equal(t, "shop_order_1_0_0", failed[0]["validatedBy"])
	assert.Equal(t, "1.1.0", failed[0]["forwardVersion"])

	passed := out.Results[later].Passed
	require.Len(t, passed, 1)
	assert.Equal(t, "shop_order_1_1_0", passed[0]["validatedBy"])
	assert.NotContains(t, passed[0], "forwardVersion")

	// Empty result lists are written as [], not null
	assert.NotNil(t, out.Results[earlier].Passed)
	assert.NotNil(t, out.Results[later].Failed)
	assert.NotContains(t, buf.String(), "null")
}

func TestReporters_UnexercisedChanges(t *testing.T) {
	t.Parallel()

	r := schema.NewTestReport()
	r.AddUnexercisedChange("d_f_1_2_0", &schema.UnexercisedChange{
		Earlier: "d_f_1_1_0",
		Change:  structdiff.Change{Location: "/a", Effect: structdiff.Widens, Detail: "removes every constraint"},
	})
	r.AddUnexercisedChange("d_f_1_1_0", &schema.UnexercisedChange{
		Earlier: "d_f_1_0_0",
		Change:  structdiff.Change{Location: "/s", Keyword: "enum", Effect: structdiff.Widens, Detail: `adds "x"`},
	})

	var text bytes.Buffer
	require.NoError(t, (&TextReporter{}).Write(&text, r))
	out := text.String()
	assert.Contains(t, out, "[WARN] d_f_1_1_0.schema.json since d_f_1_0_0:\n  ! /s: enum adds \"x\" (widens)\n"+
		"[WARN] d_f_1_2_0.schema.json since d_f_1_1_0:\n  ! /a: removes every constraint (widens)\n")
	assert.Contains(t, out, "Test summary: 0 passed, 0 failed, 2 unexercised changes")

	var js bytes.Buffer
	require.NoError(t, (&JSONReporter{}).Write(&js, r))
	assert.Contains(t, js.String(), `"totalUnexercised": 2`)
	assert.Regexp(t, `(?s)"schema": "d_f_1_1_0".*"schema": "d_f_1_2_0"`, js.String())

	js.Reset()
	require.NoError(t, (&JSONReporter{}).Write(&js, schema.NewTestReport()))
	assert.Contains(t, js.String(), `"unexercisedChanges": []`)
}

func TestReporters_Summaries(t *testing.T) {
	t.Parallel()

	r := schema.NewTestReport()
	r.AddSummary("d_a_1_0_0", &schema.Summary{PassDocs: 0, FailDocs: 2})
	r.AddSummary("d_b_1_0_0", &schema.Summary{PassDocs: 1, FailDocs: 0})
	r.AddSummary("d_c_1_0_0", &schema.Summary{
		PassDocs: 1,
		FailDocs: 1,
		Coverage: []coverage.Point{
			{Location: "/", Kind: coverage.Property, Detail: "a", Covered: true},
			{Location: "/a", Kind: coverage.EnumValue, Detail: `"x"`},
		},
		FailKeywords: map[string][]string{"fail/b.json": {"#/required", "#/type"}},
	})

	var text bytes.Buffer
	require.NoError(t, (&TextReporter{}).Write(&text, r))
	out := text.String()
	assert.Contains(t, out, "[WARN] d_a_1_0_0.schema.json (pass: 0, fail: 0)\n  ! no pass documents\n")
	assert.Contains(t, out, "[WARN] d_b_1_0_0.schema.json (pass: 0, fail: 0)\n  ! no fail documents\n")
	assert.Contains(t, out, "[PASS] d_c_1_0_0.schema.json (pass: 0, fail: 0)\n")
	assert.NotContains(t, out, "coverage:", "coverage is only shown when verbose")
	assert.Contains(t, out, "Test summary: 0 passed, 0 failed, 2 schemas without pass or fail documents")

	text.Reset()
	require.NoError(t, (&TextReporter{Verbose: true}).Write(&text, r))
	assert.Contains(t, text.String(), "  coverage: 1 of 2 points reached by pass documents\n"+
		"    - /a: enum \"x\"\n  fail/b.json is rejected by #/required, #/type\n")

	var js bytes.Buffer
	require.NoError(t, (&JSONReporter{}).Write(&js, r))
	out = js.String()
	assert.Contains(t, out, `"totalMissingDocuments": 2`)
	assert.Contains(t, out, `"points": []`, "a schema with no points has an empty list")
	assert.Contains(t, out, `"covered": 1`)
	assert.Contains(t, out, `"fail/b.json": [`)
}

func TestJUnitReporter(t *testing.T) {
	t.Parallel()

	r := schema.NewTestReport()
	r.StartTime = time.Time{}
	r.EndTime = r.StartTime.Add(1500 * time.Millisecond)
	k := schema.Key("d_f_1_1_0")
	r.AddPassedTest(
		k,
		&schema.Spec{TestInfo: schema.TestInfo{Path: "pass/b.json"}, TestDocType: schema.TestDocTypePass},
	)
	r.AddFailedTest(k, &schema.Spec{
		TestInfo:    schema.TestInfo{Path: "fail/a.json"},
		TestDocType: schema.TestDocTypeFail,
		Err:         fmt.Errorf("a <b> & c"),
	})
	r.AddSummary(k, &schema.Summary{PassDocs: 1, FailDocs: 1})
	r.AddSummary("d_g_1_0_0", &schema.Summary{PassDocs: 1})
	r.AddSummary("d_h_1_0_0", &schema.Summary{FailDocs: 1})
	r.AddUnexercisedChange("d_f_1_2_0", &schema.UnexercisedChange{
		Earlier: k,
		Change:  structdiff.Change{Location: "/s", Keyword: "enum", Effect: structdiff.Widens, Detail: `adds "x"`},
	})

	var buf bytes.Buffer
	require.NoError(t, (&JUnitReporter{}).Write(&buf, r))
	out := buf.String()
	assert.True(t, strings.HasPrefix(out, `<?xml version="1.0" encoding="UTF-8"?>`))
	assert.Contains(t, out, `<testsuites name="jsm validate" tests="2" failures="1" time="1.500">`)
	assert.Contains(t, out, `<testsuite name="d_f_1_1_0.schema.json" tests="2" failures="1">
    <testcase name="fail/a.json" classname="d_f_1_1_0.schema.json">
      <failure message="passed, when expected fail">a &lt;b&gt; &amp; c</failure>
    </testcase>
    <testcase name="pass/b.json" classname="d_f_1_1_0.schema.json"></testcase>`, "cases are sorted, and text escaped")
	assert.Contains(t, out, `<testsuite name="d_f_1_2_0.schema.json" tests="0" failures="0">
    <system-out>WARN unexercised change since d_f_1_1_0: /s: enum adds &#34;x&#34; (widens)&#xA;</system-out>`)
	assert.Contains(t, out, `<system-out>WARN no fail documents&#xA;</system-out>`)
	assert.Contains(t, out, `<system-out>WARN no pass documents&#xA;</system-out>`)
	require.NoError(t, xml.Unmarshal(buf.Bytes(), new(junitTestSuites)), "the report is well-formed XML")

	buf.Reset()
	require.NoError(t, (&JUnitReporter{Strict: true}).Write(&buf, r))
	out = buf.String()
	assert.Contains(t, out, `<testsuites name="jsm validate" tests="5" failures="4"`)
	assert.Contains(t, out, `<testcase name="warning: no fail documents" classname="d_g_1_0_0.schema.json">
      <failure message="warning, and --strict is set">no fail documents</failure>`)
	assert.NotContains(t, out, "system-out")
}

func TestJUnitReporter_WriteErrors(t *testing.T) {
	t.Parallel()

	// Fail the header, the report, and the final newline in turn
	for _, limit := range []int{0, 50, 200} {
		r := schema.NewTestReport()
		var full bytes.Buffer
		require.NoError(t, (&JUnitReporter{}).Write(&full, r))
		if limit >= full.Len() {
			limit = full.Len() - 1
		}
		err := (&JUnitReporter{}).Write(&limitWriter{n: limit}, r)
		require.Error(t, err, "limit %d of %d", limit, full.Len())
	}
}

// limitWriter accepts n bytes, then fails.
type limitWriter struct{ n int }

func (w *limitWriter) Write(p []byte) (int, error) {
	if len(p) > w.n {
		return 0, io.ErrShortWrite
	}
	w.n -= len(p)
	return len(p), nil
}
