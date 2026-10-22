// Package coverage measures how much of a JSON Schema its pass documents reach: which of its
// properties they have, which enum values and types they use, which anyOf and oneOf subschemas
// they match, and which way they go at each if.
package coverage

import (
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/bitshepherds/json-schema-manager/internal/jsonptr"
)

// Kind is a kind of point in a schema which a document can reach.
type Kind string

const (
	// Property is a property the schema declares, which a document reaches by having it.
	Property Kind = "property"
	// EnumValue is a value an enum allows, which a document reaches by using it.
	EnumValue Kind = "enum"
	// Type is one of several types the schema allows, which a document reaches with a value of it.
	Type Kind = "type"
	// Branch is a subschema of anyOf or oneOf, which a document reaches with a value it accepts.
	Branch Kind = "branch"
	// Then is the path through an if, which a document reaches with a value the if accepts.
	Then Kind = "then"
	// Else is the path through an if, which a document reaches with a value the if rejects.
	Else Kind = "else"
)

// Point is something in a schema which a document can reach.
type Point struct {
	Location string `json:"location"` // where in a document the point is, as a JSON Pointer; * is any item or property
	Kind     Kind   `json:"kind"`
	Detail   string `json:"detail"`  // e.g. the property name, the enum value as JSON, or anyOf/1
	Covered  bool   `json:"covered"` // whether a document reaches it
}

// Validates reports whether v is accepted by the subschema at the JSON Pointer pointer in the
// schema. Its ok result is false if it cannot tell.
type Validates func(pointer string, v any) (valid, ok bool)

// Measure lists the points in schema, an unmarshalled JSON Schema, and whether any of docs reach
// each. It follows local $refs, and calls validates for anyOf, oneOf and if subschemas.
func Measure(schema any, docs []any, validates Validates) []Point {
	m := &measurer{root: schema, docs: docs, validates: validates, seen: make(map[string]bool)}
	m.node(nil, "", schema)
	return m.points
}

type measurer struct {
	root      any
	docs      []any
	validates Validates
	seen      map[string]bool // the subschemas being measured, by pointer, to stop at recursive schemas
	points    []Point
}

func (m *measurer) add(path []jsonptr.Segment, kind Kind, detail string, covered bool) {
	m.points = append(m.points, Point{Location: jsonptr.Format(path), Kind: kind, Detail: detail, Covered: covered})
}

// values returns the documents' values at path.
func (m *measurer) values(path []jsonptr.Segment) []any {
	out := make([]any, 0, len(m.docs))
	for _, doc := range m.docs {
		out = append(out, jsonptr.ValuesAt(doc, path)...)
	}
	return out
}

// node measures the subschema at the JSON Pointer ptr, which applies at path.
func (m *measurer) node(path []jsonptr.Segment, ptr string, node any) {
	if target, ref := jsonptr.FollowRefs(m.root, node); ref != "" {
		node, ptr = target, ref[1:]
	}
	if m.seen[ptr] {
		return
	}
	m.seen[ptr] = true
	defer delete(m.seen, ptr)

	s, ok := node.(map[string]any)
	if !ok {
		return
	}
	values := m.values(path)

	m.types(path, s, values)
	m.enum(path, s, values)
	m.properties(path, ptr, s, values)
	m.branches(path, ptr, s, values)
	m.conditional(path, ptr, s, values)
	m.subschemas(path, ptr, s)
}

func (m *measurer) types(path []jsonptr.Segment, s map[string]any, values []any) {
	types, ok := s["type"].([]any)
	if !ok || len(types) < 2 {
		return
	}
	for _, t := range types {
		name, _ := t.(string)
		covered := slices.ContainsFunc(values, func(v any) bool { return jsonptr.MatchesType(v, []string{name}) })
		m.add(path, Type, name, covered)
	}
}

func (m *measurer) enum(path []jsonptr.Segment, s map[string]any, values []any) {
	enum, _ := s["enum"].([]any)
	for _, e := range enum {
		covered := slices.ContainsFunc(values, func(v any) bool { return reflect.DeepEqual(v, e) })
		m.add(path, EnumValue, quote(e), covered)
	}
}

func (m *measurer) properties(path []jsonptr.Segment, ptr string, s map[string]any, values []any) {
	props, _ := s["properties"].(map[string]any)
	for _, name := range sortedKeys(props) {
		covered := slices.ContainsFunc(values, func(v any) bool {
			obj, ok := v.(map[string]any)
			_, has := obj[name]
			return ok && has
		})
		m.add(path, Property, name, covered)
		m.node(append(path, jsonptr.Property(name)), ptr+"/properties/"+jsonptr.Escape(name), props[name])
	}
}

// branches measures the subschemas of anyOf and oneOf, which a value reaches by matching one.
func (m *measurer) branches(path []jsonptr.Segment, ptr string, s map[string]any, values []any) {
	for _, keyword := range []string{"anyOf", "oneOf"} {
		subs, _ := s[keyword].([]any)
		for i := range subs {
			subPtr := ptr + "/" + keyword + "/" + strconv.Itoa(i)
			valid, ok := m.anyValid(subPtr, values)
			if ok {
				m.add(path, Branch, keyword+"/"+strconv.Itoa(i), valid)
			}
		}
	}
}

// conditional measures the paths through an if: then for values it accepts, else for the rest.
func (m *measurer) conditional(path []jsonptr.Segment, ptr string, s map[string]any, values []any) {
	if _, ok := s["if"]; !ok {
		return
	}
	then, els := false, false
	for _, v := range values {
		valid, ok := m.validates(ptr+"/if", v)
		if !ok {
			return
		}
		then = then || valid
		els = els || !valid
	}
	m.add(path, Then, "if", then)
	m.add(path, Else, "if", els)
}

// anyValid reports whether the subschema at ptr accepts any of values. Its ok result is false if
// it cannot tell.
func (m *measurer) anyValid(ptr string, values []any) (valid, ok bool) {
	for _, v := range values {
		if valid, ok = m.validates(ptr, v); !ok || valid {
			return valid, ok
		}
	}
	return false, true
}

// subschemas measures the subschemas which apply to the same value, or to its items and properties.
func (m *measurer) subschemas(path []jsonptr.Segment, ptr string, s map[string]any) {
	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		subs, _ := s[keyword].([]any)
		for i, sub := range subs {
			m.node(path, ptr+"/"+keyword+"/"+strconv.Itoa(i), sub)
		}
	}
	for _, keyword := range []string{"then", "else"} {
		if sub, ok := s[keyword]; ok {
			m.node(path, ptr+"/"+keyword, sub)
		}
	}
	if sub, ok := s["additionalProperties"]; ok {
		m.node(append(path, jsonptr.AnySegment), ptr+"/additionalProperties", sub)
	}
	pp, _ := s["patternProperties"].(map[string]any)
	for _, p := range sortedKeys(pp) {
		m.node(append(path, jsonptr.AnySegment), ptr+"/patternProperties/"+jsonptr.Escape(p), pp[p])
	}
	m.items(path, ptr, s)
}

func (m *measurer) items(path []jsonptr.Segment, ptr string, s map[string]any) {
	if tuple, ok := s["items"].([]any); ok {
		for i, sub := range tuple {
			m.node(append(path, jsonptr.Item(i)), ptr+"/items/"+strconv.Itoa(i), sub)
		}
	} else if sub, ok := s["items"]; ok {
		m.node(append(path, jsonptr.AnySegment), ptr+"/items", sub)
	}
	prefix, _ := s["prefixItems"].([]any)
	for i, sub := range prefix {
		m.node(append(path, jsonptr.Item(i)), ptr+"/prefixItems/"+strconv.Itoa(i), sub)
	}
}

// quote renders v, an unmarshalled JSON value, as JSON.
func quote(v any) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSuffix(b.String(), "\n")
}

func sortedKeys(m map[string]any) []string {
	return slices.Sorted(maps.Keys(m))
}
