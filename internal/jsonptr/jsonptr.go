// Package jsonptr finds values in unmarshalled JSON documents and schemas: by JSON Pointer, by
// a location which may match many values, and by following a schema's local $refs.
package jsonptr

import (
	"encoding/json"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
)

// Segment is one step of a location in a document: a property name, an array index, or any
// property or item.
type Segment struct {
	Name  string
	Index bool // Name is an array index
	Any   bool
}

// AnySegment matches any property of an object, or any item of an array.
var AnySegment = Segment{Any: true}

// Property returns the segment for the property name.
func Property(name string) Segment {
	return Segment{Name: name}
}

// Item returns the segment for the array index i.
func Item(i int) Segment {
	return Segment{Name: strconv.Itoa(i), Index: true}
}

// Format renders path as a JSON Pointer, with * for any property or item, and / for the root.
func Format(path []Segment) string {
	if len(path) == 0 {
		return "/"
	}
	var b strings.Builder
	for _, s := range path {
		b.WriteString("/")
		if s.Any {
			b.WriteString("*")
			continue
		}
		b.WriteString(Escape(s.Name))
	}
	return b.String()
}

// Escape escapes a property name for use in a JSON Pointer.
func Escape(name string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(name)
}

// ValuesAt returns the values in doc at path.
func ValuesAt(doc any, path []Segment) []any {
	if len(path) == 0 {
		return []any{doc}
	}
	var children []any
	switch v := doc.(type) {
	case map[string]any:
		if path[0].Any {
			for _, name := range slices.Sorted(maps.Keys(v)) {
				children = append(children, v[name])
			}
		} else if child, ok := v[path[0].Name]; ok && !path[0].Index {
			children = append(children, child)
		}
	case []any:
		if path[0].Any {
			children = v
		} else if i, err := strconv.Atoi(path[0].Name); err == nil && path[0].Index && i < len(v) {
			children = append(children, v[i])
		}
	}
	var out []any
	for _, child := range children {
		out = append(out, ValuesAt(child, path[1:])...)
	}
	return out
}

// Lookup finds the value at the JSON Pointer p in doc.
func Lookup(doc any, p string) (any, bool) {
	if p == "" {
		return doc, true
	}
	if !strings.HasPrefix(p, "/") {
		return nil, false
	}
	cur := doc
	for _, tok := range strings.Split(p[1:], "/") {
		tok = strings.NewReplacer("~1", "/", "~0", "~").Replace(tok)
		switch v := cur.(type) {
		case map[string]any:
			next, ok := v[tok]
			if !ok {
				return nil, false
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(tok)
			if err != nil || i < 0 || i >= len(v) {
				return nil, false
			}
			cur = v[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

// maxRefHops limits how many local $refs FollowRefs follows from one subschema to the next.
const maxRefHops = 32

// FollowRefs follows local $refs, such as "#/$defs/money", from the subschema node to the subschema
// in root they lead to, and returns it with the last $ref followed. It returns node unchanged, and
// an empty $ref, when node has no local $ref which resolves. Named anchors are not followed.
func FollowRefs(root, node any) (target any, ref string) {
	target = node
	for range maxRefHops {
		m, ok := target.(map[string]any)
		if !ok {
			break
		}
		r, ok := m["$ref"].(string)
		if !ok || !IsLocalRef(r) {
			break
		}
		next, ok := Lookup(root, strings.TrimPrefix(r, "#"))
		if !ok {
			break
		}
		target, ref = next, r
	}
	return target, ref
}

// IsLocalRef reports whether v is a $ref within the same schema.
func IsLocalRef(v any) bool {
	s, ok := v.(string)
	return ok && strings.HasPrefix(s, "#")
}

// MatchesType reports whether v is of any of the JSON Schema types.
func MatchesType(v any, types []string) bool {
	return slices.ContainsFunc(types, func(t string) bool {
		is, ok := isType[t]
		return ok && is(v)
	})
}

// isType reports whether a value is of each JSON Schema type.
var isType = map[string]func(v any) bool{
	"null":    func(v any) bool { return v == nil },
	"boolean": func(v any) bool { _, ok := v.(bool); return ok },
	"string":  func(v any) bool { _, ok := v.(string); return ok },
	"object":  func(v any) bool { _, ok := v.(map[string]any); return ok },
	"array":   func(v any) bool { _, ok := v.([]any); return ok },
	"number":  func(v any) bool { _, ok := Number(v); return ok },
	"integer": func(v any) bool { n, ok := Number(v); return ok && n == math.Trunc(n) },
}

// Number returns v as a float64, if it is a JSON number.
func Number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
