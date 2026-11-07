package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/repo"
	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

// These tests run the whole CLI on the lifecycle families of the behaviour registry.

const (
	retentionFamily = "lifecycle/retention-documents"
	retentionYML    = retentionFamily + "/family.yml"
	retention100    = "lifecycle_retention-documents_1_0_0"
	retention110    = "lifecycle_retention-documents_1_1_0"
	retention120    = "lifecycle_retention-documents_1_2_0"

	noMigrationFamily = "lifecycle/major-without-migration"
	noMigration100    = "lifecycle_major-without-migration_1_0_0"
	noMigration200    = "lifecycle_major-without-migration_2_0_0"
)

// deployedFamily returns a registry holding the family, deployed.
func deployedFamily(t *testing.T, family string) *declTestRegistry {
	t.Helper()
	r := newBehaviourRegistry(t, family)
	r.deployTo(behaviourEnv)
	return r
}

// deleteVersion removes a version's directory, relative to the registry.
func (r *declTestRegistry) deleteVersion(rel string) {
	r.t.Helper()
	require.NoError(r.t, os.RemoveAll(filepath.Join(r.reg, filepath.FromSlash(rel))))
}

func TestCheckChangesRefusesDeletingVersionWithIndefiniteDocuments(t *testing.T) {
	t.Parallel()

	t.Run("a deployed version may not go", func(t *testing.T) {
		t.Parallel()
		r := deployedFamily(t, "lifecycle/indefinite-documents")
		r.deleteVersion("lifecycle/indefinite-documents/1/0/0")
		out, err := r.checkChangesAfter()
		require.Error(t, err, out)
		assert.Equal(t, ExitCheckFailed, ExitCode(err))
		assert.Contains(t, out, "cannot delete a deployed version")
		assert.Contains(t, out, "documents live indefinitely")
		assert.Contains(t, out, "lifecycle/indefinite-documents/1/0/0")
	})

	t.Run("nor may a whole family go, taking its declaration with it", func(t *testing.T) {
		t.Parallel()
		r := deployedFamily(t, "lifecycle/indefinite-documents")
		r.deleteVersion("lifecycle/indefinite-documents")
		out, err := r.checkChangesAfter()
		require.Error(t, err, out)
		assert.Equal(t, ExitCheckFailed, ExitCode(err))
		assert.Contains(t, out, "cannot delete a deployed version")
	})

	t.Run("where the environment allows schema mutation, it may", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, "lifecycle/indefinite-documents")
		r.write("json-schema-manager-config.yml", "environments:\n  dev:\n    privateUrlRoot: https://d/\n"+
			"    publicUrlRoot: https://d/\n    allowSchemaMutation: true\n  production:\n"+
			"    privateUrlRoot: https://p/\n    publicUrlRoot: https://p/\n    isProduction: true\n")
		r.commit("config")
		r.deployTo("dev")
		r.deleteVersion("lifecycle/indefinite-documents/1/0/0")
		r.commit("change")
		out, err := r.jsm("check-changes", "dev")
		require.NoError(t, err, out)
	})
}

func TestCheckChangesAllowsDeletingVersionWithMomentaryDocuments(t *testing.T) {
	t.Parallel()
	r := deployedFamily(t, "lifecycle/momentary-documents")
	r.deleteVersion("lifecycle/momentary-documents/1/0/0")
	out, err := r.checkChangesAfter()
	require.NoError(t, err, out)
}

func TestCheckChangesAllowsDeletingVersionBelowOldestLive(t *testing.T) {
	t.Parallel()

	t.Run("as oldestLive stood", func(t *testing.T) {
		t.Parallel()
		r := deployedFamily(t, retentionFamily)
		r.deleteVersion(retentionFamily + "/1/0/0")
		out, err := r.checkChangesAfter()
		require.NoError(t, err, out)
	})

	t.Run("with oldestLive moved forward in the same change", func(t *testing.T) {
		t.Parallel()
		r := deployedFamily(t, retentionFamily)
		r.write(retentionYML, "scenario: retained-stream\noldestLive: 1.2.0\n")
		r.deleteVersion(retentionFamily + "/1/1/0")
		out, err := r.checkChangesAfter()
		require.NoError(t, err, out)
	})
}

func TestCheckChangesRefusesDeletingVersionAtOldestLive(t *testing.T) {
	t.Parallel()

	for name, version := range map[string]string{"at it": "1/1/0", "above it": "1/2/0"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := deployedFamily(t, retentionFamily)
			r.deleteVersion(retentionFamily + "/" + version)
			out, err := r.checkChangesAfter()
			require.Error(t, err, out)
			assert.Equal(t, ExitCheckFailed, ExitCode(err))
			assert.Contains(t, out, "cannot delete a deployed version")
			assert.Contains(t, out, "oldestLive")
			assert.Contains(t, out, "1.1.0")
		})
	}

	t.Run("a whole family goes by the oldestLive it was deployed with", func(t *testing.T) {
		t.Parallel()
		r := deployedFamily(t, retentionFamily)
		r.deleteVersion(retentionFamily)
		out, err := r.checkChangesAfter()
		require.Error(t, err, out)
		assert.Equal(t, ExitCheckFailed, ExitCode(err))
		assert.Contains(t, out, "cannot delete a deployed version")
	})
}

func TestCheckChangesAllowsWholeFamilyOfMomentaryDocumentsToGo(t *testing.T) {
	t.Parallel()
	r := deployedFamily(t, "lifecycle/momentary-documents")
	r.deleteVersion("lifecycle/momentary-documents")
	out, err := r.checkChangesAfter()
	require.NoError(t, err, out)
}

func TestOldestLiveLimitsRulePairs(t *testing.T) {
	t.Parallel()

	t.Run("the pair below oldestLive takes no part, however a version is selected", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, retentionFamily)
		for _, target := range []string{retention100, retention110, retention120, retentionFamily, "all"} {
			out, err := r.jsm("validate", target)
			require.NoError(t, err, out)
		}
	})

	t.Run("no run in the report reaches a version below oldestLive", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, retentionFamily)
		out, err := r.jsm("validate", retentionFamily, "-o", "json")
		require.NoError(t, err, out)
		for _, label := range parseJSONReport(t, out).labels() {
			assert.NotRegexp(t, `^1\.[12]\.0 \w+ on 1\.0\.0$`, label)
			assert.NotRegexp(t, `^1\.0\.0 \w+ on 1\.[12]\.0$`, label)
		}
		assert.Contains(t, parseJSONReport(t, out).labels(), "1.1.0 pass on 1.2.0")
		assert.Contains(t, parseJSONReport(t, out).labels(), "1.0.0 pass on 1.0.0", "its own documents still run")
	})

	t.Run("at the oldest version, the pair still runs and fails", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, retentionFamily)
		r.write(retentionYML, "scenario: retained-stream\noldestLive: 1.0.0\n")
		out, err := r.jsm("validate", retention110)
		require.ErrorIs(t, err, ErrTestsFailed, out)
		assert.Contains(t, out, "broken between "+retention100+" and "+retention110)
	})

	t.Run("a version below oldestLive leaves the unexercised comparison", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, retentionFamily)
		out, err := r.jsm("validate", retentionFamily, "--strict")
		require.NoError(t, err, out)
	})

	t.Run("nor does the first live version inherit documents from below it", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, retentionFamily)
		require.NoError(t, os.RemoveAll(filepath.Join(r.reg, retentionFamily, "1/1/0/pass")))
		require.NoError(t, os.RemoveAll(filepath.Join(r.reg, retentionFamily, "1/2/0/pass")))
		out, err := r.jsm("validate", retention110, "--strict")
		require.ErrorIs(t, err, ErrTestsFailed, out)
		assert.Contains(t, out, "pass")
	})

	t.Run("a version which is itself below oldestLive keeps what it inherited", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, retentionFamily)
		r.write(retentionYML, "scenario: retained-stream\noldestLive: 1.2.0\n")
		r.write(retentionFamily+"/1/0/1/lifecycle_retention-documents_1_0_1.schema.json", declTestSchema)
		out, err := r.jsm("validate", "lifecycle_retention-documents_1_0_1", "--strict")
		require.NoError(t, err, out)
	})
}

func TestOldestLiveMustBeAVersionOfTheFamily(t *testing.T) {
	t.Parallel()
	r := newBehaviourRegistry(t, retentionFamily)
	r.write(retentionYML, "scenario: retained-stream\noldestLive: 1.9.0\n")

	for _, args := range [][]string{{"validate", retention110}, {"validate", "all"}, {"check-changes", behaviourEnv}} {
		out, err := r.jsm(args...)
		require.Error(t, err, out)
		assert.Equal(t, ExitCannotRun, ExitCode(err))
		assert.Contains(t, out, "oldestLive 1.9.0")
		assert.Contains(t, out, "not a version of this family")
	}
}

func TestMovingOldestLivePastAnExceptionMakesItStale(t *testing.T) {
	t.Parallel()

	file := retentionFamily + "/1/2/0/" + schema.ExceptionsFile
	entry := "exceptions:\n" + exceptionEntry("1.0.0", "pass/queued.json", "no-tightening", "1.0.0 was wrong.")

	// holding is a registry where the 1.2.0 version lists an exception on 1.0.0, while 1.0.0 is live.
	holding := func(t *testing.T) *declTestRegistry {
		t.Helper()
		r := newBehaviourRegistry(t, retentionFamily)
		r.write(file, entry)
		r.write(retentionYML, "scenario: retained-stream\noldestLive: 1.0.0\n")
		r.commit("deployable")
		r.deployTo(behaviourEnv)
		return r
	}

	t.Run("the entry is stale once its version is below oldestLive", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, retentionFamily)
		r.write(file, entry)
		requireStale(t, r, retention120, "below", "oldestLive")
	})

	t.Run("and may then be removed from a deployed file", func(t *testing.T) {
		t.Parallel()
		r := holding(t)
		r.write(retentionYML, "scenario: retained-stream\noldestLive: 1.1.0\n")
		require.NoError(t, os.Remove(filepath.Join(r.reg, file)))
		out, err := r.checkChangesAfter()
		require.NoError(t, err, out)
	})

	t.Run("but not while its version is live", func(t *testing.T) {
		t.Parallel()
		r := holding(t)
		require.NoError(t, os.Remove(filepath.Join(r.reg, file)))
		out, err := r.checkChangesAfter()
		require.Error(t, err, out)
		assert.Contains(t, out, "cannot change a deployed exception")
	})

	t.Run("a version below oldestLive answers for no entries of its own", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, retentionFamily)
		r.write(retentionYML, "scenario: retained-stream\noldestLive: 1.2.0\n")
		r.write(retentionFamily+"/1/1/0/"+schema.ExceptionsFile, entry)
		out, err := r.jsm("validate", retention110)
		require.NoError(t, err, out)
	})
}

func TestNewMajorInStoredOneSchemaRequiresMigrationStatement(t *testing.T) {
	t.Parallel()

	t.Run("validate fails on the new major, and says what to write", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, noMigrationFamily)
		out, err := r.jsm("validate", noMigration200)
		require.ErrorIs(t, err, ErrTestsFailed, out)
		assert.Equal(t, ExitCheckFailed, ExitCode(err))
		assert.Contains(t, out, "major 2")
		assert.Contains(t, out, "migrations:")
		assert.Contains(t, out, "1 missing migration statement")
	})

	t.Run("the first major needs none, so its versions still pass", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, noMigrationFamily)
		out, err := r.jsm("validate", noMigration100)
		require.NoError(t, err, out)
	})

	t.Run("validating the family reports it once, under the major's first version", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, noMigrationFamily)
		v210 := noMigrationFamily + "/2/1/0/lifecycle_major-without-migration_2_1_0.schema.json"
		r.write(v210, declTestSchema)
		out, err := r.jsm("validate", noMigrationFamily, "-o", "json", "-C")
		require.ErrorIs(t, err, ErrTestsFailed, out)
		var rep struct {
			MissingMigrations []struct {
				Schema schema.Key `json:"schema"`
				Major  uint64     `json:"major"`
				Why    string     `json:"why"`
			} `json:"missingMigrations"`
			Stats struct {
				Total int `json:"totalMissingMigrations"`
			} `json:"stats"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &rep), out)
		require.Len(t, rep.MissingMigrations, 1)
		assert.Equal(t, schema.Key(noMigration200), rep.MissingMigrations[0].Schema)
		assert.Equal(t, uint64(2), rep.MissingMigrations[0].Major)
		assert.Contains(t, rep.MissingMigrations[0].Why, "migrations:")
		assert.Equal(t, 1, rep.Stats.Total)
	})

	t.Run("JUnit reports it as a failing case", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, noMigrationFamily)
		out, err := r.jsm("validate", noMigration200, "-o", "junit")
		require.ErrorIs(t, err, ErrTestsFailed, out)
		assert.Contains(t, out, "migration statement for major 2")
		assert.Contains(t, out, "missing migration statement")
	})

	t.Run("a statement is all it takes", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, noMigrationFamily)
		r.write(noMigrationFamily+"/family.yml", "scenario: stored-one-schema\nmigrations:\n  2: See the runbook.\n")
		out, err := r.jsm("validate", noMigrationFamily)
		require.NoError(t, err, out)
	})

	t.Run("a statement for another major doesn't count", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, noMigrationFamily)
		r.write(noMigrationFamily+"/family.yml", "scenario: stored-one-schema\nmigrations:\n  3: See the runbook.\n")
		out, err := r.jsm("validate", noMigration200)
		require.ErrorIs(t, err, ErrTestsFailed, out)
	})

	t.Run("adding a statement to a deployed family is allowed", func(t *testing.T) {
		t.Parallel()
		r := deployedFamily(t, noMigrationFamily)
		r.write(noMigrationFamily+"/family.yml", "scenario: stored-one-schema\nmigrations:\n  2: See the runbook.\n")
		out, err := r.checkChangesAfter()
		require.NoError(t, err, out)
	})

	t.Run("any family whose documents stay and whose reader doesn't pick needs one", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, noMigrationFamily)
		r.write(noMigrationFamily+"/family.yml", "scenario: third-party-format\ndocumentsLive: indefinitely\n")
		out, err := r.jsm("validate", noMigration200)
		require.ErrorIs(t, err, ErrTestsFailed, out)
	})

	t.Run("create-schema-version says one is needed, on a major bump only", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, noMigrationFamily)
		out, err := r.jsm("create-schema-version", noMigration200, "major")
		require.NoError(t, err, out)
		assert.Contains(t, out, "migrations:")
		assert.Contains(t, out, "3:")

		out, err = r.jsm("create-schema-version", noMigration200, "minor")
		require.NoError(t, err, out)
		assert.NotContains(t, out, "migrations:")
	})
}

func TestNewMajorWithSelfNamingDocumentsNeedsNoMigrationStatement(t *testing.T) {
	t.Parallel()

	t.Run("self-naming documents", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, "lifecycle/major-self-naming")
		out, err := r.jsm("validate", "lifecycle/major-self-naming")
		require.NoError(t, err, out)
	})

	t.Run("documents which don't outlive their writer", func(t *testing.T) {
		t.Parallel()
		r := newBehaviourRegistry(t, noMigrationFamily)
		r.write(noMigrationFamily+"/family.yml", "scenario: consumer-first\n")
		out, err := r.jsm("validate", noMigration200)
		require.NoError(t, err, out)

		out, err = r.jsm("create-schema-version", noMigration200, "major")
		require.NoError(t, err, out)
		assert.NotContains(t, out, "migrations:")
	})
}

func TestDeletedVersionDecisionNeedsTheDeployedDeclaration(t *testing.T) {
	t.Parallel()

	const declared = "scenario: stored-one-schema\n"
	setup := func(t *testing.T) (*CLIManager, *MockGitter, []repo.Change) {
		t.Helper()
		reg := setupTestRegistry(t)
		g := &MockGitter{}
		mgr := NewCLIManager(slog.New(slog.DiscardHandler), reg, schema.NewTester(reg), g, nil, io.Discard)
		path := filepath.Join(reg.RootDirectory(), "domain/gone/1/0/0/domain_gone_1_0_0.schema.json")
		return mgr, g, []repo.Change{{Path: path, IsDeleted: true}}
	}
	listed := func(_ context.Context, _ repo.Revision, dir, _ string) ([]string, error) {
		return []string{filepath.Join(dir, schema.FamilyDeclarationFile)}, nil
	}

	t.Run("a family never declared has nothing to protect", func(t *testing.T) {
		t.Parallel()
		mgr, _, changes := setup(t)
		refused, err := mgr.refusedVersionDeletions(t.Context(), "a", changes)
		require.NoError(t, err)
		assert.Empty(t, refused)
	})

	t.Run("a declaration invalid at the anchor has nothing to protect", func(t *testing.T) {
		t.Parallel()
		mgr, g, changes := setup(t)
		g.ListFilesFunc = listed
		g.ShowFileFunc = func(context.Context, repo.Revision, string) ([]byte, error) {
			return []byte("scenario: nonsense\n"), nil
		}
		refused, err := mgr.refusedVersionDeletions(t.Context(), "a", changes)
		require.NoError(t, err)
		assert.Empty(t, refused)
	})

	t.Run("a valid declaration at the anchor decides, when none is left", func(t *testing.T) {
		t.Parallel()
		mgr, g, changes := setup(t)
		g.ListFilesFunc = listed
		g.ShowFileFunc = func(context.Context, repo.Revision, string) ([]byte, error) {
			return []byte(declared), nil
		}
		refused, err := mgr.refusedVersionDeletions(t.Context(), "a", changes)
		require.NoError(t, err)
		assert.Len(t, refused, 1)
	})

	t.Run("a schema file which isn't at a version's path isn't a version", func(t *testing.T) {
		t.Parallel()
		mgr, _, changes := setup(t)
		for _, path := range []string{
			"stray.schema.json",
			filepath.Join(filepath.Dir(changes[0].Path), "..", "..", "x", "stray.schema.json"),
		} {
			refused, err := mgr.refusedVersionDeletions(t.Context(), "a", []repo.Change{{Path: path, IsDeleted: true}})
			require.NoError(t, err)
			assert.Empty(t, refused)
		}
	})

	t.Run("a change which deletes no schema file is left alone", func(t *testing.T) {
		t.Parallel()
		mgr, _, changes := setup(t)
		refused, err := mgr.refusedVersionDeletions(t.Context(), "a", []repo.Change{
			{Path: changes[0].Path},
			{Path: changes[0].Path + ".txt", IsDeleted: true},
		})
		require.NoError(t, err)
		assert.Empty(t, refused)
	})

	t.Run("failing to list the anchor's files is reported", func(t *testing.T) {
		t.Parallel()
		mgr, g, changes := setup(t)
		g.ListFilesFunc = func(context.Context, repo.Revision, string, string) ([]string, error) {
			return nil, os.ErrPermission
		}
		_, err := mgr.refusedVersionDeletions(t.Context(), "a", changes)
		require.ErrorIs(t, err, os.ErrPermission)
	})

	t.Run("failing to read the anchor's declaration is reported", func(t *testing.T) {
		t.Parallel()
		mgr, g, changes := setup(t)
		g.ListFilesFunc = listed
		g.ShowFileFunc = func(context.Context, repo.Revision, string) ([]byte, error) {
			return nil, os.ErrPermission
		}
		_, err := mgr.refusedVersionDeletions(t.Context(), "a", changes)
		require.ErrorIs(t, err, os.ErrPermission)
	})

	t.Run("the error from either stops check-changes", func(t *testing.T) {
		t.Parallel()
		mgr, g, changes := setup(t)
		g.GetSchemaChangesFunc = func(context.Context, repo.Revision, string, string) ([]repo.Change, error) {
			return changes, nil
		}
		g.ListFilesFunc = func(context.Context, repo.Revision, string, string) ([]string, error) {
			return nil, os.ErrPermission
		}
		require.ErrorIs(t, mgr.CheckChanges(t.Context(), "prod"), os.ErrPermission)
	})
}

func TestCheckPartyOnVersionBelowOldestLive(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T) *declTestRegistry {
		t.Helper()
		r := newBehaviourRegistry(t, retentionFamily)
		r.write("jsm-party.yml", "role: reader\nschemas:\n  - "+retention100+"\n  - "+retention110+"\n")
		return r
	}

	t.Run("it warns, runs nothing for that version, and still checks a live one", func(t *testing.T) {
		t.Parallel()
		r := setup(t)
		out, err := r.jsm("check-party", filepath.Join(r.reg, "jsm-party.yml"))
		require.NoError(t, err, out)
		assert.Regexp(t, `\[WARN\]\S* `+retention100, out)
		assert.Contains(t, out, "below this family's oldestLive 1.1.0")
		assert.Regexp(t, `\[PASS\]\S* `+retention110, out)
	})

	t.Run("--strict fails on it", func(t *testing.T) {
		t.Parallel()
		r := setup(t)
		out, err := r.jsm("check-party", filepath.Join(r.reg, "jsm-party.yml"), "--strict")
		require.Error(t, err, out)
		assert.Equal(t, ExitCheckFailed, ExitCode(err))
	})

	t.Run("JSON says so", func(t *testing.T) {
		t.Parallel()
		r := setup(t)
		out, err := r.jsm("check-party", filepath.Join(r.reg, "jsm-party.yml"), "-o", "json")
		require.NoError(t, err, out)
		assert.Contains(t, out, `"retired"`)
	})
}
