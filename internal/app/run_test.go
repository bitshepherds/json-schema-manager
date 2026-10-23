package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

func TestRun(t *testing.T) {
	t.Parallel()

	// Setup a temporary registry
	regDir := t.TempDir()
	cfgData := `
environments:
  prod:
    privateUrlRoot: "https://json-schemas.internal.myorg.io/"
    publicUrlRoot: "https://json-schemas.myorg.io/"
    isProduction: true
`
	if err := os.WriteFile(
		filepath.Join(regDir, config.JsmRegistryConfigFile),
		[]byte(cfgData),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	t.Run("run help", func(t *testing.T) {
		t.Parallel()
		env := &mockEnvProvider{values: map[string]string{
			"JSM_REGISTRY_ROOT_DIR": regDir,
		}}
		err := Run(context.Background(), []string{"jsm", "--help"}, io.Discard, io.Discard, env)
		require.NoError(t, err)
	})

	t.Run("run invalid command", func(t *testing.T) {
		t.Parallel()
		env := &mockEnvProvider{values: map[string]string{
			"JSM_REGISTRY_ROOT_DIR": regDir,
		}}
		err := Run(context.Background(), []string{"jsm", "invalid-command"}, io.Discard, io.Discard, env)
		require.Error(t, err)
	})

	t.Run("run registry error", func(t *testing.T) {
		t.Parallel()
		env := &mockEnvProvider{values: map[string]string{
			"JSM_REGISTRY_ROOT_DIR": "/non/existent/path",
		}}
		err := Run(context.Background(), []string{"jsm", "validate", "some.schema.json"}, io.Discard, io.Discard, env)
		require.Error(t, err)
	})

	t.Run("run setupLogger error", func(t *testing.T) {
		t.Parallel()
		// Set log file to a directory to cause OpenFile to fail
		env := &mockEnvProvider{values: map[string]string{
			"JSM_REGISTRY_ROOT_DIR": regDir,
			"JSM_LOG_FILE":          regDir,
		}}

		// Use a command that triggers initialization
		err := Run(context.Background(), []string{"jsm", "validate", "missing_1_0_0"}, io.Discard, io.Discard, env)
		require.Error(t, err)
	})

	t.Run("run discovery failure", func(t *testing.T) {
		t.Parallel()
		// Registry root directory not set and not in a registry directory
		env := &mockEnvProvider{values: map[string]string{
			"JSM_REGISTRY_ROOT_DIR": "",
		}}
		// The tests run in this package's directory, which is not a registry

		err := Run(context.Background(), []string{"jsm", "validate", "some_schema_1_0_0"}, io.Discard, io.Discard, env)
		require.ErrorContains(t, err, "no registry found")
	})

	t.Run("run command execution error", func(t *testing.T) {
		t.Parallel()
		env := &mockEnvProvider{values: map[string]string{
			"JSM_REGISTRY_ROOT_DIR": regDir,
		}}
		err := Run(context.Background(), []string{"jsm", "validate", "missing_1_0_0"}, io.Discard, io.Discard, env)
		require.Error(t, err)
	})

	t.Run("run with debug flag", func(t *testing.T) {
		t.Parallel()
		env := &mockEnvProvider{values: map[string]string{
			"JSM_REGISTRY_ROOT_DIR": regDir,
		}}
		err := Run(
			context.Background(),
			[]string{"jsm", "--debug", "validate", "missing_1_0_0"},
			io.Discard,
			io.Discard,
			env,
		)
		require.Error(t, err)
	})

	t.Run("registry at git root: only build-dist fails", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		gitRoot := filepath.Join(tmpDir, "project")
		require.NoError(t, os.MkdirAll(gitRoot, 0o755))

		cmd := exec.CommandContext(context.Background(), "git", "init", gitRoot)
		require.NoError(t, cmd.Run())

		require.NoError(t, os.WriteFile(
			filepath.Join(gitRoot, config.JsmRegistryConfigFile),
			[]byte(cfgData),
			0o600,
		))
		env := &mockEnvProvider{values: map[string]string{
			"JSM_REGISTRY_ROOT_DIR": gitRoot,
		}}

		// Commands which don't build a distribution work
		err := Run(context.Background(), []string{"jsm", "validate", "all"}, io.Discard, io.Discard, env)
		require.NoError(t, err)

		err = Run(context.Background(), []string{"jsm", "build-dist", "prod", "--all"}, io.Discard, io.Discard, env)
		var rootErr *schema.RegistryRootAtGitRootError
		require.ErrorAs(t, err, &rootErr)
		assert.Equal(t, 2, ExitCode(err))
	})

	t.Run("run with nil env", func(t *testing.T) {
		t.Parallel()
		// If we pass nil, Run should create its own EnvProvider.
		// However, it will then try to discover the registry in the current CWD.
		// We'll just check that it doesn't panic and returns some error (since CWD likely isn't a registry root).
		var stdout, stderr bytes.Buffer
		err := Run(context.Background(), []string{"jsm", "--help"}, &stdout, &stderr, nil)
		require.NoError(t, err)
		assert.Contains(t, stdout.String(), "jsm is a CLI tool")
	})

	t.Run("run validate with failed tests", func(t *testing.T) {
		t.Parallel()
		regDir := t.TempDir()
		cfgData := `environments: {prod: {publicUrlRoot: 'https://p', privateUrlRoot: 'https://pr', isProduction: true}}`
		require.NoError(t, os.WriteFile(filepath.Join(regDir, config.JsmRegistryConfigFile), []byte(cfgData), 0o600))
		env := &mockEnvProvider{values: map[string]string{
			"JSM_REGISTRY_ROOT_DIR": regDir,
		}}

		homeDir := filepath.Join(regDir, "domain", "family", "1", "0", "0")
		require.NoError(t, os.MkdirAll(filepath.Join(homeDir, "pass"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(homeDir, "fail"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(homeDir, "domain_family_1_0_0.schema.json"),
			[]byte(`{"type":"object"}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(homeDir, "fail", "accepted.json"), []byte(`{}`), 0o600))

		var stdout, stderr bytes.Buffer
		err := Run(context.Background(), []string{"jsm", "validate", "domain_family_1_0_0"}, &stdout, &stderr, env)

		require.ErrorIs(t, err, ErrTestsFailed)
		assert.Contains(t, stdout.String(), "1 failed")
		assert.NotContains(t, stderr.String(), "Error:")
	})

	t.Run("run interrupted by user", func(t *testing.T) {
		t.Parallel()
		regDir := t.TempDir()
		cfgData := `environments: {prod: {publicUrlRoot: 'https://p', privateUrlRoot: 'https://pr', isProduction: true}}`
		require.NoError(t, os.WriteFile(filepath.Join(regDir, config.JsmRegistryConfigFile), []byte(cfgData), 0o600))
		env := &mockEnvProvider{values: map[string]string{
			"JSM_REGISTRY_ROOT_DIR": regDir,
		}}

		// Create a dummy schema so validate --watch doesn't fail immediately
		require.NoError(t, os.MkdirAll(filepath.Join(regDir, "domain", "family", "1", "0", "0"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(regDir, "domain", "family", "1", "0", "0",
			"domain_family_1_0_0.schema.json"), []byte(`{"type":"object"}`), 0o600))

		for _, tt := range []struct {
			cause    error
			wantMsg  string
			wantExit int
		}{
			{nil, "Interrupted by user", ExitInterrupted},
			{&SignalError{Signal: os.Interrupt}, "Interrupted by user", ExitInterrupted},
			{&SignalError{Signal: syscall.SIGTERM}, "Terminated", ExitTerminated},
		} {
			ctx, cancel := context.WithCancelCause(context.Background())

			var stderr bytes.Buffer
			done := make(chan error, 1)
			go func() {
				done <- Run(ctx, []string{"jsm", "validate", "domain_family_1_0_0", "--watch"}, io.Discard, &stderr, env)
			}()

			// Wait a bit for it to start watching
			time.Sleep(500 * time.Millisecond)
			cancel(tt.cause)
			err := <-done

			require.Error(t, err)
			assert.Equal(t, tt.wantExit, ExitCode(err), "a stopped run does not exit 0")
			assert.Contains(t, stderr.String(), tt.wantMsg, "Stderr was: %q, Err was: %v", stderr.String(), err)
		}
	})
}

func TestExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"success", nil, ExitOK},
		{"tests failed", ErrTestsFailed, ExitCheckFailed},
		{"wrapped tests failed", fmt.Errorf("validate: %w", ErrTestsFailed), ExitCheckFailed},
		{"deployed schemas changed", &schema.ChangedDeployedSchemasError{Paths: []string{"a"}}, ExitCheckFailed},
		{"setup error", &schema.NoSchemaTargetsError{}, ExitCannotRun},
		{"other error", errors.New("boom"), ExitCannotRun},
		{"cancelled", context.Canceled, ExitInterrupted},
		{"SIGINT", &SignalError{Signal: os.Interrupt}, ExitInterrupted},
		{"SIGTERM", fmt.Errorf("run: %w", &SignalError{Signal: syscall.SIGTERM}), ExitTerminated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ExitCode(tt.err))
		})
	}
}
