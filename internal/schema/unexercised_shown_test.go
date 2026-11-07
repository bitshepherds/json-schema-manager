package schema

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A change a document of the earlier version already shows, by the later version's verdict on it,
// needs no other document: following the warning would break the rule a second time (#157).

// runOnce runs every version as validate all does, stopping at the first failure unless goOn.
func runOnce(t *testing.T, r *Registry, goOn bool) *TestReport {
	t.Helper()
	tr := NewTester(r)
	tr.SetStopOnFirstError(!goOn)
	report, err := tr.TestFoundSchemas(context.Background(), "")
	require.NoError(t, err)
	return report
}

// rejectedEarlierPass is a reader-ahead family whose 1.1.0 removes "b", with a 1.0.0 pass document
// using "b", which 1.1.0 rejects: its no-tightening run shows the narrowing.
func rejectedEarlierPass(t *testing.T) *Registry {
	t.Helper()
	r := realRegistry(t, "scenario: owner-reads\n", schemaMap{"d_f_1_0_0": v100Bounded, "d_f_1_1_0": v110Removed})
	writeDoc(t, r, "d_f_1_0_0", TestDocTypePass, "b.json", `{"s": "b"}`)
	return r
}

// exceptB excepts the no-tightening run of rejectedEarlierPass's 1.0.0 pass document.
const exceptB = `exceptions:
  - version: 1.0.0
    document: pass/b.json
    rule: no-tightening
    reason: every writer moved first
`

func TestUnexercised_RejectedEarlierPassShowsNarrowing(t *testing.T) {
	t.Parallel()

	report := runAll(t, rejectedEarlierPass(t))
	require.Len(t, report.FailedTests["d_f_1_1_0"], 1, "1.1.0 rejects the 1.0.0 pass document")
	assert.Empty(t, warned(report, "d_f_1_1_0"))
}

func TestUnexercised_ExceptedRunShowsNarrowing(t *testing.T) {
	t.Parallel()

	r := rejectedEarlierPass(t)
	writeExceptionFile(t, r, "d_f_1_1_0", exceptB)
	report := runAll(t, r)
	assert.Empty(t, report.FailedTests["d_f_1_1_0"], "the exception clears the run")
	assert.Empty(t, report.StaleExceptions)
	assert.Empty(t, warned(report, "d_f_1_1_0"))
}

func TestUnexercised_AcceptedEarlierFailShowsWidening(t *testing.T) {
	t.Parallel()

	t.Run("a 1.0.0 fail document 1.1.0 accepts shows the widening", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, "scenario: owner-writes\n", schemaMap{"d_f_1_0_0": v100Bounded, "d_f_1_1_0": v110Added})
		writeDoc(t, r, "d_f_1_0_0", TestDocTypeFail, "c.json", `{"s": "c"}`)
		assert.Empty(t, warned(runAll(t, r), "d_f_1_1_0"))
	})

	t.Run("one 1.1.0 rejects for another reason shows nothing", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, "scenario: owner-writes\n", schemaMap{
			"d_f_1_0_0": v100Bounded,
			"d_f_1_1_0": `{"properties": {"s": {"enum": ["a", "b", "c"]}}, "required": ["n"]}`,
		})
		writeDoc(t, r, "d_f_1_0_0", TestDocTypeFail, "c.json", `{"s": "c"}`)
		assert.Contains(t, warned(runAll(t, r), "d_f_1_1_0"), `d_f_1_0_0 widens adds "c"`)
	})
}

func TestUnexercised_SameWithoutContinueOnError(t *testing.T) {
	t.Parallel()

	for _, excepted := range []bool{false, true} {
		r := rejectedEarlierPass(t)
		if excepted {
			writeExceptionFile(t, r, "d_f_1_1_0", exceptB)
		}
		for range 20 {
			assert.Empty(t, warned(runOnce(t, r, false), "d_f_1_1_0"), "stopping, excepted: %v", excepted)
			assert.Empty(t, warned(runOnce(t, r, true), "d_f_1_1_0"), "going on, excepted: %v", excepted)
		}
	}
}

func TestUnexercised_StillWarnsWithNoEvidence(t *testing.T) {
	t.Parallel()

	const want = `d_f_1_0_0 narrows removes "b"`

	t.Run("no document", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, "scenario: owner-reads\n", schemaMap{"d_f_1_0_0": v100Bounded, "d_f_1_1_0": v110Removed})
		assert.Equal(t, []string{want}, warned(runAll(t, r), "d_f_1_1_0"))
	})

	t.Run("a 1.0.0 pass document 1.1.0 still accepts", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, "scenario: owner-reads\n", schemaMap{"d_f_1_0_0": v100Bounded, "d_f_1_1_0": v110Removed})
		writeDoc(t, r, "d_f_1_0_0", TestDocTypePass, "a.json", `{"s": "a"}`)
		for _, goOn := range []bool{false, true} {
			assert.Equal(t, []string{want}, warned(runOnce(t, r, goOn), "d_f_1_1_0"), "going on: %v", goOn)
		}
	})
}

func TestUnexercised_EarlierDocumentsCantBeRead(t *testing.T) {
	t.Parallel()

	for _, dir := range []string{"pass", "fail"} {
		r := setupTestRegistry(t)
		createSchemaFiles(t, r, schemaMap{"d_f_1_0_0": `{}`, "d_f_1_1_0": `{}`})
		earlier, err := r.GetSchemaByKey("d_f_1_0_0")
		require.NoError(t, err)
		later, err := r.GetSchemaByKey("d_f_1_1_0")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(earlier.Path(HomeDir), dir), nil, 0o600))
		require.Error(t, NewTester(r).comparePair(later.Key(), earlier, later, AheadWriter), dir)
	}
}
