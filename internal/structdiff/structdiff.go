// Package structdiff compares two versions of a JSON Schema, and lists the structural changes
// between them, such as enum values added, constraints raised or lowered, and types allowed.
//
// The comparison is structural: it walks both schemas side by side, following local $refs, and
// compares the keywords at each location. It does not decide whether two schemas accept the same
// documents, so a change it reports as one that Alters a schema may in fact make no difference.
package structdiff

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// Effect says how a change affects the documents a schema accepts.
type Effect string

const (
	// Widens means the newer schema accepts documents which the older one rejects, so a consumer
	// on the older version may reject documents written to the newer one.
	Widens Effect = "widens"
	// Narrows means the newer schema rejects documents which the older one accepts. A consumer on
	// the older version still accepts every document written to the newer one.
	Narrows Effect = "narrows"
	// Alters means the change may widen what the schema accepts, and structdiff cannot tell.
	Alters Effect = "alters"
)

// Change is one structural change between two versions of a schema.
type Change struct {
	Location string // where in a document the change applies, as a JSON Pointer; * stands for any item or property
	Keyword  string // the keyword which changed, or empty when a whole subschema changed
	Effect   Effect
	Detail   string // what changed, e.g. `adds "refunded"`

	path []segment
	// uses reports whether a value at the location uses the change, so the older schema rejects it.
	// It is nil when structdiff cannot tell, and then any value at the location uses the change.
	uses func(v any) bool
}

// MayBreak reports whether the change may make the older schema reject documents which the newer
// one accepts.
func (c *Change) MayBreak() bool {
	return c.Effect != Narrows
}

// String renders the change as its location, keyword and detail.
func (c *Change) String() string {
	if c.Keyword == "" {
		return c.Location + ": " + c.Detail
	}
	return c.Location + ": " + c.Keyword + " " + c.Detail
}

// UsedBy reports whether doc has a value which uses the change. A document which uses a change
// that Widens a schema is rejected by the older version. When structdiff cannot tell which values
// use a change, such as an anyOf alternative added, any value at the change's location does: a
// compatibility check of the document then checks that value against the older version.
func (c *Change) UsedBy(doc any) bool {
	values := valuesAt(doc, c.path)
	if c.uses == nil {
		return len(values) > 0
	}
	return slices.ContainsFunc(values, c.uses)
}

// segment is one step of a location in a document: a property name, an array index, or any.
type segment struct {
	name  string
	index bool // name is an array index
	any   bool
}

var anySegment = segment{any: true}

// location renders path as a JSON Pointer, with * for any item or property.
func location(path []segment) string {
	if len(path) == 0 {
		return "/"
	}
	var b strings.Builder
	for _, s := range path {
		b.WriteString("/")
		if s.any {
			b.WriteString("*")
			continue
		}
		b.WriteString(strings.NewReplacer("~", "~0", "/", "~1").Replace(s.name))
	}
	return b.String()
}

// Compare lists the structural changes from older to newer, which are unmarshalled JSON Schemas.
func Compare(older, newer any) []Change {
	c := &comparer{
		oldRoot: older,
		newRoot: newer,
		// A $ref to the root of both schemas recurses
		seen: map[[2]string]bool{{"#", "#"}: true},
	}
	c.node(nil, older, newer)
	return c.changes
}

type comparer struct {
	oldRoot, newRoot any
	seen             map[[2]string]bool // the local $ref pairs being compared, to stop at recursive schemas
	changes          []Change
}

func (c *comparer) add(path []segment, keyword string, effect Effect, detail string, uses func(any) bool) {
	c.changes = append(c.changes, Change{
		Location: location(path),
		Keyword:  keyword,
		Effect:   effect,
		Detail:   detail,
		path:     slices.Clone(path),
		uses:     uses,
	})
}

// node compares the subschemas o and n, which apply at path.
func (c *comparer) node(path []segment, o, n any) {
	o, oRef := resolve(c.oldRoot, o)
	n, nRef := resolve(c.newRoot, n)
	if oRef != "" || nRef != "" {
		pair := [2]string{oRef, nRef}
		if c.seen[pair] {
			return
		}
		c.seen[pair] = true
		defer delete(c.seen, pair)
	}

	om, oIsMap := o.(map[string]any)
	nm, nIsMap := n.(map[string]any)
	if !oIsMap || !nIsMap {
		c.wholeSchema(path, "", o, n, nil)
		return
	}

	c.keywords(path, om, nm)
	c.subschemas(path, om, nm)
	c.otherKeywords(path, om, nm)
}

// wholeSchema compares subschemas o and n where either is a boolean schema. An absent subschema
// counts as true. When o is false, the function usesOldFalse reports whether a value uses the
// change; if it is nil, any value does.
func (c *comparer) wholeSchema(path []segment, keyword string, o, n any, usesOldFalse func(any) bool) {
	if reflect.DeepEqual(o, n) {
		return
	}
	switch {
	case o == false:
		if usesOldFalse == nil {
			usesOldFalse = func(any) bool { return true }
		}
		c.add(path, keyword, Widens, "accepts values where the earlier version accepted none", usesOldFalse)
	case n == false:
		c.add(path, keyword, Narrows, "accepts no values", nil)
	case o == true:
		c.add(path, keyword, Narrows, "adds constraints where the earlier version had none", nil)
	default:
		c.add(path, keyword, Widens, "removes every constraint", nil)
	}
}

// maxRefHops limits how many local $refs resolve follows from one subschema to the next.
const maxRefHops = 32

// resolve follows local $refs from node to the subschema in root they lead to, and returns it
// with the last $ref followed. It returns node unchanged, and an empty $ref, when node has no
// local $ref which resolves. Keywords beside a $ref are not compared.
func resolve(root, node any) (target any, ref string) {
	target = node
	for range maxRefHops {
		m, ok := target.(map[string]any)
		if !ok {
			break
		}
		r, ok := m["$ref"].(string)
		if !ok || !isLocalRef(r) {
			break
		}
		next, ok := pointer(root, strings.TrimPrefix(r, "#"))
		if !ok {
			break
		}
		target, ref = next, r
	}
	return target, ref
}

// pointer finds the value at the JSON Pointer p in doc.
func pointer(doc any, p string) (any, bool) {
	if p == "" {
		return doc, true
	}
	if !strings.HasPrefix(p, "/") {
		return nil, false // a named anchor, which structdiff does not follow
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

// handledKeywords are compared by keywords and subschemas.
var handledKeywords = []string{
	"type", "enum", "const", "pattern", "multipleOf", "required",
	"maxLength", "minLength", "maxItems", "minItems", "maxProperties", "minProperties",
	"maximum", "minimum", "exclusiveMaximum", "exclusiveMinimum",
	"properties", "patternProperties", "additionalProperties", "items", "prefixItems",
	"allOf", "anyOf", "oneOf",
}

// ignoredKeywords do not change which documents a schema accepts, or are reached through a $ref.
var ignoredKeywords = []string{
	"$schema", "$id", "$anchor", "$comment", "$defs", "definitions",
	"title", "description", "examples", "default", "deprecated", "readOnly", "writeOnly",
	"format", "contentEncoding", "contentMediaType", "contentSchema",
}

// otherKeywords reports a change to any keyword which structdiff does not otherwise compare.
// Adding one usually adds a constraint, and removing one removes it.
func (c *comparer) otherKeywords(path []segment, om, nm map[string]any) {
	for _, k := range unionKeys(om, nm) {
		if slices.Contains(handledKeywords, k) || slices.Contains(ignoredKeywords, k) {
			continue
		}
		ov, oHas := om[k]
		nv, nHas := nm[k]
		switch {
		case k == "$ref" && (isLocalRef(ov) || isLocalRef(nv)):
			continue // compared by following it
		case !oHas:
			c.add(path, k, Narrows, "adds "+k, nil)
		case !nHas:
			c.add(path, k, Widens, "removes "+k, nil)
		case !reflect.DeepEqual(ov, nv):
			c.add(path, k, Alters, "changes "+k, nil)
		}
	}
}

func isLocalRef(v any) bool {
	s, ok := v.(string)
	return ok && strings.HasPrefix(s, "#")
}

// unionKeys returns the keys of a and b, sorted.
func unionKeys(a, b map[string]any) []string {
	keys := make([]string, 0, len(a)+len(b))
	for k := range a {
		keys = append(keys, k)
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}
