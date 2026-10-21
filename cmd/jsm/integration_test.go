// Package main provides integration tests for the jsm CLI.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/app"
	"github.com/bitshepherds/json-schema-manager/internal/config"
)

var binaryPath string

var (
	errBuild  error
	buildOnce sync.Once
)

func ensureBinary() error {
	buildOnce.Do(func() {
		// Build the binary once for all legacy tests
		tmpDir, err := os.MkdirTemp("", "jsm-integration-test-*")
		if err != nil {
			errBuild = fmt.Errorf("failed to create temp dir: %w", err)
			return
		}

		binaryName := "jsm"
		if runtime.GOOS == "windows" {
			binaryName += ".exe"
		}
		binaryPath = filepath.Join(tmpDir, binaryName)

		// Build the binary from the root of the project
		cmd := exec.CommandContext(context.Background(), "go", "build", "-o", binaryPath, ".")
		if bOutput, bErr := cmd.CombinedOutput(); bErr != nil {
			errBuild = fmt.Errorf("failed to build binary: %w\nOutput: %s", bErr, string(bOutput))
		}
	})
	return errBuild
}

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"jsm": func() {
			ctx := context.Background()
			if err := app.Run(ctx, os.Args, os.Stdout, os.Stderr, nil); err != nil {
				os.Exit(1)
			}
		},
	})
}

func TestScripts(t *testing.T) {
	t.Parallel()
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
	})
}

func setupIntegrationRegistry(t *testing.T) string {
	t.Helper()
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
	return regDir
}

func TestBinary_Help(t *testing.T) {
	t.Parallel()
	if err := ensureBinary(); err != nil {
		t.Fatal(err)
	}
	regDir := setupIntegrationRegistry(t)
	cmd := exec.CommandContext(context.Background(), binaryPath, "--help")
	cmd.Env = append(os.Environ(), "JSM_REGISTRY_ROOT_DIR="+regDir)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	require.NoError(t, err, "stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "jsm is a CLI tool for developing and testing JSON Schemas")
}

func TestBinary_Validate(t *testing.T) {
	t.Parallel()
	if err := ensureBinary(); err != nil {
		t.Fatal(err)
	}
	regDir := setupIntegrationRegistry(t)

	// Create a valid schema file
	schemaPath := filepath.Join(regDir, "domain", "family", "1", "0", "0")
	err := os.MkdirAll(schemaPath, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	schemaFile := filepath.Join(schemaPath, "domain_family_1_0_0.schema.json")
	err = os.WriteFile(schemaFile, []byte(`{"type": "object"}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	// Create pass dir to avoid "no test files" error if we were running full tests,
	// but ValidateSingleSchema just needs the file.
	err = os.MkdirAll(filepath.Join(schemaPath, "pass"), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.MkdirAll(filepath.Join(schemaPath, "fail"), 0o755)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("valid schema", func(t *testing.T) {
		t.Parallel()
		cmd := exec.CommandContext(context.Background(), binaryPath, "validate", schemaFile)
		cmd.Env = append(os.Environ(), "JSM_REGISTRY_ROOT_DIR="+regDir)

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		runErr := cmd.Run()
		require.NoError(t, runErr, "stderr: %s", stderr.String())
		assert.Contains(t, stdout.String(), "TEST REPORT")
	})

	t.Run("invalid schema command", func(t *testing.T) {
		t.Parallel()
		cmd := exec.CommandContext(context.Background(), binaryPath, "validate", "/non/existent/path")
		cmd.Env = append(os.Environ(), "JSM_REGISTRY_ROOT_DIR="+regDir)

		errVal := cmd.Run()
		assert.Error(t, errVal)
	})
}

func TestBinary_ValidateFailedTestsExitCode(t *testing.T) {
	t.Parallel()
	if err := ensureBinary(); err != nil {
		t.Fatal(err)
	}
	regDir := setupIntegrationRegistry(t)

	schemaPath := filepath.Join(regDir, "domain", "family", "1", "0", "0")
	require.NoError(t, os.MkdirAll(filepath.Join(schemaPath, "pass"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(schemaPath, "fail"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(schemaPath, "domain_family_1_0_0.schema.json"),
		[]byte(`{"type": "object"}`), 0o600))
	// Valid against the schema, so this fail test fails
	require.NoError(t, os.WriteFile(filepath.Join(schemaPath, "fail", "accepted.json"), []byte(`{}`), 0o600))

	for _, format := range []string{"text", "json"} {
		cmd := exec.CommandContext(context.Background(), binaryPath, "validate", "domain_family_1_0_0", "-o", format)
		cmd.Env = append(os.Environ(), "JSM_REGISTRY_ROOT_DIR="+regDir)

		var stdout bytes.Buffer
		cmd.Stdout = &stdout

		runErr := cmd.Run()
		var exitErr *exec.ExitError
		require.ErrorAs(t, runErr, &exitErr, "%s output: %s", format, stdout.String())
		assert.Equal(t, 1, exitErr.ExitCode(), format)
		assert.Contains(t, stdout.String(), "accepted.json", format)
	}
}
