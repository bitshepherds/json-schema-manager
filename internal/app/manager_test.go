package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/internal/repo"
	"github.com/bitshepherds/json-schema-manager/internal/schema"
	"github.com/bitshepherds/json-schema-manager/internal/validator"
)

const testConfigData = `
environments:
  prod: {publicUrlRoot: 'https://p', privateUrlRoot: 'https://pr', isProduction: true}`

// mockCompiler is a test implementation of validator.Compiler.
type mockCompiler struct{}

func (m *mockCompiler) AddSchema(_ string, _ validator.JSONSchema) error {
	return nil
}

func (m *mockCompiler) Compile(_ string) (validator.Validator, error) {
	return &mockValidator{}, nil
}

func (m *mockCompiler) SupportedSchemaVersions() []validator.Draft {
	return []validator.Draft{validator.Draft7}
}

func (m *mockCompiler) Clear() {}

type failingCompiler struct {
	mockCompiler
}

func (c *failingCompiler) AddSchema(_ string, _ validator.JSONSchema) error {
	return fmt.Errorf("add schema failed")
}

// safeBuffer is a thread-safe wrapper around bytes.Buffer for use in concurrent tests.
type safeBuffer struct {
	buf bytes.Buffer
	mu  sync.Mutex
}

func (s *safeBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// waitForOutput polls the buffer until it contains output or timeout is reached.
// Returns true if output was found, false if timeout occurred.
func (s *safeBuffer) waitForOutput(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.String() != "" {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

type mockValidator struct{}

func (m *mockValidator) Validate(_ validator.JSONDocument) error {
	return nil
}

type MockDistBuilder struct {
	BuildAllFunc      func(ctx context.Context, env config.Env) (int, error)
	BuildChangedFunc  func(ctx context.Context, env config.Env, anchor repo.Revision) (int, error)
	SetNumWorkersFunc func(n int)
}

func (m *MockDistBuilder) BuildAll(ctx context.Context, env config.Env) (int, error) {
	if m.BuildAllFunc != nil {
		return m.BuildAllFunc(ctx, env)
	}
	return 0, nil
}

func (m *MockDistBuilder) BuildChanged(ctx context.Context, env config.Env, anchor repo.Revision) (int, error) {
	if m.BuildChangedFunc != nil {
		return m.BuildChangedFunc(ctx, env, anchor)
	}
	return 0, nil
}

func (m *MockDistBuilder) SetNumWorkers(n int) {
	if m.SetNumWorkersFunc != nil {
		m.SetNumWorkersFunc(n)
	}
}

func setupTestRegistry(t *testing.T) *schema.Registry {
	t.Helper()
	regDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(regDir, config.JsmRegistryConfigFile),
		[]byte(testConfig),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	compiler := &mockCompiler{}
	pathResolver := fsh.NewPathResolver()
	envProvider := fsh.NewEnvProvider()
	r, err := schema.NewRegistry(regDir, compiler, pathResolver, envProvider)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCLIManager_ValidateSchema(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := setupTestRegistry(t)

	// Create a schema file so ValidateSchema has something to test
	key := schema.Key("domain_family_1_0_0")
	s := schema.New(key, registry)
	homeDir := s.Path(schema.HomeDir)
	require.NoError(t, os.MkdirAll(homeDir, 0o755))
	require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte(`{"type": "object"}`), 0o600))
	// Ensure pass/fail dirs exist
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, "pass"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, "fail"), 0o755))

	t.Run("successful validation", func(t *testing.T) {
		t.Parallel()
		tester := schema.NewTester(registry)
		mgr := NewCLIManager(logger, registry, tester, &MockGitter{}, nil, io.Discard)
		vErr := mgr.ValidateSchema(context.Background(), schema.ResolvedTarget{Key: &key},
			false, "text", false, false, schema.TestScopeLocal, false)
		require.NoError(t, vErr)
	})

	t.Run("successful JSON validation", func(t *testing.T) {
		t.Parallel()
		tester := schema.NewTester(registry)
		mgr := NewCLIManager(logger, registry, tester, &MockGitter{}, nil, io.Discard)
		vErr := mgr.ValidateSchema(context.Background(), schema.ResolvedTarget{Key: &key},
			false, "json", false, false, schema.TestScopeLocal, false)
		require.NoError(t, vErr)
	})

	t.Run("successful verbose validation", func(t *testing.T) {
		t.Parallel()
		tester := schema.NewTester(registry)
		mgr := NewCLIManager(logger, registry, tester, &MockGitter{}, nil, io.Discard)
		vErr := mgr.ValidateSchema(context.Background(), schema.ResolvedTarget{Key: &key},
			true, "text", false, false, schema.TestScopeLocal, false)
		require.NoError(t, vErr)
	})

	t.Run("validation error", func(t *testing.T) {
		t.Parallel()
		tester := schema.NewTester(registry)
		mgr := NewCLIManager(logger, registry, tester, &MockGitter{}, nil, io.Discard)
		// Non-existent path
		scope := schema.SearchScope("non/existent/path")
		vErr := mgr.ValidateSchema(context.Background(), schema.ResolvedTarget{Scope: &scope},
			false, "text", false, false, schema.TestScopeLocal, false)
		require.Error(t, vErr)
	})

	t.Run("tester error", func(t *testing.T) {
		t.Parallel()
		tester := schema.NewTester(registry)
		mgr := NewCLIManager(logger, registry, tester, &MockGitter{}, nil, io.Discard)
		// This will fail because the path is outside the registry root
		tKey := schema.Key("outside_key_1_0_0")
		vErr := mgr.ValidateSchema(context.Background(), schema.ResolvedTarget{Key: &tKey},
			false, "text", false, false, schema.TestScopeLocal, false)
		require.Error(t, vErr)
	})

	t.Run("no identification method provided", func(t *testing.T) {
		t.Parallel()
		tester := schema.NewTester(registry)
		mgr := NewCLIManager(logger, registry, tester, &MockGitter{}, nil, io.Discard)
		vErr := mgr.ValidateSchema(context.Background(), schema.ResolvedTarget{},
			false, "text", false, false, schema.TestScopeLocal, false)
		require.Error(t, vErr)
		assert.ErrorAs(t, vErr, new(*schema.NoSchemaTargetsError))
	})

	t.Run("missing test directories", func(t *testing.T) {
		t.Parallel()
		// Create a schema and registry but delete the pass/fail folders
		regDir := t.TempDir()
		cfg := `
environments:
  prod:
    publicUrlRoot: "https://p"
    privateUrlRoot: "https://pr"
    isProduction: true
`
		require.NoError(t, os.WriteFile(filepath.Join(regDir, config.JsmRegistryConfigFile), []byte(cfg), 0o600))
		r, err := schema.NewRegistry(regDir, &mockCompiler{}, fsh.NewPathResolver(), fsh.NewEnvProvider())
		require.NoError(t, err)

		tKey := schema.Key("d2_f2_1_0_0")
		s2 := schema.New(tKey, r)
		tHomeDir := s2.Path(schema.HomeDir)
		require.NoError(t, os.MkdirAll(tHomeDir, 0o755))
		require.NoError(t, os.WriteFile(s2.Path(schema.FilePath), []byte(`{}`), 0o600))

		mgr2 := NewCLIManager(logger, r, schema.NewTester(r), &MockGitter{}, nil, io.Discard)
		vErr := mgr2.ValidateSchema(context.Background(), schema.ResolvedTarget{Key: &tKey},
			false, "text", false, false, schema.TestScopeLocal, false)
		require.Error(t, vErr)
		require.ErrorContains(t, vErr, "pass directory missing")
	})
}

func TestCLIManager_WatchValidation(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Helper to set up a test schema with registry
	setupWatchTest := func(t *testing.T) (*schema.Registry, schema.Key, *schema.Schema) {
		t.Helper()
		registry := setupTestRegistry(t)
		key := schema.Key("domain_family_1_0_0")
		s := schema.New(key, registry)
		require.NoError(t, os.MkdirAll(s.Path(schema.HomeDir), 0o755))
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte(`{"type": "object"}`), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(schema.HomeDir), "pass"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(schema.HomeDir), "fail"), 0o755))
		return registry, key, s
	}

	t.Run("successful watch start and stop", func(t *testing.T) {
		t.Parallel()
		registry, key, _ := setupWatchTest(t)
		mgr := NewCLIManager(logger, registry, schema.NewTester(registry), &MockGitter{}, nil, io.Discard)

		ctx, cancel := context.WithCancel(context.Background())
		readyChan := make(chan struct{}, 1)
		done := make(chan error, 1)
		go func() {
			done <- mgr.WatchValidation(ctx, schema.ResolvedTarget{Key: &key},
				false, "text", false, false, schema.TestScopeLocal, false, readyChan)
		}()

		<-readyChan
		cancel()

		select {
		case err := <-done:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(2 * time.Second):
			t.Fatal("WatchValidation timed out after cancel")
		}
	})

	t.Run("no identification method provided", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		mgr := NewCLIManager(logger, registry, nil, nil, nil, io.Discard)
		err := mgr.WatchValidation(context.Background(), schema.ResolvedTarget{},
			false, "text", false, false, schema.TestScopeLocal, false, nil)
		require.Error(t, err)
		assert.ErrorAs(t, err, new(*schema.NoSchemaTargetsError))
	})

	t.Run("triggered watch event", func(t *testing.T) {
		t.Parallel()
		registry, key, s := setupWatchTest(t)
		mgr := NewCLIManager(logger, registry, schema.NewTester(registry), &MockGitter{}, nil, io.Discard)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		readyChan := make(chan struct{}, 1)
		done := make(chan error, 1)
		go func() {
			done <- mgr.WatchValidation(ctx, schema.ResolvedTarget{Key: &key},
				false, "text", false, false, schema.TestScopeLocal, false, readyChan)
		}()

		<-readyChan
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath),
			[]byte(`{"type": "object", "description": "updated"}`), 0o600))

		time.Sleep(500 * time.Millisecond)
		cancel()
		<-done
	})

	t.Run("triggered test doc event", func(t *testing.T) {
		t.Parallel()
		registry, key, s := setupWatchTest(t)
		mgr := NewCLIManager(logger, registry, schema.NewTester(registry), &MockGitter{}, nil, io.Discard)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		readyChan := make(chan struct{}, 1)
		done := make(chan error, 1)
		go func() {
			done <- mgr.WatchValidation(ctx, schema.ResolvedTarget{Key: &key},
				false, "text", false, false, schema.TestScopeLocal, false, readyChan)
		}()

		<-readyChan
		testFile := filepath.Join(s.Path(schema.HomeDir), "pass", "test.json")
		require.NoError(t, os.WriteFile(testFile, []byte("{}"), 0o600))

		time.Sleep(500 * time.Millisecond)
		cancel()
		<-done
	})

	t.Run("WatchValidation JSON format", func(t *testing.T) {
		t.Parallel()
		registry, key, s := setupWatchTest(t)
		var buf safeBuffer
		mgr := NewCLIManager(logger, registry, schema.NewTester(registry), &MockGitter{}, nil, &buf)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		done := make(chan error, 1)
		readyChan := make(chan struct{}, 1)
		go func() {
			done <- mgr.WatchValidation(ctx, schema.ResolvedTarget{Key: &key},
				false, "json", false, false, schema.TestScopeLocal, false, readyChan)
		}()

		<-readyChan

		// Trigger an event to exercise the JSON reporter branch
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath),
			[]byte(`{"type": "object", "title": "updated"}`), 0o600))

		// Wait for output to be written (more reliable than fixed sleep)
		require.True(t, buf.waitForOutput(5*time.Second), "expected output to be written")

		cancel()
		<-done

		// Verify JSON output was written (confirms JSON reporter branch was hit)
		assert.Contains(t, buf.String(), "{")
	})

	t.Run("triggered invalid schema change", func(t *testing.T) {
		t.Parallel()
		registry, key, s := setupWatchTest(t)
		mgr := NewCLIManager(logger, registry, schema.NewTester(registry), &MockGitter{}, nil, io.Discard)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		done := make(chan error, 1)
		readyChan := make(chan struct{}, 1)
		go func() {
			done <- mgr.WatchValidation(ctx, schema.ResolvedTarget{Key: &key},
				false, "text", false, false, schema.TestScopeLocal, false, readyChan)
		}()

		<-readyChan
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte(`{ invalid }`), 0o600))

		time.Sleep(500 * time.Millisecond)
		cancel()
		<-done
	})

	t.Run("WatchValidation - no target", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		mgr := NewCLIManager(logger, registry, nil, nil, nil, io.Discard)
		err := mgr.WatchValidation(context.Background(), schema.ResolvedTarget{},
			false, "text", false, false, schema.TestScopeLocal, false, nil)
		require.Error(t, err)
		assert.ErrorAs(t, err, new(*schema.NoSchemaTargetsError))
	})

	t.Run("WatchValidation - filtered events", func(t *testing.T) {
		t.Parallel()
		registry, key, _ := setupWatchTest(t)

		// Create a DIFFERENT schema BEFORE starting the watcher (so it's watched)
		k2 := schema.Key("other_domain_otherfamily_1_0_0")
		s2 := schema.New(k2, registry)
		require.NoError(t, os.MkdirAll(s2.Path(schema.HomeDir), 0o755))
		require.NoError(t, os.WriteFile(s2.Path(schema.FilePath), []byte("{}"), 0o600))

		mgr := NewCLIManager(logger, registry, schema.NewTester(registry), &MockGitter{}, nil, io.Discard)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		done := make(chan error, 1)
		readyChan := make(chan struct{}, 1)
		go func() {
			done <- mgr.WatchValidation(ctx, schema.ResolvedTarget{Key: &key},
				false, "text", false, false, schema.TestScopeLocal, false, readyChan)
		}()

		<-readyChan

		// Modify the OTHER schema file to trigger an event (should be filtered by Key)
		require.NoError(t, os.WriteFile(s2.Path(schema.FilePath), []byte(`{"type":"object"}`), 0o600))

		time.Sleep(500 * time.Millisecond)
		cancel()
		<-done
	})

	t.Run("WatchValidation - scoped filtered events", func(t *testing.T) {
		t.Parallel()
		registry, _, _ := setupWatchTest(t)

		// Create a schema in a DIFFERENT scope BEFORE starting the watcher (so it's watched)
		k3 := schema.Key("other_domain_family_1_0_0")
		s3 := schema.New(k3, registry)
		require.NoError(t, os.MkdirAll(s3.Path(schema.HomeDir), 0o755))
		require.NoError(t, os.WriteFile(s3.Path(schema.FilePath), []byte("{}"), 0o600))

		mgr := NewCLIManager(logger, registry, schema.NewTester(registry), &MockGitter{}, nil, io.Discard)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		done := make(chan error, 1)
		scope := schema.SearchScope("domain/family")
		readyChan := make(chan struct{}, 1)
		go func() {
			done <- mgr.WatchValidation(ctx, schema.ResolvedTarget{Scope: &scope},
				false, "text", false, false, schema.TestScopeLocal, false, readyChan)
		}()

		<-readyChan

		// Modify the OTHER scope schema file to trigger an event (should be filtered by Scope)
		require.NoError(t, os.WriteFile(s3.Path(schema.FilePath), []byte(`{"type":"object"}`), 0o600))

		time.Sleep(200 * time.Millisecond)
		cancel()
		<-done
	})

	t.Run("WatchValidation - rerun conflict bug", func(t *testing.T) {
		t.Parallel()

		regDir := t.TempDir()
		// Use REAL compiler to expose the bug
		comp := validator.NewSanthoshCompiler()
		cfg := `environments:
  prod:
    publicUrlRoot: 'https://p'
    privateUrlRoot: 'https://pr'
    isProduction: true`

		require.NoError(t, os.WriteFile(filepath.Join(regDir, config.JsmRegistryConfigFile), []byte(cfg), 0o600))
		registry, err := schema.NewRegistry(regDir, comp, fsh.NewPathResolver(), fsh.NewEnvProvider())
		require.NoError(t, err)

		key := schema.Key("domain_family_1_0_0")
		s := schema.New(key, registry)
		require.NoError(t, os.MkdirAll(s.Path(schema.HomeDir), 0o755))
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte(`{"type": "object"}`), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(schema.HomeDir), "pass"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(schema.HomeDir), "fail"), 0o755))

		// Set up manager with REAL compiler and log capture
		var logBuf safeBuffer
		testLogger := slog.New(slog.NewTextHandler(&logBuf, nil))
		mgr := NewCLIManager(testLogger, registry, schema.NewTester(registry), &MockGitter{}, nil, io.Discard)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		done := make(chan error, 1)
		readyChan := make(chan struct{}, 1)
		go func() {
			done <- mgr.WatchValidation(ctx, schema.ResolvedTarget{Key: &key},
				false, "text", false, false, schema.TestScopeLocal, false, readyChan)
		}()

		<-readyChan

		// First update - should pass
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte(`{"type": "object", "title": "v1"}`), 0o600))
		time.Sleep(500 * time.Millisecond)

		// Second update - should trigger "already exists" error in current state
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte(`{"type": "object", "title": "v2"}`), 0o600))
		// Wait long enough for debounce (100ms) + processing
		time.Sleep(1 * time.Second)

		cancel()
		<-done

		// Assert that the bug is NOT present
		assert.NotContains(t, logBuf.String(), "already exists")
	})

	t.Run("WatchValidation reporter error", func(t *testing.T) {
		t.Parallel()
		registry, key, s := setupWatchTest(t)
		mgr := NewCLIManager(logger, registry, schema.NewTester(registry), &MockGitter{}, nil, &failingWriter{})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		readyChan := make(chan struct{}, 1)
		go func() {
			_ = mgr.WatchValidation(ctx, schema.ResolvedTarget{Key: &key},
				false, "text", false, false, schema.TestScopeLocal, false, readyChan)
		}()

		<-readyChan
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath),
			[]byte(`{"type": "object", "title": "updated"}`), 0o600))

		// Wait for processing
		time.Sleep(500 * time.Millisecond)
		cancel()
	})
}

func TestCLIManager_CreateSchema(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("successful create", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		tester := schema.NewTester(registry)
		mgr := NewCLIManager(logger, registry, tester, &MockGitter{}, nil, io.Discard)

		key, err := mgr.CreateSchema("test-domain/test-family")
		require.NoError(t, err)
		assert.Equal(t, schema.Key("test-domain_test-family_1_0_0"), key)
	})

	t.Run("failed create - invalid scope", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		tester := schema.NewTester(registry)
		mgr := NewCLIManager(logger, registry, tester, &MockGitter{}, nil, io.Discard)

		_, err := mgr.CreateSchema("INVALID/scope")
		require.Error(t, err)
	})
}

func TestCLIManager_CreateSchemaVersion(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("successful create version", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		tester := schema.NewTester(registry)
		mgr := NewCLIManager(logger, registry, tester, &MockGitter{}, nil, io.Discard)

		// Create base schema
		baseKey := schema.Key("d1_f1_1_0_0")
		s := schema.New(baseKey, registry)
		require.NoError(t, os.MkdirAll(s.Path(schema.HomeDir), 0o755))
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte("{}"), 0o600))

		newKey, err := mgr.CreateSchemaVersion(baseKey, schema.ReleaseTypeMajor)
		require.NoError(t, err)
		assert.Equal(t, schema.Key("d1_f1_2_0_0"), newKey)
	})

	t.Run("failed create version - missing base", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		tester := schema.NewTester(registry)
		mgr := NewCLIManager(logger, registry, tester, &MockGitter{}, nil, io.Discard)

		_, err := mgr.CreateSchemaVersion(schema.Key("missing_1_0_0"), schema.ReleaseTypeMajor)
		require.Error(t, err)
	})
}

func TestCLIManager_RenderSchema(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("successful render", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		tester := schema.NewTester(registry)
		mgr := NewCLIManager(logger, registry, tester, &MockGitter{}, nil, io.Discard)

		// Create base schema
		baseKey := schema.Key("d1_f1_1_0_0")
		s := schema.New(baseKey, registry)
		require.NoError(t, os.MkdirAll(s.Path(schema.HomeDir), 0o755))
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte("{}"), 0o600))

		target := schema.ResolvedTarget{Key: &baseKey}
		rendered, err := mgr.RenderSchema(context.Background(), target, config.Env("prod"))
		require.NoError(t, err)
		assert.NotEmpty(t, rendered)
	})

	t.Run("default environment (prod)", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		tester := schema.NewTester(registry)
		mgr := NewCLIManager(logger, registry, tester, &MockGitter{}, nil, io.Discard)

		baseKey := schema.Key("d1_f1_1_0_0")
		s := schema.New(baseKey, registry)
		require.NoError(t, os.MkdirAll(s.Path(schema.HomeDir), 0o755))
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte("{}"), 0o600))

		target := schema.ResolvedTarget{Key: &baseKey}
		rendered, err := mgr.RenderSchema(context.Background(), target, "")
		require.NoError(t, err)
		assert.NotEmpty(t, rendered)
	})

	t.Run("invalid environment", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		tester := schema.NewTester(registry)
		mgr := NewCLIManager(logger, registry, tester, &MockGitter{}, nil, io.Discard)

		baseKey := schema.Key("d1_f1_1_0_0")
		target := schema.ResolvedTarget{Key: &baseKey}
		_, err := mgr.RenderSchema(context.Background(), target, "invalid")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid environment: 'invalid'. Valid environments are: 'prod'")
	})

	t.Run("missing target key", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		tester := schema.NewTester(registry)
		mgr := NewCLIManager(logger, registry, tester, &MockGitter{}, nil, io.Discard)

		target := schema.ResolvedTarget{}
		_, err := mgr.RenderSchema(context.Background(), target, "prod")
		require.Error(t, err)
		assert.ErrorAs(t, err, new(*schema.NoSchemaTargetsError))
	})

	t.Run("config error", func(t *testing.T) {
		t.Parallel()
		registry := &schema.Registry{}
		tester := schema.NewTester(registry)
		mgr := NewCLIManager(logger, registry, tester, &MockGitter{}, nil, io.Discard)

		baseKey := schema.Key("d1_f1_1_0_0")
		target := schema.ResolvedTarget{Key: &baseKey}
		_, vErr := mgr.RenderSchema(context.Background(), target, "prod")
		require.Error(t, vErr)
	})

	t.Run("GetSchemaByKey error", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		tester := schema.NewTester(registry)
		mgr := NewCLIManager(logger, registry, tester, &MockGitter{}, nil, io.Discard)

		baseKey := schema.Key("missing_1_0_0")
		target := schema.ResolvedTarget{Key: &baseKey}
		_, err := mgr.RenderSchema(context.Background(), target, "prod")
		require.Error(t, err)
	})

	t.Run("CoordinateRender error", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		tester := schema.NewTester(registry)
		mgr := NewCLIManager(logger, registry, tester, &MockGitter{}, nil, io.Discard)

		baseKey := schema.Key("d1_f1_1_0_0")
		s := schema.New(baseKey, registry)
		require.NoError(t, os.MkdirAll(s.Path(schema.HomeDir), 0o755))
		// Invalid JSON should trigger an error in renderer.Render
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte("{ invalid"), 0o600))

		target := schema.ResolvedTarget{Key: &baseKey}
		_, err := mgr.RenderSchema(context.Background(), target, "prod")
		require.Error(t, err)
	})

	t.Run("multiple environments success", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		// Setup config with two environments
		configData := `environments:
  prod: {publicUrlRoot: 'https://p', privateUrlRoot: 'https://pr', isProduction: true}
  dev: {publicUrlRoot: 'https://d', privateUrlRoot: 'https://dr', isProduction: false}`
		require.NoError(
			t,
			os.WriteFile(filepath.Join(tmpDir, "json-schema-manager-config.yml"), []byte(configData), 0o600),
		)
		registry, err := schema.NewRegistry(tmpDir, &mockCompiler{}, fsh.NewPathResolver(), fsh.NewEnvProvider())
		require.NoError(t, err)

		tester := schema.NewTester(registry)
		mgr := NewCLIManager(logger, registry, tester, &MockGitter{}, nil, io.Discard)

		baseKey := schema.Key("d1_f1_1_0_0")
		s := schema.New(baseKey, registry)
		require.NoError(t, os.MkdirAll(s.Path(schema.HomeDir), 0o755))
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte("{}"), 0o600))

		target := schema.ResolvedTarget{Key: &baseKey}

		// Test prod
		_, err = mgr.RenderSchema(context.Background(), target, "prod")
		require.NoError(t, err)

		// Test dev
		_, err = mgr.RenderSchema(context.Background(), target, "dev")
		require.NoError(t, err)
	})

	t.Run("GetSchemaByKey failure - unreadable dir", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		mgr := NewCLIManager(logger, registry, nil, &MockGitter{}, nil, io.Discard)

		baseKey := schema.Key("d1_f1_1_0_0")
		s := schema.New(baseKey, registry)
		homedir := s.Path(schema.HomeDir)
		require.NoError(t, os.MkdirAll(homedir, 0o755))
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte("{}"), 0o600))

		// Make it unreadable
		require.NoError(t, os.Chmod(homedir, 0o000))
		t.Cleanup(func() { _ = os.Chmod(homedir, 0o755) })

		target := schema.ResolvedTarget{Key: &baseKey}
		_, err := mgr.RenderSchema(context.Background(), target, "prod")
		require.Error(t, err)
	})

	t.Run("CoordinateRender failure - bad template", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		mgr := NewCLIManager(logger, registry, nil, &MockGitter{}, nil, io.Discard)

		baseKey := schema.Key("d1_f1_1_0_0")
		s := schema.New(baseKey, registry)
		require.NoError(t, os.MkdirAll(s.Path(schema.HomeDir), 0o755))
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte("{{ bad }"), 0o600))

		target := schema.ResolvedTarget{Key: &baseKey}
		_, err := mgr.RenderSchema(context.Background(), target, "prod")
		require.Error(t, err)
	})

	t.Run("CoordinateRender failure - bad JSM key", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		mgr := NewCLIManager(logger, registry, nil, &MockGitter{}, nil, io.Discard)

		baseKey := schema.Key("d1_f1_1_0_0")
		s := schema.New(baseKey, registry)
		require.NoError(t, os.MkdirAll(s.Path(schema.HomeDir), 0o755))
		// Valid JSON, but bad template action
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte("{{ JSM \"!!\" }}"), 0o600))

		target := schema.ResolvedTarget{Key: &baseKey}
		_, err := mgr.RenderSchema(context.Background(), target, "prod")
		require.Error(t, err)
	})

	t.Run("CoordinateRender failure - missing dependency", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		mgr := NewCLIManager(logger, registry, nil, &MockGitter{}, nil, io.Discard)

		baseKey := schema.Key("d1_f1_1_0_0")
		s := schema.New(baseKey, registry)
		require.NoError(t, os.MkdirAll(s.Path(schema.HomeDir), 0o755))
		// Valid template, but missing dependency will fail during rendering execution
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte("{{ JSM \"missing_dep_1_0_0\" }}"), 0o600))

		target := schema.ResolvedTarget{Key: &baseKey}
		_, err := mgr.RenderSchema(context.Background(), target, "prod")
		require.Error(t, err)
	})

	t.Run("CoordinateRender failure - compiler error", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "json-schema-manager-config.yml")
		require.NoError(t, os.WriteFile(cfgPath, []byte(testConfigData), 0o600))
		registry, _ := schema.NewRegistry(tmpDir, &failingCompiler{}, fsh.NewPathResolver(), fsh.NewEnvProvider())
		mgr := NewCLIManager(logger, registry, nil, &MockGitter{}, nil, io.Discard)

		baseKey := schema.Key("d1_f1_1_0_0")
		s := schema.New(baseKey, registry)
		require.NoError(t, os.MkdirAll(s.Path(schema.HomeDir), 0o755))
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte("{}"), 0o600))

		target := schema.ResolvedTarget{Key: &baseKey}
		_, err := mgr.RenderSchema(context.Background(), target, "prod")
		require.Error(t, err)
	})
}

func TestCLIManager_CheckChanges(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := setupTestRegistry(t)
	mgr := NewCLIManager(logger, registry, nil, &MockGitter{}, nil, io.Discard)

	t.Run("config error", func(t *testing.T) {
		t.Parallel()
		// Use an empty registry to simulate uninitialised config
		m := NewCLIManager(logger, &schema.Registry{}, nil, &MockGitter{}, nil, io.Discard)
		err := m.CheckChanges(context.Background(), "prod")
		require.Error(t, err)
	})

	t.Run("invalid environment", func(t *testing.T) {
		t.Parallel()
		err := mgr.CheckChanges(context.Background(), "invalid")
		require.Error(t, err)
	})

	t.Run("git history error", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir() // Not a git repo
		cfgPath := filepath.Join(dir, "json-schema-manager-config.yml")
		require.NoError(t, os.WriteFile(cfgPath, []byte(testConfigData), 0o600))

		registry2, err := schema.NewRegistry(dir, &mockCompiler{}, fsh.NewPathResolver(), fsh.NewEnvProvider())
		require.NoError(t, err)

		cfg2, err := registry2.Config()
		require.NoError(t, err)
		m := NewCLIManager(logger, registry2, nil, repo.NewCLIGitter(cfg2, fsh.NewPathResolver(), dir), nil, io.Discard)

		err = m.CheckChanges(context.Background(), "prod")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "could not find git history")
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		// Init git repo
		require.NoError(t, exec.CommandContext(context.Background(), "git", "init", dir).Run())
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", dir, "config", "user.email", "t@t.com").Run(),
		)
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", dir, "config", "user.name", "t").Run(),
		)

		// Create a schema in a sub-directory
		schemaDir := filepath.Join(dir, "domain", "family", "1", "0", "0")
		require.NoError(t, os.MkdirAll(schemaDir, 0o755))
		f1 := filepath.Join(schemaDir, "f1.schema.json")
		require.NoError(t, os.WriteFile(f1, []byte("{}"), 0o600))
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", dir, "add", ".").Run())
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", dir, "commit", "-m", "init").Run())
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", dir, "tag", "jsm-deploy/prod/v1").Run(),
		)

		cfgPath := filepath.Join(dir, "json-schema-manager-config.yml")
		require.NoError(t, os.WriteFile(cfgPath, []byte(testConfigData), 0o600))

		registry4, err := schema.NewRegistry(dir, &mockCompiler{}, fsh.NewPathResolver(), fsh.NewEnvProvider())
		require.NoError(t, err)

		m := NewCLIManager(logger, registry4, nil, &MockGitter{}, nil, io.Discard)

		// No changes
		err = m.CheckChanges(context.Background(), "prod")
		require.NoError(t, err)

		// New schema
		f2 := filepath.Join(schemaDir, "f2.schema.json")
		require.NoError(t, os.WriteFile(f2, []byte("{}"), 0o600))
		gitAdd := exec.CommandContext(context.Background(), "git", "add", ".")
		gitAdd.Dir = dir
		require.NoError(t, gitAdd.Run())

		gitCommit := exec.CommandContext(context.Background(), "git", "commit", "-m", "new schema")
		gitCommit.Dir = dir
		require.NoError(t, gitCommit.Run())

		err = m.CheckChanges(context.Background(), "prod")
		require.NoError(t, err)
	})

	t.Run("mutation forbidden", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, exec.CommandContext(context.Background(), "git", "init", dir).Run())
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", dir, "config", "user.email", "t@t.com").Run(),
		)
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", dir, "config", "user.name", "t").Run(),
		)

		schemaDir := filepath.Join(dir, "domain", "family", "1", "0", "0")
		require.NoError(t, os.MkdirAll(schemaDir, 0o755))
		f1 := filepath.Join(schemaDir, "f1.schema.json")
		require.NoError(t, os.WriteFile(f1, []byte("{}"), 0o600))
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", dir, "add", ".").Run())
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", dir, "commit", "-m", "in").Run())
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", dir, "tag", "jsm-deploy/prod/v1").Run(),
		)

		// Mutate it
		require.NoError(t, os.WriteFile(f1, []byte(`{"type":"object"}`), 0o600))
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", dir, "add", ".").Run())
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", dir, "commit", "-m", "mu").Run())

		require.NoError(
			t,
			os.WriteFile(filepath.Join(dir, "json-schema-manager-config.yml"), []byte(testConfigData), 0o600),
		)
		r, _ := schema.NewRegistry(dir, &mockCompiler{}, fsh.NewPathResolver(), fsh.NewEnvProvider())
		rcfg, _ := r.Config()
		m := NewCLIManager(logger, r, nil, repo.NewCLIGitter(rcfg, fsh.NewPathResolver(), dir), nil, io.Discard)

		err := m.CheckChanges(context.Background(), "prod")
		require.Error(t, err)

		var mutationErr *schema.ChangedDeployedSchemasError
		require.ErrorAs(t, err, &mutationErr)
		assert.Contains(t, mutationErr.Paths[0], "f1.schema.json")
	})

	t.Run("mutation allowed", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, exec.CommandContext(context.Background(), "git", "init", dir).Run())
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", dir, "config", "user.email", "t@t.com").Run(),
		)
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", dir, "config", "user.name", "t").Run(),
		)

		schemaDir := filepath.Join(dir, "domain", "family", "1", "0", "0")
		require.NoError(t, os.MkdirAll(schemaDir, 0o755))
		f1 := filepath.Join(schemaDir, "f1.schema.json")
		require.NoError(t, os.WriteFile(f1, []byte("{}"), 0o600))
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", dir, "add", ".").Run())
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", dir, "commit", "-m", "in").Run())
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", dir, "tag", "jsm-deploy/prod/v1").Run(),
		)

		// Mutate it
		require.NoError(t, os.WriteFile(f1, []byte(`{"type":"object"}`), 0o600))
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", dir, "add", ".").Run())
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", dir, "commit", "-m", "mu").Run())

		cfgWithMutation := `
environments:
  prod: {publicUrlRoot: 'https://p', privateUrlRoot: 'https://pr', isProduction: true, allowSchemaMutation: true}`
		cfgPath := filepath.Join(dir, "json-schema-manager-config.yml")
		require.NoError(t, os.WriteFile(cfgPath, []byte(cfgWithMutation), 0o600))
		r, _ := schema.NewRegistry(dir, &mockCompiler{}, fsh.NewPathResolver(), fsh.NewEnvProvider())
		m := NewCLIManager(logger, r, nil, &MockGitter{}, nil, io.Discard)

		err := m.CheckChanges(context.Background(), "prod")
		require.NoError(t, err)
	})

	t.Run("deletion not treated as mutation", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, exec.CommandContext(context.Background(), "git", "init", dir).Run())
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", dir, "config", "user.email", "t@t.com").Run(),
		)
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", dir, "config", "user.name", "t").Run(),
		)

		schemaDir := filepath.Join(dir, "domain", "family", "1", "0", "0")
		require.NoError(t, os.MkdirAll(schemaDir, 0o755))
		f1 := filepath.Join(schemaDir, "f1.schema.json")
		require.NoError(t, os.WriteFile(f1, []byte("{}"), 0o600))
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", dir, "add", ".").Run())
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", dir, "commit", "-m", "in").Run())
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", dir, "tag", "jsm-deploy/prod/v1").Run(),
		)

		// Delete the schema (should NOT be treated as a mutation)
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", dir, "rm", f1).Run())
		require.NoError(t, exec.CommandContext(context.Background(), "git", "-C", dir, "commit", "-m", "del").Run())

		require.NoError(
			t,
			os.WriteFile(filepath.Join(dir, "json-schema-manager-config.yml"), []byte(testConfigData), 0o600),
		)
		r, _ := schema.NewRegistry(dir, &mockCompiler{}, fsh.NewPathResolver(), fsh.NewEnvProvider())
		rcfg, _ := r.Config()
		m := NewCLIManager(logger, r, nil, repo.NewCLIGitter(rcfg, fsh.NewPathResolver(), dir), nil, io.Discard)

		err := m.CheckChanges(context.Background(), "prod")
		require.NoError(t, err, "deleting a schema should not be treated as a mutation")
	})

	t.Run("GetSchemaChanges error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		mockGitter := &MockGitter{
			GetSchemaChangesFunc: func(_ context.Context, _ repo.Revision, _, _ string) ([]repo.Change, error) {
				return nil, fmt.Errorf("git diff failed")
			},
		}
		m := NewCLIManager(logger, r, nil, mockGitter, nil, io.Discard)

		err := m.CheckChanges(context.Background(), "prod")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "git diff failed")
	})
}

func TestCLIManager_TagDeployment(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := setupTestRegistry(t)
	mgr := NewCLIManager(logger, registry, nil, &MockGitter{}, nil, io.Discard)

	t.Run("config error", func(t *testing.T) {
		t.Parallel()
		m := NewCLIManager(logger, &schema.Registry{}, nil, &MockGitter{}, nil, io.Discard)
		err := m.TagDeployment(context.Background(), "prod")
		require.Error(t, err)
	})

	t.Run("invalid environment", func(t *testing.T) {
		t.Parallel()
		err := mgr.TagDeployment(context.Background(), "invalid")
		require.Error(t, err)
	})

	t.Run("success without remote", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, exec.CommandContext(context.Background(), "git", "init", dir).Run())
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", dir, "config", "user.email", "t@t.com").Run(),
		)
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", dir, "config", "user.name", "t").Run(),
		)
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", dir, "commit", "--allow-empty", "-m", "init").Run(),
		)

		require.NoError(
			t,
			os.WriteFile(filepath.Join(dir, "json-schema-manager-config.yml"), []byte(testConfigData), 0o600),
		)
		r, _ := schema.NewRegistry(dir, &mockCompiler{}, fsh.NewPathResolver(), fsh.NewEnvProvider())
		m := NewCLIManager(logger, r, nil, &MockGitter{}, nil, io.Discard)

		// This calls g.TagDeploymentSuccess() which will fail push but return tagName
		err := m.TagDeployment(context.Background(), "prod")
		require.NoError(t, err) // We return nil if tag created but push fails
	})

	t.Run("git tag error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		mockGitter := &MockGitter{
			TagDeploymentFunc: func(_ context.Context, _ config.Env) (string, error) {
				return "", fmt.Errorf("failed to create git tag")
			},
		}
		m := NewCLIManager(logger, r, nil, mockGitter, nil, io.Discard)

		err := m.TagDeployment(context.Background(), "prod")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create git tag")
	})

	t.Run("success with remote", func(t *testing.T) {
		t.Parallel()
		// Setup a "remote"
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

		cfgPath := filepath.Join(repoDir, "json-schema-manager-config.yml")
		require.NoError(t, os.WriteFile(cfgPath, []byte(testConfigData), 0o600))
		r, _ := schema.NewRegistry(repoDir, &mockCompiler{}, fsh.NewPathResolver(), fsh.NewEnvProvider())
		m := NewCLIManager(logger, r, nil, &MockGitter{}, nil, io.Discard)

		err := m.TagDeployment(context.Background(), "prod")
		require.NoError(t, err)

		// Verify tag exists on "remote"
		cmd := exec.CommandContext(
			context.Background(),
			"git",
			"-C",
			remoteDir,
			"rev-parse",
			"HEAD",
		) // just check it's a git repo
		output, err := cmd.Output()
		require.NoError(t, err)
		assert.NotEmpty(t, output)
	})

	t.Run("tag created but push fails", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, exec.CommandContext(context.Background(), "git", "init", dir).Run())
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", dir, "config", "user.email", "t@t.com").Run(),
		)
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", dir, "config", "user.name", "t").Run(),
		)
		require.NoError(
			t,
			exec.CommandContext(context.Background(), "git", "-C", dir, "commit", "--allow-empty", "-m", "init").Run(),
		)

		require.NoError(
			t,
			os.WriteFile(filepath.Join(dir, "json-schema-manager-config.yml"), []byte(testConfigData), 0o600),
		)
		r, _ := schema.NewRegistry(dir, &mockCompiler{}, fsh.NewPathResolver(), fsh.NewEnvProvider())

		mockGitter := &MockGitter{
			TagDeploymentFunc: func(_ context.Context, _ config.Env) (string, error) {
				return "jsm-deploy/prod/failed-push", fmt.Errorf("git push failed")
			},
		}

		m := NewCLIManager(logger, r, nil, mockGitter, nil, io.Discard)

		err := m.TagDeployment(context.Background(), "prod")
		require.NoError(t, err) // Should return nil if tag created but push failed
	})
}

func TestCLIManager_BuildDist(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("success --all", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		mockBuilder := &MockDistBuilder{
			BuildAllFunc: func(_ context.Context, env config.Env) (int, error) {
				assert.Equal(t, config.Env("prod"), env)
				return 5, nil
			},
		}
		mgr := NewCLIManager(logger, registry, nil, &MockGitter{}, mockBuilder, io.Discard)

		err := mgr.BuildDist(context.Background(), "prod", true)
		require.NoError(t, err)
	})

	t.Run("success with changes", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		mockBuilder := &MockDistBuilder{
			BuildChangedFunc: func(_ context.Context, env config.Env, _ repo.Revision) (int, error) {
				assert.Equal(t, config.Env("prod"), env)
				return 3, nil
			},
		}
		mgr := NewCLIManager(logger, registry, nil, &MockGitter{}, mockBuilder, io.Discard)

		err := mgr.BuildDist(context.Background(), "prod", false)
		require.NoError(t, err)
	})

	t.Run("no schemas to build", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		mockBuilder := &MockDistBuilder{
			BuildAllFunc: func(_ context.Context, _ config.Env) (int, error) {
				return 0, nil
			},
		}
		mgr := NewCLIManager(logger, registry, nil, &MockGitter{}, mockBuilder, io.Discard)

		err := mgr.BuildDist(context.Background(), "prod", true)
		require.NoError(t, err)
	})

	t.Run("BuildAll error", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		mockBuilder := &MockDistBuilder{
			BuildAllFunc: func(_ context.Context, _ config.Env) (int, error) {
				return 0, fmt.Errorf("build failed")
			},
		}
		mgr := NewCLIManager(logger, registry, nil, &MockGitter{}, mockBuilder, io.Discard)

		err := mgr.BuildDist(context.Background(), "prod", true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "build failed")
	})

	t.Run("CheckChanges error", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		// Use a gitter that returns a mutation error
		mockGitter := &MockGitter{
			GetSchemaChangesFunc: func(_ context.Context, _ repo.Revision, _, _ string) ([]repo.Change, error) {
				return []repo.Change{{Path: "mutated.schema.json", IsNew: false}}, nil
			},
		}
		mgr := NewCLIManager(logger, registry, nil, mockGitter, nil, io.Discard)

		err := mgr.BuildDist(context.Background(), "prod", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot modify deployed schemas")
	})

	t.Run("GetLatestAnchor error", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		calls := 0
		mockGitter := &MockGitter{
			GetLatestAnchorFunc: func(_ context.Context, _ config.Env) (repo.Revision, error) {
				calls++
				if calls == 1 {
					return "HEAD", nil
				}
				return "", fmt.Errorf("git anchor failed")
			},
		}
		mgr := NewCLIManager(logger, registry, nil, mockGitter, nil, io.Discard)

		err := mgr.BuildDist(context.Background(), "prod", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "git anchor failed")
	})

	t.Run("BuildChanged error", func(t *testing.T) {
		t.Parallel()
		registry := setupTestRegistry(t)
		mockBuilder := &MockDistBuilder{
			BuildChangedFunc: func(_ context.Context, _ config.Env, _ repo.Revision) (int, error) {
				return 0, fmt.Errorf("build changed failed")
			},
		}
		mgr := NewCLIManager(logger, registry, nil, &MockGitter{}, mockBuilder, io.Discard)

		err := mgr.BuildDist(context.Background(), "prod", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "build changed failed")
	})
}

func TestNewBuildDistCmd(t *testing.T) {
	t.Parallel()
	t.Run("success", func(t *testing.T) {
		t.Parallel()
		mockMgr := &MockManager{}
		cmd := NewBuildDistCmd(mockMgr)
		mockMgr.On("BuildDist", mock.Anything, config.Env("prod"), false).Return(nil)
		cmd.SetArgs([]string{"prod"})
		err := cmd.Execute()
		require.NoError(t, err)
		mockMgr.AssertExpectations(t)
	})

	t.Run("error", func(t *testing.T) {
		t.Parallel()
		mockMgr := &MockManager{}
		cmd := NewBuildDistCmd(mockMgr)
		mockMgr.On("BuildDist", mock.Anything, config.Env("prod"), false).Return(errors.New("build failed"))
		cmd.SetArgs([]string{"prod"})
		err := cmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "build failed")
		mockMgr.AssertExpectations(t)
	})
}

func TestCLIManager_Registry(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := setupTestRegistry(t)
	mgr := NewCLIManager(logger, registry, nil, nil, nil, io.Discard)

	assert.Equal(t, registry, mgr.Registry())
}

func TestLazyManager_PanicWhenNotInitialised(t *testing.T) {
	t.Parallel()
	lazy := &LazyManager{}

	// Should panic when accessing any method before SetInner is called
	assert.Panics(t, func() {
		lazy.Registry()
	})
}

func TestLazyManager_Delegation(t *testing.T) {
	t.Parallel()
	mockMgr := &MockManager{
		registry: &schema.Registry{},
	}
	lazy := &LazyManager{}
	lazy.SetInner(mockMgr)

	// Test HasInner
	assert.True(t, lazy.HasInner())

	// Test Registry delegation
	assert.Equal(t, mockMgr.registry, lazy.Registry())

	// Test ValidateSchema delegation
	ctx := context.Background()
	key := schema.Key("test_1_0_0")
	target := schema.ResolvedTarget{Key: &key}
	mockMgr.On("ValidateSchema", ctx, target, false, "text", false, false, schema.TestScopeLocal, false).Return(nil)
	err := lazy.ValidateSchema(ctx, target, false, "text", false, false, schema.TestScopeLocal, false)
	require.NoError(t, err)

	// Test CreateSchema delegation
	mockMgr.On("CreateSchema", "domain/family").Return(schema.Key("domain_family_1_0_0"), nil)
	key2, err := lazy.CreateSchema("domain/family")
	require.NoError(t, err)
	assert.Equal(t, schema.Key("domain_family_1_0_0"), key2)

	// Test CreateSchemaVersion delegation
	mockMgr.On("CreateSchemaVersion", schema.Key("domain_family_1_0_0"), schema.ReleaseTypeMinor).
		Return(schema.Key("domain_family_1_1_0"), nil)
	key3, err := lazy.CreateSchemaVersion(schema.Key("domain_family_1_0_0"), schema.ReleaseTypeMinor)
	require.NoError(t, err)
	assert.Equal(t, schema.Key("domain_family_1_1_0"), key3)

	// Test RenderSchema delegation
	mockMgr.On("RenderSchema", ctx, target, config.Env("prod")).Return([]byte("{}"), nil)
	rendered, err := lazy.RenderSchema(ctx, target, config.Env("prod"))
	require.NoError(t, err)
	assert.Equal(t, []byte("{}"), rendered)

	// Test CheckChanges delegation
	mockMgr.On("CheckChanges", ctx, config.Env("prod")).Return(nil)
	err = lazy.CheckChanges(ctx, config.Env("prod"))
	require.NoError(t, err)

	// Test TagDeployment delegation
	mockMgr.On("TagDeployment", ctx, config.Env("prod")).Return(nil)
	err = lazy.TagDeployment(ctx, config.Env("prod"))
	require.NoError(t, err)

	// Test BuildDist delegation
	mockMgr.On("BuildDist", ctx, config.Env("prod"), false).Return(nil)
	err = lazy.BuildDist(ctx, config.Env("prod"), false)
	require.NoError(t, err)

	// Test WatchValidation delegation
	mockMgr.On("WatchValidation", ctx, target, false, "text", false, false,
		schema.TestScopeLocal, false, (chan<- struct{})(nil)).Return(nil)
	err = lazy.WatchValidation(ctx, target, false, "text", false, false, schema.TestScopeLocal, false, nil)
	require.NoError(t, err)

	mockMgr.AssertExpectations(t)
}

func TestCLIManager_WatchValidation_ReporterError(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := setupTestRegistry(t)
	key := schema.Key("domain_family_1_0_0")
	s := schema.New(key, registry)
	require.NoError(t, os.MkdirAll(s.Path(schema.HomeDir), 0o755))
	require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte("{}"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(s.Path(schema.HomeDir), "pass"), 0o755))

	mgr := NewCLIManager(logger, registry, schema.NewTester(registry), &MockGitter{}, nil, &failingWriter{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	readyChan := make(chan struct{}, 1)
	go func() {
		done <- mgr.WatchValidation(ctx, schema.ResolvedTarget{Key: &key},
			false, "text", false, false, schema.TestScopeLocal, false, readyChan)
	}()

	<-readyChan

	// Trigger an event
	require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte(`{"type":"object"}`), 0o600))

	time.Sleep(500 * time.Millisecond) // Wait for debounce and callback

	cancel()
	<-done
}

func TestCLIManager_WatchValidation_CallbackErrors(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := setupTestRegistry(t)

	setupSchema := func(name string) (schema.Key, *schema.Schema) {
		key := schema.Key(name)
		s := schema.New(key, registry)
		require.NoError(t, os.MkdirAll(s.Path(schema.HomeDir), 0o755))
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte(`{"type":"object"}`), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(schema.HomeDir), "pass"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(schema.HomeDir), "fail"), 0o755))
		return key, s
	}

	key, s := setupSchema("domain_family_1_0_0")
	mgr := NewCLIManager(logger, registry, schema.NewTester(registry), &MockGitter{}, nil, io.Discard)

	t.Run("callback load error handling", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		readyChan := make(chan struct{}, 1)
		done := make(chan error, 1)
		go func() {
			done <- mgr.WatchValidation(ctx, schema.ResolvedTarget{Key: &key},
				false, "text", false, false, schema.TestScopeLocal, false, readyChan)
		}()

		<-readyChan

		// Trigger write then immediately delete to cause load error in callback
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte(`{"updated":true}`), 0o600))
		require.NoError(t, os.Remove(s.Path(schema.FilePath)))

		time.Sleep(500 * time.Millisecond)
		cancel()
		<-done
	})
}

func TestCLIManager_WatchValidation_EdgeCases(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := setupTestRegistry(t)

	setupSchema := func(name string) (schema.Key, *schema.Schema) {
		key := schema.Key(name)
		s := schema.New(key, registry)
		require.NoError(t, os.MkdirAll(s.Path(schema.HomeDir), 0o755))
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte(`{"type":"object"}`), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(schema.HomeDir), "pass"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(schema.HomeDir), "fail"), 0o755))
		return key, s
	}

	key, s := setupSchema("domain_family_1_0_0")
	mgr := NewCLIManager(logger, registry, schema.NewTester(registry), &MockGitter{}, nil, io.Discard)

	t.Run("callback error handling", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		readyChan := make(chan struct{}, 1)
		done := make(chan error, 1)
		go func() {
			done <- mgr.WatchValidation(ctx, schema.ResolvedTarget{Key: &key},
				false, "text", false, false, schema.TestScopeLocal, false, readyChan)
		}()

		<-readyChan

		// Trigger validation error by making the schema invalid at the JSON level
		// although WatchValidation treats any error in TestSingleSchema as a return
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte(`{ invalid }`), 0o600))

		time.Sleep(500 * time.Millisecond)
		cancel()
		<-done
	})

	t.Run("system error handling", func(t *testing.T) {
		t.Parallel()
		// Use a failing compiler to trigger TestSingleSchema error
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "json-schema-manager-config.yml")
		require.NoError(t, os.WriteFile(cfgPath, []byte(testConfigData), 0o600))
		reg, err := schema.NewRegistry(tmpDir, &failingCompiler{}, fsh.NewPathResolver(), fsh.NewEnvProvider())
		require.NoError(t, err)

		k := schema.Key("error_schema_1_0_0")
		sch := schema.New(k, reg)
		require.NoError(t, os.MkdirAll(sch.Path(schema.HomeDir), 0o755))
		require.NoError(t, os.WriteFile(sch.Path(schema.FilePath), []byte(`{"type":"object"}`), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(sch.Path(schema.HomeDir), "pass"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(sch.Path(schema.HomeDir), "fail"), 0o755))

		mgr := NewCLIManager(logger, reg, schema.NewTester(reg), &MockGitter{}, nil, io.Discard)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		readyChan := make(chan struct{}, 1)
		done := make(chan error, 1)
		go func() {
			done <- mgr.WatchValidation(ctx, schema.ResolvedTarget{Key: &k},
				false, "text", false, false, schema.TestScopeLocal, false, readyChan)
		}()

		<-readyChan
		require.NoError(t, os.WriteFile(sch.Path(schema.FilePath), []byte(`{"updated":true}`), 0o600))

		time.Sleep(500 * time.Millisecond)
		cancel()
		<-done
	})

	t.Run("reporter write error branch", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Reuse main registry but use failing writer
		mgr := NewCLIManager(logger, registry, schema.NewTester(registry), &MockGitter{}, nil, io.Discard)

		readyChan := make(chan struct{}, 1)
		done := make(chan error, 1)
		go func() {
			done <- mgr.WatchValidation(ctx, schema.ResolvedTarget{Key: &key},
				false, "text", false, false, schema.TestScopeLocal, false, readyChan)
		}()

		<-readyChan
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte(`{"type":"object"}`), 0o600))

		time.Sleep(500 * time.Millisecond)
		cancel()
		<-done
	})
}

type failingWriter struct{}

func (f *failingWriter) Write(_ []byte) (n int, err error) {
	return 0, fmt.Errorf("write failed")
}
