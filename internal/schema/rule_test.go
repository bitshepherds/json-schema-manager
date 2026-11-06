package schema

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/internal/validator"
)

const (
	ruleEarlier = Key("domain_family_1_0_0")
	ruleLater   = Key("domain_family_1_1_0")
)

// ruleRegistry is a family of two versions, each with a pass and a fail document, and the given declaration.
func ruleRegistry(t *testing.T, declaration string) *Registry {
	t.Helper()
	r := setupTestRegistry(t)
	createSchemaFiles(t, r, schemaMap{ruleEarlier: `{}`, ruleLater: `{}`})
	s := New(ruleEarlier, r)
	require.NoError(
		t,
		os.WriteFile(filepath.Join(s.Path(FamilyDir), FamilyDeclarationFile), []byte(declaration), 0o600),
	)
	for _, k := range []Key{ruleEarlier, ruleLater} {
		createTestDocs(t, r, k, TestDocTypePass, map[string]string{"p.json": `{}`})
		createTestDocs(t, r, k, TestDocTypeFail, map[string]string{"f.json": `[]`})
	}
	return r
}

// behaves makes the schemas behave by key: a key rejects documents if it is in rejecting, and accepts
// them otherwise.
func behaves(t *testing.T, r *Registry, rejecting ...Key) {
	t.Helper()
	mc, ok := r.compiler.(*mockCompiler)
	require.True(t, ok)
	mc.CompileFunc = func(id string) (validator.Validator, error) {
		for _, k := range rejecting {
			if strings.Contains(id, string(k)) {
				return &mockValidator{Err: errors.New("rejected")}, nil
			}
		}
		return &mockValidator{}, nil
	}
}

// runNames lists the report's runs: 1.1.0 pass on 1.0.0, sorted.
func runNames(report *TestReport) []string {
	var names []string
	for _, log := range []TestLog{report.PassedTests, report.FailedTests} {
		for _, specs := range log {
			for i := range specs {
				from, on := specs[i].DocumentFrom.Version(), specs[i].Schema.Key().Version()
				names = append(names, from.String('.')+" "+string(specs[i].TestDocType)+" on "+on.String('.'))
			}
		}
	}
	slices.Sort(names)
	return names
}

func TestRuleTable(t *testing.T) {
	t.Parallel()

	own := []string{"1.0.0 fail on 1.0.0", "1.0.0 pass on 1.0.0", "1.1.0 fail on 1.1.0", "1.1.0 pass on 1.1.0"}
	noLoosening := []string{"1.0.0 fail on 1.1.0", "1.1.0 pass on 1.0.0"}
	noTightening := []string{"1.0.0 pass on 1.1.0", "1.1.0 fail on 1.0.0"}

	for _, tc := range []struct {
		declaration string
		want        []string
	}{
		{"scenario: provider-first\n", slices.Concat(own, noLoosening)},
		{"scenario: consumer-first\n", slices.Concat(own, noTightening)},
		{"scenario: uncoordinated\ndocumentsLive: moment\n", slices.Concat(own, noLoosening, noTightening)},
		{"scenario: lock-step\n", own},
		{"scenario: stored-self-naming\n", own},
		{"scenario: provider-first\nreaderPicksSchema: true\n", own},
		{"scenario: lock-step\nahead: writer\n", slices.Concat(own, noLoosening)},
		{"scenario: third-party-format\ndocumentsLive: moment\n", slices.Concat(own, noTightening)},
	} {
		t.Run(tc.declaration, func(t *testing.T) {
			t.Parallel()
			slices.Sort(tc.want)
			for name, run := range map[string]func(tr *Tester) (*TestReport, error){
				"found": func(tr *Tester) (*TestReport, error) {
					return tr.TestFoundSchemas(context.Background(), "")
				},
				"both by key": func(tr *Tester) (*TestReport, error) {
					if _, err := tr.TestSingleSchema(context.Background(), ruleLater); err != nil {
						return nil, err
					}
					return tr.TestSingleSchema(context.Background(), ruleEarlier)
				},
			} {
				r := ruleRegistry(t, tc.declaration)
				behaves(t, r, ruleLater, ruleEarlier)
				tr := NewTester(r)
				tr.SetStopOnFirstError(false)
				report, err := run(tr)
				require.NoError(t, err, name)
				assert.Equal(t, tc.want, runNames(report), name)
			}
		})
	}
}

func TestRuleFailureIsReportedUnderTheLaterVersion(t *testing.T) {
	t.Parallel()
	r := ruleRegistry(t, "scenario: provider-first\n")
	behaves(t, r, ruleEarlier)

	tr := NewTester(r)
	tr.SetStopOnFirstError(false)
	report, err := tr.TestSingleSchema(context.Background(), ruleEarlier)
	require.NoError(t, err)

	// 1.0.0 rejects everything, so its own pass document fails, under itself. So does 1.1.0's, and 1.1.0
	// accepts 1.0.0's fail document, both under the later version.
	require.Len(t, report.FailedTests[ruleEarlier], 1)
	assert.Equal(t, ruleEarlier, report.FailedTests[ruleEarlier][0].DocumentFrom)
	require.Len(t, report.FailedTests[ruleLater], 2)
	spec := report.FailedTests[ruleLater][0]
	assert.Equal(t, ruleLater, spec.DocumentFrom)
	assert.Equal(t, ruleEarlier, spec.Schema.Key())

	var broken *RuleBrokenError
	require.ErrorAs(t, spec.Err, &broken)
	var rejected *PassTestFailedError
	assert.ErrorAs(t, spec.Err, &rejected)
}

func TestOwnFailureDoesNotSkipTheVersionsPairs(t *testing.T) {
	t.Parallel()
	r := ruleRegistry(t, "scenario: provider-first\n")
	behaves(t, r, ruleLater)

	tr := NewTester(r)
	tr.SetStopOnFirstError(false)
	report, err := tr.TestSingleSchema(context.Background(), ruleLater)
	require.NoError(t, err)
	assert.Contains(t, runNames(report), "1.1.0 pass on 1.1.0")
	assert.Contains(t, runNames(report), "1.1.0 pass on 1.0.0")
}

func TestStopOnFirstFailureStopsAcrossVersions(t *testing.T) {
	t.Parallel()
	r := ruleRegistry(t, "scenario: provider-first\n")
	behaves(t, r, ruleEarlier)
	// Accepting own fail documents is the only other way to fail, so only the run on 1.0.0 fails
	require.NoError(t, os.RemoveAll(filepath.Join(New(ruleLater, r).Path(HomeDir), "fail")))

	report, err := NewTester(r).TestSingleSchema(context.Background(), ruleLater)
	require.NoError(t, err)
	require.Len(t, report.FailedTests[ruleLater], 1)
	assert.Equal(t, ruleEarlier, report.FailedTests[ruleLater][0].Schema.Key())
}

func TestRuleErrorsStopTheRun(t *testing.T) {
	t.Parallel()

	t.Run("an undeclared family", func(t *testing.T) {
		t.Parallel()
		r := ruleRegistry(t, "scenario: provider-first\n")
		require.NoError(t, os.Remove(filepath.Join(New(ruleEarlier, r).Path(FamilyDir), FamilyDeclarationFile)))
		_, err := NewTester(r).TestSingleSchema(context.Background(), ruleEarlier)
		require.ErrorAs(t, err, new(*MissingFamilyDeclarationError))
	})

	t.Run("an unreadable pass directory", func(t *testing.T) {
		t.Parallel()
		r := ruleRegistry(t, "scenario: lock-step\n")
		s := New(ruleLater, r)
		require.NoError(t, os.RemoveAll(filepath.Join(s.Path(HomeDir), "pass")))
		require.NoError(t, os.WriteFile(filepath.Join(s.Path(HomeDir), "pass"), nil, 0o600))
		_, err := NewTester(r).TestSingleSchema(context.Background(), ruleLater)
		require.Error(t, err)
	})

	t.Run("another version's documents can't be read", func(t *testing.T) {
		t.Parallel()
		r := ruleRegistry(t, "scenario: provider-first\n")
		createTestDocs(t, r, ruleLater, TestDocTypePass, map[string]string{"bad.json": `{`})
		_, err := NewTester(r).TestSingleSchema(context.Background(), ruleEarlier)
		require.Error(t, err)
		require.ErrorAs(t, err, new(InvalidTestDocumentError))

		r = ruleRegistry(t, "scenario: provider-first\n")
		createTestDocs(t, r, ruleEarlier, TestDocTypeFail, map[string]string{"bad.json": `{`})
		_, err = NewTester(r).TestSingleSchema(context.Background(), ruleLater)
		require.Error(t, err)
		assert.ErrorAs(t, err, new(InvalidTestDocumentError))
	})

	t.Run("another version does not render", func(t *testing.T) {
		t.Parallel()
		r := ruleRegistry(t, "scenario: provider-first\n")
		mc, ok := r.compiler.(*mockCompiler)
		require.True(t, ok)
		mc.CompileFunc = func(id string) (validator.Validator, error) {
			if strings.Contains(id, string(ruleLater)) {
				return nil, errors.New("render failure")
			}
			return &mockValidator{}, nil
		}
		tr := NewTester(r)
		tr.SetStopOnFirstError(false)
		_, err := tr.TestSingleSchema(context.Background(), ruleEarlier)
		require.ErrorContains(t, err, "render failure")
	})

	t.Run("another version has no schema", func(t *testing.T) {
		t.Parallel()
		for _, k := range []Key{ruleEarlier, ruleLater} {
			r := ruleRegistry(t, "scenario: provider-first\n")
			other := ruleLater
			if k == ruleLater {
				other = ruleEarlier
			}
			require.NoError(t, os.Remove(New(other, r).Path(FilePath)))
			_, err := NewTester(r).TestSingleSchema(context.Background(), k)
			require.Error(t, err)
		}
	})

	t.Run("the versions can't be listed", func(t *testing.T) {
		t.Parallel()
		// Listing the earlier versions asks for the major's minors, and each minor's patches. So failing
		// on the call after that fails listing the later versions.
		for _, succeeding := range []int{0, 3} {
			r := ruleRegistry(t, "scenario: provider-first\n")
			_, err := r.GetSchemaByKey(ruleEarlier)
			require.NoError(t, err)
			calls := 0
			r.pathResolver = &mockPathResolver{getUintSubdirectories: func(dir string) ([]uint64, error) {
				if calls++; calls > succeeding {
					return nil, errors.New("listing error")
				}
				return fsh.GetUintSubdirectories(dir)
			}}
			_, err = NewTester(r).TestSingleSchema(context.Background(), ruleEarlier)
			require.ErrorContains(t, err, "listing error", "after %d listings", succeeding)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		t.Parallel()
		r := ruleRegistry(t, "scenario: provider-first\n")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := NewTester(r).TestSingleSchema(ctx, ruleEarlier)
		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestTestSpecificDocumentRunsAcrossVersions(t *testing.T) {
	t.Parallel()

	doc := func(r *Registry, k Key, tt TestDocType, name string) string {
		return filepath.Join(New(k, r).Path(HomeDir), string(tt), name)
	}

	for _, tc := range []struct {
		name        string
		declaration string
		key         Key
		tt          TestDocType
		file        string
		want        []string
	}{
		{
			"a later pass document, writer ahead", "scenario: provider-first\n",
			ruleLater, TestDocTypePass, "p.json",
			[]string{"1.1.0 pass on 1.0.0", "1.1.0 pass on 1.1.0"},
		},
		{
			"an earlier fail document, writer ahead", "scenario: provider-first\n",
			ruleEarlier, TestDocTypeFail, "f.json",
			[]string{"1.0.0 fail on 1.0.0", "1.0.0 fail on 1.1.0"},
		},
		{
			"an earlier pass document, writer ahead", "scenario: provider-first\n",
			ruleEarlier, TestDocTypePass, "p.json",
			[]string{"1.0.0 pass on 1.0.0"},
		},
		{
			"a later fail document, reader ahead", "scenario: consumer-first\n",
			ruleLater, TestDocTypeFail, "f.json",
			[]string{"1.1.0 fail on 1.0.0", "1.1.0 fail on 1.1.0"},
		},
		{
			"neither", "scenario: lock-step\n",
			ruleLater, TestDocTypePass, "p.json",
			[]string{"1.1.0 pass on 1.1.0"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := ruleRegistry(t, tc.declaration)
			tr := NewTester(r)
			tr.SetStopOnFirstError(false)
			report, err := tr.TestSpecificDocument(context.Background(), tc.key, doc(r, tc.key, tc.tt, tc.file))
			require.NoError(t, err)
			assert.Equal(t, tc.want, runNames(report))
		})
	}

	t.Run("a failure across versions stops the run if asked", func(t *testing.T) {
		t.Parallel()
		r := ruleRegistry(t, "scenario: provider-first\n")
		behaves(t, r, ruleEarlier)
		report, err := NewTester(r).TestSpecificDocument(context.Background(), ruleLater,
			doc(r, ruleLater, TestDocTypePass, "p.json"))
		require.NoError(t, err)
		assert.Len(t, report.FailedTests[ruleLater], 1)
	})

	t.Run("an undeclared family", func(t *testing.T) {
		t.Parallel()
		r := ruleRegistry(t, "scenario: provider-first\n")
		require.NoError(t, os.Remove(filepath.Join(New(ruleLater, r).Path(FamilyDir), FamilyDeclarationFile)))
		_, err := NewTester(r).TestSpecificDocument(context.Background(), ruleLater,
			doc(r, ruleLater, TestDocTypePass, "p.json"))
		require.ErrorAs(t, err, new(*MissingFamilyDeclarationError))
	})

	t.Run("another version has no schema", func(t *testing.T) {
		t.Parallel()
		r := ruleRegistry(t, "scenario: provider-first\n")
		path := doc(r, ruleLater, TestDocTypePass, "p.json")
		require.NoError(t, os.Remove(New(ruleEarlier, r).Path(FilePath)))
		_, err := NewTester(r).TestSpecificDocument(context.Background(), ruleLater, path)
		require.Error(t, err)
	})

	t.Run("cancelled while running", func(t *testing.T) {
		t.Parallel()
		r := ruleRegistry(t, "scenario: provider-first\n")
		ctx, cancel := context.WithCancel(context.Background())
		mc, ok := r.compiler.(*mockCompiler)
		require.True(t, ok)
		mc.CompileFunc = func(_ string) (validator.Validator, error) {
			return &mockValidator{ValidateFunc: func(_ validator.JSONDocument) error {
				cancel()
				return nil
			}}, nil
		}
		_, err := NewTester(r).TestSpecificDocument(ctx, ruleLater, doc(r, ruleLater, TestDocTypePass, "p.json"))
		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestLaterPassRejections(t *testing.T) {
	t.Parallel()

	t.Run("runs later pass documents, whatever the scenario", func(t *testing.T) {
		t.Parallel()
		r := ruleRegistry(t, "scenario: lock-step\n")
		behaves(t, r, ruleEarlier)
		rejections, err := NewTester(r).LaterPassRejections(context.Background(), ruleEarlier)
		require.NoError(t, err)
		require.Len(t, rejections, 1)
		assert.Equal(t, ruleLater, rejections[0].Later)
		assert.Equal(t, "p.json", filepath.Base(rejections[0].Path))
		assert.ErrorAs(t, rejections[0].Err, new(*PassTestFailedError))
	})

	t.Run("no later version", func(t *testing.T) {
		t.Parallel()
		r := ruleRegistry(t, "scenario: lock-step\n")
		rejections, err := NewTester(r).LaterPassRejections(context.Background(), ruleLater)
		require.NoError(t, err)
		assert.Empty(t, rejections)
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()
		_, err := NewTester(setupTestRegistry(t)).LaterPassRejections(context.Background(), ruleEarlier)
		require.Error(t, err, "no such schema")

		r := ruleRegistry(t, "scenario: lock-step\n")
		mc, ok := r.compiler.(*mockCompiler)
		require.True(t, ok)
		mc.CompileFunc = func(_ string) (validator.Validator, error) { return nil, errors.New("render failure") }
		_, err = NewTester(r).LaterPassRejections(context.Background(), ruleEarlier)
		require.ErrorContains(t, err, "render failure")

		r = ruleRegistry(t, "scenario: lock-step\n")
		require.NoError(t, os.Remove(New(ruleLater, r).Path(FilePath)))
		_, err = NewTester(r).LaterPassRejections(context.Background(), ruleEarlier)
		require.Error(t, err, "a later version with no schema")

		r = ruleRegistry(t, "scenario: lock-step\n")
		_, sErr := r.GetSchemaByKey(ruleEarlier)
		require.NoError(t, sErr)
		r.pathResolver = &mockPathResolver{getUintSubdirectories: func(string) ([]uint64, error) {
			return nil, errors.New("listing error")
		}}
		_, err = NewTester(r).LaterPassRejections(context.Background(), ruleEarlier)
		require.ErrorContains(t, err, "listing error")

		r = ruleRegistry(t, "scenario: lock-step\n")
		createTestDocs(t, r, ruleLater, TestDocTypePass, map[string]string{"bad.json": `{`})
		_, err = NewTester(r).LaterPassRejections(context.Background(), ruleEarlier)
		require.Error(t, err, "a later document which isn't JSON")

		r = ruleRegistry(t, "scenario: lock-step\n")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = NewTester(r).LaterPassRejections(ctx, ruleEarlier)
		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestRuleBrokenErrorSaysWhatItMeans(t *testing.T) {
	t.Parallel()

	declared := func(s Scenario, live DocumentsLive, role Role) *FamilyDeclaration {
		return &FamilyDeclaration{Scenario: s, Facts: FamilyFacts{DocumentsLive: live, Schema: role}}
	}
	for _, tc := range []struct {
		name        string
		rule        Rule
		docType     TestDocType
		declaration *FamilyDeclaration
		want        []string
	}{
		{
			"a loosening", RuleNoLoosening, TestDocTypePass,
			declared(ScenarioProviderFirst, DocumentsLiveMoment, SchemaPrescribes),
			[]string{"no-loosening broken", "was rejected by", "provider-first", "a writer on", "new major version"},
		},
		{
			"a tightening of documents read and gone", RuleNoTightening, TestDocTypePass,
			declared(ScenarioConsumerFirst, DocumentsLiveMoment, SchemaPrescribes),
			[]string{"no-tightening broken", "consumer-first", "a writer still on", "new major version"},
		},
		{
			"a tightening of documents kept", RuleNoTightening, TestDocTypeFail,
			declared(ScenarioConfigFile, DocumentsLiveIndefinitely, SchemaPrescribes),
			[]string{"no-tightening broken", "was accepted by", "config-file", "may still be around"},
		},
		{
			"a described format narrowing", RuleNoTightening, TestDocTypePass,
			declared(ScenarioThirdPartyFormat, DocumentsLiveMoment, SchemaDescribes),
			[]string{"real format has changed", "rejects documents"},
		},
		{
			"a described format widening", RuleNoLoosening, TestDocTypeFail,
			declared(ScenarioThirdPartyFormat, DocumentsLiveMoment, SchemaDescribes),
			[]string{"real format has changed", "accepts documents"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := &RuleBrokenError{
				Rule:         tc.rule,
				Earlier:      ruleEarlier,
				Later:        ruleLater,
				DocumentFrom: ruleLater,
				ValidatedBy:  ruleEarlier,
				DocType:      tc.docType,
				DocPath:      "doc.json",
				Declaration:  tc.declaration,
				Wrapped:      errors.New("detail"),
			}
			for _, want := range append(tc.want, string(ruleEarlier), string(ruleLater), "doc.json", "detail") {
				assert.Contains(t, err.Error(), want)
			}
			assert.EqualError(t, err.Unwrap(), "detail")
		})
	}
}

func TestSpecReportKey(t *testing.T) {
	t.Parallel()
	own := NewSpec(New(ruleEarlier, nil), TestInfo{}, TestDocTypePass)
	assert.False(t, own.AcrossVersions())
	assert.Equal(t, ruleEarlier, own.ReportKey())

	across := Spec{Schema: New(ruleEarlier, nil), Earlier: ruleEarlier, Later: ruleLater}
	assert.True(t, across.AcrossVersions())
	assert.Equal(t, ruleLater, across.ReportKey())
}

func TestFamilyFactsEffectiveAhead(t *testing.T) {
	t.Parallel()
	assert.Equal(t, AheadWriter, FamilyFacts{Ahead: AheadWriter}.EffectiveAhead())
	assert.Equal(t, AheadNeither, FamilyFacts{Ahead: AheadWriter, ReaderPicksSchema: true}.EffectiveAhead())
}

func TestKeySameMajorFamily(t *testing.T) {
	t.Parallel()
	assert.True(t, ruleEarlier.SameMajorFamily(ruleLater))
	assert.False(t, ruleEarlier.SameMajorFamily("domain_family_2_0_0"))
	assert.False(t, ruleEarlier.SameMajorFamily("domain_other_1_0_0"))
}

func TestSearchScopeContains(t *testing.T) {
	t.Parallel()
	assert.True(t, SearchScope("").Contains("a/b"))
	assert.True(t, SearchScope("a/b").Contains("a/b"))
	assert.True(t, SearchScope("a").Contains("a/b"))
	assert.False(t, SearchScope("a/b").Contains("a"))
	assert.False(t, SearchScope("a").Contains("ab/c"))
}

func TestWatcherMapsADeclarationToItsFamily(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	w := &Watcher{registry: r}
	root := r.RootDirectory()

	assert.Equal(t, &WatchEvent{Family: "a/b"}, w.mapToWatchEvent(filepath.Join(root, "a", "b", FamilyDeclarationFile)))
	assert.Nil(t, w.mapToWatchEvent(filepath.Join(root, FamilyDeclarationFile)), "the root is no family")
	assert.Nil(t, w.mapToWatchEvent(filepath.Join(filepath.Dir(root), FamilyDeclarationFile)), "outside the registry")
}
