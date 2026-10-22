package schema

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/internal/validator"
)

func TestNew(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		key             Key
		registryRootDir string
		wantDomain      []string
		wantFamilyName  string
		wantVersion     SemVer
	}{
		{
			name:            "Single domain",
			key:             Key("domain_family_1_0_0"),
			registryRootDir: "/test/registry",
			wantDomain:      []string{"domain"},
			wantFamilyName:  "family",
			wantVersion:     SemVer{1, 0, 0},
		},
		{
			name:            "Multiple domains",
			key:             Key("org_team_project-a_a-family_1_2_3"),
			registryRootDir: "/another/path",
			wantDomain:      []string{"org", "team", "project-a"},
			wantFamilyName:  "a-family",
			wantVersion:     SemVer{1, 2, 3},
		},
		{
			name:            "With hyphens",
			key:             Key("my-domain_my-family_0_0_1"),
			registryRootDir: "/registry",
			wantDomain:      []string{"my-domain"},
			wantFamilyName:  "my-family",
			wantVersion:     SemVer{0, 0, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &Registry{}
			s := New(tt.key, r)

			// Check registry
			if s.registry != r {
				t.Errorf("registry = %v, want %v", s.registry, r)
			}

			// Check domain (via the Key accessor)
			gotDomain := s.Key().Domain()
			if !slices.Equal(gotDomain, tt.wantDomain) {
				t.Errorf("domain = %v, want %v", gotDomain, tt.wantDomain)
			}

			// Check family name (via the Key accessor)
			gotFamilyName := s.Key().FamilyName()
			if gotFamilyName != tt.wantFamilyName {
				t.Errorf("familyName = %v, want %v", gotFamilyName, tt.wantFamilyName)
			}

			// Check version numbers (via the Key accessors)
			v := s.core.version
			if v != tt.wantVersion {
				t.Errorf("version = %v, want %v", v, tt.wantVersion)
			}

			// Check exists flag
			if s.exists {
				t.Errorf("exists = %v, want %v", s.exists, false)
			}
		})
	}
}

func TestNewReleaseType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input   string
		want    ReleaseType
		wantErr bool
	}{
		{"major", ReleaseTypeMajor, false},
		{"minor", ReleaseTypeMinor, false},
		{"patch", ReleaseTypePatch, false},
		{"MAJOR", ReleaseTypeMajor, false},
		{"Minor", ReleaseTypeMinor, false},
		{"pAtCh", ReleaseTypePatch, false},
		{"invalid", "", true},
		{"", "", true},
		{"foo", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, err := NewReleaseType(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				//nolint:testifylint // IsType is appropriate for table-driven tests with interface{}
				assert.IsType(t, &InvalidReleaseTypeError{}, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// createRegistry is a helper func to create a registry from the passed config.
func createRegistry(t *testing.T, tmpDir, cfgData string) *Registry {
	t.Helper()
	if err := os.WriteFile(filepath.Join(tmpDir, config.JsmRegistryConfigFile), []byte(cfgData), 0o600); err != nil {
		t.Fatalf("failed to write registry config file: %v", err)
	}
	compiler := &mockCompiler{}
	r, err := NewRegistry(tmpDir, compiler, fsh.NewPathResolver(), fsh.NewEnvProvider())
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	return r
}

func TestLoad(t *testing.T) {
	t.Parallel()

	const loadKey = Key("domain_family_1_0_0") // The key we pass to the load function

	const configData = `
environments:
  prod:
    publicUrlRoot: https://json-schema.example.com
    privateUrlRoot: "https://private.json-schema.example.com"
    allowSchemaMutation: false
    isProduction: true
`

	// explicitPublicSchema is a schema with an x-public property set to true

	explicitPublicSchema := `{
"$schema": "http://json-schema.org/draft-07/schema#",
"$id": "{{ ID }}",
"type": "object",
"x-public": true
}`

	// explicitPrivateSchema is a schema with an x-public property set to false
	explicitPrivateSchema := `{
"$schema": "http://json-schema.org/draft-07/schema#",
"$id": "{{ ID }}",
"type": "object",
"x-public": false
}`
	// brokenXPublicSchema is a schema with an x-public property set to an invalid value

	brokenXPublicSchema := `{
"$schema": "http://json-schema.org/draft-07/schema#",
"$id": "{{ ID }}",
"type": "object",
"x-public": "invalid"
}`

	// implicitPrivateSchema is a schema without an x-public property - it should be treated as private

	implicitPrivateSchema := `{
"$schema": "http://json-schema.org/draft-07/schema#",
"$id": "{{ ID }}",
"type": "object"
}`

	tests := []struct {
		name               string
		schemas            schemaMap
		wantIsPublic       bool
		wantErrMsg         string
		wantErrMsgContains string
	}{
		{
			name: "Load public production schema (x-public: true)",
			schemas: schemaMap{
				loadKey: explicitPublicSchema,
			},
			wantIsPublic: true,
		},
		{
			name: "Load private production schema (x-public: false)",
			schemas: schemaMap{
				loadKey: explicitPrivateSchema,
			},
			wantIsPublic: false,
		},
		{
			name: "Load private production schema (x-public missing)",
			schemas: schemaMap{
				loadKey: implicitPrivateSchema,
			},
			wantIsPublic: false,
		},
		{
			name:               "Schema file is missing",
			schemas:            schemaMap{},
			wantErrMsgContains: "no such file or directory",
		},
		{
			name: "is-public is an invalid value",
			schemas: schemaMap{
				loadKey: brokenXPublicSchema,
			},
			wantErrMsgContains: "contains invalid x-public property",
		},
		{
			name: "schema is not a valid JSON Document",
			schemas: schemaMap{
				loadKey: `{ // not a valid JSON document`,
			},
			wantErrMsgContains: "is not valid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup the temporary filesystem with the schemas needed for the test:
			tmpDir := t.TempDir()

			r := createRegistry(t, tmpDir, configData)
			createSchemaFiles(t, r, tt.schemas)

			schema, err := Load(loadKey, r)

			// Check error expectation
			if tt.wantErrMsg != "" {
				assert.EqualError(t, err, tt.wantErrMsg)
				return
			}
			if tt.wantErrMsgContains != "" {
				require.ErrorContains(t, err, tt.wantErrMsgContains)
				return
			}
			require.NoError(t, err)

			// Check schema was returned
			assert.NotNil(t, schema)

			// Check isPublic flag
			assert.Equal(t, tt.wantIsPublic, schema.isPublic)
		})
	}
}

func TestRendered(t *testing.T) {
	t.Parallel()

	const loadKey = Key("domain_family_1_0_0") // The key we pass to the load function

	// root URLs which use subdomains
	const pubDomainRoot = "https://json-schema.example.com"
	const privDomainRoot = "https://private.json-schema.example.com"
	const pubDevDomainRoot = "https://dev.json-schema.example.com"
	const privDevDomainRoot = "https://dev.private.json-schema.example.com"

	// root URLs which use paths
	const pubPathRoot = "https://example.com/schema/json-schemas"
	const privPathRoot = "https://example.com/schemas/private"
	const pubDevPathRoot = "https://dev.example.com/schema/json-schemas"
	const privDevPathRoot = "https://dev.private.example.com/schemas/private"

	const domainConfigData = `
environments:
  dev:
    publicUrlRoot: ` + pubDevDomainRoot + `
    privateUrlRoot: ` + privDevDomainRoot + `
    allowSchemaMutation: true
    isProduction: false
  prod:
    publicUrlRoot: ` + pubDomainRoot + `
    privateUrlRoot: ` + privDomainRoot + `
    allowSchemaMutation: false
    isProduction: true
`
	const pathsConfigData = `
environments:
  dev:
    publicUrlRoot: ` + pubDevPathRoot + `
    privateUrlRoot: ` + privDevPathRoot + `
    allowSchemaMutation: true
    isProduction: false
  prod:
    publicUrlRoot: ` + pubPathRoot + `
    privateUrlRoot: ` + privPathRoot + `
    allowSchemaMutation: false
    isProduction: true
`

	// explicitPublicSchema is a schema with an x-public property set to true
	explicitPublicSchema := `{
"$schema": "http://json-schema.org/draft-07/schema#",
"$id": "{{ ID }}",
"type": "object",
"x-public": true
}`

	// explicitPrivateSchema is a schema with an x-public property set to false
	explicitPrivateSchema := `{
"$schema": "http://json-schema.org/draft-07/schema#",
"$id": "{{ ID }}",
"type": "object",
"x-public": false
}`

	brokenTemplateSchema := `{
"$schema": "http://json-schema.org/draft-07/schema#",
"$id": "{{ ID }",
"type": "object"
}`

	invalidTemplateArgTypeSchema := `{
"$schema": "http://json-schema.org/draft-07/schema#",
"$id": "{{ JSM 123 }}",
"type": "object"
}`

	// implicitPrivateSchema is a schema without an x-public property - it should be treated as private
	implicitPrivateSchema := `{
"$schema": "http://json-schema.org/draft-07/schema#",
"$id": "{{ ID }}",
"type": "object"
}`

	// rootSchema is a schema which has a $ref to subschema-a
	rootSchema := `{
"$schema": "http://json-schema.org/draft-07/schema#",
"$id": "{{ ID }}",
"type": "object",
"properties": {
	"subschema-a": {
		"$ref": "{{ JSM %%domain_subschema-a_1_0_0%% }}"
	},
	"non-jsm-subschema": {
		"$ref": "https://schemas.other-org.com/example.schema.json"
	}
}
}`

	// subschemaA is a schema which has a $ref to subschema-b
	subschemaA := `{
"$schema": "http://json-schema.org/draft-07/schema#",
"$id": "{{ ID }}",
"type": "object",
"properties": {
	"subschema-b": {
		"$ref": "{{ JSM %%domain_subschema-b_1_0_0%% }}"
	}
}
}`

	subschemaAInvalidJSMKey := `{
"$schema": "http://json-schema.org/draft-07/schema#",
"$id": "{{ ID }}",
"type": "object",
"properties": {
	"subschema-b": {
		"$ref": "{{ JSM %%not-a-valid-key%% }}"
	}
}
}`

	subschemaB := `{
"$schema": "http://json-schema.org/draft-07/schema#",
"$id": "{{ ID }}",
"type": "object",
"properties": {
	"bValue": {
		"type": "string"
	}
}
}`

	tests := []struct {
		name               string
		schemas            schemaMap
		configData         string
		env                config.Env
		wantIsPublic       bool
		wantErrMsgContains string
		wantID             string
		wantContent        string
		wantContentPath    string
	}{
		{
			name: "Load public production schema (x-public: true)",
			schemas: schemaMap{
				loadKey: explicitPublicSchema,
			},
			wantIsPublic: true,
			wantID:       pubDomainRoot + "/" + string(loadKey) + SchemaSuffix,
		},
		{
			name: "Load private production schema (x-public: false)",
			schemas: schemaMap{
				loadKey: explicitPrivateSchema,
			},
			wantIsPublic: false,
			wantID:       privDomainRoot + "/" + string(loadKey) + SchemaSuffix,
		},
		{
			name: "Load private production schema (x-public missing)",
			schemas: schemaMap{
				loadKey: implicitPrivateSchema,
			},
			wantIsPublic: false,
			wantID:       privDomainRoot + "/" + string(loadKey) + SchemaSuffix,
		},
		{
			name: "Load private production schema with path URL base",
			schemas: schemaMap{
				loadKey: implicitPrivateSchema,
			},
			configData:   pathsConfigData,
			wantIsPublic: false,
			wantID:       privPathRoot + "/" + string(loadKey) + SchemaSuffix,
		},
		{
			name: "Load private non-production schema",
			schemas: schemaMap{
				loadKey: implicitPrivateSchema,
			},
			env:          config.Env("dev"),
			wantIsPublic: false,
			wantID:       privDevDomainRoot + "/" + string(loadKey) + SchemaSuffix,
		},
		{
			name: "Load schema with cascading $refs to JSM schemas, domain URL base",
			schemas: schemaMap{
				loadKey:                         rootSchema,
				Key("domain_subschema-a_1_0_0"): subschemaA,
				Key("domain_subschema-b_1_0_0"): subschemaB,
			},
			wantID:          privDomainRoot + "/" + string(loadKey) + SchemaSuffix,
			wantContentPath: "properties.subschema-a.$ref",
			wantContent:     privDomainRoot + `/` + "domain_subschema-a_1_0_0" + SchemaSuffix,
		},
		{
			name:       "Load schema with cascading $refs to JSM schemas, path URL base",
			configData: pathsConfigData,
			schemas: schemaMap{
				loadKey:                         rootSchema,
				Key("domain_subschema-a_1_0_0"): subschemaA,
				Key("domain_subschema-b_1_0_0"): subschemaB,
			},
			wantID:          privPathRoot + "/" + string(loadKey) + SchemaSuffix,
			wantContentPath: "properties.subschema-a.$ref",
			wantContent:     privPathRoot + `/` + "domain_subschema-a_1_0_0" + SchemaSuffix,
		},
		{
			name: "A JSM schema $ref has an invalid key",
			schemas: schemaMap{
				loadKey:                         rootSchema,
				Key("domain_subschema-a_1_0_0"): subschemaAInvalidJSMKey,
				Key("domain_subschema-b_1_0_0"): subschemaB,
			},
			wantErrMsgContains: "has an invalid <schema key>: not-a-valid-key",
		},
		{
			name: "A JSM schema $ref resolves to a missing schema",
			schemas: schemaMap{
				loadKey:                         rootSchema,
				Key("domain_subschema-a_1_0_0"): subschemaA,
				// no subschema-b
			},
			wantErrMsgContains: "A $ref to a JSM schema ({{ JSM `domain_subschema-b_1_0_0` }}) could not be loaded",
		},
		{
			name: "the env is not valid",
			schemas: schemaMap{
				loadKey: implicitPrivateSchema,
			},
			env:                config.Env("invalid-env"),
			wantErrMsgContains: "does not define environment",
		},
		{
			name: "there is a go template syntax error",
			schemas: schemaMap{
				loadKey: brokenTemplateSchema,
			},
			wantErrMsgContains: "has a template syntax error",
		},
		{
			name: "there is a template argument type error",
			schemas: schemaMap{
				loadKey: invalidTemplateArgTypeSchema,
			},
			wantErrMsgContains: "cannot be rendered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup the temporary filesystem with the schemas needed for the test:
			tmpDir := t.TempDir()

			cfgData := domainConfigData
			if tt.configData != "" {
				cfgData = tt.configData
			}
			r := createRegistry(t, tmpDir, cfgData)
			createSchemaFiles(t, r, tt.schemas)

			schema, err := Load(loadKey, r)
			if err != nil {
				if tt.wantErrMsgContains != "" {
					require.ErrorContains(t, err, tt.wantErrMsgContains)
					return
				}
				t.Fatalf("failed to load schema: %v", err)
			}
			assert.NotNil(t, schema)

			env := tt.env
			if env == "" {
				env = config.Env("prod")
			}

			envConfig, err := r.config.EnvConfig(env)
			if err != nil {
				if tt.wantErrMsgContains != "" {
					require.ErrorContains(t, err, tt.wantErrMsgContains)
					return
				}
				t.Fatalf("failed to get env config: %v", err)
			}

			ri, err := schema.Render(envConfig)

			if tt.wantErrMsgContains != "" {
				require.ErrorContains(t, err, tt.wantErrMsgContains)
				return
			}
			require.NoError(t, err)

			rBytes := ri.Rendered
			assert.Equal(t, ri, schema.computed.RenderInfo(env))

			if tt.wantID != "" {
				var doc map[string]interface{}
				require.NoError(t, json.Unmarshal(rBytes, &doc))
				assert.Equal(t, tt.wantID, doc["$id"])
			}
			if tt.wantContent != "" && tt.wantContentPath != "" {
				// Use GJSON to query the nested $ref property
				refValue := gjson.GetBytes(rBytes, tt.wantContentPath)
				assert.True(t, refValue.Exists())
				assert.Equal(t, tt.wantContent, refValue.String())
			}

			// Finally, check that calling again will return the cached result:
			ri2, err := schema.Render(envConfig)
			require.NoError(t, err)
			assert.Equal(t, ri, ri2)
			assert.Equal(t, rBytes, ri2.Rendered)
		})
	}
}

func TestBumpVersion(t *testing.T) {
	t.Parallel()

	const configData = `
environments:
  prod:
    publicUrlRoot: https://p
    privateUrlRoot: https://pr
    isProduction: true
`

	existingVersions := [][3]uint{
		{1, 0, 0},
		{1, 0, 1},
		{1, 0, 2},
		{1, 1, 0},
		{1, 1, 1},
		{1, 1, 2},
		{1, 1, 3},
		{1, 1, 4},
		{1, 2, 0},
		{1, 2, 1},
		{1, 2, 2},
		{1, 2, 3},
		{2, 0, 0},
		{2, 0, 1},
		{2, 0, 2},
	}

	tests := []struct {
		name        string
		releaseType ReleaseType
		initVersion SemVer
		wantVersion SemVer
		setupFunc   func(t *testing.T, familyDir string)
	}{
		{
			name:        "1.0.0 Major bump",
			releaseType: ReleaseTypeMajor,
			initVersion: SemVer{1, 0, 0},
			wantVersion: SemVer{3, 0, 0},
		},
		{
			name:        "1.0.0 Minor bump",
			releaseType: ReleaseTypeMinor,
			initVersion: SemVer{1, 0, 0},
			wantVersion: SemVer{1, 3, 0},
		},
		{
			name:        "1.0.0 Patch bump",
			releaseType: ReleaseTypePatch,
			initVersion: SemVer{1, 0, 0},
			wantVersion: SemVer{1, 0, 3},
		},
		{
			name:        "1.2.3 Major bump",
			releaseType: ReleaseTypeMajor,
			initVersion: SemVer{1, 2, 3},
			wantVersion: SemVer{3, 0, 0},
		},
		{
			name:        "1.2.3 Minor bump",
			releaseType: ReleaseTypeMinor,
			initVersion: SemVer{1, 2, 3},
			wantVersion: SemVer{1, 3, 0},
		},
		{
			name:        "1.2.3 Patch bump",
			releaseType: ReleaseTypePatch,
			initVersion: SemVer{1, 2, 3},
			wantVersion: SemVer{1, 2, 4},
		},
		{
			name:        "Cannot read directory",
			releaseType: ReleaseTypeMajor,
			initVersion: SemVer{1, 0, 0},
			wantVersion: SemVer{0, 0, 0},
			setupFunc: func(t *testing.T, familyDir string) {
				t.Helper()
				// Make family dir unreadable
				require.NoError(t, os.Chmod(familyDir, 0o000))
				t.Cleanup(func() { require.NoError(t, os.Chmod(familyDir, 0o755)) })
			},
		},
		{
			name:        "Non-directories should be ignored",
			releaseType: ReleaseTypeMajor,
			initVersion: SemVer{1, 0, 0},
			// Existing versions max major is 2. Next major should be 3.
			wantVersion: SemVer{3, 0, 0},
			setupFunc: func(t *testing.T, familyDir string) {
				t.Helper()
				// Add a file that should be ignored
				require.NoError(t, os.WriteFile(filepath.Join(familyDir, "README.md"), []byte("docs"), 0o600))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup temporary registry
			tmpDir := t.TempDir()

			// Create the family directory
			domainDir := filepath.Join(tmpDir, "domain")
			familyDir := filepath.Join(domainDir, "family")
			if err := os.MkdirAll(familyDir, 0o755); err != nil {
				t.Fatal(err)
			}

			// createVersion creates a dummy version in the schema family.
			createVersion := func(major, minor, patch uint) {
				dir := filepath.Join(familyDir,
					strconv.FormatUint(uint64(major), 10),
					strconv.FormatUint(uint64(minor), 10),
					strconv.FormatUint(uint64(patch), 10),
				)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				filename := fmt.Sprintf("domain_family_%d_%d_%d.schema.json", major, minor, patch)
				if err := os.WriteFile(filepath.Join(dir, filename), []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			for _, v := range existingVersions {
				createVersion(v[0], v[1], v[2])
			}

			// Run custom setup loop if provided
			if tt.setupFunc != nil {
				tt.setupFunc(t, familyDir)
			}

			// NewRegistry requires a config file.
			if err := os.WriteFile(
				filepath.Join(tmpDir, config.JsmRegistryConfigFile),
				[]byte(configData),
				0o600,
			); err != nil {
				t.Fatal(err)
			}

			compiler := &mockCompiler{}
			registry, err := NewRegistry(tmpDir, compiler, fsh.NewPathResolver(), fsh.NewEnvProvider())
			require.NoError(t, err)

			// Construct a schema with necessary fields
			s := &Schema{
				core: &Core{
					domain:     []string{"domain"},
					familyName: "family",
					version:    tt.initVersion,
				},
				registry: registry,
			}

			// Simulate computed items before bumping version
			s.computed = Computed{
				key: Key("domain_family_1_0_0"),
			}

			s.BumpVersion(tt.releaseType)

			// Check the computed items have been cleared
			assert.Equal(t, s.computed.key, Key(""))

			v := s.core.version
			if v != tt.wantVersion {
				t.Errorf("version = %v, want %v", v, tt.wantVersion)
			}
		})
	}
}

func TestFilename(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		key          Key
		wantFilename string
	}{
		{
			name:         "Simple schema key",
			key:          Key("domain_family_1_0_0"),
			wantFilename: "domain_family_1_0_0.schema.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &Registry{}
			s := New(tt.key, r)
			assert.Equal(t, tt.wantFilename, s.Filename())
		})
	}
}

func TestWriteNewSchemaFiles(t *testing.T) {
	t.Parallel()

	const configData = `
environments:
  prod:
    publicUrlRoot: https://json-schema.example.com
    privateUrlRoot: https://private.json-schema.example.com
    allowSchemaMutation: false
    isProduction: true
`

	tests := []struct {
		name            string
		key             Key
		wantDirName     string
		wantFile        string
		setupFunc       func(t *testing.T, s *Schema)
		wantErr         bool
		wantErrContains string
	}{
		{
			name:        "Create new schema for single domain",
			key:         Key("domain_family_1_0_0"),
			wantDirName: "1/0/0",
			wantFile:    "domain_family_1_0_0.schema.json",
		},
		{
			name:        "Create new schema for multi-domain",
			key:         Key("org_team_family_2_1_3"),
			wantDirName: "2/1/3",
			wantFile:    "org_team_family_2_1_3.schema.json",
		},
		{
			name: "The home directory cannot be written to",
			key:  Key("domain_family_1_0_0"),
			setupFunc: func(t *testing.T, s *Schema) {
				t.Helper()
				homeDir := s.Path(HomeDir)
				parentDir := filepath.Dir(homeDir)

				// Create the parent directory
				require.NoError(t, os.MkdirAll(parentDir, 0o755))

				// Make it read-only so subdirectories cannot be created
				require.NoError(t, os.Chmod(parentDir, 0o500))

				// Ensure we revert permissions so t.TempDir can clean up
				t.Cleanup(func() {
					require.NoError(t, os.Chmod(parentDir, 0o755))
				})
			},
			wantErr:         true,
			wantErrContains: "permission denied",
		},
		{
			name: "A folder already exists with a name that matches the schema file name",
			key:  Key("domain_family_1_0_0"),
			setupFunc: func(t *testing.T, s *Schema) {
				t.Helper()
				// Create home directory first
				homeDir := s.Path(HomeDir)
				require.NoError(t, os.MkdirAll(homeDir, 0o755))
				// Create a directory where the schema file should be
				schemaPath := s.Path(FilePath)
				require.NoError(t, os.Mkdir(schemaPath, 0o755))
			},
			wantErr:         true,
			wantErrContains: "is a directory",
		},
		{
			name: "A file called pass already exists",
			key:  Key("domain_family_1_0_0"),
			setupFunc: func(t *testing.T, s *Schema) {
				t.Helper()
				// Create home directory first
				homeDir := s.Path(HomeDir)
				require.NoError(t, os.MkdirAll(homeDir, 0o755))
				// Create a file where the pass directory should be
				passFile := filepath.Join(homeDir, string(TestDocTypePass))
				require.NoError(t, os.WriteFile(passFile, []byte("blocking"), 0o600))
			},
			wantErr:         true,
			wantErrContains: "file exists",
		},
		{
			name: "A file called fail already exists",
			key:  Key("domain_family_1_0_0"),
			setupFunc: func(t *testing.T, s *Schema) {
				t.Helper()
				// Create home directory
				homeDir := s.Path(HomeDir)
				require.NoError(t, os.MkdirAll(homeDir, 0o755))

				// Create a file where the fail directory should be
				failFile := filepath.Join(homeDir, string(TestDocTypeFail))
				require.NoError(t, os.WriteFile(failFile, []byte("blocking"), 0o600))
			},
			wantErr:         true,
			wantErrContains: "file exists",
		},
		{
			name: "MkdirAll failure",
			key:  Key("domain_family_1_0_0"),
			setupFunc: func(t *testing.T, s *Schema) {
				t.Helper()
				// Create a file where the home directory should be
				homeDir := s.Path(HomeDir)
				parent := filepath.Dir(homeDir)
				require.NoError(t, os.MkdirAll(parent, 0o755))
				require.NoError(t, os.WriteFile(homeDir, []byte("blocking"), 0o600))
			},
			wantErr: true,
		},
		{
			name: "Registry config failure",
			key:  Key("domain_family_1_0_0"),
			setupFunc: func(t *testing.T, s *Schema) {
				t.Helper()
				// Manually set config to nil to simulate a configuration error
				s.registry.config = nil
			},
			wantErr:         true,
			wantErrContains: "registry configuration not initialised",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup temporary filesystem
			tmpDir := t.TempDir()
			r := createRegistry(t, tmpDir, configData)

			s := New(tt.key, r)

			// Run setup function if provided (for error tests)
			if tt.setupFunc != nil {
				tt.setupFunc(t, s)
			}

			// Execute WriteNewSchemaFiles
			err := s.WriteNewSchemaFiles()

			// Check for expected errors
			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrContains != "" {
					require.ErrorContains(t, err, tt.wantErrContains)
				}
				return
			}

			require.NoError(t, err)

			// Verify the schema file was created
			schemaPath := s.Path(FilePath)
			assert.FileExists(t, schemaPath)

			// Verify the content matches NewSchemaContent
			content, err := os.ReadFile(schemaPath)
			require.NoError(t, err)
			assert.JSONEq(t, NewSchemaContent(validator.Draft7), string(content))

			// Verify the home directory structure
			homeDir := s.Path(HomeDir)
			assert.DirExists(t, homeDir)

			// Verify pass and fail directories were created
			passDir := filepath.Join(homeDir, string(TestDocTypePass))
			failDir := filepath.Join(homeDir, string(TestDocTypeFail))
			assert.DirExists(t, passDir)
			assert.DirExists(t, failDir)

			// Verify directory name structure matches expected version path
			assert.Contains(t, homeDir, tt.wantDirName)
		})
	}
}

func TestDuplicateSchemaFiles(t *testing.T) {
	t.Parallel()

	const configData = `
environments:
  prod:
    publicUrlRoot: https://json-schema.example.com
    privateUrlRoot: https://private.json-schema.example.com
    allowSchemaMutation: false
    isProduction: true
`

	const sourceSchemaContent = `{
"$schema": "http://json-schema.org/draft-07/schema#",
"$id": "{{ ID }}",
"type": "object",
"properties": {
	"name": {
		"type": "string"
	}
}
}`

	const passTestContent = "{}"

	const failTestContent = `{
"name": 123
}`

	tests := []struct {
		name            string
		sourceKey       Key
		targetKey       Key
		setupFunc       func(t *testing.T, r *Registry)
		wantErr         bool
		wantErrContains string
	}{
		{
			name:      "Duplicate schema to new patch version",
			sourceKey: Key("domain_family_1_0_0"),
			targetKey: Key("domain_family_1_0_1"),
		},
		{
			name:      "Duplicate schema to new minor version",
			sourceKey: Key("domain_family_1_0_0"),
			targetKey: Key("domain_family_1_1_0"),
		},
		{
			name:      "Duplicate schema to new major version",
			sourceKey: Key("domain_family_1_0_0"),
			targetKey: Key("domain_family_2_0_0"),
		},
		{
			name:      "Duplicate schema with different family",
			sourceKey: Key("domain_family-a_1_0_0"),
			targetKey: Key("domain_family-b_1_0_0"),
		},
		{
			name:      "The source schema does not exist",
			sourceKey: Key("domain_nonexistent_1_0_0"),
			targetKey: Key("domain_family_1_0_1"),
			wantErr:   true,
			setupFunc: func(_ *testing.T, _ *Registry) {
				// Don't create the source schema - it won't exist
			},
			wantErrContains: "no such file or directory",
		},
		{
			name:      "The target home directory exists as a file",
			sourceKey: Key("domain_family_1_0_0"),
			targetKey: Key("domain_family_1_0_1"),
			setupFunc: func(t *testing.T, r *Registry) {
				t.Helper()
				// Create a file where the target home directory should be
				targetSchema := New(Key("domain_family_1_0_1"), r)
				targetHomeDir := targetSchema.Path(HomeDir)
				// Create parent directory
				parentDir := filepath.Dir(targetHomeDir)
				require.NoError(t, os.MkdirAll(parentDir, 0o755))
				// Create a file instead of directory
				require.NoError(t, os.WriteFile(targetHomeDir, []byte("blocking"), 0o600))
			},
			wantErr:         true,
			wantErrContains: "not a directory",
		},
		{
			name:      "The target schema file path is blocked by a directory",
			sourceKey: Key("domain_family_1_0_0"),
			targetKey: Key("domain_family_1_0_1"),
			setupFunc: func(t *testing.T, r *Registry) {
				t.Helper()
				// Normal setup for source schema
				createSchemaFiles(t, r, schemaMap{
					Key("domain_family_1_0_0"): "{}",
				})

				// Create the target directory structure so CopyFS can run
				s := New(Key("domain_family_1_0_1"), r)
				homeDir := s.Path(HomeDir)
				require.NoError(t, os.MkdirAll(homeDir, 0o755))

				// BLOCK the renaming by putting a directory where the result file should go
				targetFile := s.Path(FilePath)
				require.NoError(t, os.Mkdir(targetFile, 0o755))
			},
			wantErr:         true,
			wantErrContains: "file exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup temporary filesystem
			tmpDir := t.TempDir()
			r := createRegistry(t, tmpDir, configData)

			// Run custom setup if provided (for error tests)
			if tt.setupFunc != nil {
				tt.setupFunc(t, r)
			}

			// Create source schema with test files (unless setupFunc handles it)
			sourceSchema := New(tt.sourceKey, r)
			sourceHomeDir := sourceSchema.Path(HomeDir)

			// Only create source schema if setupFunc didn't handle it
			// (for error tests, setupFunc might skip this)
			if tt.setupFunc == nil {
				// Create source directory structure
				require.NoError(t, os.MkdirAll(sourceHomeDir, 0o755))

				// Write source schema file
				sourceFilePath := sourceSchema.Path(FilePath)
				require.NoError(t, os.WriteFile(sourceFilePath, []byte(sourceSchemaContent), 0o600))

				// Create pass and fail directories with test files
				passDir := filepath.Join(sourceHomeDir, string(TestDocTypePass))
				failDir := filepath.Join(sourceHomeDir, string(TestDocTypeFail))
				require.NoError(t, os.Mkdir(passDir, 0o755))
				require.NoError(t, os.Mkdir(failDir, 0o755))

				passTestFile := filepath.Join(passDir, "test1.json")
				failTestFile := filepath.Join(failDir, "test1.json")
				require.NoError(t, os.WriteFile(passTestFile, []byte(passTestContent), 0o600))
				require.NoError(t, os.WriteFile(failTestFile, []byte(failTestContent), 0o600))
			}

			// Create target schema
			targetSchema := New(tt.targetKey, r)

			// Execute DuplicateSchemaFiles
			err := targetSchema.DuplicateSchemaFiles(sourceSchema)

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrContains != "" {
					require.ErrorContains(t, err, tt.wantErrContains)
				}
				return
			}

			require.NoError(t, err)

			// Verify the target schema file was created with correct name
			targetFilePath := targetSchema.Path(FilePath)
			assert.FileExists(t, targetFilePath)

			// Verify the content was copied
			content, vErr := os.ReadFile(targetFilePath)
			require.NoError(t, vErr)
			assert.JSONEq(t, sourceSchemaContent, string(content))

			// Verify the home directory structure
			targetHomeDir := targetSchema.Path(HomeDir)
			assert.DirExists(t, targetHomeDir)

			// Verify pass and fail directories were copied
			targetPassDir := filepath.Join(targetHomeDir, string(TestDocTypePass))
			targetFailDir := filepath.Join(targetHomeDir, string(TestDocTypeFail))
			assert.DirExists(t, targetPassDir)
			assert.DirExists(t, targetFailDir)

			// Verify test files were copied
			targetPassTestFile := filepath.Join(targetPassDir, "test1.json")
			targetFailTestFile := filepath.Join(targetFailDir, "test1.json")
			assert.FileExists(t, targetPassTestFile)
			assert.FileExists(t, targetFailTestFile)

			// Verify test file contents
			passContent, vErr1 := os.ReadFile(targetPassTestFile)
			require.NoError(t, vErr1)
			assert.JSONEq(t, passTestContent, string(passContent))

			failContent, vErr2 := os.ReadFile(targetFailTestFile)
			require.NoError(t, vErr2)
			assert.JSONEq(t, failTestContent, string(failContent))

			// Verify the old source schema filename is NOT in the target directory
			oldFilename := filepath.Join(targetHomeDir, sourceSchema.Filename())
			assert.NoFileExists(t, oldFilename, "Source schema filename should have been renamed")
		})
	}
}

func TestTestDocuments(t *testing.T) {
	t.Parallel()

	const configData = `
environments:
  prod:
    publicUrlRoot: https://json-schema.example.com
    privateUrlRoot: https://private.json-schema.example.com
    allowSchemaMutation: false
    isProduction: true
`

	const schemaKey = Key("domain_family_1_0_0")

	tests := []struct {
		name            string
		testType        TestDocType
		setupFunc       func(t *testing.T, homeDir string)
		wantFiles       []string // basenames without .json suffix
		wantErrContains string
	}{
		{
			name:     "Returns pass test documents",
			testType: TestDocTypePass,
			setupFunc: func(t *testing.T, homeDir string) {
				t.Helper()
				passDir := filepath.Join(homeDir, string(TestDocTypePass))
				require.NoError(t, os.MkdirAll(passDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(passDir, "test1.json"), []byte("{}"), 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(passDir, "test2.json"), []byte("{}"), 0o600))
			},
			wantFiles: []string{"test1", "test2"},
		},
		{
			name:     "Returns fail test documents",
			testType: TestDocTypeFail,
			setupFunc: func(t *testing.T, homeDir string) {
				t.Helper()
				failDir := filepath.Join(homeDir, string(TestDocTypeFail))
				require.NoError(t, os.MkdirAll(failDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(failDir, "invalid1.json"), []byte("{}"), 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(failDir, "invalid2.json"), []byte("{}"), 0o600))
			},
			wantFiles: []string{"invalid1", "invalid2"},
		},
		{
			name:     "Returns empty list when no test documents exist",
			testType: TestDocTypePass,
			setupFunc: func(t *testing.T, homeDir string) {
				t.Helper()
				passDir := filepath.Join(homeDir, string(TestDocTypePass))
				require.NoError(t, os.MkdirAll(passDir, 0o755))
				// No files created
			},
			wantFiles: []string{},
		},
		{
			name:     "Ignores subdirectories",
			testType: TestDocTypePass,
			setupFunc: func(t *testing.T, homeDir string) {
				t.Helper()
				passDir := filepath.Join(homeDir, string(TestDocTypePass))
				require.NoError(t, os.MkdirAll(passDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(passDir, "valid.json"), []byte("{}"), 0o600))
				// Create a subdirectory that should be ignored
				require.NoError(t, os.Mkdir(filepath.Join(passDir, "subdir"), 0o755))
			},
			wantFiles: []string{"valid"},
		},
		{
			name:     "Ignores non-JSON files",
			testType: TestDocTypePass,
			setupFunc: func(t *testing.T, homeDir string) {
				t.Helper()
				passDir := filepath.Join(homeDir, string(TestDocTypePass))
				require.NoError(t, os.MkdirAll(passDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(passDir, "valid.json"), []byte("{}"), 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(passDir, "readme.txt"), []byte("docs"), 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(passDir, ".gitkeep"), []byte(""), 0o600))
			},
			wantFiles: []string{"valid"},
		},
		{
			name:     "Returns files in filesystem order",
			testType: TestDocTypePass,
			setupFunc: func(t *testing.T, homeDir string) {
				t.Helper()
				passDir := filepath.Join(homeDir, string(TestDocTypePass))
				require.NoError(t, os.MkdirAll(passDir, 0o755))
				// Create files - ReadDir returns them in directory order (typically alphabetical)
				require.NoError(t, os.WriteFile(filepath.Join(passDir, "c_test.json"), []byte("{}"), 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(passDir, "a_test.json"), []byte("{}"), 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(passDir, "b_test.json"), []byte("{}"), 0o600))
			},
			wantFiles: []string{"a_test", "b_test", "c_test"},
		},
		{
			name:     "No documents when the test directory does not exist",
			testType: TestDocTypePass,
			setupFunc: func(t *testing.T, homeDir string) {
				t.Helper()
				// Create home directory but not the pass directory, as git leaves it when it was empty
				require.NoError(t, os.MkdirAll(homeDir, 0o755))
			},
			wantFiles: []string{},
		},
		{
			name:     "Err when test directory is not readable",
			testType: TestDocTypePass,
			setupFunc: func(t *testing.T, homeDir string) {
				t.Helper()
				passDir := filepath.Join(homeDir, string(TestDocTypePass))
				require.NoError(t, os.MkdirAll(passDir, 0o755))
				// Make directory unreadable
				require.NoError(t, os.Chmod(passDir, 0o000))
				t.Cleanup(func() {
					require.NoError(t, os.Chmod(passDir, 0o755))
				})
			},
			wantErrContains: "permission denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup temporary filesystem
			tmpDir := t.TempDir()
			r := createRegistry(t, tmpDir, configData)

			s := New(schemaKey, r)
			homeDir := s.Path(HomeDir)

			// Run setup function
			if tt.setupFunc != nil {
				tt.setupFunc(t, homeDir)
			}

			// Execute TestDocuments
			docs, err := s.TestDocuments(tt.testType)

			// Check for expected errors
			if tt.wantErrContains != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.wantErrContains)
				return
			}

			require.NoError(t, err)

			// Convert returned paths to basenames without suffix for comparison
			gotFiles := make([]string, len(docs))
			for i, doc := range docs {
				// Extract basename without .json suffix
				base := filepath.Base(doc.Path)
				gotFiles[i] = strings.TrimSuffix(base, ".json")
			}

			assert.Equal(t, tt.wantFiles, gotFiles)

			// Verify paths are absolute and in the correct directory
			for _, doc := range docs {
				assert.True(t, filepath.IsAbs(doc.Path), "Path should be absolute: %s", doc.Path)
				assert.Contains(t, doc.Path, homeDir, "Path should be within home directory")
				assert.Contains(t, doc.Path, string(tt.testType), "Path should contain test type directory")
			}
		})
	}
}

func TestTestDocuments_Caching(t *testing.T) {
	t.Parallel()

	const configData = `
environments:
  prod:
    publicUrlRoot: https://json-schema.example.com
    privateUrlRoot: https://private.json-schema.example.com
    allowSchemaMutation: false
    isProduction: true
`

	tmpDir := t.TempDir()
	r := createRegistry(t, tmpDir, configData)

	s := New(Key("domain_family_1_0_0"), r)
	homeDir := s.Path(HomeDir)

	// Create test documents
	passDir := filepath.Join(homeDir, string(TestDocTypePass))
	require.NoError(t, os.MkdirAll(passDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(passDir, "test1.json"), []byte("{}"), 0o600))

	// First call should read from filesystem
	docs1, err := s.TestDocuments(TestDocTypePass)
	require.NoError(t, err)
	assert.Len(t, docs1, 1)

	// Add another file after the first call
	require.NoError(t, os.WriteFile(filepath.Join(passDir, "test2.json"), []byte("{}"), 0o600))

	// Second call should return cached result (still only 1 file)
	docs2, err := s.TestDocuments(TestDocTypePass)
	require.NoError(t, err)
	assert.Len(t, docs2, 1, "Should return cached result, not re-read directory")

	// Verify the results are the same
	assert.Equal(t, docs1, docs2)
}

func TestMajorFamilyFutureSchemas(t *testing.T) { //nolint:dupl // high complexity
	t.Parallel()

	const configData = `
environments:
  prod:
    publicUrlRoot: https://p
    privateUrlRoot: https://pr
    isProduction: true
`

	// existingVersions simulates a family with multiple versions across major/minor/patch
	existingVersions := [][3]uint{
		{1, 0, 0},
		{1, 0, 1},
		{1, 0, 2},
		{1, 1, 0},
		{1, 1, 1},
		{1, 1, 2},
		{1, 2, 0},
		{1, 2, 1},
		{2, 0, 0},
		{2, 0, 1},
		{2, 1, 0},
	}

	tests := []struct {
		name            string
		schemaVersion   SemVer
		wantKeys        []Key
		setupFunc       func(t *testing.T, familyDir string)
		wantErr         bool
		wantErrContains string
	}{
		{
			name:          "1.0.0 finds all future versions in major 1",
			schemaVersion: [3]uint64{1, 0, 0},
			wantKeys: []Key{
				Key("domain_family_1_0_1"),
				Key("domain_family_1_0_2"),
				Key("domain_family_1_1_0"),
				Key("domain_family_1_1_1"),
				Key("domain_family_1_1_2"),
				Key("domain_family_1_2_0"),
				Key("domain_family_1_2_1"),
			},
		},
		{
			name:          "1.1.0 finds future patches and minors",
			schemaVersion: [3]uint64{1, 1, 0},
			wantKeys: []Key{
				Key("domain_family_1_1_1"),
				Key("domain_family_1_1_2"),
				Key("domain_family_1_2_0"),
				Key("domain_family_1_2_1"),
			},
		},
		{
			name:          "1.2.1 is the latest in major 1 - returns empty",
			schemaVersion: [3]uint64{1, 2, 1},
			wantKeys:      nil,
		},
		{
			name:          "2.0.0 finds future versions in major 2",
			schemaVersion: [3]uint64{2, 0, 0},
			wantKeys: []Key{
				Key("domain_family_2_0_1"),
				Key("domain_family_2_1_0"),
			},
		},
		{
			name:          "2.1.0 is the latest in major 2 - returns empty",
			schemaVersion: [3]uint64{2, 1, 0},
			wantKeys:      nil,
		},
		{
			name:          "Major directory cannot be read",
			schemaVersion: [3]uint64{1, 0, 0},
			setupFunc: func(t *testing.T, familyDir string) {
				t.Helper()
				majorDir := filepath.Join(familyDir, "1")
				require.NoError(t, os.Chmod(majorDir, 0o000))
				t.Cleanup(func() { require.NoError(t, os.Chmod(majorDir, 0o755)) })
			},
			wantErr:         true,
			wantErrContains: "permission denied",
		},
		{
			name:          "Minor directory cannot be read",
			schemaVersion: [3]uint64{1, 0, 0},
			setupFunc: func(t *testing.T, familyDir string) {
				t.Helper()
				minorDir := filepath.Join(familyDir, "1", "0")
				require.NoError(t, os.Chmod(minorDir, 0o000))
				t.Cleanup(func() { require.NoError(t, os.Chmod(minorDir, 0o755)) })
			},
			wantErr:         true,
			wantErrContains: "permission denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup temporary registry
			tmpDir := t.TempDir()

			// Create the family directory
			domainDir := filepath.Join(tmpDir, "domain")
			familyDir := filepath.Join(domainDir, "family")
			if err := os.MkdirAll(familyDir, 0o755); err != nil {
				t.Fatal(err)
			}

			// createVersion creates directories matching a specific version in the family.
			createVersion := func(major, minor, patch uint) {
				dir := filepath.Join(
					familyDir,
					strconv.FormatUint(uint64(major), 10),
					strconv.FormatUint(uint64(minor), 10),
					strconv.FormatUint(uint64(patch), 10),
				)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}

			for _, v := range existingVersions {
				createVersion(v[0], v[1], v[2])
			}

			// Run custom setup loop if provided
			if tt.setupFunc != nil {
				tt.setupFunc(t, familyDir)
			}

			// NewRegistry requires a config file.
			if err := os.WriteFile(
				filepath.Join(tmpDir, config.JsmRegistryConfigFile),
				[]byte(configData),
				0o600,
			); err != nil {
				t.Fatal(err)
			}

			compiler := &mockCompiler{}
			registry, err := NewRegistry(tmpDir, compiler, fsh.NewPathResolver(), fsh.NewEnvProvider())
			require.NoError(t, err)

			// Construct a schema with necessary fields
			s := &Schema{
				core: &Core{
					domain:     []string{"domain"},
					familyName: "family",
					version:    tt.schemaVersion,
				},
				registry: registry,
			}

			keys, err := s.MajorFamilyFutureSchemas()

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrContains != "" {
					require.ErrorContains(t, err, tt.wantErrContains)
				}
				return
			}

			require.NoError(t, err)

			if tt.wantKeys == nil {
				assert.Empty(t, keys)
			} else {
				assert.ElementsMatch(t, tt.wantKeys, keys)
			}
		})
	}
}

func TestMajorFamilyEarlierSchemas(t *testing.T) { //nolint:dupl // high complexity
	t.Parallel()

	const configData = `
environments:
  prod:
    publicUrlRoot: https://p
    privateUrlRoot: https://pr
    isProduction: true
`

	// existingVersions simulates a family with multiple versions across major/minor/patch
	existingVersions := [][3]uint{
		{1, 0, 0},
		{1, 0, 1},
		{1, 0, 2},
		{1, 1, 0},
		{1, 1, 1},
		{1, 1, 2},
		{1, 2, 0},
		{1, 2, 1},
		{2, 0, 0},
		{2, 0, 1},
		{2, 1, 0},
	}

	tests := []struct {
		name            string
		schemaVersion   SemVer
		wantKeys        []Key
		setupFunc       func(t *testing.T, familyDir string)
		wantErr         bool
		wantErrContains string
	}{
		{
			name:          "1.2.1 finds all earlier versions in major 1",
			schemaVersion: [3]uint64{1, 2, 1},
			wantKeys: []Key{
				Key("domain_family_1_0_0"),
				Key("domain_family_1_0_1"),
				Key("domain_family_1_0_2"),
				Key("domain_family_1_1_0"),
				Key("domain_family_1_1_1"),
				Key("domain_family_1_1_2"),
				Key("domain_family_1_2_0"),
			},
		},
		{
			name:          "1.1.1 finds earlier patches and minors",
			schemaVersion: [3]uint64{1, 1, 1},
			wantKeys: []Key{
				Key("domain_family_1_0_0"),
				Key("domain_family_1_0_1"),
				Key("domain_family_1_0_2"),
				Key("domain_family_1_1_0"),
			},
		},
		{
			name:          "1.0.0 is the earliest in major 1 - returns empty",
			schemaVersion: [3]uint64{1, 0, 0},
			wantKeys:      nil,
		},
		{
			name:          "2.1.0 finds earlier versions in major 2",
			schemaVersion: [3]uint64{2, 1, 0},
			wantKeys: []Key{
				Key("domain_family_2_0_0"),
				Key("domain_family_2_0_1"),
			},
		},
		{
			name:          "2.0.0 is the earliest in major 2 - returns empty",
			schemaVersion: [3]uint64{2, 0, 0},
			wantKeys:      nil,
		},
		{
			name:          "Major directory cannot be read",
			schemaVersion: [3]uint64{1, 2, 1},
			setupFunc: func(t *testing.T, familyDir string) {
				t.Helper()
				majorDir := filepath.Join(familyDir, "1")
				require.NoError(t, os.Chmod(majorDir, 0o000))
				t.Cleanup(func() { require.NoError(t, os.Chmod(majorDir, 0o755)) })
			},
			wantErr:         true,
			wantErrContains: "permission denied",
		},
		{
			name:          "Minor directory cannot be read",
			schemaVersion: [3]uint64{1, 2, 1},
			setupFunc: func(t *testing.T, familyDir string) {
				t.Helper()
				minorDir := filepath.Join(familyDir, "1", "2")
				require.NoError(t, os.Chmod(minorDir, 0o000))
				t.Cleanup(func() { require.NoError(t, os.Chmod(minorDir, 0o755)) })
			},
			wantErr:         true,
			wantErrContains: "permission denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup temporary registry
			tmpDir := t.TempDir()

			// Create the family directory
			domainDir := filepath.Join(tmpDir, "domain")
			familyDir := filepath.Join(domainDir, "family")
			if err := os.MkdirAll(familyDir, 0o755); err != nil {
				t.Fatal(err)
			}

			// createVersion creates directories matching a specific version in the family.
			createVersion := func(major, minor, patch uint) {
				dir := filepath.Join(
					familyDir,
					strconv.FormatUint(uint64(major), 10),
					strconv.FormatUint(uint64(minor), 10),
					strconv.FormatUint(uint64(patch), 10),
				)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}

			for _, v := range existingVersions {
				createVersion(v[0], v[1], v[2])
			}

			// Run custom setup loop if provided
			if tt.setupFunc != nil {
				tt.setupFunc(t, familyDir)
			}

			// NewRegistry requires a config file.
			if err := os.WriteFile(
				filepath.Join(tmpDir, config.JsmRegistryConfigFile),
				[]byte(configData),
				0o600,
			); err != nil {
				t.Fatal(err)
			}

			compiler := &mockCompiler{}
			registry, err := NewRegistry(tmpDir, compiler, fsh.NewPathResolver(), fsh.NewEnvProvider())
			require.NoError(t, err)

			// Construct a schema with necessary fields
			s := &Schema{
				core: &Core{
					domain:     []string{"domain"},
					familyName: "family",
					version:    tt.schemaVersion,
				},
				registry: registry,
			}

			keys, err := s.MajorFamilyEarlierSchemas()

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrContains != "" {
					require.ErrorContains(t, err, tt.wantErrContains)
				}
				return
			}

			require.NoError(t, err)

			if tt.wantKeys == nil {
				assert.Empty(t, keys)
			} else {
				assert.ElementsMatch(t, tt.wantKeys, keys)
			}
		})
	}
}

func TestLoad_TemplateParseErr(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)

	key := Key("domain_family_1_0_0")
	s := New(key, r)
	homeDir := s.Path(HomeDir)
	require.NoError(t, os.MkdirAll(homeDir, 0o755))

	// Write a valid JSON but invalid template (unterminated tag)
	schemaPath := s.Path(FilePath)
	require.NoError(t, os.WriteFile(schemaPath, []byte(`{"id": "{{ ID }"`+"}"), 0o600))

	_, err := Load(key, r)
	require.Error(t, err)
	var target *TemplateFormatInvalidError
	require.ErrorAs(t, err, &target)
}

func TestSchema_LoadTemplate_DummyFuncs(t *testing.T) {
	t.Parallel()
	s := &Schema{
		srcDoc: []byte(`{{ ID }} {{ JSM "key" }}`),
	}
	err := s.loadTemplate("test.json")
	require.NoError(t, err)

	// In loadTemplate, s.tmpl is assigned a *template.Template
	tmpl, ok := s.tmpl.(*template.Template)
	require.True(t, ok)

	// Execute the template to cover the dummy functions
	err = tmpl.Execute(io.Discard, nil)
	require.NoError(t, err)
}

type failCompiler struct {
	failAdd     bool
	failCompile bool
}

func (c *failCompiler) AddSchema(_ string, _ validator.JSONSchema) error {
	if c.failAdd {
		return errors.New("add error")
	}
	return nil
}

func (c *failCompiler) Compile(_ string) (validator.Validator, error) {
	if c.failCompile {
		return nil, errors.New("compile error")
	}
	return nil, nil
}

func (c *failCompiler) SupportedSchemaVersions() []validator.Draft {
	return []validator.Draft{validator.Draft7}
}

func (c *failCompiler) Clear() {}

func TestRender_CompilerErrs(t *testing.T) {
	t.Parallel()

	t.Run("AddSchema error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		r.compiler = &failCompiler{failAdd: true}

		s := New(Key("domain_family_1_0_0"), r)
		s.srcDoc = []byte(`{}`)
		s.exists = true
		require.NoError(t, s.loadTemplate("test.json"))

		ec := r.config.ProductionEnvConfig()
		_, err := s.Render(ec)
		require.Error(t, err)
		assert.EqualError(t, err, "add error")
	})

	t.Run("Compile error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		r.compiler = &failCompiler{failCompile: true}

		s := New(Key("domain_family_1_0_0"), r)
		s.srcDoc = []byte(`{}`)
		s.exists = true
		require.NoError(t, s.loadTemplate("test.json"))

		ec := r.config.ProductionEnvConfig()
		_, err := s.Render(ec)
		require.Error(t, err)
		var target InvalidJSONSchemaError
		require.ErrorAs(t, err, &target)
	})
}

func TestTestDocuments_InvalidJSONFile(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	s := New(Key("domain_family_1_0_0"), r)
	homeDir := s.Path(HomeDir)

	passDir := filepath.Join(homeDir, string(TestDocTypePass))
	require.NoError(t, os.MkdirAll(passDir, 0o755))

	// Create an invalid JSON file
	require.NoError(t, os.WriteFile(filepath.Join(passDir, "invalid.json"), []byte("{ invalid"), 0o600))

	_, err := s.TestDocuments(TestDocTypePass)
	require.Error(t, err)
	assert.ErrorAs(t, err, new(InvalidTestDocumentError))
}

func TestNewSchemaContent_IndentsWithSpaces(t *testing.T) {
	t.Parallel()

	content := NewSchemaContent(validator.Draft7)
	assert.NotContains(t, content, "\t")
	assert.Contains(
		t,
		content,
		"{\n  \"$schema\": \"http://json-schema.org/draft-07/schema#\",\n  \"$id\": \"{{ ID }}\",\n",
	)
}
