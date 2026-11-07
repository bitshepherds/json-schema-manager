package report

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/v2/internal/config"
	"github.com/bitshepherds/json-schema-manager/v2/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/v2/internal/schema"
	"github.com/bitshepherds/json-schema-manager/v2/internal/structdiff"
	"github.com/bitshepherds/json-schema-manager/v2/internal/validator"
)

// relativePathReport tests a registry on disk, in a temporary directory, whose 1.1.0 adds an enum value its
// pass document uses, which 1.0.0 rejects, and whose 1.0.0 has a pass document it rejects itself. It
// returns the report and the registry's root.
func relativePathReport(t *testing.T) (*schema.TestReport, string) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		config.JsmRegistryConfigFile: "environments:\n  prod:\n" +
			"    privateUrlRoot: \"https://json-schemas.internal.myorg.io/\"\n" +
			"    publicUrlRoot: \"https://json-schemas.myorg.io/\"\n    isProduction: true\n",
		"shop/order/family.yml": "scenario: owner-writes\n",
		"shop/order/1/0/0/shop_order_1_0_0.schema.json": `{"$schema": "http://json-schema.org/draft-07/schema#",
			"$id": "{{ ID }}", "properties": {"status": {"enum": ["placed"]}}}`,
		"shop/order/1/0/0/pass/placed.json": `{"status": "placed"}`,
		"shop/order/1/0/0/pass/lost.json":   `{"status": "lost"}`,
		"shop/order/1/0/0/fail/paid.json":   `{"status": "paid"}`,
		"shop/order/1/1/0/shop_order_1_1_0.schema.json": `{"$schema": "http://json-schema.org/draft-07/schema#",
			"$id": "{{ ID }}", "properties": {"status": {"enum": ["placed", "refunded"]}}}`,
		"shop/order/1/1/0/pass/refunded.json": `{"status": "refunded"}`,
		"shop/order/1/1/0/fail/paid.json":     `{"status": "paid"}`,
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	registry, err := schema.NewRegistry(
		root, validator.NewSanthoshCompiler(), fsh.NewPathResolver(), fsh.NewEnvProvider(),
	)
	require.NoError(t, err)
	tester := schema.NewTester(registry)
	tester.SetStopOnFirstError(false)
	r, err := tester.TestFoundSchemas(context.Background(), "")
	require.NoError(t, err)
	return r, registry.RootDirectory()
}

// The text report names a document that fails across versions by its path from the registry's root, and
// only once, however long the path to the registry is (#156).
func TestTextReporter_CrossVersionFailure_RelativePath(t *testing.T) {
	t.Parallel()
	r, root := relativePathReport(t)

	var buf bytes.Buffer
	require.NoError(t, (&TextReporter{}).Write(&buf, r))
	out := buf.String()

	doc := filepath.Join("shop", "order", "1", "1", "0", "pass", "refunded.json")
	assert.Contains(t, out, "  ✗ no-loosening broken: pass document of 1.1.0 rejected by 1.0.0:\n"+
		"    no-loosening broken between shop_order_1_0_0 and shop_order_1_1_0: pass document "+doc+
		" of shop_order_1_1_0 was rejected by shop_order_1_0_0.")
	assert.Equal(t, 1, strings.Count(out, doc), out)
	assert.NotContains(t, out, root)
	assert.Contains(t, out, "shop_order_1_1_0's directory. Detail: jsonschema validation failed",
		"the detail is the validator's own, without the run's error naming the document and schema again")
}

// A version's own failing document reads the same way as one failing across versions (#156).
func TestTextReporter_OwnFailure_RelativePath(t *testing.T) {
	t.Parallel()
	r, root := relativePathReport(t)

	var buf bytes.Buffer
	require.NoError(t, (&TextReporter{}).Write(&buf, r))
	out := buf.String()

	doc := filepath.Join("shop", "order", "1", "0", "0", "pass", "lost.json")
	own := filepath.Join("shop", "order", "1", "0", "0", "shop_order_1_0_0.schema.json")
	assert.Contains(t, out, "  ✗ failed:\n    Pass Test document "+doc+
		" did not pass validation as expected for schema "+own+", error: ")
	assert.Equal(t, 1, strings.Count(out, doc), out)
	assert.NotContains(t, out, root)
}

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

// untestedFindingsReport is a report which stopped at the first failure, and holds a stale exception
// and a missing migration statement, which tested schemas found, under schemas it didn't test.
func untestedFindingsReport() *schema.TestReport {
	r := schema.NewTestReport()
	r.AddFailedTest("d_b_1_0_0", &schema.Spec{
		TestInfo:    schema.TestInfo{Path: "bad.json"},
		TestDocType: schema.TestDocTypePass,
		Err:         assert.AnError,
	})
	stale := &schema.Exception{Version: schema.SemVer{1, 0, 0}, Change: "/: gone"}
	r.AddStaleException("d_f_1_1_0", stale, "never there")
	r.MarkNotTested("d_f_1_1_0")
	r.AddMissingMigration("d_m_2_0_0", schema.MissingMigration{Major: 2, Why: "major 2 needs a migration statement"})
	r.MarkNotTested("d_m_2_0_0")
	r.NotTested = 2
	return r
}

// TestTextReporter_StoppedEarlyShowsFindingsForUntestedSchemas checks that a stopped report shows what
// a tested schema found under one not tested, on a line which says it was not tested, not that it ran
// nothing.
func TestTextReporter_StoppedEarlyShowsFindingsForUntestedSchemas(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	require.NoError(t, (&TextReporter{}).Write(&buf, untestedFindingsReport()))
	out := buf.String()
	assert.Contains(t, out, "[FAIL] d_f_1_1_0.schema.json (not tested)\n"+
		"  ✗ stale exception: version 1.0.0, change /: gone:\n")
	assert.Contains(t, out, "[FAIL] d_m_2_0_0.schema.json (not tested)\n"+
		"  ✗ major 2 needs a migration statement\n")
	assert.Contains(t, out, "[FAIL] d_b_1_0_0.schema.json (runs: 0 passed, 1 failed)\n")
	assert.Contains(t, out, "Test summary: 0 runs passed, 1 failed, 1 stale exception, 1 missing migration statement\n"+
		"Stopped at the first failure: 2 schemas not tested.")
}
