package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/validator"
)

type mockCompiler struct {
	validator.Compiler // Anonymous field to satisfy interface if needed, but we implement what we use
	supported          []validator.Draft
}

func (m *mockCompiler) SupportedSchemaVersions() []validator.Draft {
	return m.supported
}

func (m *mockCompiler) AddSchema(_ string, _ validator.JSONSchema) error {
	return nil
}

func (m *mockCompiler) Compile(_ string) (validator.Validator, error) {
	return nil, nil
}

func TestNewConfig(t *testing.T) {
	t.Parallel()

	mc := &mockCompiler{
		supported: []validator.Draft{validator.Draft7, validator.Draft4},
	}

	t.Run("json-schema-manager-config.yml missing", func(t *testing.T) {
		t.Parallel()
		regDir := filepath.Join(t.TempDir(), "reg1")
		require.NoError(t, os.Mkdir(regDir, 0o755))

		_, err := New(regDir, mc)
		var target *MissingConfigError
		require.ErrorAs(t, err, &target)
		assert.EqualError(t, err, "json-schema-manager-config.yml missing in: "+regDir)
	})

	configTests := []struct {
		name    string
		content string
		errStr  string
	}{
		{
			name:    "json-schema-manager-config.yml invalid yaml",
			content: "invalid: yaml: :",
			errStr:  "json-schema-manager-config.yml is not a valid yaml document",
		},
		{
			name: "json-schema-manager-config.yml missing property (environments)",
			content: `
publicUrlRoot: "https://xxx"
privateUrlRoot: "https://yyy"
`,
			errStr: "json-schema-manager-config.yml is missing required property: environments",
		},
		{
			name: "json-schema-manager-config.yml missing nested property (publicUrlRoot)",
			content: `
environments:
  dev:
      privateUrlRoot: "https://yyy"
`,
			errStr: "json-schema-manager-config.yml is missing required property: environments.dev.publicUrlRoot",
		},
		{
			name: "json-schema-manager-config.yml missing nested property (privateUrlRoot)",
			content: `
environments:
  dev:
      publicUrlRoot: "https://xxx"
`,
			errStr: "json-schema-manager-config.yml is missing required property: environments.dev.privateUrlRoot",
		},
		{
			name: "json-schema-manager-config.yml invalid private URL",
			content: `
environments:
  dev:
      publicUrlRoot: "https://xxx"
      privateUrlRoot: "not-a-url"
`,
			errStr: "json-schema-manager-config.yml property environments.dev.privateUrlRoot has invalid URL 'not-a-url': " +
				"scheme must be https",
		},
		{
			name: "json-schema-manager-config.yml wrong type (environments should be map)",
			content: `
environments: "dev"
`,
			errStr: "json-schema-manager-config.yml is not a valid yaml document",
		},
		{
			name: "json-schema-manager-config.yml invalid public URL",
			content: `
environments:
  dev:
      publicUrlRoot: "not-a-url"
      privateUrlRoot: "https://yyy"
`,
			errStr: "json-schema-manager-config.yml property environments.dev.publicUrlRoot has invalid URL 'not-a-url': " +
				"scheme must be https",
		},
		{
			name: "json-schema-manager-config.yml allowSchemaMutation wrong type",
			content: `
environments:
  dev:
      publicUrlRoot: "https://xxx"
      privateUrlRoot: "https://yyy"
      allowSchemaMutation: "true"
`,
			errStr: "json-schema-manager-config.yml is not a valid yaml document",
		},
		{
			name:    "json-schema-manager-config.yml is a directory",
			content: "DIR", // Special flag for the test loop to create a dir instead of a file
			errStr:  "is a directory",
		},
		{
			name: "json-schema-manager-config.yml publicUrlRoot unparseable URL",
			content: `
environments:
  dev:
      publicUrlRoot: "https://xxx\x00yyy"
      privateUrlRoot: "https://yyy"
      isProduction: true
`,
			errStr: "invalid control character in URL",
		},
		{
			name: "json-schema-manager-config.yml no production environment",
			content: `
environments:
  dev:
      publicUrlRoot: "https://xxx"
      privateUrlRoot: "https://yyy"
`,
			errStr: "must have exactly one environment marked with isProduction: true",
		},
		{
			name: "json-schema-manager-config.yml multiple production environments",
			content: `
environments:
  dev:
      publicUrlRoot: "https://xxx"
      privateUrlRoot: "https://yyy"
      isProduction: true
  prod:
      publicUrlRoot: "https://zzz"
      privateUrlRoot: "https://aaa"
      isProduction: true
`,
			errStr: "must have exactly one environment marked with isProduction: true",
		},
		{
			name: "json-schema-manager-config.yml invalid defaultJsonSchemaVersion",
			content: `
defaultJsonSchemaVersion: "NotADraft"
environments:
  dev:
      publicUrlRoot: "https://xxx"
      privateUrlRoot: "https://yyy"
      isProduction: true
`,
			errStr: "json-schema-manager-config.yml property defaultJsonSchemaVersion has invalid value 'NotADraft'. " +
				"Supported versions are: [http://json-schema.org/draft-07/schema# http://json-schema.org/draft-04/schema#]",
		},
		{
			name:    "permission denied on registry root",
			content: "PERM", // Special flag to remove permissions
			errStr:  "permission denied",
		},
	}

	for _, tt := range configTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			regDir := t.TempDir()
			configPath := filepath.Join(regDir, "json-schema-manager-config.yml")
			switch tt.content {
			case "DIR":
				assert.NoError(t, os.Mkdir(configPath, 0o755))
			case "PERM":
				// To trigger a permission error on Stat, we can remove search permission from the directory
				require.NoError(t, os.Chmod(regDir, 0o000))
				t.Cleanup(func() {
					_ = os.Chmod(regDir, 0o755)
				}) // restore for Cleanup
			default:
				require.NoError(t, os.WriteFile(configPath, []byte(tt.content), 0o600))
			}
			_, err := New(regDir, mc)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.errStr)
		})
	}
}

func TestProductionEnvConfig(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	content := `
environments:
  dev:
      publicUrlRoot: "https://dev.public.io/"
      privateUrlRoot: "https://dev.private.io/"
  prod:
      publicUrlRoot: "https://prod.public.io/"
      privateUrlRoot: "https://prod.private.io/"
      isProduction: true
`
	configPath := filepath.Join(tmpDir, "json-schema-manager-config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))

	cfg, err := New(tmpDir, &mockCompiler{supported: []validator.Draft{validator.Draft7}})
	require.NoError(t, err)

	prodCfg := cfg.ProductionEnvConfig()
	assert.Equal(t, "https://prod.public.io/", prodCfg.PublicURLRoot)
}

func TestURLRoot(t *testing.T) {
	t.Parallel()
	e := &EnvConfig{
		PublicURLRoot:  "https://public.io/",
		PrivateURLRoot: "https://private.io/",
	}

	tests := []struct {
		name     string
		want     string
		isPublic bool
	}{
		{
			name:     "public",
			isPublic: true,
			want:     "https://public.io/",
		},
		{
			name:     "private",
			isPublic: false,
			want:     "https://private.io/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, e.URLRoot(tt.isPublic))
		})
	}
}

func TestEnvConfig(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Environments: map[Env]*EnvConfig{
			"prod": {
				PublicURLRoot:  "https://example.com",
				PrivateURLRoot: "https://private.example.com",
			},
		},
	}

	t.Run("environment exists", func(t *testing.T) {
		t.Parallel()
		ec, err := cfg.EnvConfig("prod")
		require.NoError(t, err)
		assert.Equal(t, "https://example.com", ec.PublicURLRoot)
		assert.Equal(t, "https://private.example.com", ec.PrivateURLRoot)
	})

	t.Run("environment does not exist", func(t *testing.T) {
		t.Parallel()
		_, err := cfg.EnvConfig("invalid-env")
		require.Error(t, err)

		var target *UnknownEnvironmentError
		require.ErrorAs(t, err, &target)
		assert.Equal(t, Env("invalid-env"), target.Env)
		assert.EqualError(t, err, "json-schema-manager-config.yml does not define environment 'invalid-env'")
	})
}

func TestDefaultConfigContentListsSupportedVersions(t *testing.T) {
	t.Parallel()

	compiler := validator.NewSanthoshCompiler()
	listed := regexp.MustCompile(`(?m)^# - (\S+)`).FindAllStringSubmatch(DefaultConfigContent, -1)

	drafts := make([]validator.Draft, 0, len(listed))
	for _, m := range listed {
		draft := validator.Draft(m[1])
		drafts = append(drafts, draft)

		// Pasting the listed value into the config must give a valid config
		content := strings.Replace(DefaultConfigContent,
			`defaultJsonSchemaVersion: "http://json-schema.org/draft-07/schema#"`,
			`defaultJsonSchemaVersion: "`+m[1]+`"`, 1)
		cfg, err := Parse([]byte(content))
		require.NoError(t, err)
		require.NoError(t, cfg.Validate(compiler), "listed version %s", draft)
		assert.Equal(t, draft, cfg.DefaultJSONSchemaVersion)
	}

	assert.ElementsMatch(t, compiler.SupportedSchemaVersions(), drafts)
}
