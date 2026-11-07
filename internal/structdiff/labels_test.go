package structdiff

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// changesOf compares the schemas, and returns the comparable part of each change.
func changesOf(t *testing.T, older, newer string, opts ...Option) []summary {
	t.Helper()
	return summaries(Compare(parse(t, older), parse(t, newer), opts...))
}

func TestDiffLabelsNotWithFlippedPolarity(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []summary{{"/", "maximum", Narrows, "relaxes the limit from 5 to 10"}},
		changesOf(t, `{"not": {"maximum": 5}}`, `{"not": {"maximum": 10}}`),
		"not excluding more accepts less")
	assert.Equal(t, []summary{{"/", "maximum", Widens, "tightens the limit from 10 to 5"}},
		changesOf(t, `{"not": {"maximum": 10}}`, `{"not": {"maximum": 5}}`),
		"not excluding less accepts more")
	assert.Equal(t, []summary{{"/", "maximum", Widens, "relaxes the limit from 5 to 10"}},
		changesOf(t, `{"not": {"not": {"maximum": 5}}}`, `{"not": {"not": {"maximum": 10}}}`),
		"two nots cancel out")
	assert.Equal(t, []summary{{"/", "not", Narrows, "adds not"}}, changesOf(t, `{}`, `{"not": {"type": "null"}}`))
	assert.Equal(t, []summary{{"/", "not", Widens, "removes not"}}, changesOf(t, `{"not": {"type": "null"}}`, `{}`))
}

func TestDiffLabelsOneOfArmConstraintAsAlters(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []summary{{"/", "maximum", Alters, "adds a limit of 10"}},
		changesOf(t, `{"oneOf": [{"type": "number"}, {"minimum": 0}]}`,
			`{"oneOf": [{"type": "number"}, {"minimum": 0, "maximum": 10}]}`))

	t.Run("an arm added or removed", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []summary{{"/", "oneOf", Alters, "adds a subschema"}},
			changesOf(t, `{"oneOf": [{"minimum": 0}]}`, `{"oneOf": [{"minimum": 0}, {"maximum": 5}]}`))
		assert.Equal(t, []summary{{"/", "oneOf", Alters, "removes a subschema"}},
			changesOf(t, `{"oneOf": [{"minimum": 0}, {"maximum": 5}]}`, `{"oneOf": [{"maximum": 5}]}`))
	})

	t.Run("the keyword appearing or going is exact", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []summary{{"/", "oneOf", Narrows, "adds oneOf"}},
			changesOf(t, `{}`, `{"oneOf": [{"minimum": 0}, {"maximum": 5}]}`))
		assert.Equal(t, []summary{{"/", "anyOf", Widens, "removes anyOf"}},
			changesOf(t, `{"anyOf": [{"minimum": 0}]}`, `{}`))
	})
}

func TestDiffLabelsIfAsAlters(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []summary{{"/", "maximum", Alters, "relaxes the limit from 1 to 2"}},
		changesOf(t, `{"if": {"maximum": 1}, "then": {}}`, `{"if": {"maximum": 2}, "then": {}}`))
	assert.Equal(t, []summary{{"/", "maximum", Alters, "relaxes the limit from 1 to 2"}},
		changesOf(t, `{"if": {}, "then": {"maximum": 1}}`, `{"if": {}, "then": {"maximum": 2}}`))
	assert.Equal(t, []summary{{"/", "maximum", Alters, "relaxes the limit from 1 to 2"}},
		changesOf(t, `{"if": {}, "else": {"maximum": 1}}`, `{"if": {}, "else": {"maximum": 2}}`))
	assert.Equal(t, []summary{{"/", "if", Alters, "adds if"}}, changesOf(t, `{}`, `{"if": {}}`))
	assert.Equal(t, []summary{{"/", "if", Alters, "removes if"}}, changesOf(t, `{"if": {}}`, `{}`))
}

func TestDiffKeepsExactLabelForDisjointOneOfArms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		older, newer string
		want         []summary
	}{
		{
			name:  "different types",
			older: `{"oneOf": [{"type": "string", "maxLength": 1}, {"type": "null"}]}`,
			newer: `{"oneOf": [{"type": "string", "maxLength": 2}, {"type": "null"}]}`,
			want:  []summary{{"/", "maxLength", Widens, "relaxes the limit from 1 to 2"}},
		},
		{
			name:  "an arm added",
			older: `{"oneOf": [{"type": "string"}]}`,
			newer: `{"oneOf": [{"type": "string"}, {"type": ["null", "boolean"]}]}`,
			want:  []summary{{"/", "oneOf", Widens, "adds a subschema"}},
		},
		{
			name: "different consts on a required property, through a $ref",
			older: `{"$defs": {"card": {"const": "card"}}, "oneOf": [
				{"required": ["kind"], "properties": {"kind": {"$ref": "#/$defs/card"}, "n": {"maximum": 1}}},
				{"required": ["kind"], "properties": {"kind": {"const": "cash"}}}]}`,
			newer: `{"$defs": {"card": {"const": "card"}}, "oneOf": [
				{"required": ["kind"], "properties": {"kind": {"$ref": "#/$defs/card"}, "n": {"maximum": 2}}},
				{"required": ["kind"], "properties": {"kind": {"const": "cash"}}}]}`,
			want: []summary{{"/n", "maximum", Widens, "relaxes the limit from 1 to 2"}},
		},
		{
			name: "the same const is not disjoint",
			older: `{"oneOf": [{"required": ["k"], "properties": {"k": {"const": 1}, "n": {"maximum": 1}}},
				{"required": ["k"], "properties": {"k": {"const": 1}}}]}`,
			newer: `{"oneOf": [{"required": ["k"], "properties": {"k": {"const": 1}, "n": {"maximum": 2}}},
				{"required": ["k"], "properties": {"k": {"const": 1}}}]}`,
			want: []summary{{"/n", "maximum", Alters, "relaxes the limit from 1 to 2"}},
		},
		{
			name: "a const on a property only one arm requires is not disjoint",
			older: `{"oneOf": [{"required": ["k"], "properties": {"k": {"const": 1}, "n": {"maximum": 1}}},
				{"properties": {"k": {"const": 2}}}]}`,
			newer: `{"oneOf": [{"required": ["k"], "properties": {"k": {"const": 1}, "n": {"maximum": 2}}},
				{"properties": {"k": {"const": 2}}}]}`,
			want: []summary{{"/n", "maximum", Alters, "relaxes the limit from 1 to 2"}},
		},
		{
			name:  "a boolean arm is not disjoint",
			older: `{"oneOf": [{"maximum": 1}, true]}`,
			newer: `{"oneOf": [{"maximum": 2}, true]}`,
			want:  []summary{{"/", "maximum", Alters, "relaxes the limit from 1 to 2"}},
		},
		{
			name:  "disjoint only in the older version",
			older: `{"oneOf": [{"type": "string", "maxLength": 1}, {"type": "null"}]}`,
			newer: `{"oneOf": [{"type": "string", "maxLength": 2}, {"type": "string"}]}`,
			want: []summary{
				{"/", "maxLength", Alters, "relaxes the limit from 1 to 2"},
				{"/", "type", Alters, `allows "string"`},
				{"/", "type", Alters, `no longer allows "null"`},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, changesOf(t, tt.older, tt.newer))
		})
	}
}

func TestOverlappingTypesAreNotDisjoint(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []summary{{"/", "maximum", Alters, "relaxes the limit from 1 to 2"}},
		changesOf(t, `{"oneOf": [{"type": "integer", "maximum": 1}, {"type": "number"}]}`,
			`{"oneOf": [{"type": "integer", "maximum": 2}, {"type": "number"}]}`))
	assert.Equal(t, []summary{{"/", "maximum", Alters, "relaxes the limit from 1 to 2"}},
		changesOf(t, `{"oneOf": [{"type": ["string", "null"], "maximum": 1}, {"type": "null"}]}`,
			`{"oneOf": [{"type": ["string", "null"], "maximum": 2}, {"type": "null"}]}`))
}

func TestDiffMatchesReorderedArms(t *testing.T) {
	t.Parallel()

	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		t.Run(keyword, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, changesOf(t,
				`{"`+keyword+`": [{"type": "string", "maxLength": 3}, {"type": "number"}, {"type": "null"}]}`,
				`{"`+keyword+`": [{"type": "null"}, {"type": "string", "maxLength": 3}, {"type": "number"}]}`))
		})
	}

	t.Run("a changed arm pairs with what's left, and duplicates pair once each", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []summary{{"/", "maxLength", Widens, "relaxes the limit from 1 to 2"}},
			changesOf(t, `{"anyOf": [{"maxLength": 1}, {"type": "null"}, {"type": "null"}]}`,
				`{"anyOf": [{"type": "null"}, {"type": "null"}, {"maxLength": 2}]}`))
	})

	t.Run("an arm added among reordered arms", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []summary{{"/", "anyOf", Widens, "adds a subschema"}},
			changesOf(t, `{"anyOf": [{"type": "string"}, {"type": "null"}]}`,
				`{"anyOf": [{"type": "number"}, {"type": "null"}, {"type": "string"}]}`))
	})
}

func TestUseTestRequiresOlderSchemaToRejectValue(t *testing.T) {
	t.Parallel()

	t.Run("an anyOf arm added", func(t *testing.T) {
		t.Parallel()
		older := `{"properties": {"a": {"anyOf": [{"type": "string"}]}}}`
		newer := `{"properties": {"a": {"anyOf": [{"type": "string"}, {"type": "number"}]}}}`
		changes := Compare(parse(t, older), parse(t, newer))
		require.Len(t, changes, 1)
		assert.False(t, changes[0].UsedBy(parse(t, `{"a": "b"}`), validates(t, older)))
		assert.True(t, changes[0].UsedBy(parse(t, `{"a": 1}`), validates(t, older)))
	})

	t.Run("a change inside an anyOf arm", func(t *testing.T) {
		t.Parallel()
		// 7 is outside the first arm's limit, but the second arm accepts it, so it shows nothing
		older := `{"anyOf": [{"maximum": 5}, {"minimum": 6, "maximum": 8}]}`
		newer := `{"anyOf": [{"maximum": 7}, {"minimum": 6, "maximum": 8}]}`
		changes := Compare(parse(t, older), parse(t, newer))
		require.Len(t, changes, 1)
		assert.False(t, changes[0].UsedBy(parse(t, `7`), validates(t, older)))
	})

	t.Run("a change deeper inside a combinator tests at the combinator", func(t *testing.T) {
		t.Parallel()
		older := `{"not": {"properties": {"a": {"maxLength": 3}}}}`
		newer := `{"not": {"properties": {"a": {"maxLength": 2}}}}`
		changes := Compare(parse(t, older), parse(t, newer))
		require.Equal(t, []summary{{"/a", "maxLength", Widens, "tightens the limit from 3 to 2"}}, summaries(changes))
		assert.True(t, changes[0].UsedBy(parse(t, `{"a": "abc"}`), validates(t, older)))
		assert.False(t, changes[0].UsedBy(parse(t, `{"a": "abcd"}`), validates(t, older)))
	})

	t.Run("a whole subschema gone, and nothing there before", func(t *testing.T) {
		t.Parallel()
		older := `{"properties": {"a": {"maximum": 1}}}`
		changes := Compare(parse(t, older), parse(t, `{}`))
		require.Len(t, changes, 1)
		assert.True(t, changes[0].UsedBy(parse(t, `{"a": 2}`), validates(t, older)))
		assert.False(t, changes[0].UsedBy(parse(t, `{"a": 1}`), validates(t, older)))

		older = `{}`
		changes = Compare(parse(t, older), parse(t, `{"properties": {"a": {"maximum": 1}}}`))
		require.Len(t, changes, 1)
		assert.False(t, changes[0].UsedBy(parse(t, `{"a": 2}`), validates(t, older)), "no subschema rejects nothing")
	})

	t.Run("a property whose name needs escaping in the fragment", func(t *testing.T) {
		t.Parallel()
		older := `{"properties": {"a/b %": {"anyOf": [{"type": "string"}]}}}`
		newer := `{"properties": {"a/b %": {"anyOf": [{"type": "string"}, {"type": "null"}]}}}`
		changes := Compare(parse(t, older), parse(t, newer))
		require.Len(t, changes, 1)
		assert.True(t, changes[0].UsedBy(parse(t, `{"a/b %": null}`), validates(t, older)))
	})

	t.Run("a $ref's target decides what's under it", func(t *testing.T) {
		t.Parallel()
		older := `{"$defs": {"x": {"properties": {"n": {"anyOf": [{"type": "string"}]}}}}, "$ref": "#/$defs/x"}`
		newer := `{"$defs": {"x": {"properties": {"n": {"anyOf": [{"type": "string"}, {"type": "null"}]}}}}, ` +
			`"$ref": "#/$defs/x"}`
		changes := Compare(parse(t, older), parse(t, newer))
		require.Len(t, changes, 1)
		assert.True(t, changes[0].UsedBy(parse(t, `{"n": null}`), validates(t, older)))
		assert.False(t, changes[0].UsedBy(parse(t, `{"n": "s"}`), validates(t, older)))
	})

	t.Run("a validator which can't tell shows nothing", func(t *testing.T) {
		t.Parallel()
		changes := Compare(parse(t, `{"anyOf": [{"type": "string"}]}`), parse(t, `{"anyOf": [{}, {"type": "string"}]}`))
		require.Len(t, changes, 1)
		cannotTell := func(string, any) (bool, bool) { return false, false }
		assert.False(t, changes[0].UsedBy(parse(t, `1`), cannotTell))
		assert.False(t, changes[0].NarrowedBy(parse(t, `1`), cannotTell, cannotTell))
	})
}

func TestNarrowedByNeedsOlderToAcceptAndNewerToReject(t *testing.T) {
	t.Parallel()

	older := `{"properties": {"status": {"enum": ["placed", "paid"]}}}`
	newer := `{"properties": {"status": {"enum": ["placed"]}}}`
	changes := Compare(parse(t, older), parse(t, newer))
	require.Len(t, changes, 1)
	o, n := validates(t, older), validates(t, newer)
	assert.True(t, changes[0].NarrowedBy(parse(t, `{"status": "paid"}`), o, n))
	assert.False(t, changes[0].NarrowedBy(parse(t, `{"status": "placed"}`), o, n), "the newer version accepts it")
	assert.False(t, changes[0].NarrowedBy(parse(t, `{"status": "lost"}`), o, n), "the older version rejects it too")

	t.Run("where a subschema appears or goes", func(t *testing.T) {
		t.Parallel()
		appears := Compare(parse(t, `{}`), parse(t, `{"properties": {"a": {"maximum": 1}}}`))
		require.Len(t, appears, 1)
		assert.True(t, appears[0].NarrowedBy(parse(t, `{"a": 2}`), validates(t, `{}`),
			validates(t, `{"properties": {"a": {"maximum": 1}}}`)), "no older subschema accepts everything")

		goes := Compare(parse(t, `{"properties": {"a": {"maximum": 1}}}`), parse(t, `{}`))
		require.Len(t, goes, 1)
		assert.False(t, goes[0].NarrowedBy(parse(t, `{"a": 0}`), validates(t, `{"properties": {"a": {"maximum": 1}}}`),
			validates(t, `{}`)), "no newer subschema rejects nothing")
	})
}

func TestDiffSeesPatternPropertiesChange(t *testing.T) {
	t.Parallel()

	t.Run("a pattern's subschema changes", func(t *testing.T) {
		t.Parallel()
		older := `{"patternProperties": {"^x-": {"maxLength": 1}}}`
		changes := Compare(parse(t, older), parse(t, `{"patternProperties": {"^x-": {"maxLength": 2}}}`))
		require.Equal(t, []summary{{"/^x-", "maxLength", Widens, "relaxes the limit from 1 to 2"}}, summaries(changes))
		assert.True(t, changes[0].UsedBy(parse(t, `{"x-a": "ab"}`), validates(t, older)))
		assert.False(t, changes[0].UsedBy(parse(t, `{"y": "ab"}`), validates(t, older)), "the name doesn't match")
	})

	t.Run("a pattern added to a closed object", func(t *testing.T) {
		t.Parallel()
		older := `{"additionalProperties": false}`
		changes := Compare(parse(t, older),
			parse(t, `{"patternProperties": {"^x-": {}}, "additionalProperties": false}`))
		require.Equal(
			t,
			[]summary{
				{"/^x-", "", Widens, "accepts values where the earlier version accepted none"},
			},
			summaries(changes),
		)
		assert.True(t, changes[0].UsedBy(parse(t, `{"x-a": 1}`), validates(t, older)))
	})

	t.Run("a pattern removed from an open object", func(t *testing.T) {
		t.Parallel()
		older := `{"patternProperties": {"^x-": {"type": "string"}}}`
		changes := Compare(parse(t, older), parse(t, `{}`))
		require.Equal(t, []summary{{"/^x-", "", Widens, "removes every constraint"}}, summaries(changes))
		assert.True(t, changes[0].UsedBy(parse(t, `{"x-a": 1}`), validates(t, older)))
	})

	t.Run("a pattern with a character the fragment must escape", func(t *testing.T) {
		t.Parallel()
		older := `{"patternProperties": {"^a%b/c$": {"anyOf": [{"type": "string"}]}}}`
		changes := Compare(parse(t, older),
			parse(t, `{"patternProperties": {"^a%b/c$": {"anyOf": [{"type": "string"}, {"type": "null"}]}}}`))
		require.Len(t, changes, 1)
		assert.True(t, changes[0].UsedBy(parse(t, `{"a%b/c": null}`), validates(t, older)))
	})
}

func TestDiffSeesRefSiblingChange(t *testing.T) {
	t.Parallel()

	const defs = `"$defs": {"code": {"type": "string"}}`

	t.Run("2020-12", func(t *testing.T) {
		t.Parallel()
		older := `{"$schema": "https://json-schema.org/draft/2020-12/schema", ` + defs +
			`, "properties": {"c": {"$ref": "#/$defs/code", "maxLength": 3}}}`
		newer := `{"$schema": "https://json-schema.org/draft/2020-12/schema", ` + defs +
			`, "properties": {"c": {"$ref": "#/$defs/code", "maxLength": 5}}}`
		changes := Compare(parse(t, older), parse(t, newer))
		require.Equal(t, []summary{{"/c", "maxLength", Widens, "relaxes the limit from 3 to 5"}}, summaries(changes))
		assert.True(t, changes[0].UsedBy(parse(t, `{"c": "abcd"}`), validates(t, older)))
	})

	t.Run("2019-09", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []summary{{"/c", "maxLength", Narrows, "adds a limit of 5"}},
			changesOf(t, `{"$schema": "https://json-schema.org/draft/2019-09/schema", `+defs+
				`, "properties": {"c": {"$ref": "#/$defs/code"}}}`,
				`{"$schema": "https://json-schema.org/draft/2019-09/schema", `+defs+
					`, "properties": {"c": {"$ref": "#/$defs/code", "maxLength": 5}}}`))
	})

	t.Run("draft-07 ignores them", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, changesOf(t, `{"$schema": "http://json-schema.org/draft-07/schema#", `+defs+
			`, "properties": {"c": {"$ref": "#/$defs/code", "maxLength": 3}}}`,
			`{"$schema": "http://json-schema.org/draft-07/schema#", `+defs+
				`, "properties": {"c": {"$ref": "#/$defs/code", "maxLength": 5}}}`))
	})

	t.Run("a sibling beside the root's own recursive $ref", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []summary{{"/c", "maxItems", Widens, "relaxes the limit from 1 to 2"}},
			changesOf(t, `{"properties": {"c": {"$ref": "#", "maxItems": 1}}}`,
				`{"properties": {"c": {"$ref": "#", "maxItems": 2}}}`))
	})

	t.Run("annotations beside a $ref are no constraint", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, changesOf(t, `{`+defs+`, "properties": {"c": {"$ref": "#/$defs/code", "title": "a"}}}`,
			`{`+defs+`, "properties": {"c": {"type": "string"}}}`))
	})

	t.Run("moving between a $ref with a sibling and no $ref can't be judged", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []summary{{"/c", "$ref", Alters, "changes between a $ref with keywords beside it and no $ref"}},
			changesOf(t, `{`+defs+`, "properties": {"c": {"$ref": "#/$defs/code", "maxLength": 3}}}`,
				`{"properties": {"c": {"type": "string", "maxLength": 3}}}`))
	})
}

func TestDiffSeesFormatChangeUnderAssertFormat(t *testing.T) {
	t.Parallel()

	const none, email, date = `{}`, `{"format": "email"}`, `{"format": "date"}`

	assert.Equal(t, []summary{{"/", "format", Narrows, "adds format"}}, changesOf(t, none, email, AssertFormat(true)))
	assert.Equal(t, []summary{{"/", "format", Widens, "removes format"}}, changesOf(t, email, none, AssertFormat(true)))
	assert.Equal(t, []summary{{"/", "format", Alters, "changes format"}}, changesOf(t, email, date, AssertFormat(true)))

	assert.Empty(t, changesOf(t, email, date), "an annotation, unless asserted")
	assert.Empty(t, changesOf(t, email, date, AssertFormat(false)))

	t.Run("a draft before 2019-09 always asserts it", func(t *testing.T) {
		t.Parallel()
		const draft07 = `"$schema": "http://json-schema.org/draft-07/schema#"`
		assert.Equal(t, []summary{{"/", "format", Alters, "changes format"}},
			changesOf(t, `{`+draft07+`, "format": "email"}`, `{`+draft07+`, "format": "date"}`))
	})
}

func TestDiffSeesDraftChange(t *testing.T) {
	t.Parallel()

	const draft07, draft2020 = "http://json-schema.org/draft-07/schema", "https://json-schema.org/draft/2020-12/schema"

	assert.Equal(t,
		[]summary{{"/", "$schema", Alters, `changes the draft from "` + draft07 + `" to "` + draft2020 + `"`}},
		changesOf(t, `{"$schema": "`+draft07+`"}`, `{"$schema": "`+draft2020+`"}`))
	assert.Empty(
		t,
		changesOf(t, `{"$schema": "`+draft07+`#"}`, `{"$schema": "`+draft07+`"}`),
		"a trailing # is no change",
	)
}

func TestPolarityWithin(t *testing.T) {
	t.Parallel()

	assert.Equal(t, keeps, keeps.within(keeps))
	assert.Equal(t, keeps, flips.within(flips))
	assert.Equal(t, flips, flips.within(keeps))
	assert.Equal(t, flips, keeps.within(flips))
	assert.Equal(t, unknowns, unknowns.within(flips))
	assert.Equal(t, unknowns, keeps.within(unknowns))
	assert.Equal(t, Alters, flips.apply(Alters))
}
