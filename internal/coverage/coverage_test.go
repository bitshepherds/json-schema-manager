package coverage

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/validator"
)

func parse(t *testing.T, s string) any {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal([]byte(s), &v))
	return v
}

// compiled returns a Validates for schema, which compiles each subschema with the validator.
func compiled(t *testing.T, schema any) Validates {
	t.Helper()
	const id = "https://example.com/s.schema.json"
	c := validator.NewSanthoshCompiler()
	require.NoError(t, c.AddSchema(id, schema))
	_, err := c.Compile(id)
	require.NoError(t, err)
	return func(pointer string, v any) (bool, bool) {
		sub, cErr := c.Compile(id + "#" + pointer)
		if cErr != nil {
			return false, false
		}
		return sub.Validate(v) == nil, true
	}
}

func measure(t *testing.T, schema string, docs ...string) []Point {
	t.Helper()
	s := parse(t, schema)
	parsed := make([]any, len(docs))
	for i, d := range docs {
		parsed[i] = parse(t, d)
	}
	return Measure(s, parsed, compiled(t, s))
}

func TestMeasure_EnumValueNotUsed(t *testing.T) {
	t.Parallel()

	points := measure(t, `{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"properties": {"status": {"enum": ["placed", "paid", "refunded"]}, "note": {"type": "string"}}
	}`, `{"status": "paid"}`, `{"status": "placed"}`)

	assert.Equal(t, []Point{
		{Location: "/", Kind: Property, Detail: "note", Covered: false},
		{Location: "/", Kind: Property, Detail: "status", Covered: true},
		{Location: "/status", Kind: EnumValue, Detail: `"placed"`, Covered: true},
		{Location: "/status", Kind: EnumValue, Detail: `"paid"`, Covered: true},
		{Location: "/status", Kind: EnumValue, Detail: `"refunded"`, Covered: false},
	}, points)
}

func TestMeasure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema string
		docs   []string
		want   []Point
	}{
		{
			name:   "types",
			schema: `{"type": ["string", "null"]}`,
			docs:   []string{`"s"`},
			want:   []Point{{"/", Type, "string", true}, {"/", Type, "null", false}},
		},
		{
			name:   "a single type is not a point",
			schema: `{"type": "string"}`,
			docs:   []string{`"s"`},
		},
		{
			name:   "anyOf and oneOf branches",
			schema: `{"anyOf": [{"type": "string"}, {"type": "number"}], "oneOf": [{"const": "a"}, {"const": "b"}]}`,
			docs:   []string{`"a"`},
			want: []Point{
				{"/", Branch, "anyOf/0", true},
				{"/", Branch, "anyOf/1", false},
				{"/", Branch, "oneOf/0", true},
				{"/", Branch, "oneOf/1", false},
			},
		},
		{
			name:   "branches with no documents",
			schema: `{"anyOf": [{"type": "string"}]}`,
			want:   []Point{{"/", Branch, "anyOf/0", false}},
		},
		{
			name:   "if paths",
			schema: `{"if": {"type": "string"}, "then": {"minLength": 1}, "else": {"type": "number"}}`,
			docs:   []string{`"a"`},
			want:   []Point{{"/", Then, "if", true}, {"/", Else, "if", false}},
		},
		{
			name:   "if paths, both reached",
			schema: `{"if": {"type": "string"}}`,
			docs:   []string{`"a"`, `1`},
			want:   []Point{{"/", Then, "if", true}, {"/", Else, "if", true}},
		},
		{
			name: "nested subschemas and local refs",
			schema: `{
				"$defs": {"money": {"properties": {"currency": {"enum": ["EUR", "GBP"]}}}},
				"properties": {
					"total": {"$ref": "#/$defs/money"},
					"lines": {"items": {"properties": {"sku": {}}}},
					"pair": {"prefixItems": [{"enum": [1]}], "items": false},
					"tags": {"additionalProperties": {"enum": [true]}},
					"meta": {"patternProperties": {"^x-": {"enum": [0]}}},
					"all": {"allOf": [{"enum": ["y"]}]}
				}
			}`,
			docs: []string{`{"total": {"currency": "EUR"}, "lines": [{"sku": "a"}], "pair": [1], "tags": {"t": true}}`},
			want: []Point{
				{"/", Property, "all", false},
				{"/all", EnumValue, `"y"`, false},
				{"/", Property, "lines", true},
				{"/lines/*", Property, "sku", true},
				{"/", Property, "meta", false},
				{"/meta/*", EnumValue, "0", false},
				{"/", Property, "pair", true},
				{"/pair/0", EnumValue, "1", true},
				{"/", Property, "tags", true},
				{"/tags/*", EnumValue, "true", true},
				{"/", Property, "total", true},
				{"/total", Property, "currency", true},
				{"/total/currency", EnumValue, `"EUR"`, true},
				{"/total/currency", EnumValue, `"GBP"`, false},
			},
		},
		{
			name:   "draft-07 tuple items",
			schema: `{"$schema": "http://json-schema.org/draft-07/schema#", "items": [{"enum": ["a"]}]}`,
			docs:   []string{`["a"]`},
			want:   []Point{{"/0", EnumValue, `"a"`, true}},
		},
		{
			name:   "recursive schema",
			schema: `{"properties": {"child": {"$ref": "#"}, "name": {"enum": ["n"]}}}`,
			docs:   []string{`{"child": {"name": "n"}}`},
			want: []Point{
				{"/", Property, "child", true},
				{"/", Property, "name", false},
				{"/name", EnumValue, `"n"`, false},
			},
		},
		{
			name:   "boolean subschemas",
			schema: `{"properties": {"a": true}}`,
			docs:   []string{`{"a": 1}`},
			want:   []Point{{"/", Property, "a", true}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := measure(t, tt.schema, tt.docs...)
			if len(tt.want) == 0 {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMeasure_CannotTell(t *testing.T) {
	t.Parallel()

	schema := parse(t, `{"anyOf": [{}], "if": {}}`)
	cannotTell := func(string, any) (bool, bool) { return false, false }
	assert.Empty(t, Measure(schema, []any{parse(t, `1`)}, cannotTell),
		"points which cannot be checked are left out")
}
