package consumer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/internal/schema"
	"github.com/bitshepherds/json-schema-manager/internal/validator"
)

const testConfig = `
environments:
  prod:
    privateUrlRoot: "https://json-schemas.internal.myorg.io/"
    publicUrlRoot: "https://json-schemas.myorg.io/"
    isProduction: true
`

// newRegistry returns a registry holding the given schemas, by key, each with empty pass and
// fail directories.
func newRegistry(t *testing.T, schemas map[schema.Key]string) *schema.Registry {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "json-schema-manager-config.yml"), []byte(testConfig), 0o600))
	r, err := schema.NewRegistry(dir, validator.NewSanthoshCompiler(), fsh.NewPathResolver(), fsh.NewEnvProvider())
	require.NoError(t, err)
	for k, content := range schemas {
		s := schema.New(k, r)
		for _, sub := range []string{"pass", "fail"} {
			require.NoError(t, os.MkdirAll(filepath.Join(s.Path(schema.HomeDir), sub), 0o755))
		}
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte(content), 0o600))
	}
	return r
}

func writeManifest(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ManifestFile)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestLoadManifest(t *testing.T) {
	t.Parallel()

	m, err := LoadManifest(writeManifest(t, "schemas:\n  - shop_order_1_0_0\n"))
	require.NoError(t, err)
	assert.Equal(t, []string{"shop_order_1_0_0"}, m.Schemas)

	tests := []struct {
		name    string
		content string
		reason  string
	}{
		{"invalid YAML", "schemas: [", "is not valid YAML"},
		{"no schemas", "schemas: []\n", "lists no schemas"},
	}
	for _, tt := range tests {
		_, err = LoadManifest(writeManifest(t, tt.content))
		var me *ManifestError
		require.ErrorAs(t, err, &me, tt.name)
		assert.Contains(t, me.Error(), tt.reason, tt.name)
	}

	_, err = LoadManifest(filepath.Join(t.TempDir(), "missing.yml"))
	require.ErrorContains(t, err, "cannot be read")
}

func TestCheck(t *testing.T) {
	t.Parallel()

	r := newRegistry(t, map[schema.Key]string{
		"shop_order_1_0_0": `{"properties": {"status": {"enum": ["paid"]}}}`,
		"shop_order_1_1_0": `{"properties": {"status": {"enum": ["paid", "refunded"]}}}`,
		"shop_order_2_0_0": `{}`,
	})
	s, err := r.GetSchemaByKey("shop_order_1_1_0")
	require.NoError(t, err)
	refunded := filepath.Join(s.Path(schema.HomeDir), "pass", "refunded.json")
	require.NoError(t, os.WriteFile(refunded, []byte(`{"status": "refunded"}`), 0o600))

	res, err := Check(context.Background(), r, &Manifest{Path: "m.yml", Schemas: []string{
		"shop_order_1_0_0", "shop_order_1_1_0", "missing_thing_1_0_0", "shop",
	}})
	require.NoError(t, err)
	require.Len(t, res.Schemas, 4)

	assert.Equal(t, Fail, res.Schemas[0].Status())
	require.Len(t, res.Schemas[0].Rejections, 1)
	assert.Equal(t, schema.Key("shop_order_1_1_0"), res.Schemas[0].Rejections[0].Later)
	assert.Equal(t, refunded, res.Schemas[0].Rejections[0].Path)
	assert.Equal(t, schema.Key("shop_order_2_0_0"), res.Schemas[0].NewerMajor)

	assert.Equal(t, Warn, res.Schemas[1].Status())
	assert.Equal(t, "not found in the registry", res.Schemas[2].Problem)
	assert.Equal(t, "not a schema key or canonical URL", res.Schemas[3].Problem)

	assert.Equal(t, 0, res.Count(Pass))
	assert.Equal(t, 3, res.Count(Fail))
	assert.Equal(t, 1, res.Count(Warn))
}

func TestCheck_Errors(t *testing.T) {
	t.Parallel()

	t.Run("pinned version does not render", func(t *testing.T) {
		t.Parallel()
		r := newRegistry(t, map[schema.Key]string{"shop_order_1_0_0": "{\"$ref\": \"{{ JSM `missing_1_0_0` }}\"}"})
		_, err := Check(context.Background(), r, &Manifest{Schemas: []string{"shop_order_1_0_0"}})
		require.Error(t, err)
	})

	t.Run("family has a major version with no versions", func(t *testing.T) {
		t.Parallel()
		r := newRegistry(t, map[schema.Key]string{"shop_order_1_0_0": `{}`})
		s, err := r.GetSchemaByKey("shop_order_1_0_0")
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(schema.FamilyDir), "2"), 0o755))
		_, err = Check(context.Background(), r, &Manifest{Schemas: []string{"shop_order_1_0_0"}})
		require.ErrorAs(t, err, new(*schema.NotFoundError))
	})
}

func TestStatus(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Pass, (&SchemaResult{}).Status())
	assert.Equal(t, Warn, (&SchemaResult{NewerMajor: "a_2_0_0"}).Status())
	assert.Equal(t, Fail, (&SchemaResult{Problem: "p", NewerMajor: "a_2_0_0"}).Status())
}
