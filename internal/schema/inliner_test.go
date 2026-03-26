package schema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/internal/validator"
)

func setupInlinerTestRegistry(t *testing.T) *Registry {
	t.Helper()
	return setupTestRegistry(t)
}

func TestInliner_SingleRef(t *testing.T) {
	t.Parallel()

	r := setupInlinerTestRegistry(t)
	depKey := Key("domain_address_1_2_3")
	createSchemaFiles(t, r, schemaMap{
		depKey: `{"$id": "{{ ID }}", "type": "object", "properties": {"city": {"type": "string"}}}`,
	})

	ec := r.config.ProductionEnvConfig()

	// Pre-render the dependency so it's available for inlining
	depSchema, err := r.GetSchemaByKey(depKey)
	require.NoError(t, err)
	_, err = r.CoordinateRender(depSchema, ec)
	require.NoError(t, err)

	depID := depSchema.CanonicalID(ec)

	// Build a rendered schema that references the dependency
	rendered := `{
  "$id": "https://json-schemas.internal.myorg.io/main_schema_1_0_0.schema.json",
  "type": "object",
  "properties": {
    "shipping_address": { "$ref": "` + string(depID) + `" },
    "billing_address": { "$ref": "` + string(depID) + `" }
  }
}`

	inliner := NewInliner(r, ec)
	result, err := inliner.Collapse([]byte(rendered))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(result, &doc))

	// Verify $refs were rewritten
	props, ok := doc["properties"].(map[string]any)
	require.True(t, ok)
	shipping, ok := props["shipping_address"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "#/$defs/domain_address_1_2_3", shipping["$ref"])

	billing, ok := props["billing_address"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "#/$defs/domain_address_1_2_3", billing["$ref"])

	// Verify $defs was populated
	defs, ok := doc["$defs"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, defs, "domain_address_1_2_3")

	defBody, ok := defs["domain_address_1_2_3"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "object", defBody["type"])
	assert.NotContains(t, defBody, "$id", "$id should be stripped from inlined $defs")
}

func TestInliner_MultipleRefsToSame(t *testing.T) {
	t.Parallel()

	r := setupTestRegistry(t)
	depKey := Key("domain_shared_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		depKey: `{"$id": "{{ ID }}", "type": "string"}`,
	})

	ec := r.config.ProductionEnvConfig()
	depSchema, err := r.GetSchemaByKey(depKey)
	require.NoError(t, err)
	_, err = r.CoordinateRender(depSchema, ec)
	require.NoError(t, err)

	depID := depSchema.CanonicalID(ec)

	rendered := `{
  "type": "object",
  "properties": {
    "a": { "$ref": "` + string(depID) + `" },
    "b": { "$ref": "` + string(depID) + `" }
  }
}`

	inliner := NewInliner(r, ec)
	result, err := inliner.Collapse([]byte(rendered))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(result, &doc))

	defs, ok := doc["$defs"].(map[string]any)
	require.True(t, ok)
	assert.Len(t, defs, 1, "should have exactly one $defs entry for the shared schema")
}

func TestInliner_TransitiveRefs(t *testing.T) {
	t.Parallel()

	r := setupTestRegistry(t)

	// C has no dependencies
	cKey := Key("domain_c_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		cKey: `{"$id": "{{ ID }}", "type": "string"}`,
	})

	// B references C via JSM template (will resolve to C's canonical ID)
	bKey := Key("domain_b_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		bKey: `{"$id": "{{ ID }}", "type": "object", "properties": {"c": {"$ref": "{{ JSM %%domain_c_1_0_0%% }}"}}}`,
	})

	ec := r.config.ProductionEnvConfig()

	// Pre-render B (which will also render C as a side effect)
	bSchema, err := r.GetSchemaByKey(bKey)
	require.NoError(t, err)
	bRI, err := r.CoordinateRender(bSchema, ec)
	require.NoError(t, err)

	bID := bSchema.CanonicalID(ec)

	// A references B
	rendered := `{
  "type": "object",
  "properties": {
    "b": { "$ref": "` + string(bID) + `" }
  }
}`

	inliner := NewInliner(r, ec)
	result, err := inliner.Collapse([]byte(rendered))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(result, &doc))

	defs, ok := doc["$defs"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, defs, "domain_b_1_0_0", "B should be in $defs")
	assert.Contains(t, defs, "domain_c_1_0_0", "C should be in $defs (transitive)")

	// Verify B's $ref to C was also rewritten
	bDef, ok := defs["domain_b_1_0_0"].(map[string]any)
	require.True(t, ok)
	bProps, ok := bDef["properties"].(map[string]any)
	require.True(t, ok)
	cRef, ok := bProps["c"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "#/$defs/domain_c_1_0_0", cRef["$ref"])

	// Ensure the rendered B content was valid before inlining (sanity check)
	assert.NotEmpty(t, bRI.Rendered)
}

func TestInliner_NoRefs(t *testing.T) {
	t.Parallel()

	r := setupTestRegistry(t)
	ec := r.config.ProductionEnvConfig()

	rendered := `{"type": "object", "properties": {"name": {"type": "string"}}}`

	inliner := NewInliner(r, ec)
	result, err := inliner.Collapse([]byte(rendered))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(result, &doc))

	assert.NotContains(t, doc, "$defs", "no $defs should be added when there are no external refs")
}

func TestInliner_NonJSMRef(t *testing.T) {
	t.Parallel()

	r := setupTestRegistry(t)
	ec := r.config.ProductionEnvConfig()

	rendered := `{
  "type": "object",
  "properties": {
    "ext": { "$ref": "https://external.example.com/some_schema.json" },
    "local": { "$ref": "#/definitions/localThing" }
  }
}`

	inliner := NewInliner(r, ec)
	result, err := inliner.Collapse([]byte(rendered))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(result, &doc))

	// External and local refs should be left untouched
	props, ok := doc["properties"].(map[string]any)
	require.True(t, ok)
	ext, ok := props["ext"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "https://external.example.com/some_schema.json", ext["$ref"])

	local, ok := props["local"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "#/definitions/localThing", local["$ref"])

	assert.NotContains(t, doc, "$defs")
}

func TestInliner_ExistingDefs(t *testing.T) {
	t.Parallel()

	r := setupTestRegistry(t)
	depKey := Key("domain_dep_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		depKey: `{"$id": "{{ ID }}", "type": "number"}`,
	})

	ec := r.config.ProductionEnvConfig()
	depSchema, err := r.GetSchemaByKey(depKey)
	require.NoError(t, err)
	_, err = r.CoordinateRender(depSchema, ec)
	require.NoError(t, err)

	depID := depSchema.CanonicalID(ec)

	rendered := `{
  "type": "object",
  "properties": {
    "d": { "$ref": "` + string(depID) + `" }
  },
  "$defs": {
    "existingDef": { "type": "string" }
  }
}`

	inliner := NewInliner(r, ec)
	result, err := inliner.Collapse([]byte(rendered))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(result, &doc))

	defs, ok := doc["$defs"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, defs, "existingDef", "pre-existing $defs entry should be preserved")
	assert.Contains(t, defs, "domain_dep_1_0_0", "inlined entry should be added")
}

func TestInliner_IdStripped(t *testing.T) {
	t.Parallel()

	r := setupTestRegistry(t)
	depKey := Key("domain_withid_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		depKey: `{"$id": "{{ ID }}", "$schema": "http://json-schema.org/draft-07/schema#", "type": "object"}`,
	})

	ec := r.config.ProductionEnvConfig()
	depSchema, err := r.GetSchemaByKey(depKey)
	require.NoError(t, err)
	_, err = r.CoordinateRender(depSchema, ec)
	require.NoError(t, err)

	depID := depSchema.CanonicalID(ec)

	rendered := `{
  "type": "object",
  "properties": {
    "w": { "$ref": "` + string(depID) + `" }
  }
}`

	inliner := NewInliner(r, ec)
	result, err := inliner.Collapse([]byte(rendered))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(result, &doc))

	defs, ok := doc["$defs"].(map[string]any)
	require.True(t, ok)
	defBody, ok := defs["domain_withid_1_0_0"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, defBody, "$id", "$id should be stripped from inlined schemas")
	assert.Contains(t, defBody, "$schema", "other properties should be preserved")
}

func TestInliner_InvalidJSON(t *testing.T) {
	t.Parallel()

	r := setupTestRegistry(t)
	ec := r.config.ProductionEnvConfig()

	inliner := NewInliner(r, ec)
	_, err := inliner.Collapse([]byte("not json"))
	require.Error(t, err)
}

func TestInliner_RefInArray(t *testing.T) {
	t.Parallel()

	r := setupTestRegistry(t)
	depKey := Key("domain_item_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		depKey: `{"$id": "{{ ID }}", "type": "string"}`,
	})

	ec := r.config.ProductionEnvConfig()
	depSchema, err := r.GetSchemaByKey(depKey)
	require.NoError(t, err)
	_, err = r.CoordinateRender(depSchema, ec)
	require.NoError(t, err)

	depID := depSchema.CanonicalID(ec)

	rendered := `{
  "type": "object",
  "oneOf": [
    { "$ref": "` + string(depID) + `" },
    { "type": "number" }
  ]
}`

	inliner := NewInliner(r, ec)
	result, err := inliner.Collapse([]byte(rendered))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(result, &doc))

	oneOf, ok := doc["oneOf"].([]any)
	require.True(t, ok)
	first, ok := oneOf[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "#/$defs/domain_item_1_0_0", first["$ref"])

	defs, ok := doc["$defs"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, defs, "domain_item_1_0_0")
}

func TestInliner_Errors(t *testing.T) {
	t.Parallel()

	t.Run("invalid json input", func(t *testing.T) {
		t.Parallel()
		r := setupInlinerTestRegistry(t)
		inl := NewInliner(r, r.config.ProductionEnvConfig())
		_, err := inl.Collapse([]byte(`{invalid json}`))
		require.Error(t, err)
	})

	t.Run("missing ref schema - hits renderDef GetSchemaByKey error", func(t *testing.T) {
		t.Parallel()
		r := setupInlinerTestRegistry(t)
		inl := NewInliner(r, r.config.ProductionEnvConfig())
		// Pattern matches but no such schema file
		rendered := `{"$ref": "https://json-schemas.myorg.io/domain_family_missing_1_0_0.schema.json"}`
		_, err := inl.Collapse([]byte(rendered))
		require.Error(t, err)
	})

	t.Run("nested ref schema error - hits processObject rewriteRefs error", func(t *testing.T) {
		t.Parallel()
		r := setupInlinerTestRegistry(t)
		ec := r.config.ProductionEnvConfig()
		inl := NewInliner(r, ec)

		// B exists and is valid, but refers to C which is missing
		keyB := Key("domain_family_b_1_0_0")
		keyC := "https://json-schemas.myorg.io/domain_family_c_1_0_0.schema.json"
		createSchemaFiles(t, r, schemaMap{
			keyB: `{"properties": {"c": {"$ref": "` + keyC + `"}}}`,
		})

		rendered := `{"$ref": "https://json-schemas.myorg.io/domain_family_b_1_0_0.schema.json"}`
		_, err := inl.Collapse([]byte(rendered))
		require.Error(t, err)
	})

	t.Run("renderDef coordinate render error directly", func(t *testing.T) {
		t.Parallel()
		r := setupInlinerTestRegistry(t)
		inl := NewInliner(r, r.config.ProductionEnvConfig())

		key := Key("domain_family_broken_direct_1_0_0")
		createSchemaFiles(t, r, schemaMap{
			key: `{"type": "{{ if }}"}`,
		})

		_, err := inl.renderDef(key)
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing value for if")
	})

	t.Run("unrenderable ref schema - hits renderDef CoordinateRender error", func(t *testing.T) {
		t.Parallel()
		r := setupInlinerTestRegistry(t)
		ec := r.config.ProductionEnvConfig()
		inl := NewInliner(r, ec)

		// Configure compiler to fail during rendering/compilation
		mock, ok := r.compiler.(*mockCompiler)
		require.True(t, ok)
		mock.CompileFunc = func(_ string) (validator.Validator, error) {
			return nil, &InvalidJSONSchemaError{Path: "mock", Wrapped: os.ErrInvalid}
		}

		key := Key("domain_family_test_1_0_0")
		createSchemaFiles(t, r, schemaMap{
			key: `{"type": "string"}`,
		})

		rendered := `{"$ref": "https://json-schemas.myorg.io/domain_family_test_1_0_0.schema.json"}`
		_, err := inl.Collapse([]byte(rendered))
		require.Error(t, err)
	})

	t.Run("matchJSMRef ensures trailing slash", func(t *testing.T) {
		t.Parallel()
		regDir := t.TempDir()
		badConfig := `
environments:
  prod:
    publicUrlRoot: "https://no-slash.io"
    privateUrlRoot: "https://internal.no-slash.io"
    isProduction: true
`
		err := os.WriteFile(filepath.Join(regDir, "json-schema-manager-config.yml"), []byte(badConfig), 0o600)
		require.NoError(t, err)

		r, err := NewRegistry(regDir, &mockCompiler{}, fsh.NewPathResolver(), fsh.NewEnvProvider())
		require.NoError(t, err)

		inl := NewInliner(r, r.config.Environments["prod"])

		createSchemaFiles(t, r, schemaMap{
			Key("domain_family_test_1_0_0"): `{"type": "string"}`,
		})

		// This should still match because matchJSMRef adds the slash
		rendered := `{"$ref": "https://no-slash.io/domain_family_test_1_0_0.schema.json"}`
		res, err := inl.Collapse([]byte(rendered))
		require.NoError(t, err)
		require.Contains(t, string(res), "$defs")
	})

	t.Run("boolean schema - hits renderDef Unmarshal error", func(t *testing.T) {
		t.Parallel()
		r := setupInlinerTestRegistry(t)
		inl := NewInliner(r, r.config.ProductionEnvConfig())

		key := Key("domain_family_bool_schema_1_0_0")
		// 1. Create a valid object schema
		createSchemaFiles(t, r, schemaMap{
			key: `{"x-public": true}`,
		})

		// 2. Load it
		s, err := r.GetSchemaByKey(key)
		require.NoError(t, err)

		// 3. Directly set RenderInfo with a non-object Rendered but non-nil Validator
		// to force CoordinateRender to return it early.
		s.mu.Lock()
		s.computed.StoreRenderInfo("prod", RenderInfo{
			Rendered:     []byte("true"),
			Unmarshalled: true, // boolean schema
			Validator:    &mockValidator{},
		})
		s.computed.StoreID("prod", "https://json-schemas.myorg.io/domain_family_bool_schema_1_0_0.schema.json")
		s.mu.Unlock()

		rendered := `{"$ref": "https://json-schemas.myorg.io/domain_family_bool_schema_1_0_0.schema.json"}`
		res, err := inl.Collapse([]byte(rendered))
		if err == nil {
			t.Fatalf("Expected Unmarshal error but got nil. Result: %s", string(res))
		}
		require.Contains(t, err.Error(), "cannot unmarshal bool")
	})

	t.Run("error in array items", func(t *testing.T) {
		t.Parallel()
		r := setupInlinerTestRegistry(t)
		inl := NewInliner(r, r.config.ProductionEnvConfig())
		rendered := `{"items": [{"$ref": "https://json-schemas.myorg.io/domain_family_missing_1_0_0.schema.json"}]}`
		_, err := inl.Collapse([]byte(rendered))
		require.Error(t, err)
	})

	t.Run("error in deep object properties", func(t *testing.T) {
		t.Parallel()
		r := setupInlinerTestRegistry(t)
		inl := NewInliner(r, r.config.ProductionEnvConfig())
		rendered := `{"a": {"b": {"$ref": "https://json-schemas.myorg.io/domain_family_missing_1_0_0.schema.json"}}}`
		_, err := inl.Collapse([]byte(rendered))
		require.Error(t, err)
	})

	t.Run("hits mergeDefs and continue branches", func(t *testing.T) {
		t.Parallel()
		r := setupInlinerTestRegistry(t)
		inl := NewInliner(r, r.config.ProductionEnvConfig())

		key := Key("domain_family_simple_1_0_0")
		createSchemaFiles(t, r, schemaMap{
			key: `{"type": "string"}`,
		})

		// Full workflow to hit defs merging and the continue branch in processObject
		rendered := `{"$ref": "https://json-schemas.myorg.io/domain_family_simple_1_0_0.schema.json", "other": "prop"}`
		res, err := inl.Collapse([]byte(rendered))
		require.NoError(t, err)
		require.Contains(t, string(res), "$defs")
		require.Contains(t, string(res), "domain_family_simple_1_0_0")
	})

	t.Run("external ref - hits matchJSMRef false branch", func(t *testing.T) {
		t.Parallel()
		r := setupInlinerTestRegistry(t)
		inl := NewInliner(r, r.config.ProductionEnvConfig())
		rendered := `{"$ref": "https://example.com/schema.json"}`
		res, err := inl.Collapse([]byte(rendered))
		require.NoError(t, err)
		// Should NOT be rewritten
		require.Contains(t, string(res), "https://example.com/schema.json")
		require.NotContains(t, string(res), "$defs")
	})
}
