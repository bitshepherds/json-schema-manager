package structdiff

import (
	"regexp"
	"unicode/utf8"

	"github.com/bitshepherds/json-schema-manager/internal/jsonptr"
)

// subschemas compares the subschemas of om and nm, which apply at path.
func (c *comparer) subschemas(path []jsonptr.Segment, om, nm map[string]any) {
	c.properties(path, om, nm)
	c.additionalProperties(path, om, nm)
	c.items(path, "items", om["items"], nm["items"])
	c.items(path, "prefixItems", om["prefixItems"], nm["prefixItems"])
	c.combinator(path, "allOf", om, nm, Narrows)
	c.combinator(path, "anyOf", om, nm, Widens)
	c.combinator(path, "oneOf", om, nm, Widens)
}

// properties compares each property's subschema. A property which only one version declares is
// compared with the subschema the other version applies to it, such as its additionalProperties.
func (c *comparer) properties(path []jsonptr.Segment, om, nm map[string]any) {
	op, _ := om["properties"].(map[string]any)
	np, _ := nm["properties"].(map[string]any)
	for _, name := range unionKeys(op, np) {
		ov, oHas := op[name]
		if !oHas {
			ov = undeclared(om, name)
		}
		nv, nHas := np[name]
		if !nHas {
			nv = undeclared(nm, name)
		}
		c.node(append(path, jsonptr.Property(name)), ov, nv)
	}
}

// undeclared returns the subschema which m applies to a property it does not declare in properties.
func undeclared(m map[string]any, name string) any {
	pp, _ := m["patternProperties"].(map[string]any)
	for _, p := range unionKeys(pp, nil) {
		if re, err := regexp.Compile(p); err == nil && re.MatchString(name) {
			return pp[p]
		}
	}
	if ap, ok := m["additionalProperties"]; ok {
		return ap
	}
	return true
}

func (c *comparer) additionalProperties(path []jsonptr.Segment, om, nm map[string]any) {
	oa, oHas := om["additionalProperties"]
	na, nHas := nm["additionalProperties"]
	if !oHas {
		oa = true
	}
	if !nHas {
		na = true
	}
	_, oIsMap := oa.(map[string]any)
	_, nIsMap := na.(map[string]any)
	if oIsMap && nIsMap {
		c.node(append(path, jsonptr.AnySegment), oa, na)
		return
	}
	c.wholeSchema(path, "additionalProperties", oa, na, func(v any) bool { return hasUndeclared(v, om) })
}

// hasUndeclared reports whether v is an object with a property which m does not declare.
func hasUndeclared(v any, m map[string]any) bool {
	obj, ok := v.(map[string]any)
	if !ok {
		return false
	}
	props, _ := m["properties"].(map[string]any)
	pp, _ := m["patternProperties"].(map[string]any)
	for name := range obj {
		if _, declared := props[name]; declared {
			continue
		}
		if !matchesAnyPattern(pp, name) {
			return true
		}
	}
	return false
}

func matchesAnyPattern(patterns map[string]any, name string) bool {
	for p := range patterns {
		if re, err := regexp.Compile(p); err == nil && re.MatchString(name) {
			return true
		}
	}
	return false
}

// items compares array item subschemas: a single subschema for every item, or one per position.
func (c *comparer) items(path []jsonptr.Segment, keyword string, o, n any) {
	oa, oIsArr := o.([]any)
	na, nIsArr := n.([]any)
	switch {
	case !oIsArr && !nIsArr:
		if o == nil && n == nil {
			return
		}
		if o == nil {
			o = true
		}
		if n == nil {
			n = true
		}
		c.node(append(path, jsonptr.AnySegment), o, n)
	case oIsArr && nIsArr:
		for i := range max(len(oa), len(na)) {
			c.node(append(path, jsonptr.Item(i)), at(oa, i), at(na, i))
		}
	default:
		c.add(path, keyword, Alters, "changes between one subschema for every item and one per position", nil)
	}
}

// at returns the subschema at position i, or true if there is none.
func at(schemas []any, i int) any {
	if i < len(schemas) {
		return schemas[i]
	}
	return true
}

// combinator compares the subschemas of allOf, anyOf or oneOf position by position. The effect of
// adding a subschema is added: it Narrows an allOf, and Widens an anyOf or oneOf.
func (c *comparer) combinator(path []jsonptr.Segment, keyword string, om, nm map[string]any, added Effect) {
	oa, _ := om[keyword].([]any)
	na, _ := nm[keyword].([]any)
	for i := range min(len(oa), len(na)) {
		c.node(path, oa[i], na[i])
	}
	removed := Widens
	if added == Widens {
		removed = Narrows
	}
	switch {
	case len(na) > len(oa):
		c.add(path, keyword, added, "adds a subschema", nil)
	case len(na) < len(oa):
		c.add(path, keyword, removed, "removes a subschema", nil)
	}
}

func stringLength(v any) (float64, bool) {
	s, ok := v.(string)
	return float64(utf8.RuneCountInString(s)), ok
}

func arrayLength(v any) (float64, bool) {
	a, ok := v.([]any)
	return float64(len(a)), ok
}

func objectSize(v any) (float64, bool) {
	m, ok := v.(map[string]any)
	return float64(len(m)), ok
}
