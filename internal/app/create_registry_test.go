package app

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/v2/internal/config"
	"github.com/bitshepherds/json-schema-manager/v2/internal/fsh"
)

func TestNewCreateRegistryCmd(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T) *cobra.Command {
		t.Helper()
		pathResolver := fsh.NewPathResolver()
		cmd := NewCreateRegistryCmd(pathResolver)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		return cmd
	}

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		registryDir := filepath.Join(tmpDir, "my-registry")

		cmd := setup(t)
		cmd.SetArgs([]string{registryDir})

		err := cmd.Execute()
		require.NoError(t, err)

		// Verify directory exists
		info, err := os.Stat(registryDir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())

		// Verify config file exists
		configPath := filepath.Join(registryDir, config.JsmRegistryConfigFile)
		data, err := os.ReadFile(configPath)
		require.NoError(t, err)
		assert.Equal(t, config.DefaultConfigContent, string(data))
	})

	t.Run("error - config file already exists", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, config.JsmRegistryConfigFile)
		err := os.WriteFile(configPath, []byte("existing"), 0o600)
		require.NoError(t, err)

		cmd := setup(t)
		cmd.SetArgs([]string{tmpDir})

		err = cmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "registry already exists")
	})

	t.Run("error - cannot create directory", func(t *testing.T) {
		t.Parallel()
		// On most systems, creating a directory in a non-existent path without MkdirAll -p equivalent would fail,
		// but MkdirAll handles it. So we need a real permission error or similar.
		// A simple way is to use a file where a directory should be.
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "some-file")
		err := os.WriteFile(filePath, []byte("not-a-dir"), 0o600)
		require.NoError(t, err)

		badDir := filepath.Join(filePath, "nested")

		cmd := setup(t)
		cmd.SetArgs([]string{badDir})

		err = cmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create directory")
	})

	t.Run("error - missing argument", func(t *testing.T) {
		t.Parallel()
		cmd := setup(t)
		cmd.SetArgs([]string{})

		// Cobra will handle this and return an error before RunE
		err := cmd.Execute()
		require.Error(t, err)
	})

	t.Run("error - failed to write config file", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		registryDir := filepath.Join(tmpDir, "readonly-dir")
		err := os.Mkdir(registryDir, 0o555) // Read and execute but no write
		require.NoError(t, err)

		// Ensure cleanup if it fails (t.TempDir usually handles this but good to be safe)
		defer func() {
			_ = os.Chmod(registryDir, 0o755)
		}()

		cmd := setup(t)
		cmd.SetArgs([]string{registryDir})

		err = cmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to write configuration file")
	})
}

// TestRegistration verifies the command is registered in RootCmd.
func TestRootCmd_CreateRegistryRegistration(t *testing.T) {
	t.Parallel()
	lazy := &LazyManager{}
	ll := &slog.LevelVar{}
	rootCmd := NewRootCmd(lazy, ll, os.Stdout, os.Stderr, fsh.NewEnvProvider())

	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == CreateRegistryCmdName {
			found = true
			break
		}
	}
	assert.True(t, found, CreateRegistryCmdName+" command should be registered")
}

// TestPersistentPreRunE_CreateRegistry_SkipsInitialisation verifies that create-registry skips registry init.
func TestPersistentPreRunE_CreateRegistry_SkipsInitialisation(t *testing.T) {
	t.Parallel()
	lazy := &LazyManager{}
	ll := &slog.LevelVar{}
	rootCmd := NewRootCmd(lazy, ll, os.Stdout, os.Stderr, fsh.NewEnvProvider())

	// Find the create-registry command
	var createRegistryCmd *cobra.Command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == CreateRegistryCmdName {
			createRegistryCmd = cmd
			break
		}
	}
	assert.NotNil(t, createRegistryCmd)

	// Call PersistentPreRunE
	if rootCmd.PersistentPreRunE != nil {
		err := rootCmd.PersistentPreRunE(createRegistryCmd, []string{"some-path"})
		require.NoError(t, err)
	}

	// Verify that registry was NOT initialised (lazy manager remains empty)
	assert.False(t, lazy.HasInner())
}

func TestCreateRegistry_NoGlobalExportAdvice(t *testing.T) {
	t.Parallel()
	// The advice no longer depends on the OS: there is no shell profile or setx to name. So one
	// run covers darwin, linux and windows alike.
	registryDir := filepath.Join(t.TempDir(), "my-registry")
	var out bytes.Buffer
	cmd := NewCreateRegistryCmd(fsh.NewPathResolver())
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{registryDir})
	require.NoError(t, cmd.Execute())

	got := out.String()
	for _, global := range []string{".zshrc", ".bashrc", ".profile", "setx", "export "} {
		assert.NotContains(t, got, global)
	}
	assert.Contains(t, got, "cd "+registryDir, "advises running jsm from the registry's folder")
	assert.Contains(t, got, "-r "+registryDir, "advises giving the registry with -r")
	assert.Contains(t, got, "JSM_REGISTRY_ROOT_DIR")
	assert.Contains(t, got, "mise")
	assert.Contains(t, got, "direnv")
}

func TestRegistryUsageAdvice_AbsError(t *testing.T) {
	t.Parallel()
	dir := "/tmp/bg-registry"
	mockResolver := &mockPathResolver{
		absFn: func(_ string) (string, error) {
			return "", errors.New("mock-error")
		},
	}

	got := registryUsageAdvice(mockResolver, dir)
	assert.Contains(t, got, dir)
}

// mockPathResolver is a test implementation of fsh.PathResolver.
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
