package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/v2/internal/config"
	"github.com/bitshepherds/json-schema-manager/v2/internal/repo"
	"github.com/bitshepherds/json-schema-manager/v2/internal/schema"
)

const (
	declTestSchema = `{"$id": "{{ ID }}", "$schema": "http://json-schema.org/draft-07/schema#", "type": "object"}`
	declTestKey    = "domain_family_1_0_0"
	declTestLater  = "domain_family_1_1_0"
)

// declTestRegistry is a registry in a git repository, with one family of two versions and no
// declaration, committed, and a party manifest listing the earlier version.
type declTestRegistry struct {
	t    *testing.T
	repo string
	reg  string
	log  string
}

func newDeclTestRegistry(t *testing.T) *declTestRegistry {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	r := &declTestRegistry{
		t:    t,
		repo: repo,
		reg:  filepath.Join(repo, "reg"),
		log:  filepath.Join(t.TempDir(), "jsm.log"),
	}
	r.write(config.JsmRegistryConfigFile, testConfig)
	for _, v := range []string{"1/0/0", "1/1/0"} {
		key := "domain_family_" + strings.ReplaceAll(v, "/", "_")
		r.write(filepath.Join("domain/family", v, key+".schema.json"), declTestSchema)
		require.NoError(t, os.MkdirAll(filepath.Join(r.reg, "domain/family", v, "pass"), 0o750))
		require.NoError(t, os.MkdirAll(filepath.Join(r.reg, "domain/family", v, "fail"), 0o750))
	}
	r.write("jsm-party.yml", "role: reader\nschemas:\n  - "+declTestKey+"\n")
	r.git("init", "-q")
	r.commit("first")
	return r
}

func (r *declTestRegistry) write(rel, content string) {
	r.t.Helper()
	p := filepath.Join(r.reg, rel)
	require.NoError(r.t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(r.t, os.WriteFile(p, []byte(content), 0o600))
}

func (r *declTestRegistry) git(args ...string) {
	r.t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = r.repo
	out, err := cmd.CombinedOutput()
	require.NoError(r.t, err, "git %v: %s", args, out)
}

func (r *declTestRegistry) commit(msg string) {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "--allow-empty", "-m", msg)
}

// deploy records everything committed so far as deployed to prod.
func (r *declTestRegistry) deploy() {
	r.t.Helper()
	r.git("tag", "-a", "jsm-deploy/prod/20260101-000000", "-m", "deployed")
}

// jsm runs the whole CLI, as the binary would, and returns everything it wrote.
func (r *declTestRegistry) jsm(args ...string) (string, error) {
	r.t.Helper()
	var out bytes.Buffer
	env := &mockEnvProvider{values: map[string]string{LogEnvVar: r.log}}
	full := append([]string{"jsm", "--registry", r.reg}, args...)
	err := Run(context.Background(), full, &out, &out, env)
	return out.String(), err
}

func TestCommandsFailOnFamilyWithoutDeclaration(t *testing.T) {
	t.Parallel()

	commands := map[string][]string{
		"validate by key":       {"validate", declTestKey},
		"validate all":          {"validate", "all"},
		"validate by scope":     {"validate", "domain/family"},
		"build-dist all":        {"build-dist", "prod", "--all"},
		"build-dist skip tests": {"build-dist", "prod", "--all", "--skip-tests"},
		"build-dist changed":    {"build-dist", "prod"},
		"check-party":           {"check-party"},
		"diff":                  {"diff", declTestKey, declTestLater},
		"create-schema-version": {"create-schema-version", declTestKey, "minor"},
		"check-changes":         {"check-changes", "prod"},
	}

	for name, args := range commands {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newDeclTestRegistry(t)
			if args[0] == "check-party" {
				args = []string{"check-party", filepath.Join(r.reg, "jsm-party.yml")}
			}

			out, err := r.jsm(args...)

			require.Error(t, err, out)
			assert.Equal(t, ExitCannotRun, ExitCode(err))
			assert.Contains(t, out, filepath.Join("domain", "family", "family.yml"))
			assert.Contains(t, out, "scenario:", "the message says how to declare it")
			assert.Contains(t, out, "scenarios.md", "the message says where to read about it")
		})
	}
}

func TestCommandsThatDoNotNeedDeclarationStillRun(t *testing.T) {
	t.Parallel()

	t.Run("render-schema", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		out, err := r.jsm("render-schema", declTestKey)
		require.NoError(t, err, out)
		assert.Contains(t, out, declTestKey)
	})

	t.Run("tag-deployment", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		out, err := r.jsm("tag-deployment", "prod", "--no-push")
		require.NoError(t, err, out)
	})

	t.Run("create-registry", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		out, err := r.jsm("create-registry", filepath.Join(t.TempDir(), "another"))
		require.NoError(t, err, out)
	})
}

// declare commits a declaration of the family, and deploys it with the family's versions.
func (r *declTestRegistry) declareAndDeploy(content string) {
	r.t.Helper()
	r.write("domain/family/family.yml", content)
	r.commit("declare")
	r.deploy()
}

func (r *declTestRegistry) refusedByCheckChanges(want string) {
	r.t.Helper()
	r.commit("change")
	out, err := r.jsm("check-changes", "prod")
	require.Error(r.t, err, out)
	assert.Equal(r.t, ExitCheckFailed, ExitCode(err))
	assert.Contains(r.t, out, "cannot change the declaration of a deployed family")
	assert.Contains(r.t, out, want)
}

func (r *declTestRegistry) allowedByCheckChanges() {
	r.t.Helper()
	r.commit("change")
	out, err := r.jsm("check-changes", "prod")
	require.NoError(r.t, err, out)
}

func TestCheckChangesRefusesDeclarationChangeOnDeployedFamily(t *testing.T) {
	t.Parallel()

	t.Run("a changed scenario", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		r.declareAndDeploy("scenario: owner-writes\n")
		r.write("domain/family/family.yml", "scenario: owner-reads\n")
		r.refusedByCheckChanges("a changed scenario means a new family")
	})

	t.Run("an overridden fact", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		r.declareAndDeploy("scenario: owner-writes\n")
		r.write("domain/family/family.yml", "scenario: owner-writes\nahead: either\n")
		r.refusedByCheckChanges("a changed scenario means a new family")
	})

	t.Run("a removed declaration, while the family has versions", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		r.declareAndDeploy("scenario: owner-writes\n")
		require.NoError(t, os.Remove(filepath.Join(r.reg, "domain/family/family.yml")))
		r.refusedByCheckChanges("cannot be removed")
	})

	t.Run("a deployed version of any age counts, even one deleted since", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		r.declareAndDeploy("scenario: owner-writes\n")
		require.NoError(t, os.RemoveAll(filepath.Join(r.reg, "domain/family/1/0/0")))
		r.write("domain/family/family.yml", "scenario: lock-step\n")
		r.refusedByCheckChanges("a changed scenario means a new family")
	})

	t.Run("an environment which allows mutation may change it", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		r.write(config.JsmRegistryConfigFile, testConfig+`
  dev:
    privateUrlRoot: "https://dev.internal.myorg.io/"
    publicUrlRoot: "https://dev.myorg.io/"
    allowSchemaMutation: true
`)
		r.write("domain/family/family.yml", "scenario: owner-writes\n")
		r.commit("declare")
		r.git("tag", "-a", "jsm-deploy/dev/20260101-000000", "-m", "deployed")
		r.write("domain/family/family.yml", "scenario: owner-reads\n")
		r.commit("change")
		out, err := r.jsm("check-changes", "dev")
		require.NoError(t, err, out)
	})
}

func TestCheckChangesAllowsMovingOldestLiveForward(t *testing.T) {
	t.Parallel()

	r := newDeclTestRegistry(t)
	r.declareAndDeploy("scenario: retained-stream\noldestLive: 1.0.0\n")
	r.write("domain/family/family.yml", "scenario: retained-stream\noldestLive: 1.1.0\n")
	r.allowedByCheckChanges()
}

func TestCheckChangesRefusesMovingOldestLiveBack(t *testing.T) {
	t.Parallel()

	r := newDeclTestRegistry(t)
	r.declareAndDeploy("scenario: retained-stream\noldestLive: 1.1.0\n")
	r.write("domain/family/family.yml", "scenario: retained-stream\noldestLive: 1.0.0\n")
	r.refusedByCheckChanges("oldestLive cannot move back, from 1.1.0 to 1.0.0")
}

func TestCheckChangesAllowsWhatLeavesADeployedDeclarationAlone(t *testing.T) {
	t.Parallel()

	t.Run("declaring a family deployed before it had a declaration", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		r.deploy()
		r.write("domain/family/family.yml", "scenario: owner-writes\n")
		r.allowedByCheckChanges()
	})

	t.Run("a comment, or stating what the scenario implies", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		r.declareAndDeploy("scenario: owner-writes\n")
		r.write("domain/family/family.yml", "# A comment\nscenario: owner-writes\nahead: writer\n")
		r.allowedByCheckChanges()
	})

	t.Run("a family with no deployed version", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		r.deploy()
		r.write("domain/new/family.yml", "scenario: owner-writes\n")
		r.write("domain/new/1/0/0/domain_new_1_0_0.schema.json", declTestSchema)
		r.write("domain/family/family.yml", "scenario: lock-step\n")
		r.commit("declare both, undeployed")
		r.git("tag", "-a", "jsm-deploy/prod/20260102-000000", "-m", "deployed")
		r.write("domain/stray/family.yml", "scenario: owner-writes\n")
		r.commit("a declaration whose family has no version")
		r.git("tag", "-a", "jsm-deploy/prod/20260103-000000", "-m", "deployed")
		r.write("domain/stray/family.yml", "scenario: lock-step\n")
		r.allowedByCheckChanges()
	})

	t.Run("deleting a whole family", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		r.declareAndDeploy("scenario: owner-writes\n")
		require.NoError(t, os.RemoveAll(filepath.Join(r.reg, "domain")))
		r.allowedByCheckChanges()
	})

	t.Run("replacing a deployed declaration which was invalid", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		r.declareAndDeploy("scenario: nonsense\n")
		r.write("domain/family/family.yml", "scenario: owner-writes\n")
		r.allowedByCheckChanges()
	})

	t.Run("an invalid new declaration is reported as invalid", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		r.declareAndDeploy("scenario: owner-writes\n")
		r.write("domain/family/family.yml", "scenario: nonsense\n")
		r.commit("change")
		out, err := r.jsm("check-changes", "prod")
		require.Error(t, err, out)
		assert.Equal(t, ExitCannotRun, ExitCode(err))
		assert.Contains(t, out, "is invalid")
	})
}

func TestCheckChangesReportsGitFailuresReadingADeclaration(t *testing.T) {
	t.Parallel()

	changed := func(reg *schema.Registry) []repo.Change {
		return []repo.Change{{Path: reg.FamilyDeclarationPath("domain_family_1_0_0")}}
	}
	setup := func(t *testing.T) (*schema.Registry, *MockGitter) {
		t.Helper()
		reg := setupTestRegistry(t)
		path := reg.FamilyDeclarationPath("domain_family_1_0_0")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte("scenario: lock-step"), 0o600))
		g := &MockGitter{
			GetSchemaChangesFunc: func(context.Context, repo.Revision, string, string) ([]repo.Change, error) {
				return changed(reg), nil
			},
		}
		return reg, g
	}
	check := func(reg *schema.Registry, g *MockGitter) error {
		mgr := NewCLIManager(slog.New(slog.DiscardHandler), reg, schema.NewTester(reg), g, nil, io.Discard)
		return mgr.CheckChanges(context.Background(), "prod")
	}

	t.Run("listing the family at the anchor", func(t *testing.T) {
		t.Parallel()
		reg, g := setup(t)
		g.ListFilesFunc = func(context.Context, repo.Revision, string, string) ([]string, error) {
			return nil, errors.New("ls-tree went wrong")
		}
		require.ErrorContains(t, check(reg, g), "ls-tree went wrong")
	})

	t.Run("reading the declaration at the anchor", func(t *testing.T) {
		t.Parallel()
		reg, g := setup(t)
		g.ListFilesFunc = func(_ context.Context, _ repo.Revision, dir, _ string) ([]string, error) {
			return []string{filepath.Join(dir, "1", "0", "0", "domain_family_1_0_0.schema.json")}, nil
		}
		g.ShowFileFunc = func(context.Context, repo.Revision, string) ([]byte, error) {
			return nil, errors.New("show went wrong")
		}
		require.ErrorContains(t, check(reg, g), "show went wrong")
	})
}

func TestWatchRequiresDeclarations(t *testing.T) {
	t.Parallel()

	key := schema.Key(declTestKey)

	setup := func(t *testing.T) (*CLIManager, *bytes.Buffer) {
		t.Helper()
		reg := setupTestRegistry(t)
		s := schema.New(key, reg)
		require.NoError(t, os.MkdirAll(s.Path(schema.HomeDir), 0o750))
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte(declTestSchema), 0o600))
		var logs bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logs, nil))
		return NewCLIManager(logger, reg, schema.NewTester(reg), &MockGitter{}, nil, io.Discard), &logs
	}

	t.Run("won't start on an undeclared family", func(t *testing.T) {
		t.Parallel()
		mgr, _ := setup(t)
		err := mgr.WatchValidation(context.Background(), schema.ResolvedTarget{Key: &key}, false, "text", false, false,
			false, nil)
		var missing *schema.MissingFamilyDeclarationError
		require.ErrorAs(t, err, &missing)
	})

	t.Run("logs an event on an undeclared family, rather than testing it", func(t *testing.T) {
		t.Parallel()
		mgr, logs := setup(t)
		mgr.handleWatchEvent(context.Background(), schema.WatchEvent{Key: key}, schema.ResolvedTarget{Key: &key},
			false, "text", false, false, false)
		assert.Contains(t, logs.String(), "does not declare how it is used")
	})
}

func TestValidateWithNoTargetStillSaysSo(t *testing.T) {
	t.Parallel()

	reg := setupTestRegistry(t)
	mgr := NewCLIManager(slog.New(slog.DiscardHandler), reg, schema.NewTester(reg), &MockGitter{}, nil, io.Discard)
	err := mgr.ValidateSchema(context.Background(), schema.ResolvedTarget{}, false, "text", false, false,
		false)
	var none *schema.NoSchemaTargetsError
	require.ErrorAs(t, err, &none)
}
