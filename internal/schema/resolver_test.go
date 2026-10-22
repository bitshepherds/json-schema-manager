package schema

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTargetResolver(t *testing.T) {
	t.Parallel()

	// common tests that use a shared registry (but don't mutate it)
	r := setupTestRegistry(t)
	k := Key("domain_family_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		k: `{"type": "object"}`,
	})

	// Additional files for error cases
	badFile := filepath.Join(r.rootDirectory, "bad.txt")
	require.NoError(t, os.WriteFile(badFile, []byte(`{}`), 0o600))

	outsideFile := filepath.Join(t.TempDir(), "outside.schema.json")
	require.NoError(t, os.WriteFile(outsideFile, []byte(`{}`), 0o600))

	notASchemaFile := filepath.Join(t.TempDir(), "not-a-schema.txt")
	require.NoError(t, os.WriteFile(notASchemaFile, []byte(`{}`), 0o600))

	outsideDir := t.TempDir()

	tests := []struct {
		name        string
		arg         string
		setup       func(ar *TargetResolver)
		wantKey     Key
		wantScope   SearchScope
		wantErr     bool
		wantErrType interface{}
	}{
		{
			name: "explicit key",
			setup: func(ar *TargetResolver) {
				ar.SetKey("explicit_key_1_0_0")
			},
			wantKey: "explicit_key_1_0_0",
		},
		{
			name: "explicit key with slashes",
			setup: func(ar *TargetResolver) {
				ar.SetKey("explicit/key/1/0/0")
			},
			wantKey: "explicit_key_1_0_0",
		},
		{
			name: "explicit ID",
			setup: func(ar *TargetResolver) {
				ar.SetID("https://example.com/explicit_id_1_0_0.schema.json")
			},
			wantKey: "explicit_id_1_0_0",
		},
		{
			name: "explicit scope",
			setup: func(ar *TargetResolver) {
				ar.SetScope("explicit/scope")
			},
			wantScope: "explicit/scope",
		},
		{
			name:    "guess ID from URL",
			arg:     "https://example.com/guess_id_1_0_0.schema.json",
			wantKey: "guess_id_1_0_0",
		},
		{
			name:    "guess Key from string with underscore",
			arg:     "guess_key_1_0_0",
			wantKey: "guess_key_1_0_0",
		},
		{
			name:      "guess Scope from string without underscore",
			arg:       "guess/scope",
			wantScope: "guess/scope",
		},
		{
			name:    "resolve file path",
			arg:     "SCHEMA_PATH",
			wantKey: k,
		},
		{
			name:      "resolve directory path",
			arg:       "SCHEMA_DIR",
			wantScope: "domain/family/1/0/0",
		},
		{
			name:      "resolve registry root directory path",
			arg:       "REGISTRY_ROOT",
			wantScope: "",
		},
		{
			name:        "error resolving file outside root",
			arg:         outsideFile,
			wantErr:     true,
			wantErrType: &LocationOutsideRootDirectoryError{},
		},
		{
			name:        "error resolving non-schema file",
			arg:         badFile,
			wantErr:     true,
			wantErrType: &NotASchemaFileError{},
		},
		{
			name:    "invalid URL",
			arg:     "https://:invalid",
			wantErr: true,
		},
		{
			name:        "URL with invalid suffix",
			arg:         "https://example.com/bad.txt",
			wantErr:     true,
			wantErrType: &NotASchemaFileError{},
		},
		{
			name: "explicit ID with invalid suffix",
			setup: func(ar *TargetResolver) {
				ar.SetID("https://example.com/bad.txt")
			},
			wantErr:     true,
			wantErrType: &NotASchemaFileError{},
		},
		{
			name: "explicit empty ID",
			setup: func(ar *TargetResolver) {
				ar.SetID("")
			},
			wantErr:     true,
			wantErrType: &NoSchemaTargetsError{},
		},
		{
			name: "invalid explicit ID",
			setup: func(ar *TargetResolver) {
				ar.SetID("https://:invalid")
			},
			wantErr: true,
		},
		{
			name: "empty explicit ID",
			setup: func(ar *TargetResolver) {
				ar.SetID("")
			},
			wantErr: true,
		},
		{
			name: "invalid explicit key",
			setup: func(ar *TargetResolver) {
				ar.SetKey("invalid_key")
			},
			wantErr: true,
		},
		{
			name: "invalid explicit scope",
			setup: func(ar *TargetResolver) {
				ar.SetScope("Invalid Scope!")
			},
			wantErr: true,
		},
		{
			name: "empty explicit scope",
			setup: func(ar *TargetResolver) {
				ar.SetScope("")
			},
			wantErr: true,
		},
		{
			name:    "invalid URL arg",
			arg:     "https://example.com/not-a-schema.txt",
			wantErr: true,
		},
		{
			name:    "invalid key-like arg",
			arg:     "not_a_key",
			wantErr: true,
		},
		{
			name:    "invalid scope-like arg",
			arg:     "Invalid Scope!",
			wantErr: true,
		},
		{
			name:        "resolve directory outside root",
			arg:         outsideDir,
			wantErr:     true,
			wantErrType: &LocationOutsideRootDirectoryError{},
		},
		{
			name:      "all schemas",
			arg:       "all",
			wantScope: "",
		},
		{
			name:        "no targets",
			arg:         "",
			wantErr:     true,
			wantErrType: &NoSchemaTargetsError{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			subR := setupTestRegistry(t)
			createSchemaFiles(t, subR, schemaMap{
				k: `{"type": "object"}`,
			})

			arg := tt.arg
			switch arg {
			case "SCHEMA_PATH":
				s, _ := subR.GetSchemaByKey(k)
				arg = s.Path(FilePath)
			case "SCHEMA_DIR":
				s, _ := subR.GetSchemaByKey(k)
				arg = s.Path(HomeDir)
			case "REGISTRY_ROOT":
				arg = subR.rootDirectory
			}

			ar := NewTargetResolver(subR, arg)
			if tt.setup != nil {
				tt.setup(ar)
			}

			res, err := ar.Resolve()

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrType != nil {
					//nolint:testifylint // IsType is appropriate for table-driven tests with interface{}
					assert.IsType(t, tt.wantErrType, err)
				}
				return
			}

			require.NoError(t, err)
			if tt.wantKey != "" {
				assert.NotNil(t, res.Key)
				assert.Equal(t, tt.wantKey, *res.Key)
			} else {
				assert.Nil(t, res.Key)
			}

			if tt.wantScope != "" || (tt.name == "all schemas" || tt.name == "resolve registry root directory path") {
				assert.NotNil(t, res.Scope)
				assert.Equal(t, tt.wantScope, *res.Scope)
			} else {
				assert.Nil(t, res.Scope)
			}
		})
	}

	t.Run("resolvePathToScope canonicalPath error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		ar := NewTargetResolver(r, "")
		_, err := ar.resolvePathToScope("/non/existent/path")
		require.Error(t, err)
	})

	t.Run("resolvePathToScope out of bounds", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		ar := NewTargetResolver(r, "")
		parentDir := filepath.Dir(r.rootDirectory)
		_, err := ar.resolvePathToScope(parentDir)
		require.Error(t, err)
		assert.ErrorAs(t, err, new(*LocationOutsideRootDirectoryError))
	})

	t.Run("ResolveScopeToSingleKey exactly one match", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		createSchemaFiles(t, r, schemaMap{
			k: `{"type": "object"}`,
		})
		ar := NewTargetResolver(r, "")
		result, err := ar.ResolveScopeToSingleKey(context.Background(), "domain/family", "domain/family")
		require.NoError(t, err)
		assert.Equal(t, k, result)
	})

	t.Run("ResolveScopeToSingleKey zero matches", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		ar := NewTargetResolver(r, "")
		_, err := ar.ResolveScopeToSingleKey(context.Background(), "nonexistent", "nonexistent")
		require.Error(t, err)
		assert.ErrorAs(t, err, new(*NotFoundError))
	})

	t.Run("ResolveScopeToSingleKey zero matches with existing directory", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		ar := NewTargetResolver(r, "")
		// Create an empty directory within the registry
		emptyDir := filepath.Join(r.rootDirectory, "empty")
		require.NoError(t, os.MkdirAll(emptyDir, 0o755))

		_, err := ar.ResolveScopeToSingleKey(context.Background(), "empty", "empty")
		require.Error(t, err)
		assert.ErrorAs(t, err, new(*NotFoundError))
	})

	t.Run("ResolveScopeToSingleKey searcher error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		ar := NewTargetResolver(r, "")

		// Create a directory and make it unreadable
		unreadableDir := filepath.Join(r.rootDirectory, "unreadable")
		require.NoError(t, os.MkdirAll(unreadableDir, 0o755))
		schemaPath := filepath.Join(unreadableDir, "some_1_0_0.schema.json")
		require.NoError(t, os.WriteFile(schemaPath, []byte("{}"), 0o600))

		require.NoError(t, os.Chmod(unreadableDir, 0o000))
		defer func() { _ = os.Chmod(unreadableDir, 0o755) }()
		// ResolveScopeToSingleKey should hit the error in searcher.Schemas
		_, err := ar.ResolveScopeToSingleKey(context.Background(), "unreadable", "unreadable")
		require.Error(t, err)
	})

	t.Run("ResolveScopeToSingleKey multiple matches", func(t *testing.T) {
		t.Parallel()
		// Use a separate registry to avoid interfering with other tests
		r2 := setupTestRegistry(t)
		createSchemaFiles(t, r2, schemaMap{
			Key("domain_family_1_0_0"): `{"type": "object"}`,
			Key("domain_family_2_0_0"): `{"type": "object"}`,
		})
		ar := NewTargetResolver(r2, "")
		_, err := ar.ResolveScopeToSingleKey(context.Background(), "domain", "domain")
		require.Error(t, err)
		assert.ErrorAs(t, err, new(*TargetArgumentTargetsMultipleSchemasError))
	})
}
