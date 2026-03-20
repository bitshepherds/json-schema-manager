package repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/fsh"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	git := func(args ...string) {
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}

	git("init")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test User")
	git("commit", "--allow-empty", "-m", "initial commit")

	return dir
}

func newTestConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		Environments: map[config.Env]*config.EnvConfig{
			"prod": {
				Env:                 "prod",
				IsProduction:        true,
				AllowSchemaMutation: false,
				PublicURLRoot:       "https://example.com",
				PrivateURLRoot:      "https://example.com",
			},
		},
	}
}

func TestCLIGitter_GetLatestAnchor(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	pathResolver := fsh.NewPathResolver()

	t.Run("no tags found - returns root commit", func(t *testing.T) {
		t.Parallel()
		tmpDir := setupTestRepo(t)
		g := NewCLIGitter(cfg, pathResolver, tmpDir)

		anchor, err := g.GetLatestAnchor(context.Background(), "prod")
		require.NoError(t, err)

		// Get root commit hash to compare
		revCmd := exec.CommandContext(context.Background(), "git", "rev-list", "--max-parents=0", "HEAD")
		revCmd.Dir = tmpDir
		revOut, err := revCmd.Output()
		require.NoError(t, err)
		expected := Revision(strings.TrimSpace(string(revOut)))

		assert.Equal(t, expected, anchor)
	})

	t.Run("tag found", func(t *testing.T) {
		t.Parallel()
		tmpDir := setupTestRepo(t)
		g := NewCLIGitter(cfg, pathResolver, tmpDir)

		// Create a tag
		cmd := exec.CommandContext(context.Background(), "git", "tag", "jsm-deploy/prod/v1")
		cmd.Dir = tmpDir
		require.NoError(t, cmd.Run())

		anchor, err := g.GetLatestAnchor(context.Background(), "prod")
		require.NoError(t, err)
		assert.Equal(t, Revision("jsm-deploy/prod/v1"), anchor)
	})

	t.Run("error - invalid env", func(t *testing.T) {
		t.Parallel()
		g := NewCLIGitter(cfg, pathResolver, "")
		_, err := g.GetLatestAnchor(context.Background(), "invalid-env")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not define environment")
	})

	t.Run("error - not a git repo", func(t *testing.T) {
		t.Parallel()
		emptyDir := t.TempDir()
		gEmpty := NewCLIGitter(cfg, pathResolver, emptyDir)

		_, err := gEmpty.GetLatestAnchor(context.Background(), "prod")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "could not find git history")
	})
}

func TestCLIGitter_GetSchemaChanges(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	pathResolver := fsh.NewPathResolver()

	t.Run("modified file", func(t *testing.T) {
		t.Parallel()
		tmpDir := setupTestRepo(t)
		g := NewCLIGitter(cfg, pathResolver, tmpDir)

		srcDir := filepath.Join(tmpDir, "src", "schemas")
		require.NoError(t, os.MkdirAll(srcDir, 0o755))
		f1 := filepath.Join(srcDir, "user.schema.json")
		require.NoError(t, os.WriteFile(f1, []byte("{}"), 0o600))

		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", tmpDir, "add", ".").Run())
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", tmpDir, "commit", "-m", "1").Run())
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", tmpDir, "tag", "jsm-deploy/prod/v1").Run(),
		)

		require.NoError(t, os.WriteFile(f1, []byte(`{"type": "object"}`), 0o600))
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", tmpDir, "add", ".").Run())
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", tmpDir, "commit", "-m", "2").Run())

		changes, err := g.GetSchemaChanges(context.Background(), "jsm-deploy/prod/v1", srcDir, ".schema.json")
		require.NoError(t, err)
		require.Len(t, changes, 1)

		expected, _ := filepath.EvalSymlinks(f1)
		actual, _ := filepath.EvalSymlinks(changes[0].Path)
		assert.Equal(t, expected, actual)
		assert.False(t, changes[0].IsNew)
	})

	t.Run("new file", func(t *testing.T) {
		t.Parallel()
		tmpDir := setupTestRepo(t)
		g := NewCLIGitter(cfg, pathResolver, tmpDir)

		srcDir := filepath.Join(tmpDir, "src", "schemas")
		require.NoError(t, os.MkdirAll(srcDir, 0o755))
		f1 := filepath.Join(srcDir, "user.schema.json")
		require.NoError(t, os.WriteFile(f1, []byte("{}"), 0o600))

		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", tmpDir, "add", ".").Run())
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", tmpDir, "commit", "-m", "1").Run())
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", tmpDir, "tag", "jsm-deploy/prod/v1").Run(),
		)

		f2 := filepath.Join(srcDir, "product.schema.json")
		require.NoError(t, os.WriteFile(f2, []byte("{}"), 0o600))
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", tmpDir, "add", ".").Run())
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", tmpDir, "commit", "-m", "2").Run())

		changes, err := g.GetSchemaChanges(context.Background(), "jsm-deploy/prod/v1", srcDir, ".schema.json")
		require.NoError(t, err)
		require.Len(t, changes, 1) // Only f2 is changed relative to tag

		expected, _ := filepath.EvalSymlinks(f2)
		actual, _ := filepath.EvalSymlinks(changes[0].Path)
		assert.Equal(t, expected, actual)
		assert.True(t, changes[0].IsNew)
	})

	t.Run("deleted file", func(t *testing.T) {
		t.Parallel()
		tmpDir := setupTestRepo(t)
		g := NewCLIGitter(cfg, pathResolver, tmpDir)

		srcDir := filepath.Join(tmpDir, "src", "schemas")
		require.NoError(t, os.MkdirAll(srcDir, 0o755))
		f1 := filepath.Join(srcDir, "user.schema.json")
		require.NoError(t, os.WriteFile(f1, []byte("{}"), 0o600))

		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", tmpDir, "add", ".").Run())
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", tmpDir, "commit", "-m", "1").Run())
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", tmpDir, "tag", "jsm-deploy/prod/v1").Run(),
		)

		// Delete the schema file via git rm
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", tmpDir, "rm", f1).Run())
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", tmpDir, "commit", "-m", "delete").Run(),
		)

		changes, err := g.GetSchemaChanges(context.Background(), "jsm-deploy/prod/v1", srcDir, ".schema.json")
		require.NoError(t, err)
		require.Len(t, changes, 1)

		assert.True(t, changes[0].IsDeleted)
		assert.False(t, changes[0].IsNew)
	})

	t.Run("ignore non-schema files", func(t *testing.T) {
		t.Parallel()
		tmpDir := setupTestRepo(t)
		g := NewCLIGitter(cfg, pathResolver, tmpDir)

		srcDir := filepath.Join(tmpDir, "src", "schemas")
		require.NoError(t, os.MkdirAll(srcDir, 0o755))
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", tmpDir, "tag", "jsm-deploy/prod/v1").Run(),
		)

		f1 := filepath.Join(srcDir, "README.md")
		require.NoError(t, os.WriteFile(f1, []byte("docs"), 0o600))
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", tmpDir, "add", ".").Run())
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", tmpDir, "commit", "-m", "1").Run())

		changes, err := g.GetSchemaChanges(context.Background(), "jsm-deploy/prod/v1", srcDir, ".schema.json")
		require.NoError(t, err)
		assert.Empty(t, changes)
	})

	t.Run("git diff error", func(t *testing.T) {
		t.Parallel()
		tmpDir := setupTestRepo(t)
		g := NewCLIGitter(cfg, pathResolver, tmpDir)
		_, err := g.GetSchemaChanges(context.Background(), Revision("invalid-anchor"), ".", ".schema.json")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "git diff failed")
	})

	t.Run("no changes", func(t *testing.T) {
		t.Parallel()
		tmpDir := setupTestRepo(t)
		g := NewCLIGitter(cfg, pathResolver, tmpDir)
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", tmpDir, "tag", "jsm-deploy/prod/v1").Run(),
		)

		changes, err := g.GetSchemaChanges(context.Background(), Revision("jsm-deploy/prod/v1"), ".", ".schema.json")
		require.NoError(t, err)
		assert.Empty(t, changes)
	})

	t.Run("absPath error", func(t *testing.T) {
		t.Parallel()
		mockResolver := &mockPathResolver{
			absFn: func(_ string) (string, error) {
				return "", errors.New("absPath failure")
			},
		}
		gWithMock := NewCLIGitter(cfg, mockResolver, "")

		_, err := gWithMock.GetSchemaChanges(context.Background(), Revision("HEAD"), "some/path", ".schema.json")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "absPath failure")
	})

	t.Run("subdirectory path resolution", func(t *testing.T) {
		t.Parallel()
		repoDir := setupTestRepo(t)
		registryDir := filepath.Join(repoDir, "my-registry")
		require.NoError(t, os.MkdirAll(registryDir, 0o755))

		anchorOut, err := exec.CommandContext(context.Background(), "git", "-C", repoDir, "rev-parse", "HEAD").Output()
		require.NoError(t, err)
		initialAnchor := Revision(strings.TrimSpace(string(anchorOut)))

		schemaFile := filepath.Join(registryDir, "test.schema.json")
		require.NoError(t, os.WriteFile(schemaFile, []byte("{}"), 0o600))
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", repoDir, "add", ".").Run())
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", repoDir, "commit", "-m", "add schema").Run(),
		)

		g := NewCLIGitter(cfg, pathResolver, registryDir)
		changes, err := g.GetSchemaChanges(context.Background(), initialAnchor, ".", ".schema.json")
		require.NoError(t, err)

		require.Len(t, changes, 1)
		_, statErr := os.Stat(changes[0].Path)
		assert.NoError(t, statErr)
	})

	t.Run("getGitRoot error", func(t *testing.T) {
		t.Parallel()
		emptyDir := t.TempDir()
		g := NewCLIGitter(cfg, pathResolver, emptyDir)

		_, err := g.GetSchemaChanges(context.Background(), Revision("HEAD"), ".", ".schema.json")
		require.Error(t, err)
	})
}

func TestCLIGitter_getGitRoot(t *testing.T) {
	t.Parallel()
	t.Run("success", func(t *testing.T) {
		t.Parallel()
		repoDir := setupTestRepo(t)
		g := NewCLIGitter(newTestConfig(t), fsh.NewPathResolver(), repoDir)
		root, err := g.getGitRoot(context.Background())
		require.NoError(t, err)

		expected, _ := filepath.EvalSymlinks(repoDir)
		actual, _ := filepath.EvalSymlinks(root)
		assert.Equal(t, expected, actual)
	})

	t.Run("error - not a git repo", func(t *testing.T) {
		t.Parallel()
		emptyDir := t.TempDir()
		g := NewCLIGitter(newTestConfig(t), fsh.NewPathResolver(), emptyDir)
		_, err := g.getGitRoot(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to find git root")
	})
}

func TestCLIGitter_TagDeploymentSuccess(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	pathResolver := fsh.NewPathResolver()

	t.Run("success without remote", func(t *testing.T) {
		t.Parallel()
		tmpDir := setupTestRepo(t)
		g := NewCLIGitter(cfg, pathResolver, tmpDir)

		tagName, err := g.TagDeploymentSuccess(context.Background(), "prod")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to push git tag to origin")
		assert.NotEmpty(t, tagName)

		cmd := exec.CommandContext(context.Background(), "git", "rev-parse", tagName)
		cmd.Dir = tmpDir
		require.NoError(t, cmd.Run())
	})

	t.Run("success with remote", func(t *testing.T) {
		t.Parallel()
		remoteDir := t.TempDir()
		require.NoError(t, exec.CommandContext(context.Background(), "git", "init", "--bare", remoteDir).Run())

		repoDir := t.TempDir()
		require.NoError(t, exec.CommandContext(context.Background(), "git", "init", repoDir).Run())
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", repoDir, "config", "user.email", "t@t.com").Run(),
		)
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", repoDir, "config", "user.name", "t").Run(),
		)
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", repoDir, "commit", "--allow-empty", "-m", "init").
				Run(),
		)
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", repoDir, "remote", "add", "origin", remoteDir).Run(),
		)

		g := NewCLIGitter(cfg, pathResolver, repoDir)
		tagName, err := g.TagDeploymentSuccess(context.Background(), "prod")
		require.NoError(t, err)

		cmd := exec.CommandContext(context.Background(), "git", "-C", remoteDir, "rev-parse", tagName)
		require.NoError(t, cmd.Run())
	})

	t.Run("tag failure", func(t *testing.T) {
		t.Parallel()
		tmpDir := setupTestRepo(t)
		binDir := filepath.Join(t.TempDir(), "bin")
		require.NoError(t, os.MkdirAll(binDir, 0o755))

		realGit, _ := exec.LookPath("git")
		gitScript := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "tag" ]; then exit 1; fi
exec %s "$@"
`, realGit)
		gitPath := filepath.Join(binDir, "git")

		require.NoError(t, os.WriteFile(gitPath, []byte(gitScript), 0o755))

		g := NewCLIGitter(cfg, pathResolver, tmpDir)
		g.SetGitBinary(gitPath)

		_, err := g.TagDeploymentSuccess(context.Background(), "prod")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create git tag")
	})

	t.Run("error - invalid env", func(t *testing.T) {
		t.Parallel()
		g := NewCLIGitter(cfg, pathResolver, "")
		_, err := g.TagDeploymentSuccess(context.Background(), "invalid-env")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not define environment")
	})
}

func TestCLIGitter_tagPrefix(t *testing.T) {
	t.Parallel()
	g := NewCLIGitter(newTestConfig(t), fsh.NewPathResolver(), "")
	assert.Equal(t, "jsm-deploy/prod", g.tagPrefix("prod"))
}

type mockPathResolver struct {
	canonicalPathFn       func(path string) (string, error)
	absFn                 func(path string) (string, error)
	getUintSubdirectories func(dirPath string) ([]uint64, error)
}

func (m *mockPathResolver) CanonicalPath(path string) (string, error) {
	if m.canonicalPathFn != nil {
		return m.canonicalPathFn(path)
	}
	return fsh.NewPathResolver().CanonicalPath(path)
}

func (m *mockPathResolver) Abs(path string) (string, error) {
	if m.absFn != nil {
		return m.absFn(path)
	}
	return filepath.Abs(path)
}

func (m *mockPathResolver) GetUintSubdirectories(dirPath string) ([]uint64, error) {
	if m.getUintSubdirectories != nil {
		return m.getUintSubdirectories(dirPath)
	}
	return fsh.GetUintSubdirectories(dirPath)
}
