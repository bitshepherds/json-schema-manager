package schema

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/internal/repo"
	"github.com/bitshepherds/json-schema-manager/internal/validator"
)

// mockGitter is a test mock for the repo.Gitter interface.
type mockGitter struct {
	getLatestAnchorFunc  func(ctx context.Context, env config.Env) (repo.Revision, error)
	tagDeploymentFunc    func(ctx context.Context, env config.Env) (string, error)
	getSchemaChangesFunc func(ctx context.Context, anchor repo.Revision, sourceDir, suffix string) ([]repo.Change, error)
}

func (m *mockGitter) GetLatestAnchor(ctx context.Context, env config.Env) (repo.Revision, error) {
	if m.getLatestAnchorFunc != nil {
		return m.getLatestAnchorFunc(ctx, env)
	}
	return "HEAD", nil
}

func (m *mockGitter) TagDeploymentSuccess(ctx context.Context, env config.Env) (string, error) {
	if m.tagDeploymentFunc != nil {
		return m.tagDeploymentFunc(ctx, env)
	}
	return "jsm-deploy/prod/20260130-120000", nil
}

func (m *mockGitter) GetSchemaChanges(
	ctx context.Context,
	anchor repo.Revision,
	sourceDir, suffix string,
) ([]repo.Change, error) {
	if m.getSchemaChangesFunc != nil {
		return m.getSchemaChangesFunc(ctx, anchor, sourceDir, suffix)
	}
	return nil, nil
}

func TestDistBuilder_BuildAll(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistryWithSchema(t)
		cfg, err := reg.Config()
		require.NoError(t, err)

		builder, err := NewFSDistBuilder(context.Background(), reg, cfg, &mockGitter{}, "dist")
		require.NoError(t, err)

		count, err := builder.BuildAll(context.Background(), "production")
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		// Verify subdirectories exist and file is in private (default)
		distDir := filepath.Join(filepath.Dir(reg.RootDirectory()), "dist")
		envDir := filepath.Join(distDir, "production")

		for _, sub := range []string{"public", "private"} {
			_, err = os.Stat(filepath.Join(envDir, sub))
			require.NoError(t, err)
		}

		files, err := os.ReadDir(filepath.Join(envDir, "private"))
		require.NoError(t, err)
		assert.Len(t, files, 1)
	})

	t.Run("context cancellation", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistryWithSchema(t)
		cfg, err := reg.Config()
		require.NoError(t, err)

		builder, err := NewFSDistBuilder(context.Background(), reg, cfg, &mockGitter{}, "dist")
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err = builder.BuildAll(ctx, "production")
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("init error - registry root equals git root", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		// Init git in reg dir
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", dir, "init").Run())

		// Create a config file so NewRegistry succeeds
		cfgData := `environments: {prod: {publicUrlRoot: 'https://p', privateUrlRoot: 'https://pr', isProduction: true}}`
		require.NoError(t, os.WriteFile(filepath.Join(dir, config.JsmRegistryConfigFile), []byte(cfgData), 0o600))

		reg, err := NewRegistry(dir, &mockCompiler{}, fsh.NewPathResolver(), fsh.NewEnvProvider())
		require.NoError(t, err)
		cfg, _ := reg.Config()
		gitter := &mockGitter{}

		_, err = NewFSDistBuilder(context.Background(), reg, cfg, gitter, "dist")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be the same as the git root")
	})

	t.Run("ensureDistDir error in BuildAll", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistryWithSchema(t)
		cfg, err := reg.Config()
		require.NoError(t, err)

		// Create a non-writable sibling directory's parent
		parentDir := filepath.Dir(reg.RootDirectory())
		require.NoError(t, os.Chmod(parentDir, 0o555))
		defer func() { _ = os.Chmod(parentDir, 0o755) }()

		builder, err := NewFSDistBuilder(context.Background(), reg, cfg, &mockGitter{}, "dist")
		require.NoError(t, err)

		_, err = builder.BuildAll(context.Background(), "production")
		require.Error(t, err)
	})

	t.Run("NewSearcher error in BuildAll", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistryWithSchema(t)
		cfg, err := reg.Config()
		require.NoError(t, err)

		builder, err := NewFSDistBuilder(context.Background(), reg, cfg, &mockGitter{}, "dist")
		require.NoError(t, err)

		// Delete the registry root to make NewSearcher fail
		require.NoError(t, os.RemoveAll(reg.rootDirectory))

		_, err = builder.BuildAll(context.Background(), "production")
		require.Error(t, err)
	})

	t.Run("context cancelled in loop", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistryWithSchema(t)
		cfg, err := reg.Config()
		require.NoError(t, err)

		// Add multiple schemas
		for i := 1; i <= 5; i++ {
			k := Key(fmt.Sprintf("domain_test_1_0_%d", i))
			createSchemaFiles(t, reg, schemaMap{k: `{"type": "object"}`})
		}

		builder, err := NewFSDistBuilder(context.Background(), reg, cfg, &mockGitter{}, "dist")
		require.NoError(t, err)
		builder.SetNumWorkers(1)

		ctx, cancel := context.WithCancel(context.Background())

		workerStarted := make(chan struct{})
		proceedWorker := make(chan struct{})

		compiler, ok := reg.compiler.(*mockCompiler)
		require.True(t, ok)
		compiler.CompileFunc = func(_ string) (validator.Validator, error) {
			select {
			case workerStarted <- struct{}{}:
			default:
			}
			<-proceedWorker
			return &mockValidator{}, nil
		}

		// Run BuildAll in a goroutine
		errC := make(chan error, 1)
		go func() {
			_, bErr := builder.BuildAll(ctx, "production")
			errC <- bErr
		}()

		// Wait for first worker to start
		<-workerStarted
		// Now the worker is blocked on proceedWorker.
		// The loop should be blocked on sem <- because numWorkers=1.

		cancel()
		close(proceedWorker)

		err = <-errC
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("searcher error in loop", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistryWithSchema(t)
		cfg, err := reg.Config()
		require.NoError(t, err)

		// Create a non-readable subdirectory to trigger searcher error
		badDir := filepath.Join(reg.rootDirectory, "unreadable")
		require.NoError(t, os.MkdirAll(badDir, 0o000))
		defer func() { _ = os.Chmod(badDir, 0o755) }()

		builder, err := NewFSDistBuilder(context.Background(), reg, cfg, &mockGitter{}, "dist")
		require.NoError(t, err)

		_, err = builder.BuildAll(context.Background(), "production")
		require.Error(t, err)
	})
}

func TestDistBuilder_SetNumWorkers(t *testing.T) {
	t.Parallel()

	builder := &FSDistBuilder{numWorkers: 4}
	builder.SetNumWorkers(8)
	assert.Equal(t, 8, builder.numWorkers)
}

func TestDistBuilder_renderAndWrite(t *testing.T) {
	t.Parallel()

	t.Run("context cancelled", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistryWithSchema(t)
		cfg, err := reg.Config()
		require.NoError(t, err)

		distDir := filepath.Join(filepath.Dir(reg.RootDirectory()), "dist")
		builder := &FSDistBuilder{
			registry: reg,
			config:   cfg,
			distDir:  distDir,
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err = builder.renderAndWrite(ctx, "production", Key("domain_test_1_0_0"))
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("EnvConfig error", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistryWithSchema(t)
		cfg, err := reg.Config()
		require.NoError(t, err)

		distDir := filepath.Join(filepath.Dir(reg.RootDirectory()), "dist")
		builder := &FSDistBuilder{
			registry: reg,
			config:   cfg,
			distDir:  distDir,
		}

		err = builder.renderAndWrite(context.Background(), "invalid", Key("domain_test_1_0_0"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get environment config")
	})

	t.Run("schema not found", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistryWithSchema(t)
		cfg, err := reg.Config()
		require.NoError(t, err)

		distDir := filepath.Join(filepath.Dir(reg.RootDirectory()), "dist")
		builder := &FSDistBuilder{
			registry: reg,
			config:   cfg,
			distDir:  distDir,
		}

		err = builder.renderAndWrite(context.Background(), "production", Key("nonexistent_1_0_0"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get schema")
	})

	t.Run("write error", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistryWithSchema(t)
		cfg, err := reg.Config()
		require.NoError(t, err)

		distDir := filepath.Join(filepath.Dir(reg.RootDirectory()), "dist")
		require.NoError(t, os.MkdirAll(distDir, 0o755))

		envDir := filepath.Join(distDir, "production")
		require.NoError(t, os.MkdirAll(envDir, 0o755))

		builder := &FSDistBuilder{
			registry: reg,
			config:   cfg,
			distDir:  distDir,
		}

		// Make env directory non-writable
		require.NoError(t, os.Chmod(envDir, 0o000))
		defer func() { _ = os.Chmod(envDir, 0o755) }()

		err = builder.renderAndWrite(context.Background(), "production", Key("domain_test_1_0_0"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to write schema")
	})

	t.Run("CoordinateRender error", func(t *testing.T) {
		t.Parallel()

		// Use a compiler that fails
		reg := newTestRegistryWithSchema(t)
		compiler, ok := reg.compiler.(*mockCompiler)
		require.True(t, ok)
		compiler.CompileFunc = func(_ string) (validator.Validator, error) {
			return nil, errors.New("compile error")
		}

		cfg, err := reg.Config()
		require.NoError(t, err)

		distDir := filepath.Join(filepath.Dir(reg.RootDirectory()), "dist")
		builder := &FSDistBuilder{
			registry: reg,
			config:   cfg,
			distDir:  distDir,
		}

		err = builder.renderAndWrite(context.Background(), "production", Key("domain_test_1_0_0"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to render schema")
	})
}

func TestDistBuilder_BuildChanged(t *testing.T) {
	t.Parallel()

	t.Run("success with changes", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistryWithSchema(t)
		cfg, err := reg.Config()
		require.NoError(t, err)

		schemaPath := filepath.Join("domain", "test", "1", "0", "0", "domain_test_1_0_0.schema.json")
		distDir := filepath.Join(filepath.Dir(reg.RootDirectory()), "dist")
		gitter := &mockGitter{
			getSchemaChangesFunc: func(_ context.Context, _ repo.Revision, _, _ string) ([]repo.Change, error) {
				return []repo.Change{
					{Path: filepath.Join(reg.rootDirectory, schemaPath), IsNew: false},
				}, nil
			},
		}
		builder, err := NewFSDistBuilder(context.Background(), reg, cfg, gitter, "dist")
		require.NoError(t, err)

		count, err := builder.BuildChanged(context.Background(), "production", repo.Revision("HEAD"))
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		// Verify file exists in private subdirectory
		envDir := filepath.Join(distDir, "production")
		files, err := os.ReadDir(filepath.Join(envDir, "private"))
		require.NoError(t, err)
		assert.Len(t, files, 1)
	})

	t.Run("no changes", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistryWithSchema(t)
		cfg, err := reg.Config()
		require.NoError(t, err)

		gitter := &mockGitter{
			getSchemaChangesFunc: func(_ context.Context, _ repo.Revision, _, _ string) ([]repo.Change, error) {
				return []repo.Change{}, nil
			},
		}
		builder, err := NewFSDistBuilder(context.Background(), reg, cfg, gitter, "dist")
		require.NoError(t, err)

		count, err := builder.BuildChanged(context.Background(), "production", repo.Revision("HEAD"))
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("skips deleted schemas", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistryWithSchema(t)
		cfg, err := reg.Config()
		require.NoError(t, err)

		schemaPath := filepath.Join("domain", "test", "1", "0", "0", "domain_test_1_0_0.schema.json")
		gitter := &mockGitter{
			getSchemaChangesFunc: func(_ context.Context, _ repo.Revision, _, _ string) ([]repo.Change, error) {
				return []repo.Change{
					{Path: filepath.Join(reg.rootDirectory, schemaPath), IsNew: false},
					{Path: filepath.Join(reg.rootDirectory, "deleted.schema.json"), IsDeleted: true},
				}, nil
			},
		}
		builder, err := NewFSDistBuilder(context.Background(), reg, cfg, gitter, "dist")
		require.NoError(t, err)

		count, err := builder.BuildChanged(context.Background(), "production", repo.Revision("HEAD"))
		require.NoError(t, err)
		assert.Equal(t, 1, count, "deleted schemas should be skipped")
	})

	t.Run("GetSchemaChanges error", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistryWithSchema(t)
		cfg, err := reg.Config()
		require.NoError(t, err)

		gitter := &mockGitter{
			getSchemaChangesFunc: func(_ context.Context, _ repo.Revision, _, _ string) ([]repo.Change, error) {
				return nil, errors.New("git failed")
			},
		}
		builder, err := NewFSDistBuilder(context.Background(), reg, cfg, gitter, "dist")
		require.NoError(t, err)

		_, err = builder.BuildChanged(context.Background(), "production", repo.Revision("HEAD"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "git failed")
	})

	t.Run("skip invalid paths", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistryWithSchema(t)
		cfg, err := reg.Config()
		require.NoError(t, err)

		schemaPath := filepath.Join("domain", "test", "1", "0", "0", "domain_test_1_0_0.schema.json")
		gitter := &mockGitter{
			getSchemaChangesFunc: func(_ context.Context, _ repo.Revision, _, _ string) ([]repo.Change, error) {
				return []repo.Change{
					{Path: "invalid/path.schema.json", IsNew: false},
					{Path: filepath.Join(reg.rootDirectory, schemaPath), IsNew: false},
				}, nil
			},
		}
		builder, err := NewFSDistBuilder(context.Background(), reg, cfg, gitter, "dist")
		require.NoError(t, err)

		count, err := builder.BuildChanged(context.Background(), "production", repo.Revision("HEAD"))
		require.NoError(t, err)
		assert.Equal(t, 1, count, "should skip invalid paths and process valid ones")
	})

	t.Run("context cancelled mid-loop", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistryWithSchema(t)
		cfg, err := reg.Config()
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		var changes []repo.Change
		for i := 0; i < 500; i++ {
			schemaPath := fmt.Sprintf("schema%d.json", i)
			changes = append(changes, repo.Change{
				Path:  filepath.Join(reg.rootDirectory, schemaPath),
				IsNew: true,
			})
		}

		returned := make(chan struct{})
		gitter := &mockGitter{
			getSchemaChangesFunc: func(_ context.Context, _ repo.Revision, _, _ string) ([]repo.Change, error) {
				close(returned)
				return changes, nil
			},
		}
		builder, err := NewFSDistBuilder(context.Background(), reg, cfg, gitter, "dist")
		require.NoError(t, err)
		builder.SetNumWorkers(1)

		go func() {
			<-returned
			cancel()
		}()

		_, err = builder.BuildChanged(ctx, "production", repo.Revision("HEAD"))
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("worker result error in BuildChanged", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistryWithSchema(t)
		cfg, err := reg.Config()
		require.NoError(t, err)

		schemaPath := filepath.Join("domain", "test", "1", "0", "0", "domain_test_1_0_0.schema.json")
		gitter := &mockGitter{
			getSchemaChangesFunc: func(_ context.Context, _ repo.Revision, _, _ string) ([]repo.Change, error) {
				return []repo.Change{
					{Path: filepath.Join(reg.rootDirectory, schemaPath), IsNew: false},
				}, nil
			},
		}
		builder, err := NewFSDistBuilder(context.Background(), reg, cfg, gitter, "dist")
		require.NoError(t, err)
		builder.SetNumWorkers(1)

		compiler, ok := reg.compiler.(*mockCompiler)
		require.True(t, ok)
		compiler.CompileFunc = func(_ string) (validator.Validator, error) {
			return nil, errors.New("worker failed")
		}

		_, err = builder.BuildChanged(context.Background(), "production", repo.Revision("HEAD"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "worker failed")
	})

	t.Run("ensureDistDir error in BuildChanged", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistryWithSchema(t)
		cfg, err := reg.Config()
		require.NoError(t, err)

		// Create a non-writable sibling directory's parent
		parentDir := filepath.Dir(reg.RootDirectory())
		require.NoError(t, os.Chmod(parentDir, 0o555))
		defer func() { _ = os.Chmod(parentDir, 0o755) }()

		builder, err := NewFSDistBuilder(context.Background(), reg, cfg, &mockGitter{}, "dist")
		require.NoError(t, err)

		_, err = builder.BuildChanged(context.Background(), "production", repo.Revision("HEAD"))
		require.Error(t, err)
	})
}

func TestDistBuilder_BuildAll_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("renderAndWrite error in worker", func(t *testing.T) {
		t.Parallel()

		// Build registry with our custom compiler
		regDir := t.TempDir()
		envDir := filepath.Join(filepath.Dir(regDir), "dist", "production")

		require.NoError(t, os.WriteFile(filepath.Join(regDir, "json-schema-manager-config.yml"), []byte(`
schemaStoreBaseURI: https://example.com
environments:
  production:
    isProduction: true
    allowSchemaMutation: false
    publicUrlRoot: https://example.com
    privateUrlRoot: https://example.com
`), 0o600))

		schemaDir := filepath.Join(regDir, "domain", "test", "1", "0", "0")
		require.NoError(t, os.MkdirAll(schemaDir, 0o755))
		schemaFile := filepath.Join(schemaDir, "domain_test_1_0_0.schema.json")
		idContent := []byte(`{"$id": "https://example.com/domain/test/1/0/0/domain_test_1_0_0.schema.json"}`)
		require.NoError(t, os.WriteFile(schemaFile, idContent, 0o600))

		// Create a compiler that makes the private distDir read-only when called
		compiler := &mockCompiler{
			CompileFunc: func(_ string) (validator.Validator, error) {
				_ = os.Chmod(filepath.Join(envDir, "private"), 0o555)
				return &mockValidator{}, nil
			},
		}

		reg, err := NewRegistry(regDir, compiler, fsh.NewPathResolver(), fsh.NewEnvProvider())
		require.NoError(t, err)

		cfg, err := reg.Config()
		require.NoError(t, err)

		builder, err := NewFSDistBuilder(context.Background(), reg, cfg, &mockGitter{}, "dist")
		require.NoError(t, err)
		builder.SetNumWorkers(1)

		// Pre-create so we can Chmod it
		require.NoError(t, os.MkdirAll(filepath.Join(envDir, "private"), 0o755))
		defer func() { _ = os.Chmod(filepath.Join(envDir, "private"), 0o755) }()

		_, err = builder.BuildAll(context.Background(), "production")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to write schema")
	})

	t.Run("worker result cancellation in BuildAll", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistryWithSchema(t)
		cfg, err := reg.Config()
		require.NoError(t, err)

		// Many schemas
		schemaDir := filepath.Join(reg.rootDirectory, "domain", "test", "1", "0", "0")
		for i := 1; i <= 50; i++ {
			filename := fmt.Sprintf("domain_test_1_0_%d.schema.json", i)
			require.NoError(t, os.WriteFile(filepath.Join(schemaDir, filename), []byte(`{"type": "object"}`), 0o600))
		}

		builder, err := NewFSDistBuilder(context.Background(), reg, cfg, &mockGitter{}, "dist")
		require.NoError(t, err)
		builder.SetNumWorkers(1)

		ctx, cancel := context.WithCancel(context.Background())
		// Cancel mid-processing
		go func() {
			time.Sleep(2 * time.Millisecond)
			cancel()
		}()

		_, err = builder.BuildAll(ctx, "production")
		_ = err
	})
}

func TestDistBuilder_ensureDistDir(t *testing.T) {
	t.Parallel()

	t.Run("creates directory", func(t *testing.T) {
		t.Parallel()

		regDir := t.TempDir()
		distDir := filepath.Join(regDir, "dist")
		builder := &FSDistBuilder{distDir: distDir}

		err := builder.ensureDistDir("production")
		require.NoError(t, err)

		// Verify subdirectories exist
		envDir := filepath.Join(distDir, "production")
		for _, sub := range []string{"public", "private"} {
			sInfo, sErr := os.Stat(filepath.Join(envDir, sub))
			require.NoError(t, sErr)
			assert.True(t, sInfo.IsDir())
		}
	})

	t.Run("cleans environment-specific directory", func(t *testing.T) {
		t.Parallel()

		regDir := t.TempDir()
		distDir := filepath.Join(regDir, "dist")
		envDir := filepath.Join(distDir, "production")
		otherDir := filepath.Join(distDir, "staging")

		require.NoError(t, os.MkdirAll(envDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(envDir, "old.json"), []byte("{}"), 0o600))

		require.NoError(t, os.MkdirAll(otherDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(otherDir, "keep.json"), []byte("{}"), 0o600))

		builder := &FSDistBuilder{distDir: distDir}
		err := builder.ensureDistDir("production")
		require.NoError(t, err)

		// Env subdirs should be empty
		for _, sub := range []string{"public", "private"} {
			sFiles, sErr := os.ReadDir(filepath.Join(envDir, sub))
			require.NoError(t, sErr)
			assert.Empty(t, sFiles)
		}

		// Other dir should be intact
		files, err := os.ReadDir(otherDir)
		require.NoError(t, err)
		assert.Len(t, files, 1)
	})

	t.Run("MkdirAll baseDistDir error", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		baseFile := filepath.Join(tmpDir, "dist-file")
		require.NoError(t, os.WriteFile(baseFile, []byte("not a dir"), 0o600))

		// Under dist-file, we can't create anything
		distDir := filepath.Join(baseFile, "sub")
		builder := &FSDistBuilder{distDir: distDir}
		err := builder.ensureDistDir("production")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create base dist directory")
	})

	t.Run("RemoveAll error", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		distDir := filepath.Join(tmpDir, "dist")
		envName := "production"
		envDir := filepath.Join(distDir, envName)

		require.NoError(t, os.MkdirAll(envDir, 0o755))
		// Make distDir non-writable, so we can't remove envDir
		require.NoError(t, os.Chmod(distDir, 0o000))
		defer func() { _ = os.Chmod(distDir, 0o755) }()

		builder := &FSDistBuilder{distDir: distDir}
		err := builder.ensureDistDir(config.Env(envName))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to clean environment dist directory")
	})

	t.Run("MkdirAll envDir error", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		distDir := filepath.Join(tmpDir, "dist")
		require.NoError(t, os.MkdirAll(distDir, 0o755))

		// Make distDir non-writable
		require.NoError(t, os.Chmod(distDir, 0o555))
		defer func() { _ = os.Chmod(distDir, 0o755) }()

		builder := &FSDistBuilder{distDir: distDir}
		// envDir DOES NOT EXIST, so RemoveAll succeeds, but MkdirAll fails
		err := builder.ensureDistDir("production")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create environment dist subdirectory")
	})
}

func TestDistDirectory(t *testing.T) {
	t.Parallel()

	t.Run("returns sibling when not at git root", func(t *testing.T) {
		t.Parallel()
		// Create a mock git repo structure
		tmpDir := t.TempDir()
		gitRoot := filepath.Join(tmpDir, "project")
		require.NoError(t, os.MkdirAll(gitRoot, 0o755))
		gitRoot, _ = filepath.EvalSymlinks(gitRoot)

		// Init git
		cmd := exec.CommandContext(context.Background(), "git", "-C", gitRoot, "init")
		require.NoError(t, cmd.Run())

		regRoot := filepath.Join(gitRoot, "schemas")
		require.NoError(t, os.MkdirAll(regRoot, 0o755))

		pathResolver := fsh.NewPathResolver()
		dist, err := distDirectory(context.Background(), pathResolver, regRoot, "dist")
		require.NoError(t, err)

		expected := filepath.Join(gitRoot, "dist")
		assert.Equal(t, expected, dist)
	})

	t.Run("errors when registry is at git root", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		gitRoot := filepath.Join(tmpDir, "project")
		require.NoError(t, os.MkdirAll(gitRoot, 0o755))
		gitRoot, _ = filepath.EvalSymlinks(gitRoot)

		// Init git
		cmd := exec.CommandContext(context.Background(), "git", "-C", gitRoot, "init")
		require.NoError(t, cmd.Run())

		pathResolver := fsh.NewPathResolver()
		_, err := distDirectory(context.Background(), pathResolver, gitRoot, "dist")
		require.Error(t, err)
		var rootErr *RegistryRootAtGitRootError
		require.ErrorAs(t, err, &rootErr)
		assert.Contains(t, err.Error(), "registry root cannot be the same as the git root")
	})

	t.Run("returns sibling when not in a git repo", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		regRoot := filepath.Join(tmpDir, "registry")
		require.NoError(t, os.MkdirAll(regRoot, 0o755))
		regRoot, _ = filepath.EvalSymlinks(regRoot)

		pathResolver := fsh.NewPathResolver()
		dist, err := distDirectory(context.Background(), pathResolver, regRoot, "dist")
		require.NoError(t, err)

		expected := filepath.Join(filepath.Dir(regRoot), "dist")
		assert.Equal(t, expected, dist)
	})

	t.Run("returns error when canonicalPath fails", func(t *testing.T) {
		t.Parallel()

		mockResolver := &mockPathResolver{
			canonicalPathFn: func(_ string) (string, error) {
				return "", fmt.Errorf("canonical error")
			},
		}

		tmpDir := t.TempDir()
		gitRoot := filepath.Join(tmpDir, "project")
		require.NoError(t, os.MkdirAll(gitRoot, 0o755))

		// Init git so distDirectory reaches the canonicalPath call
		cmd := exec.CommandContext(context.Background(), "git", "-C", gitRoot, "init")
		require.NoError(t, cmd.Run())

		_, err := distDirectory(context.Background(), mockResolver, gitRoot, "dist")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "canonical error")
	})
}

func newTestRegistryWithSchema(t *testing.T) *Registry {
	t.Helper()
	dir := t.TempDir()

	configContent := `
schemaStoreBaseURI: https://example.com
environments:
  production:
    isProduction: true
    allowSchemaMutation: false
    publicUrlRoot: https://example.com
    privateUrlRoot: https://example.com
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "json-schema-manager-config.yml"), []byte(configContent), 0o600))

	schemaDir := filepath.Join(dir, "domain", "test", "1", "0", "0")
	require.NoError(t, os.MkdirAll(schemaDir, 0o755))
	schemaFile := filepath.Join(schemaDir, "domain_test_1_0_0.schema.json")
	require.NoError(t, os.WriteFile(schemaFile, []byte(`{"type": "object"}`), 0o600))

	reg, err := NewRegistry(dir, &mockCompiler{}, fsh.NewPathResolver(), fsh.NewEnvProvider())
	require.NoError(t, err)
	return reg
}

func TestDistBuilder_PublicPrivate(t *testing.T) {
	t.Parallel()

	regDir := t.TempDir()
	// distDirectory returns a sibling of the registry root
	distDir := filepath.Join(filepath.Dir(regDir), "dist")

	configContent := `
schemaStoreBaseURI: https://example.com
environments:
  production:
    isProduction: true
    allowSchemaMutation: false
    publicUrlRoot: https://example.com/public
    privateUrlRoot: https://example.com/private
`
	require.NoError(
		t,
		os.WriteFile(filepath.Join(regDir, "json-schema-manager-config.yml"), []byte(configContent), 0o600),
	)

	// Create a public schema
	publicDir := filepath.Join(regDir, "domain", "public", "1", "0", "0")
	require.NoError(t, os.MkdirAll(publicDir, 0o755))
	publicContent := []byte(`{"x-public": true}`)
	require.NoError(t, os.WriteFile(filepath.Join(publicDir, "domain_public_1_0_0.schema.json"), publicContent, 0o600))

	// Create a private schema
	privateDir := filepath.Join(regDir, "domain", "private", "1", "0", "0")
	require.NoError(t, os.MkdirAll(privateDir, 0o755))
	privateContent := []byte(`{"type": "object"}`)
	require.NoError(
		t,
		os.WriteFile(filepath.Join(privateDir, "domain_private_1_0_0.schema.json"), privateContent, 0o600),
	)

	reg, err := NewRegistry(regDir, &mockCompiler{}, fsh.NewPathResolver(), fsh.NewEnvProvider())
	require.NoError(t, err)
	cfg, err := reg.Config()
	require.NoError(t, err)

	builder, err := NewFSDistBuilder(context.Background(), reg, cfg, &mockGitter{}, "dist")
	require.NoError(t, err)

	count, err := builder.BuildAll(context.Background(), "production")
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	envDir := filepath.Join(distDir, "production")

	// Verify public schema is in public
	publicFiles, err := os.ReadDir(filepath.Join(envDir, "public"))
	require.NoError(t, err)
	assert.Len(t, publicFiles, 1)
	assert.Equal(t, "domain_public_1_0_0.schema.json", publicFiles[0].Name())

	// Verify private schema is in private
	privateFiles, err := os.ReadDir(filepath.Join(envDir, "private"))
	require.NoError(t, err)
	assert.Len(t, privateFiles, 1)
	assert.Equal(t, "domain_private_1_0_0.schema.json", privateFiles[0].Name())
}
