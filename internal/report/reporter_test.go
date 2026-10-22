package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
