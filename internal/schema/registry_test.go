package schema

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/fsh"
)

func TestNewRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func(t *testing.T) string
		expectErr func(t *testing.T, err error)
	}{
		{
			name: "root folder does not exist",
			setup: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "non-existent")
			},
			expectErr: func(t *testing.T, err error) {
				t.Helper()
				var target *RegistryInitError
				if !errors.As(err, &target) {
					t.Fatalf("expected RegistryInitError, got %v", err)
				}
				assert.Equal(t, "registry root "+target.Path+", given by --registry, does not exist", err.Error())
			},
		},
		{
			name: "exists but not a folder",
			setup: func(t *testing.T) string {
				t.Helper()
				filePath := filepath.Join(t.TempDir(), "somefile.txt")
				if err := os.WriteFile(filePath, []byte("hello"), 0o600); err != nil {
					t.Fatal(err)
				}
				return filePath
			},
			expectErr: func(t *testing.T, err error) {
				t.Helper()
				var target *RegistryRootNotFolderError
				if !errors.As(err, &target) {
					t.Fatalf("expected RegistryRootNotFolderError, got %v", err)
				}
				assert.Contains(t, err.Error(), "Registry could not be initialised")
				assert.Contains(t, err.Error(), "is not a directory")
			},
		},
		{
			name: "os.Stat error for symlink with missing target",
			setup: func(t *testing.T) string {
				t.Helper()
				tmpDir := t.TempDir()
				// Create a directory, then create a symlink
				realDir := filepath.Join(tmpDir, "real")
				if err := os.MkdirAll(realDir, 0o755); err != nil {
					t.Fatal(err)
				}
				symlinkPath := filepath.Join(tmpDir, "symlink")
				if err := os.Symlink(realDir, symlinkPath); err != nil {
					t.Fatal(err)
				}
				// Remove the real directory to create a broken symlink
				if err := os.RemoveAll(realDir); err != nil {
					t.Fatal(err)
				}
				return symlinkPath
			},
			expectErr: func(t *testing.T, err error) {
				t.Helper()
				var target *RegistryInitError
				if !errors.As(err, &target) {
					t.Fatalf("expected RegistryInitError, got %v", err)
				}
			},
		},
		{
			name: "config error",
			setup: func(t *testing.T) string {
				t.Helper()
				regDir := t.TempDir()
				if err := os.WriteFile(
					filepath.Join(regDir, "json-schema-manager-config.yml"),
					[]byte("invalid config"),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				return regDir
			},
			expectErr: func(t *testing.T, err error) {
				t.Helper()
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				// We expect a config error (e.g. ErrInvalidYAML)
				// You could also assert the specific type here if desired.
			},
		},
		{
			name: "success",
			setup: func(t *testing.T) string {
				t.Helper()
				regDir := t.TempDir()
				content := `
environments:
  prod:
      privateUrlRoot: "https://json-schemas.internal.myorg.io/"
      publicUrlRoot: "https://json-schemas.myorg.io/"
      isProduction: true
`
				if err := os.WriteFile(
					filepath.Join(regDir, config.JsmRegistryConfigFile),
					[]byte(content),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				return regDir
			},
			expectErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := tt.setup(t)
			compiler := &mockCompiler{}
			pathResolver := fsh.NewPathResolver()
			envProvider := fsh.NewEnvProvider()
			r, err := NewRegistry(path, compiler, pathResolver, envProvider)

			if tt.expectErr != nil {
				tt.expectErr(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, r)
			}
		})
	}
}

func TestNewRegistry_NotFound(t *testing.T) {
	t.Parallel()

	t.Run("no registry given, and the current directory is not one", func(t *testing.T) {
		t.Parallel()
		_, err := NewRegistry("", &mockCompiler{}, fsh.NewPathResolver(), &mockEnvProvider{})
		var target *NoRegistryError
		require.ErrorAs(t, err, &target)
		assert.Contains(t, err.Error(), "has no json-schema-manager-config.yml. Run jsm in a registry's root directory")
		assert.Contains(t, err.Error(), "jsm create-registry <dir>")
	})

	t.Run("a path from the environment which does not exist", func(t *testing.T) {
		t.Parallel()
		missing := filepath.Join(t.TempDir(), "missing")
		env := &mockEnvProvider{values: map[string]string{RootDirEnvVar: missing}}
		_, err := NewRegistry("", &mockCompiler{}, fsh.NewPathResolver(), env)
		require.EqualError(t, err, "registry root "+missing+", given by $JSM_REGISTRY_ROOT_DIR, does not exist")
		require.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("a registry given without a config file", func(t *testing.T) {
		t.Parallel()
		_, err := NewRegistry(t.TempDir(), &mockCompiler{}, fsh.NewPathResolver(), &mockEnvProvider{})
		require.ErrorAs(t, err, new(*config.MissingConfigError), "only a missing registry gets the setup advice")
	})
}

func TestKeyFromSchemaPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		setup              func(t *testing.T, r *Registry) string
		wantKey            Key
		wantErrMsgContains string
	}{
		{
			name: "valid schema file path",
			setup: func(t *testing.T, r *Registry) string {
				t.Helper()
				// Create a valid schema file
				s := New(Key("domain_family_1_0_0"), r)
				homeDir := s.Path(HomeDir)
				if err := os.MkdirAll(homeDir, 0o755); err != nil {
					t.Fatal(err)
				}
				filePath := s.Path(FilePath)
				if err := os.WriteFile(filePath, []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
				return filePath
			},
			wantKey: Key("domain_family_1_0_0"),
		},
		{
			name: "non-existent path",
			setup: func(_ *testing.T, r *Registry) string {
				return filepath.Join(r.rootDirectory, "non-existent.schema.json")
			},
			wantErrMsgContains: "schema not found",
		},
		{
			name: "path is a directory",
			setup: func(t *testing.T, r *Registry) string {
				t.Helper()
				dirPath := filepath.Join(r.rootDirectory, "some-dir")
				if err := os.MkdirAll(dirPath, 0o755); err != nil {
					t.Fatal(err)
				}
				return dirPath
			},
			wantErrMsgContains: "is not a JSON schema file",
		},
		{
			name: "path without schema suffix",
			setup: func(t *testing.T, r *Registry) string {
				t.Helper()
				filePath := filepath.Join(r.rootDirectory, "not-a-schema.json")
				if err := os.WriteFile(filePath, []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
				return filePath
			},
			wantErrMsgContains: "is not a JSON schema file",
		},
		{
			name: "path outside root directory",
			setup: func(t *testing.T, _ *Registry) string {
				t.Helper()
				// Create a file outside the registry root
				outsideDir := t.TempDir()
				filePath := filepath.Join(outsideDir, "domain_family_1_0_0.schema.json")
				if err := os.WriteFile(filePath, []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
				return filePath
			},
			wantErrMsgContains: "is outside root directory",
		},
		{
			name: "invalid key format in filename",
			setup: func(t *testing.T, r *Registry) string {
				t.Helper()
				// Create a file with an invalid key (not enough parts)
				filePath := filepath.Join(r.rootDirectory, "invalid.schema.json")
				if err := os.WriteFile(filePath, []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
				return filePath
			},
			wantErrMsgContains: "is not a valid schema key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := setupTestRegistry(t)
			path := tt.setup(t, r)

			key, err := r.KeyFromSchemaPath(path)

			if tt.wantErrMsgContains != "" {
				require.ErrorContains(t, err, tt.wantErrMsgContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantKey, key)
		})
	}
}

func TestGetSchema(t *testing.T) {
	t.Parallel()

	const validSchemaContent = `{
"$schema": "http://json-schema.org/draft-07/schema#",
"$id": "{{ ID }}",
"type": "object"
}`

	tests := []struct {
		name               string
		setup              func(t *testing.T, r *Registry) string
		preCache           bool // if true, pre-cache the schema
		wantErrMsgContains string
	}{
		{
			name: "load new schema",
			setup: func(t *testing.T, r *Registry) string {
				t.Helper()
				s := New(Key("domain_family_1_0_0"), r)
				homeDir := s.Path(HomeDir)
				if err := os.MkdirAll(homeDir, 0o755); err != nil {
					t.Fatal(err)
				}
				filePath := s.Path(FilePath)
				if err := os.WriteFile(filePath, []byte(validSchemaContent), 0o600); err != nil {
					t.Fatal(err)
				}
				return filePath
			},
		},
		{
			name: "return cached schema",
			setup: func(t *testing.T, r *Registry) string {
				t.Helper()
				s := New(Key("domain_family_1_0_0"), r)
				homeDir := s.Path(HomeDir)
				if err := os.MkdirAll(homeDir, 0o755); err != nil {
					t.Fatal(err)
				}
				filePath := s.Path(FilePath)
				if err := os.WriteFile(filePath, []byte(validSchemaContent), 0o600); err != nil {
					t.Fatal(err)
				}
				return filePath
			},
			preCache: true,
		},
		{
			name: "invalid path returns error",
			setup: func(_ *testing.T, r *Registry) string {
				return filepath.Join(r.rootDirectory, "non-existent.schema.json")
			},
			wantErrMsgContains: "schema not found",
		},
		{
			name: "load error propagates",
			setup: func(t *testing.T, r *Registry) string {
				t.Helper()
				// Create schema file with invalid JSON
				s := New(Key("domain_family_1_0_0"), r)
				homeDir := s.Path(HomeDir)
				if err := os.MkdirAll(homeDir, 0o755); err != nil {
					t.Fatal(err)
				}
				filePath := s.Path(FilePath)
				if err := os.WriteFile(filePath, []byte("{ invalid json"), 0o600); err != nil {
					t.Fatal(err)
				}
				return filePath
			},
			wantErrMsgContains: "is not valid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := setupTestRegistry(t)
			path := tt.setup(t, r)

			if tt.preCache {
				// Pre-load the schema into cache
				_, err := r.GetSchema(path)
				require.NoError(t, err)
			}

			schema, err := r.GetSchema(path)

			if tt.wantErrMsgContains != "" {
				require.ErrorContains(t, err, tt.wantErrMsgContains)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, schema)

			// Verify schema is in cache
			key := schema.Key()
			cachedSchema, ok := r.cache[key]
			assert.True(t, ok)
			assert.Same(t, schema, cachedSchema)
		})
	}
}

func TestCreateSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		domainAndFamily    string
		setup              func(t *testing.T, r *Registry)
		wantErrMsgContains string
		wantKey            Key
	}{
		{
			name:            "create new schema successfully",
			domainAndFamily: "domain/family-name",
			wantKey:         Key("domain_family-name_1_0_0"),
		},
		{
			name:            "create schema with multiple domains",
			domainAndFamily: "org/team/project/schema-name",
			wantKey:         Key("org_team_project_schema-name_1_0_0"),
		},
		{
			name:            "schema already exists in cache",
			domainAndFamily: "domain/family",
			setup: func(_ *testing.T, r *Registry) {
				r.cache[Key("domain_family_1_0_0")] = &Schema{}
			},
			wantErrMsgContains: "already exists",
		},
		{
			name:               "invalid key format - only one part",
			domainAndFamily:    "invalid",
			wantErrMsgContains: "Invalid create schema argument",
		},
		{
			name:               "invalid characters in domain",
			domainAndFamily:    "INVALID/family",
			wantErrMsgContains: "is not a valid JSM Search Scope",
		},
		{
			name:            "schema files already exist on disk",
			domainAndFamily: "domain/existing",
			setup: func(t *testing.T, r *Registry) {
				t.Helper()
				s := New(Key("domain_existing_1_0_0"), r)
				if err := os.MkdirAll(s.Path(HomeDir), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(s.Path(FilePath), []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
				// Do NOT add to cache, otherwise we hit the cache check instead
			},
			wantErrMsgContains: "already exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := setupTestRegistry(t)

			if tt.setup != nil {
				tt.setup(t, r)
			}

			schema, err := r.CreateSchema(tt.domainAndFamily)

			if tt.wantErrMsgContains != "" {
				require.ErrorContains(t, err, tt.wantErrMsgContains)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, schema)
			assert.Equal(t, tt.wantKey, schema.Key())

			// Verify schema is in cache
			cachedSchema, ok := r.cache[tt.wantKey]
			assert.True(t, ok)
			assert.Same(t, schema, cachedSchema)

			// Verify files were created
			filePath := schema.Path(FilePath)
			_, err = os.Stat(filePath)
			require.NoError(t, err, "schema file should exist")
		})
	}
}

func TestCreateNewSchemaVersion(t *testing.T) {
	t.Parallel()

	const validSchemaContent = `{
"$schema": "http://json-schema.org/draft-07/schema#",
"$id": "{{ ID }}",
"type": "object"
}`

	tests := []struct {
		name            string
		releaseType     ReleaseType
		setup           func(t *testing.T, r *Registry) string
		wantErr         bool
		wantErrContains string
		wantVersion     [3]uint64
	}{
		{
			name:        "create major version",
			releaseType: ReleaseTypeMajor,
			setup: func(t *testing.T, r *Registry) string {
				t.Helper()
				s := New(Key("domain_family_1_0_0"), r)
				homeDir := s.Path(HomeDir)
				if err := os.MkdirAll(homeDir, 0o755); err != nil {
					t.Fatal(err)
				}
				filePath := s.Path(FilePath)
				if err := os.WriteFile(filePath, []byte(validSchemaContent), 0o600); err != nil {
					t.Fatal(err)
				}
				return filePath
			},
			wantVersion: [3]uint64{2, 0, 0},
		},
		{
			name:        "create minor version",
			releaseType: ReleaseTypeMinor,
			setup: func(t *testing.T, r *Registry) string {
				t.Helper()
				s := New(Key("domain_family_1_0_0"), r)
				homeDir := s.Path(HomeDir)
				if err := os.MkdirAll(homeDir, 0o755); err != nil {
					t.Fatal(err)
				}
				filePath := s.Path(FilePath)
				if err := os.WriteFile(filePath, []byte(validSchemaContent), 0o600); err != nil {
					t.Fatal(err)
				}
				return filePath
			},
			wantVersion: [3]uint64{1, 1, 0},
		},
		{
			name:        "create patch version",
			releaseType: ReleaseTypePatch,
			setup: func(t *testing.T, r *Registry) string {
				t.Helper()
				s := New(Key("domain_family_1_0_0"), r)
				homeDir := s.Path(HomeDir)
				if err := os.MkdirAll(homeDir, 0o755); err != nil {
					t.Fatal(err)
				}
				filePath := s.Path(FilePath)
				if err := os.WriteFile(filePath, []byte(validSchemaContent), 0o600); err != nil {
					t.Fatal(err)
				}
				return filePath
			},
			wantVersion: [3]uint64{1, 0, 1},
		},
		{
			name:        "error when source schema not found",
			releaseType: ReleaseTypeMajor,
			setup: func(_ *testing.T, r *Registry) string {
				return filepath.Join(r.rootDirectory, "non-existent.schema.json")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := setupTestRegistry(t)
			path := tt.setup(t, r)

			newSchema, err := r.CreateNewSchemaVersion(path, tt.releaseType)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			if tt.wantErrContains != "" {
				require.ErrorContains(t, err, tt.wantErrContains)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, newSchema)

			// Verify version was bumped correctly
			v := [3]uint64{newSchema.Key().Major(), newSchema.Key().Minor(), newSchema.Key().Patch()}
			assert.Equal(t, tt.wantVersion, v)

			// Verify new schema is in cache
			key := newSchema.Key()
			cachedSchema, ok := r.cache[key]
			assert.True(t, ok)
			assert.Same(t, newSchema, cachedSchema)

			// Verify files were created
			filePath := newSchema.Path(FilePath)
			_, err = os.Stat(filePath)
			require.NoError(t, err, "new schema file should exist")
		})
	}
}

func TestCreateSchemaVersion(t *testing.T) {
	t.Parallel()

	const validSchemaContent = `{
"$schema": "http://json-schema.org/draft-07/schema#",
"$id": "{{ ID }}",
"type": "object"
}`

	tests := []struct {
		name        string
		key         Key
		releaseType ReleaseType
		setup       func(t *testing.T, r *Registry)
		wantErr     bool
		wantVersion [3]uint64
	}{
		{
			name:        "create new version from key successfully",
			key:         Key("domain_family_1_0_0"),
			releaseType: ReleaseTypeMinor,
			setup: func(t *testing.T, r *Registry) {
				t.Helper()
				s := New(Key("domain_family_1_0_0"), r)
				homeDir := s.Path(HomeDir)
				require.NoError(t, os.MkdirAll(homeDir, 0o755))
				require.NoError(t, os.WriteFile(s.Path(FilePath), []byte(validSchemaContent), 0o600))
			},
			wantVersion: [3]uint64{1, 1, 0},
		},
		{
			name:        "source schema not in registry/disk",
			key:         Key("non_existent_1_0_0"),
			releaseType: ReleaseTypeMajor,
			wantErr:     true,
		},
		{
			name:        "DuplicateSchemaFiles failure",
			key:         Key("domain_family_1_0_0"),
			releaseType: ReleaseTypeMajor,
			setup: func(t *testing.T, r *Registry) {
				t.Helper()
				s := New(Key("domain_family_1_0_0"), r)
				homeDir := s.Path(HomeDir)
				require.NoError(t, os.MkdirAll(homeDir, 0o755))
				require.NoError(t, os.WriteFile(s.Path(FilePath), []byte("{}"), 0o600))

				// Make the family directory read-only so subdirectories cannot be created.
				familyDir := s.Path(FamilyDir)
				require.NoError(t, os.Chmod(familyDir, 0o500))
				t.Cleanup(func() { _ = os.Chmod(familyDir, 0o755) })
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := setupTestRegistry(t)
			if tt.setup != nil {
				tt.setup(t, r)
			}

			newSchema, err := r.CreateSchemaVersion(tt.key, tt.releaseType)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, newSchema)
			assert.Equal(
				t,
				tt.wantVersion,
				[3]uint64{newSchema.Key().Major(), newSchema.Key().Minor(), newSchema.Key().Patch()},
			)
			assert.Contains(t, r.cache, newSchema.Key())
		})
	}
}

// Tests for os.Stat edge cases to achieve 100% coverage.
func TestNewRegistry_StatErrAfterCanonicalPath(t *testing.T) {
	t.Parallel()

	// Mock to return a valid-looking path that doesn't exist
	// This simulates the race condition where file is deleted between
	// canonicalPath and os.Stat
	mockResolver := &mockPathResolver{
		canonicalPathFn: func(_ string) (string, error) {
			return "/path/that/definitely/does/not/exist/anywhere", nil
		},
	}

	compiler := &mockCompiler{}
	envProvider := fsh.NewEnvProvider()
	_, err := NewRegistry("/some/path", compiler, mockResolver, envProvider)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no such file or directory")
}

func TestKeyFromSchemaPath_StatErrAfterCanonicalPath(t *testing.T) {
	t.Parallel()

	// First create a real registry, then replace its pathResolver
	r := setupTestRegistry(t)

	// Replace the pathResolver with a mock that returns a non-existent path
	r.pathResolver = &mockPathResolver{
		canonicalPathFn: func(_ string) (string, error) {
			return "/path/that/definitely/does/not/exist.schema.json", nil
		},
	}

	_, err := r.KeyFromSchemaPath("/some/schema.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no such file or directory")
}

func TestCreateSchema_WriteNewSchemaFilesErr(t *testing.T) {
	t.Parallel()

	r := setupTestRegistry(t)

	// Create a file where the schema directory should be created
	// This blocks os.MkdirAll from succeeding when WriteNewSchemaFiles runs
	blockingPath := filepath.Join(r.rootDirectory, "domain", "family")
	if err := os.MkdirAll(filepath.Dir(blockingPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blockingPath, []byte("blocking"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := r.CreateSchema("domain/family")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestCreateNewSchemaVersion_DuplicateSchemaFilesErr(t *testing.T) {
	t.Parallel()

	const validSchemaContent = `{
"$schema": "http://json-schema.org/draft-07/schema#",
"$id": "{{ ID }}",
"type": "object"
}`

	r := setupTestRegistry(t)

	// Create the source schema properly
	sourceKey := Key("domain_family_1_0_0")
	sourceSchema := New(sourceKey, r)
	sourceHomeDir := sourceSchema.Path(HomeDir)
	if err := os.MkdirAll(sourceHomeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sourceFilePath := sourceSchema.Path(FilePath)
	if err := os.WriteFile(sourceFilePath, []byte(validSchemaContent), 0o600); err != nil {
		t.Fatal(err)
	}
	// Create pass and fail directories
	if err := os.Mkdir(filepath.Join(sourceHomeDir, "pass"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(sourceHomeDir, "fail"), 0o755); err != nil {
		t.Fatal(err)
	}

	// After BumpVersion with ReleaseTypeMajor, the new version will be 2_0_0
	// (since only 1_0_0 exists in dir .../domain/family/1/, nextVersion finds [1] and returns 2).
	// Make the family directory read-only to prevent creating new major version directories.
	// This will cause os.MkdirAll in DuplicateSchemaFiles to fail with "permission denied".
	familyDir := sourceSchema.Path(FamilyDir)

	if err := os.Chmod(familyDir, 0o500); err != nil {
		t.Fatal(err)
	}
	// Cleanup: restore permissions so temp dir can be removed
	t.Cleanup(func() {
		_ = os.Chmod(familyDir, 0o755)
	})

	_, err := r.CreateNewSchemaVersion(sourceFilePath, ReleaseTypeMajor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

func TestGetSchemaByKey_DoubleCheck_Deterministic(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	k := Key("domain_family_1_0_0")
	s := New(k, r)
	require.NoError(t, os.MkdirAll(s.Path(HomeDir), 0o755))
	require.NoError(t, os.WriteFile(s.Path(FilePath), []byte(`{}`), 0o600))

	// The double-check cache path inside singleflight is hit when:
	// 1. A goroutine enters singleflight.Do()
	// 2. Before the double-check runs, another path populates the cache
	// This is inherently racy, but we can increase probability by running concurrent calls.

	// Pre-populate the cache to ensure the double-check path returns early
	r.mu.Lock()
	r.cache[k] = s
	r.mu.Unlock()

	// Now any call should hit the initial cache check (line 147)
	s2, err := r.GetSchemaByKey(k)
	require.NoError(t, err)
	assert.Same(t, s, s2)

	// For the singleflight double-check path, we rely on concurrent test execution
	// to occasionally hit it. The 90% threshold in the tester script handles this.
}

func TestGetSchemaByKey_LoadError(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	k := Key("domain_family_1_0_0")
	s := New(k, r)
	require.NoError(t, os.MkdirAll(s.Path(HomeDir), 0o755))
	// Write invalid JSON to trigger data unmarshal error in Load
	require.NoError(t, os.WriteFile(s.Path(FilePath), []byte(`{ invalid`), 0o600))

	s2, err := r.GetSchemaByKey(k)
	require.Error(t, err)
	assert.Nil(t, s2)
}

func TestGetSchemaByKey_DoubleCheck_Race(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	k := Key("domain_family_1_0_0")
	s := New(k, r)
	require.NoError(t, os.MkdirAll(s.Path(HomeDir), 0o755))
	require.NoError(t, os.WriteFile(s.Path(FilePath), []byte(`{}`), 0o600))

	for i := 0; i < 5000; i++ {
		r.mu.Lock()
		delete(r.cache, k)
		r.mu.Unlock()

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = r.GetSchemaByKey(k)
		}()
		go func() {
			defer wg.Done()
			runtime.Gosched()
			r.mu.Lock()
			r.cache[k] = s
			r.mu.Unlock()
		}()
		wg.Wait()
	}
}

func TestRegistryDiscovery(t *testing.T) {
	t.Parallel()

	// Setup a temporary directory for the tests
	tmpDir := t.TempDir()

	t.Run("initRootDirectory from env", func(t *testing.T) {
		t.Parallel()

		// Use mock env provider to inject the value
		mockEnv := &mockEnvProvider{
			values: map[string]string{
				"JSM_REGISTRY_ROOT_DIR": tmpDir,
			},
		}
		pathResolver := fsh.NewPathResolver()

		rd, err := initRootDirectory("", pathResolver, mockEnv)
		require.NoError(t, err)
		expected, _ := pathResolver.CanonicalPath(tmpDir)
		assert.Equal(t, expected, rd)
	})
}

func TestRegistry_Accessors(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)

	// Test RootDirectory()
	assert.NotEmpty(t, r.RootDirectory())
	assert.DirExists(t, r.RootDirectory())

	// Test Config() - success case
	cfg, err := r.Config()
	require.NoError(t, err)
	assert.NotNil(t, cfg)

	// Test Config() - error case (simulated)
	r.config = nil
	cfg, err = r.Config()
	require.Error(t, err)
	assert.Nil(t, cfg)
}

func TestGetSchemaByKey_DoubleCheckCache(t *testing.T) {
	t.Parallel()
	reg := setupTestRegistry(t)
	key := Key("domain_family_1_0_0")
	createSchemaFiles(t, reg, schemaMap{key: "{}"})

	// Fill cache manually for a different key to simulate some state
	reg.mu.Lock()
	reg.cache[Key("other_1_0_0")] = &Schema{}
	reg.mu.Unlock()

	// Call GetSchemaByKey. The actual double-check is hard to hit without timing,
	// but we can at least ensure the method works and covers the lines.
	s, err := reg.GetSchemaByKey(key)
	require.NoError(t, err)
	assert.NotNil(t, s)
}

func TestRegistry_GetSchemaByKey_Concurrent(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	k := Key("domain_family_1_0_0")
	// Pre-setup schema file
	s := New(k, r)
	require.NoError(t, os.MkdirAll(s.Path(HomeDir), 0o755))
	require.NoError(t, os.WriteFile(s.Path(FilePath), []byte("{}"), 0o600))

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.GetSchemaByKey(k)
		}()
	}
	wg.Wait()
}

func TestRegistry_Reset(t *testing.T) {
	t.Parallel()
	registry := setupTestRegistry(t)
	k := Key("domain_family_1_0_0")
	createSchemaFiles(t, registry, schemaMap{k: "{}"})

	// Load a schema into cache
	s, err := registry.GetSchemaByKey(k)
	require.NoError(t, err)
	require.NotNil(t, s)

	// Verify it's cached
	registry.mu.RLock()
	_, exists := registry.cache[k]
	registry.mu.RUnlock()
	assert.True(t, exists, "schema should be cached")

	// Reset should clear the cache
	registry.Reset()

	// Verify cache is now empty
	registry.mu.RLock()
	_, exists = registry.cache[k]
	registry.mu.RUnlock()
	assert.False(t, exists, "schema cache should be cleared after Reset")
}
