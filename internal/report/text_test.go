package report

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/v2/internal/schema"
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
