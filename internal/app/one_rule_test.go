package app

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/internal/schema"
	"github.com/bitshepherds/json-schema-manager/internal/validator"
)

// These tests run the whole CLI on families of the behaviour registry, by scenario. Each family's
// expect.yml says what the promise wants, and cmd/jsm/behaviour_registry_test.go holds the built
// binary to it. Here we look at which documents ran, and what the reports say about them.

const (
	behaviourRegistry = "../../cmd/jsm/testdata/behaviour-registry"
	behaviourEnv      = "production"
)

// newBehaviourRegistry copies the named families of the behaviour registry to a registry of their
// own, in a git repository.
func newBehaviourRegistry(t *testing.T, families ...string) *declTestRegistry {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	r := &declTestRegistry{
		t:    t,
		repo: repo,
		reg:  filepath.Join(repo, "reg"),
		log:  filepath.Join(t.TempDir(), "jsm.log"),
	}
	cfg, err := os.ReadFile(filepath.Join(behaviourRegistry, config.JsmRegistryConfigFile))
	require.NoError(t, err)
	r.write(config.JsmRegistryConfigFile, string(cfg))

	for _, family := range families {
		from := filepath.Join(behaviourRegistry, filepath.FromSlash(family))
		require.NoError(t, filepath.WalkDir(from, func(p string, d os.DirEntry, wErr error) error {
			if wErr != nil || d.IsDir() || d.Name() == "expect.yml" {
				return wErr
			}
			rel, rErr := filepath.Rel(behaviourRegistry, p)
			if rErr != nil {
				return rErr
			}
			data, rErr := os.ReadFile(p)
			if rErr != nil {
				return rErr
			}
			r.write(rel, string(data))
			return nil
		}))
	}
	r.git("init", "-q")
	r.commit("first")
	return r
}

// docRun is one document run, from a JSON report.
type docRun struct {
	Path         string     `json:"path"`
	Type         string     `json:"type"`
	ValidatedBy  schema.Key `json:"validatedBy"`
	Rule         string     `json:"rule"`
	Earlier      schema.Key `json:"earlier"`
	Later        schema.Key `json:"later"`
	DocumentFrom schema.Key `json:"documentFrom"`
	Error        string     `json:"error"`
}

// label names a run by what matters for the rule: 1.1.0 pass on 1.0.0.
func (d docRun) label() string {
	return versionOf(d.DocumentFrom) + " " + d.Type + " on " + versionOf(d.ValidatedBy)
}

func versionOf(k schema.Key) string {
	v := k.Version()
	return v.String('.')
}

type jsonReport struct {
	Results map[schema.Key]struct {
		Passed []docRun `json:"passed"`
		Failed []docRun `json:"failed"`
	} `json:"results"`
}

func parseJSONReport(t *testing.T, out string) jsonReport {
	t.Helper()
	var rep jsonReport
	require.NoError(t, json.Unmarshal([]byte(out), &rep), "%s", out)
	return rep
}

// labels returns every run in the report, sorted.
func (rep jsonReport) labels() []string {
	var all []string
	for _, res := range rep.Results {
		for _, run := range slices.Concat(res.Passed, res.Failed) {
			all = append(all, run.label())
		}
	}
	slices.Sort(all)
	return all
}

// failed returns the failed runs, each under the key the report puts it under.
func (rep jsonReport) failed() map[schema.Key][]docRun {
	failed := map[schema.Key][]docRun{}
	for k, res := range rep.Results {
		if len(res.Failed) > 0 {
			failed[k] = res.Failed
		}
	}
	return failed
}

// runJSON runs validate with the JSON report, testing everything rather than stopping at the first
// failure, and returns the report and the command's error.
func (r *declTestRegistry) runJSON(args ...string) (jsonReport, error) {
	r.t.Helper()
	out, err := r.jsm(append(append([]string{"validate"}, args...), "-C", "-o", "json")...)
	return parseJSONReport(r.t, out), err
}

func TestWriterAheadCatchesLooseningByEarlierFailDocument(t *testing.T) {
	t.Parallel()

	for name, family := range map[string]string{
		"an open anyOf arm added":         "loosen/anyof-arm",
		"a constraint on a oneOf arm":     "loosen/oneof-constraint",
		"a family that can go either way": "loosen/either-family",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			key := strings.ReplaceAll(family, "/", "_") + "_1_1_0"
			rep, err := newBehaviourRegistry(t, family).runJSON(key)
			require.ErrorIs(t, err, ErrTestsFailed)

			failed := rep.failed()[schema.Key(key)]
			require.Len(t, failed, 1)
			assert.Equal(t, "1.0.0 fail on 1.1.0", failed[0].label())
			assert.Equal(t, "no-loosening", failed[0].Rule)
		})
	}
}

func TestWriterAheadAllowsTightening(t *testing.T) {
	t.Parallel()
	r := newBehaviourRegistry(t, "allowed/writer-ahead-tightens")

	rep, err := r.runJSON("all")
	require.NoError(t, err)
	assert.Empty(t, rep.failed())
	assert.Equal(t, []string{
		"1.0.0 fail on 1.0.0",
		"1.0.0 fail on 1.1.0",
		"1.0.0 pass on 1.0.0",
		"1.1.0 fail on 1.1.0",
		"1.1.0 pass on 1.0.0",
		"1.1.0 pass on 1.1.0",
	}, rep.labels())
}

func TestReaderAheadCatchesTighteningByEarlierPassDocument(t *testing.T) {
	t.Parallel()

	for name, family := range map[string]string{
		"an anyOf arm dropped":            "tighten/anyof-arm",
		"a newly required property":       "tighten/new-required",
		"a family that can go either way": "tighten/either-family",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			key := strings.ReplaceAll(family, "/", "_") + "_1_1_0"
			rep, err := newBehaviourRegistry(t, family).runJSON(key)
			require.ErrorIs(t, err, ErrTestsFailed)

			failed := rep.failed()[schema.Key(key)]
			require.Len(t, failed, 1)
			assert.Equal(t, "1.0.0 pass on 1.1.0", failed[0].label())
			assert.Equal(t, "no-tightening", failed[0].Rule)
		})
	}
}

func TestReaderAheadCatchesTighteningByLaterFailDocument(t *testing.T) {
	t.Parallel()
	const later = "tighten_later-fail-document_1_1_0"

	rep, err := newBehaviourRegistry(t, "tighten/later-fail-document").runJSON(later)
	require.ErrorIs(t, err, ErrTestsFailed)

	failed := rep.failed()[later]
	require.Len(t, failed, 1)
	assert.Equal(t, "1.1.0 fail on 1.0.0", failed[0].label())
	assert.Equal(t, "no-tightening", failed[0].Rule)
}

func TestReaderAheadAllowsLoosening(t *testing.T) {
	t.Parallel()
	r := newBehaviourRegistry(t, "allowed/reader-ahead-loosens")

	rep, err := r.runJSON("all")
	require.NoError(t, err)
	assert.Empty(t, rep.failed())
	assert.Equal(t, []string{
		"1.0.0 fail on 1.0.0",
		"1.0.0 pass on 1.0.0",
		"1.0.0 pass on 1.1.0",
		"1.1.0 fail on 1.0.0",
		"1.1.0 fail on 1.1.0",
		"1.1.0 pass on 1.1.0",
	}, rep.labels())
}

func TestEitherRunsAllFourPairs(t *testing.T) {
	t.Parallel()

	rep, err := newBehaviourRegistry(t, "allowed/either-no-change").runJSON("all")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"1.0.0 fail on 1.0.0",
		"1.0.0 fail on 1.1.0",
		"1.0.0 pass on 1.0.0",
		"1.0.0 pass on 1.1.0",
		"1.1.0 fail on 1.0.0",
		"1.1.0 fail on 1.1.0",
		"1.1.0 pass on 1.0.0",
		"1.1.0 pass on 1.1.0",
	}, rep.labels())
}

var ownDocumentsOfTwoVersions = []string{
	"1.0.0 fail on 1.0.0",
	"1.0.0 pass on 1.0.0",
	"1.1.0 fail on 1.1.0",
	"1.1.0 pass on 1.1.0",
}

func TestNeitherRunsOwnDocumentsOnly(t *testing.T) {
	t.Parallel()

	rep, err := newBehaviourRegistry(t, "allowed/lock-step-rename").runJSON("all")
	require.NoError(t, err)
	assert.Equal(t, ownDocumentsOfTwoVersions, rep.labels())
}

func TestNoDocumentCrossesAMajor(t *testing.T) {
	t.Parallel()

	rep, err := newBehaviourRegistry(t, "major/incompatible-change").runJSON("all")
	require.NoError(t, err)
	for _, label := range rep.labels() {
		parts := strings.Split(label, " on ")
		assert.Equal(t, parts[0][:1], parts[1][:1], "%s crosses a major", label)
	}
}

func TestEveryEarlierVersionInMajorIsChecked(t *testing.T) {
	t.Parallel()
	const newest = "loosen_reaches-every-earlier-version_1_2_0"

	rep, err := newBehaviourRegistry(t, "loosen/reaches-every-earlier-version").runJSON(newest)
	require.ErrorIs(t, err, ErrTestsFailed)
	assert.Contains(t, rep.labels(), "1.2.0 pass on 1.1.0")
	assert.Contains(t, rep.labels(), "1.2.0 pass on 1.0.0")

	failed := rep.failed()[newest]
	require.Len(t, failed, 1)
	assert.Equal(t, "1.2.0 pass on 1.0.0", failed[0].label())
}

// involving returns the labels of the runs which involve the version.
func involving(labels []string, version string) []string {
	return slices.DeleteFunc(slices.Clone(labels), func(label string) bool { return !strings.Contains(label, version) })
}

func TestPairsAreTheSameBySelection(t *testing.T) {
	t.Parallel()
	const family = "loosen/reaches-every-earlier-version"
	r := newBehaviourRegistry(t, family)

	everything, allErr := r.runJSON("all")
	require.ErrorIs(t, allErr, ErrTestsFailed)
	assert.Len(t, everything.labels(), 12, "each of 3 pairs runs 2 documents, and each of 3 versions its own 2: "+
		"each once, not once from each side")

	for _, tc := range []struct {
		version, key string
		wantErr      bool
	}{
		{"1.2.0", "loosen_reaches-every-earlier-version_1_2_0", true},
		{"1.1.0", "loosen_reaches-every-earlier-version_1_1_0", false},
		{"1.0.0", "loosen_reaches-every-earlier-version_1_0_0", true},
	} {
		t.Run(tc.version, func(t *testing.T) {
			t.Parallel()
			scope := family + "/" + strings.ReplaceAll(tc.version, ".", "/")

			byKey, keyErr := r.runJSON(tc.key)
			byScope, scopeErr := r.runJSON(scope)

			assert.Equal(t, byKey.labels(), byScope.labels())
			assert.Equal(t, involving(everything.labels(), tc.version), byKey.labels())
			assert.Equal(t, tc.wantErr, keyErr != nil)
			assert.Equal(t, tc.wantErr, scopeErr != nil, "a scope holding only the version gives the same verdict")
		})
	}
}

func TestFailureAppearsUnderLaterVersion(t *testing.T) {
	t.Parallel()
	const (
		earlier = "loosen_later-pass-document_1_0_0"
		later   = "loosen_later-pass-document_1_1_0"
	)
	r := newBehaviourRegistry(t, "loosen/later-pass-document")

	for _, asked := range []string{earlier, later, "all", "loosen/later-pass-document"} {
		t.Run(asked, func(t *testing.T) {
			t.Parallel()
			rep, err := r.runJSON(asked)
			require.ErrorIs(t, err, ErrTestsFailed)

			failed := rep.failed()
			require.Len(t, failed, 1, "only the later version shows the failure")
			require.Len(t, failed[later], 1)
			assert.Equal(t, schema.Key(earlier), failed[later][0].ValidatedBy)
		})
	}
}

func TestReaderPicksSchemaRunsNoCrossVersionPairs(t *testing.T) {
	t.Parallel()

	for _, family := range []string{"allowed/reader-picks-schema", "allowed/reader-picks-schema-override"} {
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			rep, err := newBehaviourRegistry(t, family).runJSON("all")
			require.NoError(t, err)
			assert.Equal(t, ownDocumentsOfTwoVersions, rep.labels())
		})
	}
}

func TestStrictFailsOnWarningInValidateAndBuildDist(t *testing.T) {
	t.Parallel()
	const (
		family = "warned/unexercised-enum-value"
		later  = "warned_unexercised-enum-value_1_1_0"
	)

	t.Run("validate", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, family)
		_, err := r.jsm("validate", later)
		require.NoError(t, err)

		_, err = r.jsm("validate", later, "--strict")
		require.ErrorIs(t, err, ErrTestsFailed)
	})

	t.Run("build-dist prints warnings and still builds", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, family)
		out, err := r.jsm("build-dist", behaviourEnv, "--all")
		require.NoError(t, err)
		assert.Contains(t, out, "Unexercised changes")
		assert.Contains(t, out, "Successfully built")
	})

	t.Run("build-dist --strict builds nothing", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, family)
		out, err := r.jsm("build-dist", behaviourEnv, "--all", "--strict")
		require.ErrorIs(t, err, ErrTestsFailed)
		assert.Contains(t, out, "Unexercised changes")
		assert.NotContains(t, out, "Successfully built")
	})

	t.Run("build-dist --strict and --skip-tests contradict each other", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, family)
		_, err := r.jsm("build-dist", behaviourEnv, "--all", "--strict", "--skip-tests")
		require.Error(t, err)
		assert.Equal(t, ExitCannotRun, ExitCode(err))
	})

	t.Run("watch", func(t *testing.T) {
		t.Parallel()
		mgr, logs, _ := newWatchManager(t, newBehaviourRegistry(t, family))
		k := schema.Key(later)
		mgr.handleWatchEvent(context.Background(), schema.WatchEvent{Key: k}, schema.ResolvedTarget{Key: &k},
			false, "json", false, true, true)
		assert.Contains(t, logs.String(), "warnings")
	})
}

// newWatchManager returns a manager of the registry, which writes logs and the report to buffers,
// using a real compiler.
func newWatchManager(t *testing.T, r *declTestRegistry) (mgr *CLIManager, logs, reports *safeBuffer) {
	t.Helper()
	reg, err := schema.NewRegistry(r.reg, validator.NewSanthoshCompiler(), fsh.NewPathResolver(), fsh.NewEnvProvider())
	require.NoError(t, err)
	logs, reports = &safeBuffer{}, &safeBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	return NewCLIManager(logger, reg, schema.NewTester(reg), &MockGitter{}, nil, reports), logs, reports
}

// watchUntilReported starts watching the target, makes the change, and returns the first report.
func watchUntilReported(
	t *testing.T, mgr *CLIManager, reports *safeBuffer, target schema.ResolvedTarget, change func(),
) jsonReport {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- mgr.WatchValidation(ctx, target, false, "json", false, true, false, ready)
	}()
	<-ready

	change()
	require.True(t, reports.waitForOutput(5*time.Second), "no report")
	// The report is written as one encoded value, so wait until it is whole
	require.Eventually(
		t,
		func() bool { return json.Valid([]byte(reports.String())) },
		5*time.Second,
		10*time.Millisecond,
	)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	return parseJSONReport(t, reports.String())
}

func TestWatchRunsSamePairsAsValidate(t *testing.T) {
	t.Parallel()
	const (
		family = "loosen/reaches-every-earlier-version"
		first  = "loosen_reaches-every-earlier-version_1_0_0"
		middle = "loosen_reaches-every-earlier-version_1_1_0"
	)
	schemaPath := func(r *declTestRegistry, key string) string {
		v := schema.Key(key).Version()
		return filepath.Join(r.reg, family, strings.ReplaceAll(v.String('.'), ".", string(filepath.Separator)),
			key+schema.SchemaSuffix)
	}
	touch := func(t *testing.T, path string) {
		t.Helper()
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, data, 0o600))
	}

	t.Run("a changed schema runs what validate runs for that version", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, family)
		want, err := r.runJSON(middle)
		require.NoError(t, err)

		mgr, _, reports := newWatchManager(t, r)
		scope := schema.SearchScope(family)
		got := watchUntilReported(t, mgr, reports, schema.ResolvedTarget{Scope: &scope}, func() {
			touch(t, schemaPath(r, middle))
		})
		assert.Equal(t, want.labels(), got.labels())
	})

	t.Run("watching an earlier version sees a change to a later one", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, family)
		want, err := r.runJSON(first)
		require.ErrorIs(t, err, ErrTestsFailed)

		mgr, _, reports := newWatchManager(t, r)
		key := schema.Key(first)
		got := watchUntilReported(t, mgr, reports, schema.ResolvedTarget{Key: &key}, func() {
			touch(t, schemaPath(r, middle))
		})
		assert.Equal(t, want.labels(), got.labels())
	})

	t.Run("a changed document runs where validate would run it", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, family)
		mgr, _, reports := newWatchManager(t, r)
		scope := schema.SearchScope(family)
		doc := filepath.Join(r.reg, family, "1", "2", "0", "pass", "paid.json")
		got := watchUntilReported(t, mgr, reports, schema.ResolvedTarget{Scope: &scope}, func() {
			touch(t, doc)
		})
		assert.Equal(t, []string{"1.2.0 pass on 1.0.0", "1.2.0 pass on 1.1.0", "1.2.0 pass on 1.2.0"}, got.labels())
	})

	t.Run("a corrected declaration reruns the family", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, family)
		mgr, _, reports := newWatchManager(t, r)
		scope := schema.SearchScope(family)
		got := watchUntilReported(t, mgr, reports, schema.ResolvedTarget{Scope: &scope}, func() {
			r.write(filepath.Join(family, schema.FamilyDeclarationFile), "scenario: lock-step\n")
		})
		assert.Empty(t, got.failed(), "lock-step has no cross-version runs")
		assert.Len(t, got.labels(), 6)
	})

	t.Run("a corrected declaration reruns a watched version", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, family)
		mgr, _, reports := newWatchManager(t, r)
		key := schema.Key(first)
		got := watchUntilReported(t, mgr, reports, schema.ResolvedTarget{Key: &key}, func() {
			r.write(filepath.Join(family, schema.FamilyDeclarationFile), "scenario: lock-step\n")
		})
		assert.Equal(t, []string{"1.0.0 fail on 1.0.0", "1.0.0 pass on 1.0.0"}, got.labels())
	})

	t.Run("a change outside the watched major is ignored", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, "major/incompatible-change")
		mgr, _, reports := newWatchManager(t, r)
		first := schema.Key("major_incompatible-change_1_0_0")
		mgr.handleWatchEvent(context.Background(), schema.WatchEvent{Key: "major_incompatible-change_2_0_0"},
			schema.ResolvedTarget{Key: &first}, false, "json", false, true, false)
		assert.Empty(t, reports.String())
	})

	t.Run("a declaration of another family is ignored", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, family)
		mgr, _, reports := newWatchManager(t, r)
		key := schema.Key(first)
		other := schema.SearchScope("loosen/other-family")
		scope := schema.SearchScope("tighten")
		mgr.handleWatchEvent(context.Background(), schema.WatchEvent{Family: other},
			schema.ResolvedTarget{Key: &key}, false, "json", false, true, false)
		mgr.handleWatchEvent(context.Background(), schema.WatchEvent{Family: other},
			schema.ResolvedTarget{Scope: &scope}, false, "json", false, true, false)
		assert.Empty(t, reports.String())
	})

	t.Run("watching a version by its scope sees its family's declaration", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, family)
		mgr, _, reports := newWatchManager(t, r)
		scope := schema.SearchScope(family + "/1/0/0")
		got := watchUntilReported(t, mgr, reports, schema.ResolvedTarget{Scope: &scope}, func() {
			r.write(filepath.Join(family, schema.FamilyDeclarationFile), "scenario: lock-step\n")
		})
		assert.Equal(t, []string{"1.0.0 fail on 1.0.0", "1.0.0 pass on 1.0.0"}, got.labels())
	})

	t.Run("watching a version by its scope sees another version in its major", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, family)
		scope := schema.SearchScope(family + "/1/0/0")
		want, err := r.runJSON(first)
		require.ErrorIs(t, err, ErrTestsFailed)

		mgr, logs, reports := newWatchManager(t, r)
		got := watchUntilReported(t, mgr, reports, schema.ResolvedTarget{Scope: &scope}, func() {
			touch(t, schemaPath(r, middle))
		})
		assert.Equal(t, want.labels(), got.labels())
		assert.Contains(t, logs.String(), "Schema changed:")
		assert.NotContains(t, logs.String(), "Declaration changed:")
	})

	t.Run("a scope below the family ignores another major", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, "major/incompatible-change")
		mgr, _, reports := newWatchManager(t, r)
		scope := schema.SearchScope("major/incompatible-change/1/0/0")
		mgr.handleWatchEvent(context.Background(), schema.WatchEvent{Key: "major_incompatible-change_2_0_0"},
			schema.ResolvedTarget{Scope: &scope}, false, "json", false, true, false)
		assert.Empty(t, reports.String())
	})

	t.Run("a broken declaration is logged, and the watch goes on", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, family)
		mgr, logs, reports := newWatchManager(t, r)
		r.write(filepath.Join(family, schema.FamilyDeclarationFile), "scenario: nonsense\n")
		f := schema.SearchScope(family)
		mgr.handleWatchEvent(context.Background(), schema.WatchEvent{Family: f},
			schema.ResolvedTarget{Scope: &f}, false, "json", false, true, false)
		assert.Empty(t, reports.String())
		assert.Contains(t, logs.String(), "nonsense")
	})
}

func TestRemovedFlagsAreRefused(t *testing.T) {
	t.Parallel()
	const key = "own_documents-behave_1_0_0"

	for _, args := range [][]string{
		{"-t", "all"},
		{"--test-scope", "all"},
		{"--skip-compatible"},
	} {
		t.Run(args[0], func(t *testing.T) {
			t.Parallel()
			out, err := newBehaviourRegistry(
				t,
				"own/documents-behave",
			).jsm(append([]string{"validate", key}, args...)...)
			require.Error(t, err)
			assert.Equal(t, ExitCannotRun, ExitCode(err))
			assert.Contains(t, out, "unknown")
		})
	}
}

func TestValidatingEarlierVersionRunsLaterPairs(t *testing.T) {
	t.Parallel()
	const earlier = "loosen_later-pass-document_1_0_0"
	r := newBehaviourRegistry(t, "loosen/later-pass-document")

	_, err := r.jsm("validate", earlier)
	require.ErrorIs(t, err, ErrTestsFailed)

	rep, err := r.runJSON(earlier)
	require.ErrorIs(t, err, ErrTestsFailed)
	assert.Contains(t, rep.labels(), "1.1.0 pass on 1.0.0")
	assert.Contains(t, rep.labels(), "1.0.0 fail on 1.1.0")
}

func TestBuildDistStopsOnRuleFailure(t *testing.T) {
	t.Parallel()

	for name, args := range map[string][]string{
		"every schema":         {"build-dist", behaviourEnv, "--all"},
		"schemas that changed": {"build-dist", behaviourEnv},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newBehaviourRegistry(t, "loosen/anyof-arm")
			out, err := r.jsm(args...)
			require.ErrorIs(t, err, ErrTestsFailed)
			assert.Contains(t, out, "no-loosening")
			assert.NotContains(t, out, "Successfully built")
			assert.NoDirExists(t, filepath.Join(r.repo, "dist", behaviourEnv))
		})
	}
}

func TestDescriptiveFamilyFailureSaysTheFormatChanged(t *testing.T) {
	t.Parallel()

	out, err := newBehaviourRegistry(t, "tighten/third-party-format").jsm("validate", "all", "-C")
	require.ErrorIs(t, err, ErrTestsFailed)
	assert.Contains(t, out, "real format has changed")
	assert.NotContains(t, out, "new major version")

	// A prescriptive family's failure says the author can fix it
	out, err = newBehaviourRegistry(t, "tighten/new-required").jsm("validate", "all", "-C")
	require.ErrorIs(t, err, ErrTestsFailed)
	assert.NotContains(t, out, "real format has changed")
	assert.Contains(t, out, "new major version")
}

// junitReport is the part of a JUnit report that names a failure.
type junitReport struct {
	Suites []struct {
		Name  string `xml:"name,attr"`
		Cases []struct {
			Name    string `xml:"name,attr"`
			Failure *struct {
				Message string `xml:"message,attr"`
				Text    string `xml:",chardata"`
			} `xml:"failure"`
		} `xml:"testcase"`
	} `xml:"testsuite"`
}

func TestFailureNamesRuleVersionsAndDocument(t *testing.T) {
	t.Parallel()
	const (
		family  = "tighten/new-required"
		earlier = "tighten_new-required_1_0_0"
		later   = "tighten_new-required_1_1_0"
		doc     = "sku.json"
	)
	t.Run("text", func(t *testing.T) {
		t.Parallel()
		out, err := newBehaviourRegistry(t, family).jsm("validate", later, "-C")
		require.ErrorIs(t, err, ErrTestsFailed)
		for _, want := range []string{"no-tightening", earlier, later, doc, "consumer-first"} {
			assert.Contains(t, out, want)
		}
	})

	t.Run("JSON", func(t *testing.T) {
		t.Parallel()
		rep, err := newBehaviourRegistry(t, family).runJSON(later)
		require.ErrorIs(t, err, ErrTestsFailed)

		failed := rep.failed()[later]
		require.Len(t, failed, 1)
		assert.Equal(t, "no-tightening", failed[0].Rule)
		assert.Equal(t, schema.Key(earlier), failed[0].Earlier)
		assert.Equal(t, schema.Key(later), failed[0].Later)
		assert.Equal(t, schema.Key(earlier), failed[0].DocumentFrom)
		assert.Equal(t, schema.Key(later), failed[0].ValidatedBy)
		assert.Contains(t, failed[0].Path, doc)
		assert.Contains(t, failed[0].Error, "consumer-first")
	})

	t.Run("an own document names its rule", func(t *testing.T) {
		t.Parallel()
		rep, err := newBehaviourRegistry(t, "own/pass-document-rejected").runJSON("all")
		require.ErrorIs(t, err, ErrTestsFailed)
		for _, failed := range rep.failed() {
			require.Len(t, failed, 1)
			assert.Equal(t, "own-documents", failed[0].Rule)
			assert.Empty(t, failed[0].Earlier)
		}
	})

	t.Run("JUnit", func(t *testing.T) {
		t.Parallel()
		out, err := newBehaviourRegistry(t, family).jsm("validate", later, "-C", "-o", "junit")
		require.ErrorIs(t, err, ErrTestsFailed)

		var rep junitReport
		require.NoError(t, xml.Unmarshal([]byte(out), &rep))
		var found bool
		for _, suite := range rep.Suites {
			for _, c := range suite.Cases {
				if c.Failure == nil {
					continue
				}
				found = true
				assert.Equal(t, later+schema.SchemaSuffix, suite.Name)
				assert.Contains(t, c.Name, doc)
				assert.Contains(t, c.Failure.Message, "no-tightening")
				assert.Contains(t, c.Failure.Text, earlier)
				assert.Contains(t, c.Failure.Text, later)
			}
		}
		assert.True(t, found, "no failure in %s", out)
	})
}
