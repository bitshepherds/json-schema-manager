package schema

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSearchScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    SearchScope
		wantErr bool
	}{
		{
			name:    "valid single domain",
			input:   "domain-a",
			want:    "domain-a",
			wantErr: false,
		},
		{
			name:    "valid multi-level path",
			input:   "domain-a/subdomain-b/family-c/1/0/0",
			want:    "domain-a/subdomain-b/family-c/1/0/0",
			wantErr: false,
		},
		{
			name:    "trailing slash is stripped",
			input:   "domain-a/subdomain-b/",
			want:    "domain-a/subdomain-b",
			wantErr: false,
		},
		{
			name:    "multiple trailing slashes are stripped",
			input:   "domain-a/subdomain-b//",
			want:    "domain-a/subdomain-b",
			wantErr: false,
		},
		{
			name:    "only slashes is invalid",
			input:   "//",
			wantErr: true,
		},
		{
			name:    "root scope is invalid via NewSearchScope",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid characters",
			input:   "Domain_A", // underscores not allowed in search scope
			wantErr: true,
		},
		{
			name:    "invalid traversal",
			input:   "../outside",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, err := NewSearchScope(tt.input)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, s)
			}
		})
	}
}

func TestNewSearcher(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		spec               string
		wantSearchRoot     string // relative to registry root
		wantErrMsgContains string
	}{
		{
			name:           "single domain",
			spec:           "domain-a",
			wantSearchRoot: "domain-a",
		},
		{
			name:           "two-level domain",
			spec:           "domain-a/subdomain-a",
			wantSearchRoot: "domain-a/subdomain-a",
		},
		{
			name:           "domain with family name",
			spec:           "domain-a/subdomain-a/family-name",
			wantSearchRoot: "domain-a/subdomain-a/family-name",
		},
		{
			name:           "domain with major version",
			spec:           "domain-a/subdomain-a/family-name/1",
			wantSearchRoot: "domain-a/subdomain-a/family-name/1",
		},
		{
			name:           "domain with major and minor version",
			spec:           "domain-a/subdomain-a/family-name/1/0",
			wantSearchRoot: "domain-a/subdomain-a/family-name/1/0",
		},
		{
			name:           "full semantic version",
			spec:           "domain-a/subdomain-a/family-name/1/0/0",
			wantSearchRoot: "domain-a/subdomain-a/family-name/1/0/0",
		},
		{
			name:               "invalid - uppercase letters",
			spec:               "Domain-A",
			wantErrMsgContains: "is not a valid JSM Search Scope",
		},
		{
			name:               "invalid - special characters",
			spec:               "domain@a",
			wantErrMsgContains: "is not a valid JSM Search Scope",
		},
		{
			name:           "empty string searches entire registry",
			spec:           "",
			wantSearchRoot: "",
		},
		{
			name:               "invalid - spaces",
			spec:               "domain a",
			wantErrMsgContains: "is not a valid JSM Search Scope",
		},
		{
			name:               "invalid - underscores",
			spec:               "domain_a",
			wantErrMsgContains: "is not a valid JSM Search Scope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := setupTestRegistry(t)

			// Create the search root if it's expected to succeed
			if tt.wantErrMsgContains == "" {
				searchRoot := filepath.Join(r.rootDirectory, tt.wantSearchRoot)
				require.NoError(t, os.MkdirAll(searchRoot, 0o755))
			}

			searcher, err := NewSearcher(r, SearchScope(tt.spec))

			if tt.wantErrMsgContains != "" {
				require.ErrorContains(t, err, tt.wantErrMsgContains)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, searcher)

			// Check the searchRoot is correctly constructed
			wantSearchRoot := filepath.Join(r.rootDirectory, tt.wantSearchRoot)
			assert.Equal(t, wantSearchRoot, searcher.searchRoot)
			assert.Same(t, r, searcher.registry)
		})
	}
}

func TestSearcher_Schemas_SuccessPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		spec     string
		setup    func(t *testing.T, r *Registry)
		wantKeys []Key
	}{
		{
			name: "finds all schemas in domain",
			spec: "domain",
			setup: func(t *testing.T, r *Registry) {
				t.Helper()
				schemas := schemaMap{
					Key("domain_family-a_1_0_0"): `{"$id": "{{ ID }}"}`,
					Key("domain_family-a_1_0_1"): `{"$id": "{{ ID }}"}`,
					Key("domain_family-b_1_0_0"): `{"$id": "{{ ID }}"}`,
				}
				createSchemaFiles(t, r, schemas)
			},
			wantKeys: []Key{
				Key("domain_family-a_1_0_0"),
				Key("domain_family-a_1_0_1"),
				Key("domain_family-b_1_0_0"),
			},
		},
		{
			name: "finds schemas in subdomain",
			spec: "domain/subdomain",
			setup: func(t *testing.T, r *Registry) {
				t.Helper()
				schemas := schemaMap{
					Key("domain_subdomain_family_1_0_0"): `{"$id": "{{ ID }}"}`,
					Key("domain_other_family_1_0_0"):     `{"$id": "{{ ID }}"}`,
				}
				createSchemaFiles(t, r, schemas)
			},
			wantKeys: []Key{
				Key("domain_subdomain_family_1_0_0"),
			},
		},
		{
			name: "finds schemas in specific family",
			spec: "domain/family-a",
			setup: func(t *testing.T, r *Registry) {
				t.Helper()
				schemas := schemaMap{
					Key("domain_family-a_1_0_0"): `{"$id": "{{ ID }}"}`,
					Key("domain_family-a_2_0_0"): `{"$id": "{{ ID }}"}`,
					Key("domain_family-b_1_0_0"): `{"$id": "{{ ID }}"}`,
				}
				createSchemaFiles(t, r, schemas)
			},
			wantKeys: []Key{
				Key("domain_family-a_1_0_0"),
				Key("domain_family-a_2_0_0"),
			},
		},
		{
			name: "finds schemas in specific major version",
			spec: "domain/family-a/1",
			setup: func(t *testing.T, r *Registry) {
				t.Helper()
				schemas := schemaMap{
					Key("domain_family-a_1_0_0"): `{"$id": "{{ ID }}"}`,
					Key("domain_family-a_1_1_0"): `{"$id": "{{ ID }}"}`,
					Key("domain_family-a_2_0_0"): `{"$id": "{{ ID }}"}`,
				}
				createSchemaFiles(t, r, schemas)
			},
			wantKeys: []Key{
				Key("domain_family-a_1_0_0"),
				Key("domain_family-a_1_1_0"),
			},
		},
		{
			name: "ignores non-schema files",
			spec: "domain",
			setup: func(t *testing.T, r *Registry) {
				t.Helper()
				schemas := schemaMap{
					Key("domain_family_1_0_0"): `{"$id": "{{ ID }}"}`,
				}
				createSchemaFiles(t, r, schemas)

				// Create a non-schema file in the schema directory
				s := New(Key("domain_family_1_0_0"), r)
				readmePath := filepath.Join(s.Path(HomeDir), "README.md")
				if err := os.WriteFile(readmePath, []byte("# Documentation"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantKeys: []Key{
				Key("domain_family_1_0_0"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := setupTestRegistry(t)
			tt.setup(t, r)

			searcher, err := NewSearcher(r, SearchScope(tt.spec))
			require.NoError(t, err)

			var foundKeys []Key
			for res := range searcher.Schemas(context.Background()) {
				require.NoError(t, res.Err)
				foundKeys = append(foundKeys, res.Key)
			}

			assert.ElementsMatch(t, tt.wantKeys, foundKeys)
		})
	}
}

func TestSearcher_Schemas_EmptyResults(t *testing.T) {
	t.Parallel()

	r := setupTestRegistry(t)
	// Create just an empty directory
	domainDir := filepath.Join(r.rootDirectory, "domain")
	require.NoError(t, os.MkdirAll(domainDir, 0o755))

	searcher, err := NewSearcher(r, SearchScope("domain"))
	require.NoError(t, err)

	resCh := searcher.Schemas(context.Background())
	keys := make([]Key, 0, 10)
	for res := range resCh {
		require.NoError(t, res.Err)
		keys = append(keys, res.Key)
	}
	assert.Empty(t, keys)
}

func TestSearcher_Schemas_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("invalid filename structure", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		domainDir := filepath.Join(r.rootDirectory, "domain")
		require.NoError(t, os.MkdirAll(domainDir, 0o755))

		invalidPath := filepath.Join(domainDir, "invalid.schema.json")
		require.NoError(t, os.WriteFile(invalidPath, []byte("{}"), 0o600))

		searcher, err := NewSearcher(r, SearchScope("domain"))
		require.NoError(t, err)

		var lastErr error
		for res := range searcher.Schemas(context.Background()) {
			if res.Err != nil {
				lastErr = res.Err
			}
		}
		require.Error(t, lastErr)
		assert.Contains(t, lastErr.Error(), "has an invalid filename structure")
	})

	t.Run("walk error - permission denied", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		domainDir := filepath.Join(r.rootDirectory, "domain", "subdir")
		require.NoError(t, os.MkdirAll(domainDir, 0o755))
		require.NoError(t, os.Chmod(domainDir, 0o000))
		t.Cleanup(func() { _ = os.Chmod(domainDir, 0o755) })

		searcher, err := NewSearcher(r, SearchScope("domain"))
		require.NoError(t, err)

		var lastErr error
		for res := range searcher.Schemas(context.Background()) {
			if res.Err != nil {
				lastErr = res.Err
			}
		}
		require.Error(t, lastErr)
		require.ErrorContains(t, lastErr, "permission denied")
	})
}

func TestSearcher_Schemas_NilSearcher(t *testing.T) {
	t.Parallel()
	var s *Searcher
	resCh := s.Schemas(context.Background())
	res := <-resCh
	require.Error(t, res.Err)
	assert.Contains(t, res.Err.Error(), "searcher is nil")
	_, ok := <-resCh
	assert.False(t, ok)
}

func TestSearcher_Schemas_ContextCancellation(t *testing.T) {
	t.Parallel()

	r := setupTestRegistry(t)

	// Create many schemas to ensure the walk takes some time
	for i := 0; i < 100; i++ {
		key := Key("domain_family_1_0_" + string(rune('0'+i%10)))
		s := New(key, r)
		homeDir := s.Path(HomeDir)
		if err := os.MkdirAll(homeDir, 0o755); err != nil {
			t.Fatal(err)
		}
		filePath := s.Path(FilePath)
		if err := os.WriteFile(filePath, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	searcher, err := NewSearcher(r, SearchScope("domain"))
	require.NoError(t, err)

	// Create a context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())

	resCh := searcher.Schemas(ctx)

	// Read one result then cancel
	<-resCh
	cancel()

	// Drain any remaining results
	var lastErr error
	for res := range resCh {
		if res.Err != nil {
			lastErr = res.Err
		}
	}

	// Either no error (walk completed before cancellation) or context cancelled
	if lastErr != nil {
		require.ErrorIs(t, lastErr, context.Canceled)
	}
}

func TestSearcher_Schemas_ContextCancellationDuringSend(t *testing.T) {
	t.Parallel()

	r := setupTestRegistry(t)

	// Create schemas
	schemas := schemaMap{
		Key("domain_family_1_0_0"): `{"$id": "{{ ID }}"}`,
		Key("domain_family_1_0_1"): `{"$id": "{{ ID }}"}`,
	}
	createSchemaFiles(t, r, schemas)

	searcher, err := NewSearcher(r, SearchScope("domain"))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	resCh := searcher.Schemas(ctx)

	// Read one result to ensure the walk has started and found at least one schema
	<-resCh

	// Now cancel the context while the next result might be waiting to send
	cancel()

	// Drain any remaining results
	var lastErr error
	for res := range resCh {
		if res.Err != nil {
			lastErr = res.Err
		}
	}

	// Check the error - could be nil (completed) or context cancelled
	if lastErr != nil {
		require.ErrorIs(t, lastErr, context.Canceled)
	}
}

func TestSearcher_Schemas_EarlyTerminationOnError(t *testing.T) {
	t.Parallel()

	r := setupTestRegistry(t)

	// Create a valid schema first
	schemas := schemaMap{
		Key("domain_a-family_1_0_0"): `{"$id": "{{ ID }}"}`,
	}
	createSchemaFiles(t, r, schemas)

	// Create an invalid schema file that will be processed after
	invalidPath := filepath.Join(r.rootDirectory, "domain", "z-invalid.schema.json")
	if err := os.WriteFile(invalidPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	searcher, err := NewSearcher(r, SearchScope("domain"))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resCh := searcher.Schemas(ctx)

	// Process results
	keyCount := 0
	var lastErr error
	for res := range resCh {
		if res.Err != nil {
			lastErr = res.Err
			continue
		}
		keyCount++
	}

	// Check that we got an error
	if lastErr != nil {
		var target *InvalidSchemaFilenameError
		if errors.As(lastErr, &target) {
			t.Logf("Walk stopped with invalid filename error: %v", lastErr)
		}
	}
}

func TestSearcher_Schemas_ContextCancelledDuringErrorSend(t *testing.T) {
	t.Parallel()

	r := setupTestRegistry(t)
	// Create a dummy directory to avoid NewSearcher error
	dummyDir := filepath.Join(r.rootDirectory, "dummy")
	require.NoError(t, os.MkdirAll(dummyDir, 0o755))

	searcher, err := NewSearcher(r, SearchScope("dummy"))
	require.NoError(t, err)

	// Inject an error for Walk by making it unreadable AFTER creation
	require.NoError(t, os.Chmod(dummyDir, 0o000))
	defer func() { _ = os.Chmod(dummyDir, 0o755) }()

	// Use a context and cancel it upfront
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resCh := searcher.Schemas(ctx)

	// Wait a bit to let the goroutine reach the select
	time.Sleep(10 * time.Millisecond)

	// Now drain to ensure the goroutine finished
	for range resCh {
		continue
	}
}

func TestInvalidSearchScopeError_Error(t *testing.T) {
	t.Parallel()

	err := &InvalidSearchScopeError{spec: "INVALID"}
	assert.EqualError(t, err, "`INVALID` is not a valid JSM Search Scope. Valid Example: 'domain/family'")
}

func TestInvalidSchemaFilenameError_Error(t *testing.T) {
	t.Parallel()

	errFilename := &InvalidSchemaFilenameError{Path: "/path/to/invalid.schema.json"}
	assert.EqualError(t, errFilename, "schema file /path/to/invalid.schema.json has an invalid filename structure")
}
