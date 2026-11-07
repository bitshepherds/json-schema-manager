package schema

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/v2/internal/config"
	"github.com/bitshepherds/json-schema-manager/v2/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/v2/internal/structdiff"
	"github.com/bitshepherds/json-schema-manager/v2/internal/validator"
)

// realRegistry returns a registry with the real validator, holding the schemas, in a family of the scenario.
func realRegistry(t *testing.T, scenario string, schemas schemaMap) *Registry {
	t.Helper()
	regDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(regDir, config.JsmRegistryConfigFile),
		[]byte(testConfigData), 0o600))
	r, err := NewRegistry(regDir, validator.NewSanthoshCompiler(), fsh.NewPathResolver(), fsh.NewEnvProvider())
	require.NoError(t, err)
	createSchemaFiles(t, r, schemas)
	for k := range schemas {
		declaration := filepath.Join(New(k, r).Path(FamilyDir), FamilyDeclarationFile)
		require.NoError(t, os.WriteFile(declaration, []byte(scenario), 0o600))
	}
	return r
}

// writeDoc writes a test document of the given type for the schema with key k.
func writeDoc(t *testing.T, r *Registry, k Key, tt TestDocType, name, content string) {
	t.Helper()
	dir := filepath.Join(New(k, r).Path(HomeDir), string(tt))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

// writeExceptionFile writes the exception file of the schema with key k.
func writeExceptionFile(t *testing.T, r *Registry, k Key, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(New(k, r).Path(HomeDir), ExceptionsFile), []byte(content), 0o600))
}

// runAll runs every version, going on after a failure, as validate all -C does.
func runAll(t *testing.T, r *Registry) *TestReport {
	t.Helper()
	tr := NewTester(r)
	tr.SetStopOnFirstError(false)
	report, err := tr.TestFoundSchemas(context.Background(), "")
	require.NoError(t, err)
	return report
}

// warned renders the report's unexercised changes for one schema, each as earlier, direction and detail.
func warned(report *TestReport, k Key) []string {
	out := make([]string, 0, len(report.UnexercisedChanges[k]))
	for _, uc := range report.UnexercisedChanges[k] {
		out = append(out, string(uc.Earlier)+" "+string(uc.Direction)+" "+uc.Change.Detail)
	}
	return out
}

const (
	v100Bounded = `{"properties": {"s": {"enum": ["a", "b"]}}}`
	v110Added   = `{"properties": {"s": {"enum": ["a", "b", "c"]}}}`
	v110Removed = `{"properties": {"s": {"enum": ["a"]}}}`
)

func TestUnexercisedWarningFollowsScenario(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		scenario     string
		wantWidening []string
		wantNarrow   []string
	}{
		{
			name:         "writer ahead needs evidence for widenings",
			scenario:     "scenario: owner-writes\n",
			wantWidening: []string{`d_f_1_0_0 widens adds "c"`},
			wantNarrow:   []string{},
		},
		{
			name:         "reader ahead needs evidence for narrowings",
			scenario:     "scenario: owner-reads\n",
			wantWidening: []string{},
			wantNarrow:   []string{`d_f_1_0_0 narrows removes "b"`},
		},
		{
			name:         "either needs both",
			scenario:     "scenario: retained-stream\noldestLive: 1.0.0\n",
			wantWidening: []string{`d_f_1_0_0 widens adds "c"`},
			wantNarrow:   []string{`d_f_1_0_0 narrows removes "b"`},
		},
		{
			name:         "neither needs none",
			scenario:     "scenario: lock-step\n",
			wantWidening: []string{},
			wantNarrow:   []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			widening := runAll(t, realRegistry(t, tt.scenario,
				schemaMap{"d_f_1_0_0": v100Bounded, "d_f_1_1_0": v110Added}))
			assert.Equal(t, tt.wantWidening, warned(widening, "d_f_1_1_0"))
			narrowing := runAll(t, realRegistry(t, tt.scenario,
				schemaMap{"d_f_1_0_0": v100Bounded, "d_f_1_1_0": v110Removed}))
			assert.Equal(t, tt.wantNarrow, warned(narrowing, "d_f_1_1_0"))
		})
	}

	t.Run("a pass document clears a widening", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, "scenario: owner-writes\n", schemaMap{"d_f_1_0_0": v100Bounded, "d_f_1_1_0": v110Added})
		writeDoc(t, r, "d_f_1_1_0", TestDocTypePass, "c.json", `{"s": "c"}`)
		assert.Empty(t, warned(runAll(t, r), "d_f_1_1_0"))
	})

	t.Run("a fail document clears a narrowing", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(
			t,
			"scenario: owner-reads\n",
			schemaMap{"d_f_1_0_0": v100Bounded, "d_f_1_1_0": v110Removed},
		)
		writeDoc(t, r, "d_f_1_1_0", TestDocTypeFail, "b.json", `{"s": "b"}`)
		assert.Empty(t, warned(runAll(t, r), "d_f_1_1_0"))
	})

	t.Run("a fail document both versions reject shows no narrowing", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(
			t,
			"scenario: owner-reads\n",
			schemaMap{"d_f_1_0_0": v100Bounded, "d_f_1_1_0": v110Removed},
		)
		writeDoc(t, r, "d_f_1_1_0", TestDocTypeFail, "z.json", `{"s": "z"}`)
		assert.Equal(t, []string{`d_f_1_0_0 narrows removes "b"`}, warned(runAll(t, r), "d_f_1_1_0"))
	})

	t.Run("the same whichever version is selected", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, "scenario: owner-writes\n", schemaMap{"d_f_1_0_0": v100Bounded, "d_f_1_1_0": v110Added})
		report, err := NewTester(r).TestSingleSchema(context.Background(), "d_f_1_0_0")
		require.NoError(t, err)
		assert.Equal(t, []string{`d_f_1_0_0 widens adds "c"`}, warned(report, "d_f_1_1_0"))
		assert.Empty(t, warned(report, "d_f_1_0_0"), "a warning appears under the later version")
	})
}

// A change between two values goes both ways: a family protected in either direction hears about it.
func TestChangeGoingBothWaysWarnsInTheDirectionProtected(t *testing.T) {
	t.Parallel()

	const widensConst, narrowsConst = `d_f_1_0_0 widens allows "b"`, `d_f_1_0_0 narrows no longer allows "a"`
	const widensMultiple = "d_f_1_0_0 widens allows multiples of 6 which aren't multiples of 4"
	const narrowsMultiple = "d_f_1_0_0 narrows no longer allows multiples of 4 which aren't multiples of 6"
	consts := schemaMap{"d_f_1_0_0": `{"const": "a"}`, "d_f_1_1_0": `{"const": "b"}`}
	multiples := schemaMap{"d_f_1_0_0": `{"multipleOf": 4}`, "d_f_1_1_0": `{"multipleOf": 6}`}

	tests := []struct {
		name         string
		scenario     string
		wantConst    []string
		wantMultiple []string
	}{
		{"writer ahead", "scenario: owner-writes\n", []string{widensConst}, []string{widensMultiple}},
		{"reader ahead", "scenario: owner-reads\n", []string{narrowsConst}, []string{narrowsMultiple}},
		{
			"either",
			"scenario: retained-stream\noldestLive: 1.0.0\n",
			[]string{widensConst, narrowsConst},
			[]string{widensMultiple, narrowsMultiple},
		},
		{"neither", "scenario: lock-step\n", []string{}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.wantConst,
				warned(runAll(t, realRegistry(t, tt.scenario, maps.Clone(consts))), "d_f_1_1_0"))
			assert.Equal(t, tt.wantMultiple,
				warned(runAll(t, realRegistry(t, tt.scenario, maps.Clone(multiples))), "d_f_1_1_0"))
		})
	}

	t.Run("a fail document showing the old value clears the narrowing half", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, "scenario: owner-reads\n", maps.Clone(consts))
		writeDoc(t, r, "d_f_1_1_0", TestDocTypeFail, "a.json", `"a"`)
		assert.Empty(t, warned(runAll(t, r), "d_f_1_1_0"))
	})

	t.Run("a divisor only widens", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, "scenario: owner-reads\n",
			schemaMap{"d_f_1_0_0": `{"multipleOf": 4}`, "d_f_1_1_0": `{"multipleOf": 2}`})
		assert.Empty(t, warned(runAll(t, r), "d_f_1_1_0"))
	})
}

func TestAltersChangeNeedsEvidenceInProtectedDirection(t *testing.T) {
	t.Parallel()

	const older = `{"oneOf": [{"type": "number"}, {"minimum": 0}]}`
	const newer = `{"oneOf": [{"type": "number"}, {"minimum": 0, "maximum": 10}]}`
	schemas := func() schemaMap { return schemaMap{"d_f_1_0_0": older, "d_f_1_1_0": newer} }
	const detail = "adds a limit of 10"

	t.Run("writer ahead asks for a pass document", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, "scenario: owner-writes\n", schemas())
		assert.Equal(t, []string{"d_f_1_0_0 widens " + detail}, warned(runAll(t, r), "d_f_1_1_0"))
	})

	t.Run("reader ahead asks for a fail document", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, "scenario: owner-reads\n", schemas())
		assert.Equal(t, []string{"d_f_1_0_0 narrows " + detail}, warned(runAll(t, r), "d_f_1_1_0"))
	})

	t.Run("either asks for both, and each document clears its own", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, "scenario: retained-stream\noldestLive: 1.0.0\n", schemas())
		assert.Equal(t, []string{"d_f_1_0_0 widens " + detail, "d_f_1_0_0 narrows " + detail},
			warned(runAll(t, r), "d_f_1_1_0"))

		// 1.0.0 rejects 11: it matches both arms. 1.1.0 accepts it: it matches only the first
		r = realRegistry(t, "scenario: retained-stream\noldestLive: 1.0.0\n", schemas())
		writeDoc(t, r, "d_f_1_1_0", TestDocTypePass, "eleven.json", `11`)
		assert.Equal(t, []string{"d_f_1_0_0 narrows " + detail}, warned(runAll(t, r), "d_f_1_1_0"))
	})

	t.Run("neither asks for nothing", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, "scenario: lock-step\n", schemas())
		assert.Empty(t, warned(runAll(t, r), "d_f_1_1_0"))
	})
}

func TestUnexercisedWarningComparesEveryEarlierVersion(t *testing.T) {
	t.Parallel()

	t.Run("a widening only the older version shows", func(t *testing.T) {
		t.Parallel()
		// 1.2.0 narrows 1.1.0, but still accepts more than 1.0.0. A document 1.1.0 holds shows the first
		// widening, and one at 6 long would show this one too. 1.1.0 has no such document
		r := realRegistry(t, "scenario: owner-writes\n", schemaMap{
			"d_f_1_0_0": `{"maxLength": 5}`, "d_f_1_1_0": `{"maxLength": 10}`, "d_f_1_2_0": `{"maxLength": 7}`,
		})
		report := runAll(t, r)
		assert.Equal(t, []string{"d_f_1_0_0 widens relaxes the limit from 5 to 10"}, warned(report, "d_f_1_1_0"))
		assert.Equal(t, []string{"d_f_1_0_0 widens relaxes the limit from 5 to 7"}, warned(report, "d_f_1_2_0"))
	})

	t.Run("a document of a version between counts", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, "scenario: owner-writes\n", schemaMap{
			"d_f_1_0_0": `{"maxLength": 5}`, "d_f_1_1_0": `{"maxLength": 10}`, "d_f_1_2_0": `{"maxLength": 7}`,
		})
		writeDoc(t, r, "d_f_1_1_0", TestDocTypePass, "six.json", `"123456"`)
		report := runAll(t, r)
		assert.Empty(t, warned(report, "d_f_1_1_0"))
		assert.Empty(t, warned(report, "d_f_1_2_0"))
	})

	t.Run("a change carried over unchanged belongs to the version which made it", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, "scenario: owner-writes\n", schemaMap{
			"d_f_1_0_0": v100Bounded, "d_f_1_1_0": v110Added, "d_f_1_2_0": v110Added,
		})
		report := runAll(t, r)
		assert.Equal(t, []string{`d_f_1_0_0 widens adds "c"`}, warned(report, "d_f_1_1_0"))
		assert.Empty(t, warned(report, "d_f_1_2_0"))
	})

	t.Run("each pair is compared once however many versions are selected", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, "scenario: owner-writes\n", schemaMap{"d_f_1_0_0": v100Bounded, "d_f_1_1_0": v110Added})
		tr := NewTester(r)
		_, err := tr.TestSingleSchema(context.Background(), "d_f_1_0_0")
		require.NoError(t, err)
		report, err := tr.TestSingleSchema(context.Background(), "d_f_1_1_0")
		require.NoError(t, err)
		assert.Len(t, report.UnexercisedChanges["d_f_1_1_0"], 1)
	})
}

func TestWarnsOnFailDocumentWithSeveralReasons(t *testing.T) {
	t.Parallel()

	const schema = `{"properties": {"status": {"enum": ["paid"]}, "quantity": {"type": "number"}}, ` +
		`"anyOf": [{"required": ["a"]}, {"properties": {"b": {"type": "string"}}, "required": ["b"]}]}`
	k := Key("d_f_1_0_0")

	reasons := func(t *testing.T, scenario, doc string) *TestReport {
		t.Helper()
		r := realRegistry(t, scenario, schemaMap{k: schema})
		writeDoc(t, r, k, TestDocTypePass, "ok.json", `{"a": 1}`)
		writeDoc(t, r, k, TestDocTypeFail, "doc.json", doc)
		return runAll(t, r)
	}
	several := func(report *TestReport) []string {
		for _, v := range report.Summaries[k].SeveralReasons {
			return v
		}
		return nil
	}

	t.Run("two faults at two locations", func(t *testing.T) {
		t.Parallel()
		report := reasons(t, "scenario: owner-writes\n", `{"a": 1, "status": "lost", "quantity": "two"}`)
		assert.Equal(t, []string{"/quantity", "/status"}, several(report))
		assert.True(t, report.HasWarnings())
		assert.Equal(t, 1, report.FailDocumentsWithSeveralReasons())
	})

	t.Run("one fault has one reason", func(t *testing.T) {
		t.Parallel()
		report := reasons(t, "scenario: owner-writes\n", `{"a": 1, "status": "lost"}`)
		assert.Nil(t, several(report))
		assert.False(t, report.HasWarnings())
	})

	t.Run("an anyOf whose arms fail at different locations is one fault", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, several(reasons(t, "scenario: owner-writes\n", `{"b": 1}`)))
	})

	t.Run("a family with no pairs doesn't run the document on another version", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, several(reasons(t, "scenario: lock-step\n", `{"a": 1, "status": "lost", "quantity": "two"}`)))
	})
}

func TestExceptionFileAcknowledgesDiffWarning(t *testing.T) {
	t.Parallel()

	// maxLength moved into allOf accepts the same documents, but the structural walk sees a limit removed,
	// and no pass document can use that
	const older = `{"maxLength": 5}`
	const newer = `{"allOf": [{"maxLength": 5}]}`
	const removed = "/: maxLength removes the limit of 5"
	schemas := func() schemaMap { return schemaMap{"d_f_1_0_0": older, "d_f_1_1_0": newer} }
	const writerAhead = "scenario: owner-writes\n"

	t.Run("no pass document can clear it", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, writerAhead, schemas())
		writeDoc(t, r, "d_f_1_1_0", TestDocTypePass, "short.json", `"abc"`)
		assert.Equal(t, []string{"d_f_1_0_0 widens removes the limit of 5"}, warned(runAll(t, r), "d_f_1_1_0"))
	})

	t.Run("an entry clears only what it names", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, writerAhead, schemaMap{
			"d_f_1_0_0": `{"maxLength": 5, "enum": ["a"]}`,
			"d_f_1_1_0": `{"allOf": [{"maxLength": 5}], "enum": ["a", "b"]}`,
		})
		writeExceptionFile(t, r, "d_f_1_1_0", `exceptions:
  - version: 1.0.0
    change: "`+removed+`"
    reason: the limit only moved
`)
		report := runAll(t, r)
		assert.Equal(t, []string{`d_f_1_0_0 widens adds "b"`}, warned(report, "d_f_1_1_0"))
		require.Len(t, report.AcknowledgedChanges["d_f_1_1_0"], 1)
		ac := report.AcknowledgedChanges["d_f_1_1_0"][0]
		assert.Equal(t, Key("d_f_1_0_0"), ac.Earlier)
		assert.Equal(t, Key("d_f_1_1_0"), ac.Later)
		assert.Equal(t, structdiff.Widens, ac.Change.Effect)
		assert.Equal(t, "the limit only moved", ac.Reason)
		assert.Empty(t, report.StaleExceptions)
		assert.True(t, report.HasWarnings(), "the other change still warns")
	})

	t.Run("an entry which matches nothing is stale", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, writerAhead, schemas())
		writeExceptionFile(t, r, "d_f_1_1_0", `exceptions:
  - version: 1.0.0
    change: "`+removed+`"
    reason: the limit only moved
  - version: 1.0.0
    change: "/: maxLength nothing like this"
    reason: never there
`)
		report := runAll(t, r)
		assert.Empty(t, warned(report, "d_f_1_1_0"))
		require.Len(t, report.StaleExceptions["d_f_1_1_0"], 1)
		stale := report.StaleExceptions["d_f_1_1_0"][0]
		assert.Equal(t, "/: maxLength nothing like this", stale.Exception.Change)
		assert.Contains(t, stale.Why, "needs acknowledging")
		assert.True(t, report.HasFailures())
	})

	t.Run("a narrowing a writer-ahead family doesn't protect needs no acknowledging", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, writerAhead, schemaMap{"d_f_1_0_0": v100Bounded, "d_f_1_1_0": v110Removed})
		writeExceptionFile(t, r, "d_f_1_1_0", `exceptions:
  - version: 1.0.0
    change: '/s: enum removes "b"'
    reason: no need
`)
		assert.Len(t, runAll(t, r).StaleExceptions["d_f_1_1_0"], 1)
	})

	t.Run("an entry for another pair is judged with that pair", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, writerAhead, schemaMap{"d_f_1_0_0": older, "d_f_1_1_0": newer, "d_f_1_2_0": newer})
		writeExceptionFile(t, r, "d_f_1_1_0", `exceptions:
  - version: 1.0.0
    change: "`+removed+`"
    reason: the limit only moved
`)
		report := runAll(t, r)
		assert.Empty(t, report.StaleExceptions)
		assert.Empty(t, warned(report, "d_f_1_2_0"), "1.2.0 carries the change over, so it needs no entry of its own")
	})
}

func TestUnexercisedChangeNeededDocument(t *testing.T) {
	t.Parallel()

	assert.Equal(t, TestDocTypePass, (&UnexercisedChange{Direction: structdiff.Widens}).NeededDocument())
	assert.Equal(t, TestDocTypeFail, (&UnexercisedChange{Direction: structdiff.Narrows}).NeededDocument())
}

func TestComparePairErrors(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T) (*Registry, *Schema, *Schema) {
		t.Helper()
		r := setupTestRegistry(t)
		createSchemaFiles(t, r, schemaMap{"d_f_1_0_0": `{}`, "d_f_1_1_0": `{}`, "d_f_1_2_0": `{}`})
		earlier, err := r.GetSchemaByKey("d_f_1_0_0")
		require.NoError(t, err)
		later, err := r.GetSchemaByKey("d_f_1_2_0")
		require.NoError(t, err)
		return r, earlier, later
	}

	t.Run("no family declaration", func(t *testing.T) {
		t.Parallel()
		r, earlier, _ := setup(t)
		require.NoError(t, os.Remove(filepath.Join(earlier.Path(FamilyDir), FamilyDeclarationFile)))
		require.Error(t, NewTester(r).findUnexercisedChanges("d_f_1_0_0"))
	})

	t.Run("a version between has no schema file", func(t *testing.T) {
		t.Parallel()
		r, earlier, later := setup(t)
		require.NoError(t, os.Remove(New("d_f_1_1_0", r).Path(FilePath)))
		require.Error(t, NewTester(r).comparePair(earlier, later, AheadWriter))
	})

	t.Run("a version's documents can't be read", func(t *testing.T) {
		t.Parallel()
		for _, dir := range []string{"pass", "fail"} {
			r, earlier, later := setup(t)
			require.NoError(t, os.WriteFile(filepath.Join(later.Path(HomeDir), dir), nil, 0o600))
			require.Error(t, NewTester(r).comparePair(earlier, later, AheadWriter))
		}
	})

	t.Run("the version before doesn't render", func(t *testing.T) {
		t.Parallel()
		r, earlier, later := setup(t)
		failToCompile(t, r, "d_f_1_1_0")
		require.Error(t, NewTester(r).comparePair(earlier, later, AheadWriter))
	})

	t.Run("an exception file which isn't valid", func(t *testing.T) {
		t.Parallel()
		r, earlier, later := setup(t)
		writeExceptionFile(t, r, "d_f_1_2_0", "exceptions: nope")
		require.Error(t, NewTester(r).comparePair(earlier, later, AheadWriter))
	})

	t.Run("the earlier versions can't be listed", func(t *testing.T) {
		t.Parallel()
		r, earlier, later := setup(t)
		majorDir := filepath.Join(later.Path(FamilyDir), "1")
		require.NoError(t, os.RemoveAll(majorDir))
		require.NoError(t, os.WriteFile(majorDir, nil, 0o600))
		require.Error(t, NewTester(r).comparePair(earlier, later, AheadWriter))
	})
}

func TestExceptionForAChange(t *testing.T) {
	t.Parallel()

	entries, err := ParseExceptions([]byte(`exceptions:
  - version: 1.0.0
    change: "/a: maxLength removes the limit of 5"
    reason: moved
`))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "version 1.0.0, change /a: maxLength removes the limit of 5", entries[0].String())

	t.Run("it names no document or rule", func(t *testing.T) {
		t.Parallel()
		_, pErr := ParseExceptions([]byte(`exceptions:
  - version: 1.0.0
    change: "/a: x"
    document: pass/a.json
    rule: no-loosening
    reason: moved
`))
		require.ErrorContains(t, pErr, "names no document or rule")
	})

	t.Run("it needs a reason", func(t *testing.T) {
		t.Parallel()
		_, pErr := ParseExceptions([]byte(`exceptions:
  - version: 1.0.0
    change: "/a: x"
`))
		require.ErrorContains(t, pErr, "reason")
	})

	t.Run("it's listed once", func(t *testing.T) {
		t.Parallel()
		_, pErr := ParseExceptions([]byte(`exceptions:
  - version: 1.0.0
    change: "/a: x"
    reason: moved
  - version: 1.0.0
    change: "/a: x"
    reason: moved again
`))
		require.ErrorContains(t, pErr, "twice")
	})

	t.Run("its target has gone only with the earlier version", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		createSchemaFiles(t, r, schemaMap{"d_f_1_0_0": `{}`, "d_f_1_1_0": `{}`})
		laterDir := New("d_f_1_1_0", r).Path(HomeDir)
		assert.False(t, entries[0].TargetGone(laterDir))
		require.NoError(t, os.RemoveAll(New("d_f_1_0_0", r).Path(HomeDir)))
		assert.True(t, entries[0].TargetGone(laterDir))
	})

	t.Run("its target has gone once the earlier version is below oldestLive", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, "scenario: retained-stream\noldestLive: 1.0.0\n",
			schemaMap{"d_f_1_0_0": `{}`, "d_f_1_1_0": `{}`})
		laterDir := New("d_f_1_1_0", r).Path(HomeDir)
		assert.False(t, entries[0].TargetGone(laterDir))
		require.NoError(t, os.WriteFile(r.FamilyDeclarationPath("d_f_1_0_0"),
			[]byte("scenario: retained-stream\noldestLive: 1.1.0\n"), 0o600))
		assert.True(t, entries[0].TargetGone(laterDir))
	})
}

func TestStaleAmong(t *testing.T) {
	t.Parallel()

	stale := func(t *testing.T, r *Registry, content string) []Exception {
		t.Helper()
		entries, err := ParseExceptions([]byte("exceptions:\n" + content))
		require.NoError(t, err)
		got, err := StaleAmong(context.Background(), r, "d_f_1_1_0", entries)
		require.NoError(t, err)
		return got
	}
	changeEntry := func(change string) string {
		return "  - version: 1.0.0\n    change: \"" + change + "\"\n    reason: moved\n"
	}
	docEntry := func(document string) string {
		return "  - version: 1.0.0\n    document: " + document + "\n    rule: no-loosening\n    reason: moved\n"
	}
	widened := func(t *testing.T) *Registry {
		t.Helper()
		r := realRegistry(t, "scenario: owner-writes\n", schemaMap{"d_f_1_0_0": v100Bounded, "d_f_1_1_0": v110Added})
		writeDoc(t, r, "d_f_1_0_0", TestDocTypeFail, "bad.json", `{"s": "c"}`)
		writeDoc(t, r, "d_f_1_0_0", TestDocTypeFail, "worse.json", `{"s": "z"}`)
		return r
	}
	const realChange = `/s: enum adds \"c\"`

	t.Run("an entry for a change which still warns clears something", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, "scenario: owner-writes\n", schemaMap{"d_f_1_0_0": v100Bounded, "d_f_1_1_0": v110Added})
		assert.Empty(t, stale(t, r, changeEntry(realChange)))
	})

	t.Run("an entry for a change which isn't there clears nothing", func(t *testing.T) {
		t.Parallel()
		r := realRegistry(t, "scenario: owner-writes\n", schemaMap{"d_f_1_0_0": v100Bounded, "d_f_1_1_0": v110Added})
		got := stale(t, r, changeEntry("/s: enum adds nothing")+changeEntry(realChange))
		require.Len(t, got, 1)
		assert.Equal(t, "/s: enum adds nothing", got[0].Change)
	})

	t.Run("an entry for a run which fails clears something", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, stale(t, widened(t), docEntry("fail/bad.json")))
	})

	t.Run("an entry for a run which passes clears nothing", func(t *testing.T) {
		t.Parallel()
		got := stale(t, widened(t), docEntry("fail/bad.json")+docEntry("fail/worse.json"))
		require.Len(t, got, 1)
		assert.Equal(t, "fail/worse.json", got[0].Document)
	})

	t.Run("an entry for a document which isn't there clears nothing", func(t *testing.T) {
		t.Parallel()
		assert.Len(t, stale(t, widened(t), docEntry("fail/missing.json")), 1)
	})

	t.Run("what the version's own file says doesn't count", func(t *testing.T) {
		t.Parallel()
		r := widened(t)
		writeExceptionFile(t, r, "d_f_1_1_0", "exceptions:\n"+docEntry("fail/missing.json"))
		assert.Empty(t, stale(t, r, docEntry("fail/bad.json")))
	})

	t.Run("a version which can't be tested says so", func(t *testing.T) {
		t.Parallel()
		_, err := StaleAmong(context.Background(), widened(t), "d_f_9_0_0", nil)
		require.Error(t, err)
	})
}

func TestDiffFollowsTheRegistrysAssertFormat(t *testing.T) {
	t.Parallel()

	regDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(regDir, config.JsmRegistryConfigFile),
		[]byte("assertFormat: true\n"+testConfigData), 0o600))
	r, err := NewRegistry(regDir, validator.NewSanthoshCompiler(), fsh.NewPathResolver(), fsh.NewEnvProvider())
	require.NoError(t, err)
	createSchemaFiles(t, r, schemaMap{"d_f_1_0_0": `{"format": "email"}`, "d_f_1_1_0": `{}`})
	older, err := r.GetSchemaByKey("d_f_1_0_0")
	require.NoError(t, err)
	newer, err := r.GetSchemaByKey("d_f_1_1_0")
	require.NoError(t, err)

	changes, err := Diff(older, newer)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, "format", changes[0].Change.Keyword)
	assert.Equal(t, structdiff.Widens, changes[0].Change.Effect)
}
