package jsonptr

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

func TestFormat(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/", Format(nil))
	assert.Equal(t, "/a~1b~0c/*/0", Format([]Segment{Property("a/b~c"), AnySegment, Item(0)}))
}

func TestValuesAt(t *testing.T) {
	t.Parallel()

	doc := parse(t, `{"a": [{"b": 1}, {"b": 2}, {"c": 3}], "0": "named zero", "z": {"b": 4}}`)
	tests := []struct {
		name string
		path []Segment
		want []any
	}{
		{"root", nil, []any{doc}},
		{"property", []Segment{Property("0")}, []any{"named zero"}},
		{"missing property", []Segment{Property("x")}, nil},
		{"any item", []Segment{Property("a"), AnySegment, Property("b")}, []any{1.0, 2.0}},
		{"any property, in name order", []Segment{AnySegment, Property("b")}, []any{4.0}},
		{"index", []Segment{Property("a"), Item(1), Property("b")}, []any{2.0}},
		{"index past the end", []Segment{Property("a"), Item(3)}, nil},
		{"index into an object", []Segment{Item(0)}, nil},
		{"name into an array", []Segment{Property("a"), Property("0")}, nil},
		{"into a scalar", []Segment{Property("0"), Property("x")}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ValuesAt(doc, tt.path))
		})
	}
}

func TestLookup(t *testing.T) {
	t.Parallel()

	doc := parse(t, `{"a/b": {"c~d": [10, 20]}}`)
	tests := []struct {
		pointer string
		want    any
		ok      bool
	}{
		{"", doc, true},
		{"/a~1b/c~0d/1", 20.0, true},
		{"/missing", nil, false},
		{"/a~1b/c~0d/2", nil, false},
		{"/a~1b/c~0d/x", nil, false},
		{"/a~1b/c~0d/-1", nil, false},
		{"/a~1b/c~0d/0/x", nil, false},
		{"anchor", nil, false},
	}
	for _, tt := range tests {
		got, ok := Lookup(doc, tt.pointer)
		assert.Equal(t, tt.ok, ok, tt.pointer)
		assert.Equal(t, tt.want, got, tt.pointer)
	}
}

func TestFollowRefs(t *testing.T) {
	t.Parallel()

	root := parse(
		t,
		`{"$defs": {"a": {"$ref": "#/$defs/b"}, "b": {"type": "string"}, "loop": {"$ref": "#/$defs/loop"}}}`,
	)
	defs, ok := root.(map[string]any)["$defs"].(map[string]any)
	require.True(t, ok)

	target, ref := FollowRefs(root, map[string]any{"$ref": "#/$defs/a"})
	assert.Equal(t, defs["b"], target)
	assert.Equal(t, "#/$defs/b", ref)

	target, ref = FollowRefs(root, defs["loop"])
	assert.Equal(t, defs["loop"], target, "a $ref to itself stops after a limit")
	assert.Equal(t, "#/$defs/loop", ref)

	for _, node := range []any{
		true,
		map[string]any{"type": "string"},
		map[string]any{"$ref": "https://example.com/x.json"},
		map[string]any{"$ref": "#/missing"},
	} {
		target, ref = FollowRefs(root, node)
		assert.Equal(t, node, target)
		assert.Empty(t, ref)
	}
}

func TestMatchesType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value any
		types []string
	}{
		{nil, []string{"null"}},
		{true, []string{"boolean"}},
		{"s", []string{"string"}},
		{map[string]any{}, []string{"object"}},
		{[]any{}, []string{"array"}},
		{1.5, []string{"number"}},
		{2.0, []string{"integer"}},
		{json.Number("2"), []string{"integer"}},
	}
	for _, tt := range tests {
		assert.True(t, MatchesType(tt.value, tt.types), "%v is %v", tt.value, tt.types)
	}
	assert.False(t, MatchesType(1.5, []string{"integer", "string"}))
	assert.False(t, MatchesType(1, []string{"unknown"}))
}

func TestNumber(t *testing.T) {
	t.Parallel()

	n, ok := Number(json.Number("1.5"))
	assert.True(t, ok)
	assert.InDelta(t, 1.5, n, 0)
	_, ok = Number(json.Number("x"))
	assert.False(t, ok)
	_, ok = Number("1")
	assert.False(t, ok)
}
