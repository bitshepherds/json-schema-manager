package structdiff

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

// keywords compares the assertion keywords of the subschemas om and nm, which apply at path.
func (c *comparer) keywords(path []segment, om, nm map[string]any) {
	c.types(path, om, nm)
	c.enum(path, om, nm)
	c.constant(path, om, nm)
	for _, b := range bounds {
		c.bound(path, b, om, nm)
	}
	c.pattern(path, om, nm)
	c.multipleOf(path, om, nm)
	c.required(path, om, nm)
}

func (c *comparer) types(path []segment, om, nm map[string]any) {
	ot, oHas := typeSet(om)
	nt, nHas := typeSet(nm)
	switch {
	case !oHas && !nHas:
	case !nHas:
		c.add(path, "type", Widens, "no longer restricts the type", func(v any) bool { return !matchesType(v, ot) })
	case !oHas:
		c.add(path, "type", Narrows, "restricts the type to "+quoteAll(nt), nil)
	default:
		if added := uncovered(nt, ot); len(added) > 0 {
			c.add(path, "type", Widens, "allows "+quoteAll(added), func(v any) bool {
				return matchesType(v, added) && !matchesType(v, ot)
			})
		}
		if removed := uncovered(ot, nt); len(removed) > 0 {
			c.add(path, "type", Narrows, "no longer allows "+quoteAll(removed), nil)
		}
	}
}

// typeSet returns the types which the type keyword of m allows, and whether m has the keyword.
func typeSet(m map[string]any) ([]string, bool) {
	switch t := m["type"].(type) {
	case string:
		return []string{t}, true
	case []any:
		types := make([]string, 0, len(t))
		for _, v := range t {
			if s, ok := v.(string); ok {
				types = append(types, s)
			}
		}
		return types, true
	default:
		return nil, false
	}
}

// uncovered returns the types in a which b does not allow.
func uncovered(a, b []string) []string {
	var out []string
	for _, t := range a {
		if !slices.Contains(b, t) && (t != "integer" || !slices.Contains(b, "number")) {
			out = append(out, t)
		}
	}
	return out
}

func (c *comparer) enum(path []segment, om, nm map[string]any) {
	ov, oHas := om["enum"].([]any)
	nv, nHas := nm["enum"].([]any)
	switch {
	case !oHas && !nHas:
	case !nHas:
		c.add(path, "enum", Widens, "no longer restricts the values", func(v any) bool { return !containsValue(ov, v) })
	case !oHas:
		c.add(path, "enum", Narrows, "restricts the values to "+quoteAll(nv), nil)
	default:
		if added := missingValues(nv, ov); len(added) > 0 {
			c.add(path, "enum", Widens, "adds "+quoteAll(added), func(v any) bool { return containsValue(added, v) })
		}
		if removed := missingValues(ov, nv); len(removed) > 0 {
			c.add(path, "enum", Narrows, "removes "+quoteAll(removed), nil)
		}
	}
}

// missingValues returns the values in a which are not in b.
func missingValues(a, b []any) []any {
	var out []any
	for _, v := range a {
		if !containsValue(b, v) {
			out = append(out, v)
		}
	}
	return out
}

func containsValue(values []any, v any) bool {
	return slices.ContainsFunc(values, func(e any) bool { return reflect.DeepEqual(e, v) })
}

func (c *comparer) constant(path []segment, om, nm map[string]any) {
	ov, oHas := om["const"]
	nv, nHas := nm["const"]
	switch {
	case !oHas && !nHas, reflect.DeepEqual(ov, nv):
	case !nHas:
		c.add(
			path,
			"const",
			Widens,
			"no longer requires "+quote(ov),
			func(v any) bool { return !reflect.DeepEqual(v, ov) },
		)
	case !oHas:
		c.add(path, "const", Narrows, "requires "+quote(nv), nil)
	default:
		c.add(path, "const", Widens, "requires "+quote(nv)+" instead of "+quote(ov),
			func(v any) bool { return !reflect.DeepEqual(v, ov) })
	}
}

// bound is a keyword which sets an upper or lower limit on some measure of a value.
type bound struct {
	keyword   string
	upper     bool
	exclusive bool
	measure   func(v any) (float64, bool)
}

var bounds = []bound{
	{keyword: "maxLength", upper: true, measure: stringLength},
	{keyword: "minLength", measure: stringLength},
	{keyword: "maxItems", upper: true, measure: arrayLength},
	{keyword: "minItems", measure: arrayLength},
	{keyword: "maxProperties", upper: true, measure: objectSize},
	{keyword: "minProperties", measure: objectSize},
	{keyword: "maximum", upper: true, measure: number},
	{keyword: "minimum", measure: number},
	{keyword: "exclusiveMaximum", upper: true, exclusive: true, measure: number},
	{keyword: "exclusiveMinimum", exclusive: true, measure: number},
}

// outside reports whether the measure m is outside the limit set by b.
func (b bound) outside(m, limit float64) bool {
	switch {
	case b.upper && b.exclusive:
		return m >= limit
	case b.upper:
		return m > limit
	case b.exclusive:
		return m <= limit
	default:
		return m < limit
	}
}

func (c *comparer) bound(path []segment, b bound, om, nm map[string]any) {
	ov, oHas := om[b.keyword]
	nv, nHas := nm[b.keyword]
	if !oHas && !nHas || reflect.DeepEqual(ov, nv) {
		return
	}
	ol, oNum := number(ov)
	nl, nNum := number(nv)
	if oHas && !oNum || nHas && !nNum {
		// e.g. draft-04's boolean exclusiveMaximum
		c.add(path, b.keyword, Alters, "changes "+b.keyword, nil)
		return
	}
	rejectedByOld := func(v any) bool {
		m, ok := b.measure(v)
		return ok && b.outside(m, ol)
	}
	switch {
	case !nHas:
		c.add(path, b.keyword, Widens, fmt.Sprintf("removes the limit of %v", ov), rejectedByOld)
	case !oHas:
		c.add(path, b.keyword, Narrows, fmt.Sprintf("adds a limit of %v", nv), nil)
	case b.upper && nl > ol, !b.upper && nl < ol:
		c.add(path, b.keyword, Widens, fmt.Sprintf("relaxes the limit from %v to %v", ov, nv), rejectedByOld)
	default:
		c.add(path, b.keyword, Narrows, fmt.Sprintf("tightens the limit from %v to %v", ov, nv), nil)
	}
}

func (c *comparer) pattern(path []segment, om, nm map[string]any) {
	op, oHas := om["pattern"].(string)
	np, nHas := nm["pattern"].(string)
	if !oHas && !nHas || op == np {
		return
	}
	var rejectedByOld func(any) bool
	if re, err := regexp.Compile(op); err == nil {
		rejectedByOld = func(v any) bool {
			s, ok := v.(string)
			return ok && !re.MatchString(s)
		}
	}
	switch {
	case !nHas:
		c.add(path, "pattern", Widens, "removes the pattern "+quote(op), rejectedByOld)
	case !oHas:
		c.add(path, "pattern", Narrows, "adds the pattern "+quote(np), nil)
	default:
		c.add(path, "pattern", Alters, "changes the pattern from "+quote(op)+" to "+quote(np), rejectedByOld)
	}
}

func (c *comparer) multipleOf(path []segment, om, nm map[string]any) {
	ov, oHas := number(om["multipleOf"])
	nv, nHas := number(nm["multipleOf"])
	if !oHas && !nHas || ov == nv {
		return
	}
	rejectedByOld := func(v any) bool {
		n, ok := number(v)
		return ok && !isMultiple(n, ov)
	}
	switch {
	case !nHas:
		c.add(path, "multipleOf", Widens, fmt.Sprintf("no longer requires a multiple of %v", ov), rejectedByOld)
	case !oHas:
		c.add(path, "multipleOf", Narrows, fmt.Sprintf("requires a multiple of %v", nv), nil)
	case isMultiple(nv, ov):
		c.add(path, "multipleOf", Narrows, fmt.Sprintf("requires a multiple of %v instead of %v", nv, ov), nil)
	default:
		c.add(path, "multipleOf", Widens, fmt.Sprintf("requires a multiple of %v instead of %v", nv, ov),
			rejectedByOld)
	}
}

func isMultiple(v, m float64) bool {
	q := v / m
	return math.Abs(q-math.Round(q)) < 1e-9
}

func (c *comparer) required(path []segment, om, nm map[string]any) {
	or := stringsIn(om["required"])
	nr := stringsIn(nm["required"])
	for _, name := range or {
		if !slices.Contains(nr, name) {
			c.add(path, "required", Widens, "no longer requires "+quote(name), func(v any) bool {
				obj, ok := v.(map[string]any)
				if !ok {
					return false
				}
				_, has := obj[name]
				return !has
			})
		}
	}
	for _, name := range nr {
		if !slices.Contains(or, name) {
			c.add(path, "required", Narrows, "requires "+quote(name), nil)
		}
	}
}

// stringsIn returns the strings in v, an unmarshalled JSON array.
func stringsIn(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// quote renders v, an unmarshalled JSON value, as JSON.
func quote(v any) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSuffix(b.String(), "\n")
}

// quoteAll renders each element of values as JSON, separated by commas.
func quoteAll[T any](values []T) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = quote(v)
	}
	return strings.Join(parts, ", ")
}
