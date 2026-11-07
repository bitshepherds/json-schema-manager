package report

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/v2/internal/schema"
	"github.com/bitshepherds/json-schema-manager/v2/internal/structdiff"
)

func TestCountsSuffix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                     string
		passed, failed, excepted int
		want                     string
	}{
		{"no excepted runs", 2, 0, 0, "(runs: 2 passed, 0 failed)"},
		{"excepted runs", 2, 0, 1, "(runs: 2 passed, 0 failed, 1 excepted)"},
		{"no runs", 0, 0, 0, "(runs: 0 passed, 0 failed)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, countsSuffix(tt.passed, tt.failed, tt.excepted))
		})
	}
}

func TestTextReporter_SummaryLineNamesRuns(t *testing.T) {
	t.Parallel()
	k := schema.Key("d_f_1_0_0")
	pass := schema.Spec{TestInfo: schema.TestInfo{Path: "pass.json"}, TestDocType: schema.TestDocTypePass}
	fail := schema.Spec{
		TestInfo:    schema.TestInfo{Path: "bad.json"},
		TestDocType: schema.TestDocTypePass,
		Err:         assert.AnError,
	}

	tests := []struct {
		name   string
		passed int
		failed int
		want   string
	}{
		{"several runs", 2, 1, "Test summary: 2 runs passed, 1 failed\n"},
		{"one run", 1, 0, "Test summary: 1 run passed, 0 failed\n"},
		{"no runs", 0, 0, "Test summary: 0 runs passed, 0 failed\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := schema.NewTestReport()
			for range tt.passed {
				r.AddPassedTest(k, &pass)
			}
			for range tt.failed {
				r.AddFailedTest(k, &fail)
			}
			var buf bytes.Buffer
			require.NoError(t, (&TextReporter{}).Write(&buf, r))
			assert.Contains(t, buf.String(), tt.want)
		})
	}
}

func TestTextReporter_StoppedEarly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		notTested int
		want      string
	}{
		{"several schemas", 7, "Stopped at the first failure: 7 schemas not tested. Run with -C to test them all.\n"},
		{"one schema", 1, "Stopped at the first failure: 1 schema not tested. Run with -C to test them all.\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := schema.NewTestReport()
			r.AddFailedTest("d_f_1_0_0", &schema.Spec{
				TestInfo:    schema.TestInfo{Path: "bad.json"},
				TestDocType: schema.TestDocTypePass,
				Err:         assert.AnError,
			})
			r.NotTested = tt.notTested
			var buf bytes.Buffer
			require.NoError(t, (&TextReporter{}).Write(&buf, r))
			assert.Contains(t, buf.String(), "Test summary: 0 runs passed, 1 failed\n"+tt.want)
		})
	}

	t.Run("not stopped", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		require.NoError(t, (&TextReporter{}).Write(&buf, schema.NewTestReport()))
		assert.NotContains(t, buf.String(), "Stopped at the first failure")
	})
}

func TestTextReporter_StoppedEarlyKeepsWarnings(t *testing.T) {
	t.Parallel()
	r := schema.NewTestReport()
	r.AddSummary("d_a_1_0_0", &schema.Summary{PassDocs: 1})
	r.AddUnexercisedChange("d_a_1_1_0", &schema.UnexercisedChange{
		Earlier: "d_a_1_0_0",
		Change:  structdiff.Change{Location: "/a", Effect: structdiff.Widens, Detail: "removes every constraint"},
	})
	r.AddFailedTest("d_b_1_0_0", &schema.Spec{
		TestInfo:    schema.TestInfo{Path: "bad.json"},
		TestDocType: schema.TestDocTypePass,
		Err:         assert.AnError,
	})
	r.NotTested = 3

	var buf bytes.Buffer
	require.NoError(t, (&TextReporter{}).Write(&buf, r))
	out := buf.String()
	assert.Contains(t, out, "[WARN] d_a_1_0_0.schema.json (runs: 0 passed, 0 failed)\n  ! no fail documents\n")
	assert.Contains(t, out, "[WARN] d_a_1_1_0.schema.json since d_a_1_0_0:\n")
	assert.Contains(t, out, "1 unexercised change, 1 schema without pass or fail documents\n"+
		"Stopped at the first failure: 3 schemas not tested.")
}
