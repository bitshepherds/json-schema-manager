package app

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/repo"
	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

// These tests run the whole CLI on the exception families of the behaviour registry.

const (
	clearedFamily  = "exception/cleared-pair"
	clearedLaterK  = "exception_cleared-pair_1_1_0"
	clearedEarlier = "exception_cleared-pair_1_0_0"
	clearedFile    = "exception/cleared-pair/1/1/0/compat-exceptions.yml"
	clearedReason  = "1.0.0 wrongly rejected refunded orders."
)

// exceptionEntry returns the text of one entry of an exception file.
func exceptionEntry(version, document, rule, reason string) string {
	return "  - version: " + version + "\n    document: " + document + "\n    rule: " + rule +
		"\n    reason: " + reason + "\n"
}

// withExceptions returns a registry of the cleared-pair family, with the given exception file.
func withExceptions(t *testing.T, entries ...string) *declTestRegistry {
	t.Helper()
	r := newBehaviourRegistry(t, clearedFamily)
	r.write(clearedFile, "exceptions:\n"+strings.Join(entries, ""))
	return r
}

// requireStale runs validate on the family, and requires the entry named by want to be reported stale.
func requireStale(t *testing.T, r *declTestRegistry, key string, want ...string) {
	t.Helper()
	out, err := r.jsm("validate", key, "-C")
	require.ErrorIs(t, err, ErrTestsFailed, out)
	assert.Equal(t, ExitCheckFailed, ExitCode(err))
	assert.Contains(t, out, "stale exception")
	for _, w := range want {
		assert.Contains(t, out, w)
	}
}

func TestExceptionClearsNamedPair(t *testing.T) {
	t.Parallel()

	t.Run("a failure the file names exits 0, by key and by scope", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, clearedFamily)
		for _, target := range []string{clearedLaterK, clearedEarlier, "all", clearedFamily} {
			out, err := r.jsm("validate", target)
			require.NoError(t, err, out)
		}
	})

	t.Run("without the file, the same run fails", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, clearedFamily)
		require.NoError(t, os.Remove(filepath.Join(r.reg, clearedFile)))
		out, err := r.jsm("validate", clearedLaterK)
		require.ErrorIs(t, err, ErrTestsFailed, out)
		assert.Contains(t, out, "compat-exceptions.yml", "a broken rule says how to declare it deliberate")
	})

	t.Run("each run of the rule table can be named", func(t *testing.T) {
		t.Parallel()
		// A later pass document, on the earlier version
		r := withExceptions(t,
			exceptionEntry("1.0.0", "fail/refunded.json", "no-loosening", clearedReason),
			exceptionEntry("1.0.0", "pass/refunded.json", "no-loosening", "A new sample, with the same fault."),
		)
		r.write("exception/cleared-pair/1/1/0/pass/refunded.json", `{"status": "refunded"}`)
		out, err := r.jsm("validate", clearedLaterK)
		require.NoError(t, err, out)

		// Both runs of a reader-ahead family
		r = newBehaviourRegistry(t, "tighten/new-required")
		document := "pass/" + onlyDocument(t, r, "tighten/new-required/1/0/0/pass")
		r.write("tighten/new-required/1/1/0/compat-exceptions.yml", "exceptions:\n"+
			exceptionEntry("1.0.0", document, "no-tightening", "Deliberate."))
		out, err = r.jsm("validate", "tighten_new-required_1_1_0")
		require.NoError(t, err, out)
	})

	t.Run("build-dist builds", func(t *testing.T) {
		t.Parallel()
		out, err := newBehaviourRegistry(t, clearedFamily).jsm("build-dist", behaviourEnv, "--all")
		require.NoError(t, err, out)
	})

	t.Run("an excepted failure doesn't stop the run", func(t *testing.T) {
		t.Parallel()
		// Without --continue-on-error, a run stops at its first failure. a-refunded.json runs first
		// and is excepted, and if that stopped the run, the failure of refunded.json would go unseen.
		r := withExceptions(t, exceptionEntry("1.0.0", "fail/a-refunded.json", "no-loosening", clearedReason))
		r.write("exception/cleared-pair/1/0/0/fail/a-refunded.json", `{"status": "refunded"}`)
		out, err := r.jsm("validate", clearedLaterK)
		require.ErrorIs(t, err, ErrTestsFailed, out)
	})
}

// onlyDocument returns the name of the only document in a directory of the registry.
func onlyDocument(t *testing.T, r *declTestRegistry, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(r.reg, dir))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	return entries[0].Name()
}

func TestExceptionDoesNotClearAnotherPair(t *testing.T) {
	t.Parallel()

	t.Run("another document of the pair still fails", func(t *testing.T) {
		t.Parallel()
		const later = "exception_another-pair_1_1_0"
		rep, err := newBehaviourRegistry(t, "exception/another-pair").runJSON(later)
		require.ErrorIs(t, err, ErrTestsFailed)
		failed := rep.failed()[later]
		require.Len(t, failed, 1)
		assert.Contains(t, failed[0].Path, "refunded-again.json")
	})

	t.Run("the same document with another earlier version still fails", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, clearedFamily)
		const v110 = "exception/cleared-pair/1/1/0/"
		const v120 = "exception/cleared-pair/1/2/0/"
		schemaFile, err := os.ReadFile(filepath.Join(r.reg, v110, clearedLaterK+".schema.json"))
		require.NoError(t, err)
		r.write(v120+"exception_cleared-pair_1_2_0.schema.json", string(schemaFile))

		rep, err := r.runJSON("exception_cleared-pair_1_2_0")
		require.ErrorIs(t, err, ErrTestsFailed, "1.1.0's file clears nothing for 1.2.0")
		assert.Len(t, rep.failed()["exception_cleared-pair_1_2_0"], 1)
	})
}

func TestExceptionReportedWithReason(t *testing.T) {
	t.Parallel()

	t.Run("text", func(t *testing.T) {
		t.Parallel()
		out, err := newBehaviourRegistry(t, clearedFamily).jsm("validate", clearedLaterK)
		require.NoError(t, err)
		assert.Contains(t, out, "refunded.json")
		assert.Contains(t, out, "no-loosening excepted")
		assert.Contains(t, out, clearedReason)
		assert.Contains(t, out, "1 excepted")
		assert.Contains(t, out, "excepted: 1")
	})

	t.Run("JSON", func(t *testing.T) {
		t.Parallel()
		out, err := newBehaviourRegistry(t, clearedFamily).jsm("validate", clearedLaterK, "-o", "json")
		require.NoError(t, err, out)
		var rep struct {
			Stats struct {
				TotalExcepted int `json:"totalExcepted"`
				TotalFailed   int `json:"totalFailed"`
			} `json:"stats"`
			Results map[string]struct {
				Excepted []struct {
					Path    string `json:"path"`
					Status  string `json:"status"`
					Rule    string `json:"rule"`
					Reason  string `json:"reason"`
					Earlier string `json:"earlier"`
					Later   string `json:"later"`
				} `json:"excepted"`
			} `json:"results"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &rep))
		assert.Equal(t, 1, rep.Stats.TotalExcepted)
		assert.Equal(t, 0, rep.Stats.TotalFailed)
		excepted := rep.Results[clearedLaterK].Excepted
		require.Len(t, excepted, 1)
		assert.Equal(t, "excepted", excepted[0].Status)
		assert.Equal(t, "no-loosening", excepted[0].Rule)
		assert.Equal(t, clearedReason, excepted[0].Reason)
		assert.Equal(t, clearedEarlier, excepted[0].Earlier)
		assert.Equal(t, clearedLaterK, excepted[0].Later)
		assert.Contains(t, excepted[0].Path, "refunded.json")
	})

	t.Run("JUnit", func(t *testing.T) {
		t.Parallel()
		out, err := newBehaviourRegistry(t, clearedFamily).jsm("validate", clearedLaterK, "-o", "junit")
		require.NoError(t, err, out)
		var rep struct {
			Skipped  int `xml:"skipped,attr"`
			Failures int `xml:"failures,attr"`
			Suites   []struct {
				Name    string `xml:"name,attr"`
				Skipped int    `xml:"skipped,attr"`
				Cases   []struct {
					Name    string `xml:"name,attr"`
					Skipped *struct {
						Message string `xml:"message,attr"`
					} `xml:"skipped"`
				} `xml:"testcase"`
			} `xml:"testsuite"`
		}
		require.NoError(t, xml.Unmarshal([]byte(out), &rep))
		assert.Equal(t, 1, rep.Skipped)
		assert.Equal(t, 0, rep.Failures)
		var skipped []string
		for _, suite := range rep.Suites {
			for _, c := range suite.Cases {
				if c.Skipped != nil {
					assert.Equal(t, 1, suite.Skipped)
					assert.Contains(t, c.Name, "refunded.json")
					skipped = append(skipped, c.Skipped.Message)
				}
			}
		}
		require.Len(t, skipped, 1)
		assert.Contains(t, skipped[0], clearedReason)
	})
}

func TestExceptionWithoutReasonIsRefused(t *testing.T) {
	t.Parallel()

	t.Run("by validate and build-dist, whichever version is selected", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, clearedFamily)
		r.write(clearedFile, "exceptions:\n  - version: 1.0.0\n    document: fail/refunded.json\n"+
			"    rule: no-loosening\n")

		for _, target := range []string{clearedLaterK, clearedEarlier} {
			out, err := r.jsm("validate", target)
			require.Error(t, err, out)
			assert.Equal(t, ExitCannotRun, ExitCode(err))
			assert.Contains(t, out, "compat-exceptions.yml")
			assert.Contains(t, out, "reason")
		}

		_, err := r.jsm("build-dist", behaviourEnv, "--all")
		assert.Equal(t, ExitCannotRun, ExitCode(err))
	})

	t.Run("a file that can't be read", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, clearedFamily)
		path := filepath.Join(r.reg, clearedFile)
		require.NoError(t, os.Remove(path))
		require.NoError(t, os.Mkdir(path, 0o750))
		_, err := r.jsm("validate", clearedLaterK)
		assert.Equal(t, ExitCannotRun, ExitCode(err))
	})

	t.Run("an empty file excepts nothing", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, clearedFamily)
		r.write(clearedFile, "")
		_, err := r.jsm("validate", clearedLaterK)
		require.ErrorIs(t, err, ErrTestsFailed)
	})
}

func TestExceptionNamingMissingDocumentIsRefused(t *testing.T) {
	t.Parallel()
	valid := exceptionEntry("1.0.0", "fail/refunded.json", "no-loosening", clearedReason)

	t.Run("a document that doesn't exist", func(t *testing.T) {
		t.Parallel()
		r := withExceptions(t, valid, exceptionEntry("1.0.0", "fail/gone.json", "no-loosening", "x"))
		requireStale(t, r, clearedLaterK, "fail/gone.json", "1.0.0 has no document")
	})

	t.Run("a document the later version would own", func(t *testing.T) {
		t.Parallel()
		r := withExceptions(t, valid, exceptionEntry("1.0.0", "pass/gone.json", "no-loosening", "x"))
		requireStale(t, r, clearedEarlier, "pass/gone.json", "1.1.0 has no document")
	})

	t.Run("a version that doesn't exist", func(t *testing.T) {
		t.Parallel()
		r := withExceptions(t, valid, exceptionEntry("1.0.5", "fail/refunded.json", "no-loosening", "x"))
		requireStale(t, r, clearedLaterK, "1.0.5", "no earlier version")
	})

	t.Run("a version that isn't earlier", func(t *testing.T) {
		t.Parallel()
		r := withExceptions(t, valid, exceptionEntry("1.1.0", "fail/lost.json", "no-loosening", "x"))
		requireStale(t, r, clearedLaterK, "no earlier version")
	})

	t.Run("a rule the family doesn't hold its versions to", func(t *testing.T) {
		t.Parallel()
		r := withExceptions(t, valid, exceptionEntry("1.0.0", "pass/placed.json", "no-tightening", "x"))
		requireStale(t, r, clearedLaterK, "no-tightening", "doesn't hold")
	})

	t.Run("a family with no runs across versions", func(t *testing.T) {
		t.Parallel()
		r := withExceptions(t, valid)
		r.write("exception/cleared-pair/family.yml", "scenario: lock-step\n")
		requireStale(t, r, clearedLaterK, "runs no documents across versions")
	})

	t.Run("build-dist fails too", func(t *testing.T) {
		t.Parallel()
		r := withExceptions(t, valid, exceptionEntry("1.0.0", "fail/gone.json", "no-loosening", "x"))
		out, err := r.jsm("build-dist", behaviourEnv, "--all")
		require.ErrorIs(t, err, ErrTestsFailed, out)
		assert.Contains(t, out, "stale exception")
		assert.Contains(t, out, "Not building")
	})
}

func TestExceptionForPassingPairIsRefused(t *testing.T) {
	t.Parallel()
	const staleFamily, staleLater = "exception/stale-entry", "exception_stale-entry_1_1_0"

	t.Run("text", func(t *testing.T) {
		t.Parallel()
		requireStale(t, newBehaviourRegistry(t, staleFamily), staleLater, "fail/lost.json", "doesn't fail")
	})

	t.Run("by the earlier version too", func(t *testing.T) {
		t.Parallel()
		requireStale(t, newBehaviourRegistry(t, staleFamily), "exception_stale-entry_1_0_0", "doesn't fail")
	})

	t.Run("JSON", func(t *testing.T) {
		t.Parallel()
		out, err := newBehaviourRegistry(t, staleFamily).jsm("validate", staleLater, "-o", "json")
		require.ErrorIs(t, err, ErrTestsFailed, out)
		var rep struct {
			Stats struct {
				TotalStale int `json:"totalStaleExceptions"`
			} `json:"stats"`
			Stale []struct {
				Schema   string `json:"schema"`
				Version  string `json:"version"`
				Document string `json:"document"`
				Rule     string `json:"rule"`
				Reason   string `json:"reason"`
				Why      string `json:"why"`
			} `json:"staleExceptions"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &rep))
		assert.Equal(t, 1, rep.Stats.TotalStale)
		require.Len(t, rep.Stale, 1)
		assert.Equal(t, staleLater, rep.Stale[0].Schema)
		assert.Equal(t, "1.0.0", rep.Stale[0].Version)
		assert.Equal(t, "fail/lost.json", rep.Stale[0].Document)
		assert.Equal(t, "no-loosening", rep.Stale[0].Rule)
		assert.NotEmpty(t, rep.Stale[0].Reason)
		assert.Contains(t, rep.Stale[0].Why, "doesn't fail")
	})

	t.Run("JUnit", func(t *testing.T) {
		t.Parallel()
		out, err := newBehaviourRegistry(t, staleFamily).jsm("validate", staleLater, "-o", "junit")
		require.ErrorIs(t, err, ErrTestsFailed, out)
		assert.Contains(t, out, "stale exception")
		assert.Contains(t, out, `failures="1"`)
	})
}

// deployedExceptions returns a registry with the cleared-pair family's versions deployed.
func deployedExceptions(t *testing.T) *declTestRegistry {
	t.Helper()
	r := newBehaviourRegistry(t, clearedFamily)
	r.deployTo(behaviourEnv)
	return r
}

// deployTo records everything committed so far as deployed to the environment.
func (r *declTestRegistry) deployTo(env string) {
	r.t.Helper()
	r.git("tag", "-a", "jsm-deploy/"+env+"/20260101-000000", "-m", "deployed")
}

// checkChangesAfter commits what's been changed, and runs check-changes.
func (r *declTestRegistry) checkChangesAfter() (string, error) {
	r.t.Helper()
	r.commit("change")
	return r.jsm("check-changes", behaviourEnv)
}

func TestCheckChangesAllowsAddingExceptionFileToDeployedVersion(t *testing.T) {
	t.Parallel()
	r := newBehaviourRegistry(t, "exception/red-deployed")
	r.deployTo(behaviourEnv)
	r.write("exception/red-deployed/1/1/0/compat-exceptions.yml", "exceptions:\n"+
		exceptionEntry("1.0.0", "fail/refunded.json", "no-loosening", clearedReason))
	out, err := r.checkChangesAfter()
	require.NoError(t, err, out)
}

func TestCheckChangesRefusesEditingDeployedException(t *testing.T) {
	t.Parallel()

	edits := map[string]string{
		"its reason":   exceptionEntry("1.0.0", "fail/refunded.json", "no-loosening", "Another reason."),
		"its document": exceptionEntry("1.0.0", "fail/lost.json", "no-loosening", clearedReason),
		"its rule":     exceptionEntry("1.0.0", "fail/refunded.json", "no-tightening", clearedReason),
	}
	for name, entry := range edits {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := deployedExceptions(t)
			r.write(clearedFile, "exceptions:\n"+entry)
			out, err := r.checkChangesAfter()
			require.Error(t, err, out)
			assert.Equal(t, ExitCheckFailed, ExitCode(err))
			assert.Contains(t, out, "cannot change a deployed exception")
			assert.Contains(t, out, "fail/refunded.json")
		})
	}

	t.Run("removing the file", func(t *testing.T) {
		t.Parallel()
		r := deployedExceptions(t)
		require.NoError(t, os.Remove(filepath.Join(r.reg, clearedFile)))
		out, err := r.checkChangesAfter()
		require.Error(t, err, out)
		assert.Contains(t, out, "cannot change a deployed exception")
	})

	t.Run("making the file invalid", func(t *testing.T) {
		t.Parallel()
		r := deployedExceptions(t)
		r.write(clearedFile, "exceptions: [")
		out, err := r.checkChangesAfter()
		require.Error(t, err, out)
		assert.Equal(t, ExitCheckFailed, ExitCode(err))
		assert.Contains(t, out, "cannot change a deployed exception")
	})

	t.Run("build-dist refuses too", func(t *testing.T) {
		t.Parallel()
		r := deployedExceptions(t)
		r.write(clearedFile, "exceptions:\n"+edits["its reason"])
		r.commit("change")
		out, err := r.jsm("build-dist", behaviourEnv)
		require.Error(t, err, out)
		assert.Contains(t, out, "cannot change a deployed exception")
	})
}

func TestCheckChangesAllowsNewEntryInDeployedException(t *testing.T) {
	t.Parallel()
	original := exceptionEntry("1.0.0", "fail/refunded.json", "no-loosening", clearedReason)

	t.Run("a new entry", func(t *testing.T) {
		t.Parallel()
		r := deployedExceptions(t)
		r.write(clearedFile, "exceptions:\n"+original+
			exceptionEntry("1.0.0", "fail/refunded-too.json", "no-loosening", "The same fault."))
		out, err := r.checkChangesAfter()
		require.NoError(t, err, out)
	})

	t.Run("a comment, or another order, changes no entry", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, clearedFamily)
		another := exceptionEntry("1.0.0", "fail/lost.json", "no-loosening", "Another.")
		r.write(clearedFile, "exceptions:\n"+original+another)
		r.commit("two entries")
		r.deployTo(behaviourEnv)
		r.write(clearedFile, "# Reviewed.\nexceptions:\n"+another+original)
		out, err := r.checkChangesAfter()
		require.NoError(t, err, out)
	})
}

func TestCheckChangesAllowsWhatLeavesDeployedExceptionsAlone(t *testing.T) {
	t.Parallel()
	const earlierDir = "exception/cleared-pair/1/0/0"
	const laterDir = "exception/cleared-pair/1/1/0"

	t.Run("a file on a version not deployed yet", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, "exception/red-deployed")
		r.write("exception/red-deployed/1/1/0/compat-exceptions.yml", "exceptions:\n"+
			exceptionEntry("1.0.0", "fail/refunded.json", "no-loosening", clearedReason))
		out, err := r.checkChangesAfter()
		require.NoError(t, err, out)
	})

	t.Run("deleting the later version whole", func(t *testing.T) {
		t.Parallel()
		r := deployedExceptions(t)
		require.NoError(t, os.RemoveAll(filepath.Join(r.reg, laterDir)))
		out, err := r.checkChangesAfter()
		require.NoError(t, err, out)
	})

	t.Run("a file invalid when deployed protects nothing", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, clearedFamily)
		r.write(clearedFile, "exceptions: [")
		r.commit("invalid")
		r.deployTo(behaviourEnv)
		r.write(clearedFile, "exceptions:\n"+
			exceptionEntry("1.0.0", "fail/refunded.json", "no-loosening", clearedReason))
		out, err := r.checkChangesAfter()
		require.NoError(t, err, out)
	})

	// An entry naming a deleted version is stale, so it must be able to go, or deleting a deployed
	// version would leave the family red for good.
	t.Run("an entry may go once the earlier version has been deleted", func(t *testing.T) {
		t.Parallel()
		r := deployedExceptions(t)
		require.NoError(t, os.RemoveAll(filepath.Join(r.reg, earlierDir)))
		r.write(clearedFile, "exceptions: []\n")
		out, err := r.checkChangesAfter()
		require.NoError(t, err, out)

		out, err = r.jsm("validate", "all")
		require.NoError(t, err, out)
	})

	t.Run("an entry may go once its document has gone", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, clearedFamily)
		// The document is the later version's own, added after the deployment of the file naming it
		r.write(laterDir+"/pass/refunded.json", `{"status": "refunded"}`)
		r.write(clearedFile, "exceptions:\n"+
			exceptionEntry("1.0.0", "fail/refunded.json", "no-loosening", clearedReason)+
			exceptionEntry("1.0.0", "pass/refunded.json", "no-loosening", "Another."))
		r.commit("two entries")
		r.deployTo(behaviourEnv)

		r.write(clearedFile, "exceptions:\n"+
			exceptionEntry("1.0.0", "fail/refunded.json", "no-loosening", clearedReason))
		require.NoError(t, os.Remove(filepath.Join(r.reg, laterDir, "pass/refunded.json")))
		out, err := r.checkChangesAfter()
		require.Error(t, err, out, "the deleted deployed document is still refused")
		assert.NotContains(t, out, "cannot change a deployed exception")
		assert.Contains(t, out, "cannot modify deployed schemas")
	})

	t.Run("an environment which allows mutation doesn't look", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, clearedFamily)
		cfg := "environments:\n  dev:\n    privateUrlRoot: https://d/\n    publicUrlRoot: https://d/\n" +
			"    allowSchemaMutation: true\n  production:\n    privateUrlRoot: https://p/\n" +
			"    publicUrlRoot: https://p/\n    isProduction: true\n"
		r.write("json-schema-manager-config.yml", cfg)
		r.commit("config")
		r.deployTo("dev")
		r.write(clearedFile, "exceptions: []\n")
		r.commit("change")
		out, err := r.jsm("check-changes", "dev")
		require.NoError(t, err, out)
	})
}

func TestCheckChangesStopsIfDeployedExceptionsCantBeRead(t *testing.T) {
	t.Parallel()
	registry := setupTestRegistry(t)
	gitter := &MockGitter{
		GetSchemaChangesFunc: func(_ context.Context, _ repo.Revision, _, _ string) ([]repo.Change, error) {
			path := filepath.Join(registry.RootDirectory(), "d/f/1/0/0/compat-exceptions.yml")
			return []repo.Change{{Path: path}}, nil
		},
		ShowFileFunc: func(_ context.Context, _ repo.Revision, _ string) ([]byte, error) {
			return nil, errors.New("git failed")
		},
	}
	m := NewCLIManager(slog.New(slog.DiscardHandler), registry, nil, gitter, nil, io.Discard)
	require.ErrorContains(t, m.CheckChanges(context.Background(), "prod"), "git failed")
}

func TestLockedExceptionChangesLocksAnEntryForAChange(t *testing.T) {
	t.Parallel()

	const entry = `exceptions:
  - version: 1.0.0
    change: "/a: x"
    reason: moved
`
	family := t.TempDir()
	dir := filepath.Join(family, "1", "1", "0")
	earlierDir := filepath.Join(family, "1", "0", "0")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.MkdirAll(earlierDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(earlierDir, "d_f_1_0_0"+schema.SchemaSuffix), nil, 0o600))
	path := filepath.Join(dir, schema.ExceptionsFile)
	deployed, err := schema.ParseExceptions([]byte(entry))
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(path, []byte(entry), 0o600))
	assert.Empty(t, lockedExceptionChanges(path, deployed))

	require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(entry, "/a: x", "/a: y")), 0o600))
	assert.Len(t, lockedExceptionChanges(path, deployed), 1)
}
