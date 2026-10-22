package structdiff

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parse(t *testing.T, s string) any {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal([]byte(s), &v))
	return v
}

// summary is the comparable part of a Change.
type summary struct {
	Location string
	Keyword  string
	Effect   Effect
	Detail   string
}

func summaries(changes []Change) []summary {
	out := make([]summary, len(changes))
	for i, c := range changes {
		out[i] = summary{c.Location, c.Keyword, c.Effect, c.Detail}
	}
	return out
}

func TestCompare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		older  string
		newer  string
		want   []summary
		used   []string // documents which use the first change
		unused []string // documents which do not
	}{
		{
			name:  "identical schemas",
			older: `{"type": "object", "title": "a"}`,
			newer: `{"type": "object", "title": "b", "description": "annotations are ignored"}`,
		},
		{
			name:   "enum value added",
			older:  `{"properties": {"status": {"enum": ["placed", "paid"]}}}`,
			newer:  `{"properties": {"status": {"enum": ["placed", "paid", "refunded"]}}}`,
			want:   []summary{{"/status", "enum", Widens, `adds "refunded"`}},
			used:   []string{`{"status": "refunded"}`},
			unused: []string{`{"status": "paid"}`, `{}`, `"not an object"`},
		},
		{
			name:  "enum value removed",
			older: `{"enum": [1, 2]}`,
			newer: `{"enum": [1]}`,
			want:  []summary{{"/", "enum", Narrows, `removes 2`}},
		},
		{
			name:   "enum removed",
			older:  `{"enum": ["a"]}`,
			newer:  `{}`,
			want:   []summary{{"/", "enum", Widens, "no longer restricts the values"}},
			used:   []string{`"b"`},
			unused: []string{`"a"`},
		},
		{
			name:  "type added",
			older: `{}`,
			newer: `{"type": "string"}`,
			want:  []summary{{"/", "type", Narrows, `restricts the type to "string"`}},
		},
		{
			name:  "enum added",
			older: `{}`,
			newer: `{"enum": ["a", "b"]}`,
			want:  []summary{{"/", "enum", Narrows, `restricts the values to "a", "b"`}},
		},
		{
			name:   "type widened",
			older:  `{"type": "string"}`,
			newer:  `{"type": ["string", "null"]}`,
			want:   []summary{{"/", "type", Widens, `allows "null"`}},
			used:   []string{`null`},
			unused: []string{`"s"`},
		},
		{
			name:  "type narrowed",
			older: `{"type": ["string", "number"]}`,
			newer: `{"type": "string"}`,
			want:  []summary{{"/", "type", Narrows, `no longer allows "number"`}},
		},
		{
			name:  "integer is covered by number",
			older: `{"type": "number"}`,
			newer: `{"type": "integer"}`,
			want:  []summary{{"/", "type", Narrows, `no longer allows "number"`}},
		},
		{
			name:   "number widens integer",
			older:  `{"type": "integer"}`,
			newer:  `{"type": "number"}`,
			want:   []summary{{"/", "type", Widens, `allows "number"`}},
			used:   []string{`1.5`},
			unused: []string{`2`},
		},
		{
			name:   "type removed",
			older:  `{"type": ["object", "array", "boolean", "number"]}`,
			newer:  `{}`,
			want:   []summary{{"/", "type", Widens, "no longer restricts the type"}},
			used:   []string{`"s"`, `null`},
			unused: []string{`{}`, `[]`, `true`, `1`},
		},
		{
			name:  "const changed",
			older: `{"const": "a"}`,
			newer: `{"const": "b"}`,
			want:  []summary{{"/", "const", Widens, `requires "b" instead of "a"`}},
			used:  []string{`"b"`},
		},
		{
			name:   "const removed",
			older:  `{"const": "a"}`,
			newer:  `{}`,
			want:   []summary{{"/", "const", Widens, `no longer requires "a"`}},
			used:   []string{`"c"`},
			unused: []string{`"a"`},
		},
		{
			name:  "const added",
			older: `{}`,
			newer: `{"const": 1}`,
			want:  []summary{{"/", "const", Narrows, `requires 1`}},
		},
		{
			name:   "maxLength raised",
			older:  `{"properties": {"name": {"maxLength": 3}}}`,
			newer:  `{"properties": {"name": {"maxLength": 5}}}`,
			want:   []summary{{"/name", "maxLength", Widens, "relaxes the limit from 3 to 5"}},
			used:   []string{`{"name": "abcd"}`},
			unused: []string{`{"name": "abc"}`, `{"name": 12345}`},
		},
		{
			name:  "maxLength lowered",
			older: `{"maxLength": 5}`,
			newer: `{"maxLength": 3}`,
			want:  []summary{{"/", "maxLength", Narrows, "tightens the limit from 5 to 3"}},
		},
		{
			name:   "minimum lowered",
			older:  `{"minimum": 0}`,
			newer:  `{"minimum": -1}`,
			want:   []summary{{"/", "minimum", Widens, "relaxes the limit from 0 to -1"}},
			used:   []string{`-0.5`},
			unused: []string{`0`},
		},
		{
			name:  "minimum raised",
			older: `{"minimum": 0}`,
			newer: `{"minimum": 1}`,
			want:  []summary{{"/", "minimum", Narrows, "tightens the limit from 0 to 1"}},
		},
		{
			name:   "exclusiveMaximum removed",
			older:  `{"exclusiveMaximum": 10}`,
			newer:  `{}`,
			want:   []summary{{"/", "exclusiveMaximum", Widens, "removes the limit of 10"}},
			used:   []string{`10`},
			unused: []string{`9`},
		},
		{
			name:   "exclusiveMinimum removed",
			older:  `{"exclusiveMinimum": 0}`,
			newer:  `{}`,
			want:   []summary{{"/", "exclusiveMinimum", Widens, "removes the limit of 0"}},
			used:   []string{`0`},
			unused: []string{`1`},
		},
		{
			name:  "maxItems added",
			older: `{}`,
			newer: `{"maxItems": 2}`,
			want:  []summary{{"/", "maxItems", Narrows, "adds a limit of 2"}},
		},
		{
			name:   "minItems removed",
			older:  `{"minItems": 1}`,
			newer:  `{}`,
			want:   []summary{{"/", "minItems", Widens, "removes the limit of 1"}},
			used:   []string{`[]`},
			unused: []string{`[1]`},
		},
		{
			name:   "maxProperties raised",
			older:  `{"maxProperties": 1}`,
			newer:  `{"maxProperties": 2}`,
			want:   []summary{{"/", "maxProperties", Widens, "relaxes the limit from 1 to 2"}},
			used:   []string{`{"a": 1, "b": 2}`},
			unused: []string{`{"a": 1}`},
		},
		{
			name:  "draft-04 boolean exclusiveMaximum",
			older: `{"maximum": 5, "exclusiveMaximum": false}`,
			newer: `{"maximum": 5, "exclusiveMaximum": true}`,
			want:  []summary{{"/", "exclusiveMaximum", Alters, "changes exclusiveMaximum"}},
		},
		{
			name:   "pattern changed",
			older:  `{"pattern": "^[A-Z]{3}$"}`,
			newer:  `{"pattern": "^[A-Za-z]{3}$"}`,
			want:   []summary{{"/", "pattern", Alters, `changes the pattern from "^[A-Z]{3}$" to "^[A-Za-z]{3}$"`}},
			used:   []string{`"eur"`},
			unused: []string{`"EUR"`, `3`},
		},
		{
			name:  "pattern removed",
			older: `{"pattern": "^a"}`,
			newer: `{}`,
			want:  []summary{{"/", "pattern", Widens, `removes the pattern "^a"`}},
			used:  []string{`"b"`},
		},
		{
			name:  "pattern added",
			older: `{}`,
			newer: `{"pattern": "^a"}`,
			want:  []summary{{"/", "pattern", Narrows, `adds the pattern "^a"`}},
		},
		{
			name:  "pattern Go cannot compile",
			older: `{"pattern": "(?<=a)b"}`,
			newer: `{}`,
			want:  []summary{{"/", "pattern", Widens, `removes the pattern "(?<=a)b"`}},
			used:  []string{`"c"`}, // any value, since Go cannot tell which the pattern rejects
		},
		{
			name:   "multipleOf removed",
			older:  `{"multipleOf": 2}`,
			newer:  `{}`,
			want:   []summary{{"/", "multipleOf", Widens, "no longer requires a multiple of 2"}},
			used:   []string{`3`},
			unused: []string{`4`, `"s"`},
		},
		{
			name:  "multipleOf added",
			older: `{}`,
			newer: `{"multipleOf": 0.5}`,
			want:  []summary{{"/", "multipleOf", Narrows, "requires a multiple of 0.5"}},
		},
		{
			name:  "multipleOf of a multiple",
			older: `{"multipleOf": 2}`,
			newer: `{"multipleOf": 4}`,
			want:  []summary{{"/", "multipleOf", Narrows, "requires a multiple of 4 instead of 2"}},
		},
		{
			name:  "multipleOf of a divisor",
			older: `{"multipleOf": 4}`,
			newer: `{"multipleOf": 2}`,
			want:  []summary{{"/", "multipleOf", Widens, "requires a multiple of 2 instead of 4"}},
			used:  []string{`6`},
		},
		{
			name:   "required removed",
			older:  `{"required": ["a", "b"]}`,
			newer:  `{"required": ["a"]}`,
			want:   []summary{{"/", "required", Widens, `no longer requires "b"`}},
			used:   []string{`{"a": 1}`},
			unused: []string{`{"a": 1, "b": 2}`, `[]`},
		},
		{
			name:  "required added",
			older: `{}`,
			newer: `{"required": ["a"]}`,
			want:  []summary{{"/", "required", Narrows, `requires "a"`}},
		},
		{
			name:   "property added to a closed object",
			older:  `{"properties": {"a": {}}, "additionalProperties": false}`,
			newer:  `{"properties": {"a": {}, "b": {"type": "string"}}, "additionalProperties": false}`,
			want:   []summary{{"/b", "", Widens, "accepts values where the earlier version accepted none"}},
			used:   []string{`{"b": "x"}`},
			unused: []string{`{"a": 1}`},
		},
		{
			name:  "property added to an open object",
			older: `{"properties": {"a": {}}}`,
			newer: `{"properties": {"a": {}, "b": {"type": "string"}}}`,
			want:  []summary{{"/b", "", Narrows, "adds constraints where the earlier version had none"}},
		},
		{
			name:  "property removed from an open object",
			older: `{"properties": {"b": {"type": "string"}}}`,
			newer: `{}`,
			want:  []summary{{"/b", "", Widens, "removes every constraint"}},
		},
		{
			name:  "property removed from a closed object",
			older: `{"properties": {"b": {}}, "additionalProperties": false}`,
			newer: `{"additionalProperties": false}`,
			want:  []summary{{"/b", "", Narrows, "accepts no values"}},
		},
		{
			name:  "property declared by a pattern",
			older: `{"patternProperties": {"^x-": {"type": "string"}}, "additionalProperties": false}`,
			newer: `{"properties": {"x-a": {"type": "string"}}, "patternProperties": {"^x-": {"type": "string"}},` +
				`"additionalProperties": false}`,
		},
		{
			name:  "object opened",
			older: `{"properties": {"a": {}}, "patternProperties": {"^x-": {}}, "additionalProperties": false}`,
			newer: `{"properties": {"a": {}}, "patternProperties": {"^x-": {}}}`,
			want: []summary{
				{"/", "additionalProperties", Widens, "accepts values where the earlier version accepted none"},
			},
			used:   []string{`{"a": 1, "b": 2}`},
			unused: []string{`{"a": 1, "x-b": 2}`, `[]`},
		},
		{
			name:  "object closed",
			older: `{}`,
			newer: `{"additionalProperties": false}`,
			want:  []summary{{"/", "additionalProperties", Narrows, "accepts no values"}},
		},
		{
			name:   "additionalProperties subschema relaxed",
			older:  `{"additionalProperties": {"maxLength": 1}}`,
			newer:  `{"additionalProperties": {"maxLength": 2}}`,
			want:   []summary{{"/*", "maxLength", Widens, "relaxes the limit from 1 to 2"}},
			used:   []string{`{"z": "ab"}`},
			unused: []string{`{"z": "a"}`},
		},
		{
			name:   "items subschema",
			older:  `{"items": {"enum": [1]}}`,
			newer:  `{"items": {"enum": [1, 2]}}`,
			want:   []summary{{"/*", "enum", Widens, "adds 2"}},
			used:   []string{`[1, 2]`},
			unused: []string{`[1]`},
		},
		{
			name:  "items added",
			older: `{}`,
			newer: `{"items": {"type": "string"}}`,
			want:  []summary{{"/*", "", Narrows, "adds constraints where the earlier version had none"}},
		},
		{
			name:  "items removed",
			older: `{"items": false}`,
			newer: `{}`,
			want:  []summary{{"/*", "", Widens, "accepts values where the earlier version accepted none"}},
			used:  []string{`[1]`},
		},
		{
			name:   "tuple items",
			older:  `{"prefixItems": [{"const": 1}]}`,
			newer:  `{"prefixItems": [{"const": 1}, {"const": 2}]}`,
			want:   []summary{{"/1", "", Narrows, "adds constraints where the earlier version had none"}},
			used:   []string{`[1, 2]`},
			unused: []string{`[1]`},
		},
		{
			name:   "tuple item relaxed",
			older:  `{"items": [{"maximum": 1}]}`,
			newer:  `{"items": [{"maximum": 2}]}`,
			want:   []summary{{"/0", "maximum", Widens, "relaxes the limit from 1 to 2"}},
			used:   []string{`[2]`},
			unused: []string{`[1]`, `[]`, `{"0": 2}`},
		},
		{
			name:  "tuple to list",
			older: `{"items": [{}]}`,
			newer: `{"items": {}}`,
			want: []summary{
				{"/", "items", Alters, "changes between one subschema for every item and one per position"},
			},
		},
		{
			name:  "anyOf alternative added",
			older: `{"anyOf": [{"type": "string"}]}`,
			newer: `{"anyOf": [{"type": "string"}, {"type": "null"}]}`,
			want:  []summary{{"/", "anyOf", Widens, "adds a subschema"}},
		},
		{
			name:  "oneOf alternative removed",
			older: `{"oneOf": [{"type": "string"}, {"type": "null"}]}`,
			newer: `{"oneOf": [{"type": "string"}]}`,
			want:  []summary{{"/", "oneOf", Narrows, "removes a subschema"}},
		},
		{
			name:  "allOf subschema added and removed",
			older: `{"properties": {"a": {"allOf": [{}]}, "b": {"allOf": [{}, {"maxLength": 2}]}}}`,
			newer: `{"properties": {"a": {"allOf": [{}, {}]}, "b": {"allOf": [{}]}}}`,
			want: []summary{
				{"/a", "allOf", Narrows, "adds a subschema"},
				{"/b", "allOf", Widens, "removes a subschema"},
			},
		},
		{
			name:  "allOf subschema changed",
			older: `{"allOf": [{"maxLength": 2}]}`,
			newer: `{"allOf": [{"maxLength": 3}]}`,
			want:  []summary{{"/", "maxLength", Widens, "relaxes the limit from 2 to 3"}},
		},
		{
			name:  "boolean schemas",
			older: `{"properties": {"a": true, "b": {}, "c": false}}`,
			newer: `{"properties": {"a": false, "b": true, "c": false}}`,
			want:  []summary{{"/a", "", Narrows, "accepts no values"}, {"/b", "", Widens, "removes every constraint"}},
		},
		{
			name:   "local $ref followed",
			older:  `{"$defs": {"code": {"maxLength": 3}}, "properties": {"c": {"$ref": "#/$defs/code"}}}`,
			newer:  `{"$defs": {"code": {"maxLength": 4}}, "properties": {"c": {"$ref": "#/$defs/code"}}}`,
			want:   []summary{{"/c", "maxLength", Widens, "relaxes the limit from 3 to 4"}},
			used:   []string{`{"c": "abcd"}`},
			unused: []string{`{"c": "abc"}`},
		},
		{
			name: "chained local $refs and an inlined subschema",
			older: `{"definitions": {"a": {"$ref": "#/definitions/b"}, "b": {"maxLength": 3}},` +
				`"properties": {"c": {"$ref": "#/definitions/a"}}}`,
			newer: `{"properties": {"c": {"maxLength": 3}}}`,
		},
		{
			name:  "recursive local $ref",
			older: `{"properties": {"child": {"$ref": "#"}, "n": {"maximum": 1}}}`,
			newer: `{"properties": {"child": {"$ref": "#"}, "n": {"maximum": 2}}}`,
			want:  []summary{{"/n", "maximum", Widens, "relaxes the limit from 1 to 2"}},
			used:  []string{`{"n": 2}`},
		},
		{
			name:  "unresolvable and anchor $refs are compared as keywords",
			older: `{"properties": {"a": {"$ref": "#/missing"}, "b": {"$ref": "#name"}, "c": {"$ref": "#/properties/a/x"}}}`,
			newer: `{"properties": {"a": {"$ref": "#/missing"}, "b": {"$ref": "#name"}, "c": {"$ref": "#/properties/a/x"}}}`,
		},
		{
			name: "pointer into an array and through a non-container",
			older: `{"anyOf": [{"maxLength": 1}], "properties": {"a": {"$ref": "#/anyOf/0"},` +
				`"b": {"$ref": "#/anyOf/x"}, "c": {"$ref": "#/anyOf/0/maxLength/x"}}}`,
			newer: `{"anyOf": [{"maxLength": 1}], "properties": {"a": {"maxLength": 2},` +
				`"b": {"$ref": "#/anyOf/x"}, "c": {"$ref": "#/anyOf/0/maxLength/x"}}}`,
			want: []summary{{"/a", "maxLength", Widens, "relaxes the limit from 1 to 2"}},
		},
		{
			name:  "external $ref changed",
			older: `{"$ref": "https://example.com/a_1_0_0.schema.json"}`,
			newer: `{"$ref": "https://example.com/a_1_1_0.schema.json"}`,
			want:  []summary{{"/", "$ref", Alters, "changes $ref"}},
		},
		{
			name:  "other keywords added and removed",
			older: `{"not": {"type": "null"}}`,
			newer: `{"uniqueItems": true}`,
			want:  []summary{{"/", "not", Widens, "removes not"}, {"/", "uniqueItems", Narrows, "adds uniqueItems"}},
		},
		{
			name:  "property names escaped in the location",
			older: `{"properties": {"a/b~c": {"maxLength": 1}}}`,
			newer: `{"properties": {"a/b~c": {"maxLength": 2}}}`,
			want:  []summary{{"/a~1b~0c", "maxLength", Widens, "relaxes the limit from 1 to 2"}},
			used:  []string{`{"a/b~c": "ab"}`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			changes := Compare(parse(t, tt.older), parse(t, tt.newer))
			if len(tt.want) == 0 {
				assert.Empty(t, changes)
				return
			}
			assert.Equal(t, tt.want, summaries(changes))
			for _, d := range tt.used {
				assert.True(t, changes[0].UsedBy(parse(t, d)), "used by %s", d)
			}
			for _, d := range tt.unused {
				assert.False(t, changes[0].UsedBy(parse(t, d)), "not used by %s", d)
			}
		})
	}
}

func TestChangeMayBreak(t *testing.T) {
	t.Parallel()

	assert.True(t, (&Change{Effect: Widens}).MayBreak())
	assert.True(t, (&Change{Effect: Alters}).MayBreak())
	assert.False(t, (&Change{Effect: Narrows}).MayBreak())
}

func TestChangeString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/a: maxLength adds a limit of 1", (&Change{
		Location: "/a", Keyword: "maxLength",
		Detail: "adds a limit of 1",
	}).String())
	assert.Equal(t, "/a: accepts no values", (&Change{Location: "/a", Detail: "accepts no values"}).String())
}

func TestUsedByUnknown(t *testing.T) {
	t.Parallel()

	changes := Compare(parse(t, `{"properties": {"a": {"anyOf": [{}]}}}`),
		parse(t, `{"properties": {"a": {"anyOf": [{}, {}]}}}`))
	require.Len(t, changes, 1)
	assert.True(t, changes[0].UsedBy(parse(t, `{"a": 1}`)), "any value at the location uses the change")
	assert.False(t, changes[0].UsedBy(parse(t, `{"b": 1}`)))
}

func TestNumber(t *testing.T) {
	t.Parallel()

	n, ok := number(json.Number("1.5"))
	assert.True(t, ok)
	assert.InDelta(t, 1.5, n, 0)
	_, ok = number(json.Number("x"))
	assert.False(t, ok)
	assert.True(t, matchesType(json.Number("2"), []string{"integer"}))
	assert.False(t, matchesType(1, []string{"unknown"}))
}
