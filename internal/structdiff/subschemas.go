package structdiff

import (
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"unicode/utf8"

	"github.com/bitshepherds/json-schema-manager/v2/internal/jsonptr"
)

// subschemas compares the subschemas of om and nm, at the pointers oPtr and nPtr, which apply at l.
func (c *comparer) subschemas(l *location, oPtr, nPtr string, om, nm map[string]any) {
	c.properties(l, oPtr, nPtr, om, nm)
	c.patternProperties(l, oPtr, nPtr, om, nm)
	c.additionalProperties(l, oPtr, nPtr, om, nm)
	c.items(l, oPtr, nPtr, "items", om["items"], nm["items"])
	c.items(l, oPtr, nPtr, "prefixItems", om["prefixItems"], nm["prefixItems"])

	// An allOf arm simply applies, so a change in one bears directly on the values at l
	c.combinator(l, l, oPtr, nPtr, "allOf", om, nm, Narrows)
	c.combinator(l, l.condition(keeps), oPtr, nPtr, "anyOf", om, nm, Widens)
	oneOf := unknowns
	if armsDisjoint(c.oldRoot, om) && armsDisjoint(c.newRoot, nm) {
		oneOf = keeps
	}
	c.combinator(l, l.condition(oneOf), oPtr, nPtr, "oneOf", om, nm, Widens)

	c.walked(l.condition(flips), oPtr, nPtr, "not", om, nm)
	for _, k := range []string{"if", "then", "else"} {
		c.walked(l.condition(unknowns), oPtr, nPtr, k, om, nm)
	}
}

// walked compares the subschema under the keyword, if both versions have it. If only one does,
// otherKeywords reports it.
func (c *comparer) walked(inner *location, oPtr, nPtr, keyword string, om, nm map[string]any) {
	ov, oHas := om[keyword]
	nv, nHas := nm[keyword]
	if oHas && nHas {
		c.node(inner, join(oPtr, keyword), join(nPtr, keyword), ov, nv)
	}
}

// properties compares each property's subschema. A property which only one version declares is
// compared with the subschema the other version applies to it, such as its additionalProperties.
func (c *comparer) properties(l *location, oPtr, nPtr string, om, nm map[string]any) {
	op, _ := om["properties"].(map[string]any)
	np, _ := nm["properties"].(map[string]any)
	for _, name := range unionKeys(op, np) {
		ov, oHas := op[name]
		oChild := join(oPtr, "properties", name)
		if !oHas {
			ov, oChild = undeclared(om, oPtr, name)
		}
		nv, nHas := np[name]
		nChild := join(nPtr, "properties", name)
		if !nHas {
			nv, nChild = undeclared(nm, nPtr, name)
		}
		c.node(l.descend(jsonptr.Property(name), oChild, nChild), oChild, nChild, ov, nv)
	}
}

// undeclared returns the subschema which m applies to a property it does not declare in properties,
// and the pointer to it, given the pointer to m.
func undeclared(m map[string]any, ptr, name string) (sub any, subPtr string) {
	pp, _ := m["patternProperties"].(map[string]any)
	for _, p := range unionKeys(pp, nil) {
		if re, err := regexp.Compile(p); err == nil && re.MatchString(name) {
			return pp[p], join(ptr, "patternProperties", p)
		}
	}
	return additional(m, ptr)
}

// additional returns the subschema which m applies to a property nothing else in it covers, and the
// pointer to it, given the pointer to m.
func additional(m map[string]any, ptr string) (sub any, subPtr string) {
	if ap, ok := m["additionalProperties"]; ok {
		return ap, join(ptr, "additionalProperties")
	}
	return true, noSubschema
}

// patternProperties compares the subschema of each pattern. A pattern which only one version has is
// compared with what the other version applies to properties nothing else covers.
func (c *comparer) patternProperties(l *location, oPtr, nPtr string, om, nm map[string]any) {
	op, _ := om["patternProperties"].(map[string]any)
	np, _ := nm["patternProperties"].(map[string]any)
	for _, p := range unionKeys(op, np) {
		ov, oHas := op[p]
		oChild := join(oPtr, "patternProperties", p)
		if !oHas {
			ov, oChild = additional(om, oPtr)
		}
		nv, nHas := np[p]
		nChild := join(nPtr, "patternProperties", p)
		if !nHas {
			nv, nChild = additional(nm, nPtr)
		}
		c.node(l.descend(jsonptr.PatternSegment(p), oChild, nChild), oChild, nChild, ov, nv)
	}
}

func (c *comparer) additionalProperties(l *location, oPtr, nPtr string, om, nm map[string]any) {
	oa, oChild := additional(om, oPtr)
	na, nChild := additional(nm, nPtr)
	_, oIsMap := oa.(map[string]any)
	_, nIsMap := na.(map[string]any)
	if oIsMap && nIsMap {
		c.node(l.descend(jsonptr.AnySegment, oChild, nChild), oChild, nChild, oa, na)
		return
	}
	c.wholeSchema(l, "additionalProperties", oa, na, func(v any) bool { return hasUndeclared(v, om) })
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
func (c *comparer) items(l *location, oPtr, nPtr, keyword string, o, n any) {
	oa, oIsArr := o.([]any)
	na, nIsArr := n.([]any)
	switch {
	case !oIsArr && !nIsArr:
		if o == nil && n == nil {
			return
		}
		oChild, nChild := join(oPtr, keyword), join(nPtr, keyword)
		if o == nil {
			o, oChild = true, noSubschema
		}
		if n == nil {
			n, nChild = true, noSubschema
		}
		c.node(l.descend(jsonptr.AnySegment, oChild, nChild), oChild, nChild, o, n)
	case oIsArr && nIsArr:
		for i := range max(len(oa), len(na)) {
			ov, oChild := at(oa, oPtr, keyword, i)
			nv, nChild := at(na, nPtr, keyword, i)
			c.node(l.descend(jsonptr.Item(i), oChild, nChild), oChild, nChild, ov, nv)
		}
	default:
		c.add(l, keyword, Alters, "changes between one subschema for every item and one per position", nil)
	}
}

// at returns the subschema at position i, or true if there is none, and the pointer to it.
func at(schemas []any, ptr, keyword string, i int) (sub any, subPtr string) {
	if i < len(schemas) {
		return schemas[i], join(ptr, keyword, strconv.Itoa(i))
	}
	return true, noSubschema
}

// combinator compares the subschemas of allOf, anyOf or oneOf. Equal arms pair up first, so reordering
// them changes nothing, and any others pair up by position. The effect of adding a subschema is added:
// it Narrows an allOf, and Widens an anyOf or oneOf. Inside the arms, the walk is at inner. An anyOf or
// oneOf appearing or going has the same effect whatever its arms, as having none constrains nothing.
func (c *comparer) combinator(l, inner *location, oPtr, nPtr, keyword string, om, nm map[string]any, added Effect) {
	oa, _ := om[keyword].([]any)
	na, _ := nm[keyword].([]any)
	if added == Widens {
		switch {
		case len(oa) == 0 && len(na) > 0:
			c.add(l, keyword, Narrows, "adds "+keyword, nil)
			return
		case len(oa) > 0 && len(na) == 0:
			c.add(l, keyword, Widens, "removes "+keyword, nil)
			return
		}
	}

	oLeft, nLeft := unmatchedArms(oa, na)
	for i := range min(len(oLeft), len(nLeft)) {
		c.node(inner, join(oPtr, keyword, strconv.Itoa(oLeft[i])), join(nPtr, keyword, strconv.Itoa(nLeft[i])),
			oa[oLeft[i]], na[nLeft[i]])
	}
	removed := Widens
	if added == Widens {
		removed = Narrows
	}
	switch {
	case len(nLeft) > len(oLeft):
		c.add(inner, keyword, added, "adds a subschema", nil)
	case len(nLeft) < len(oLeft):
		c.add(inner, keyword, removed, "removes a subschema", nil)
	}
}

// unmatchedArms returns the positions of the arms in oa and na with no equal arm in the other.
func unmatchedArms(oa, na []any) (oLeft, nLeft []int) {
	taken := make([]bool, len(na))
	for i, o := range oa {
		matched := false
		for j, n := range na {
			if !taken[j] && reflect.DeepEqual(o, n) {
				taken[j], matched = true, true
				break
			}
		}
		if !matched {
			oLeft = append(oLeft, i)
		}
	}
	for j := range na {
		if !taken[j] {
			nLeft = append(nLeft, j)
		}
	}
	return oLeft, nLeft
}

// armsDisjoint reports whether no value can match two arms of m's oneOf, so it acts as an anyOf: every
// pair of arms has different types, or different consts on the same required property.
func armsDisjoint(root any, m map[string]any) bool {
	arms, _ := m["oneOf"].([]any)
	for i, a := range arms {
		for _, b := range arms[i+1:] {
			if !armPairDisjoint(root, a, b) {
				return false
			}
		}
	}
	return true
}

func armPairDisjoint(root, a, b any) bool {
	aTarget, _ := jsonptr.FollowRefs(root, a)
	bTarget, _ := jsonptr.FollowRefs(root, b)
	am, aIsMap := aTarget.(map[string]any)
	bm, bIsMap := bTarget.(map[string]any)
	if !aIsMap || !bIsMap {
		return false
	}
	return typesDisjoint(am, bm) || constsDiffer(root, am, bm)
}

// typesDisjoint reports whether a and b each restrict the type, and no type satisfies both. An integer
// is a number, so those two overlap.
func typesDisjoint(a, b map[string]any) bool {
	aTypes, aHas := typeSet(a)
	bTypes, bHas := typeSet(b)
	return aHas && bHas && len(uncovered(aTypes, bTypes)) == len(aTypes) &&
		len(uncovered(bTypes, aTypes)) == len(bTypes)
}

// constsDiffer reports whether a and b both require a property, with a different const for it in each.
func constsDiffer(root any, a, b map[string]any) bool {
	return slices.ContainsFunc(stringsIn(a["required"]), func(name string) bool {
		if !slices.Contains(stringsIn(b["required"]), name) {
			return false
		}
		ac, aHas := propertyConst(root, a, name)
		bc, bHas := propertyConst(root, b, name)
		return aHas && bHas && !reflect.DeepEqual(ac, bc)
	})
}

func propertyConst(root any, m map[string]any, name string) (any, bool) {
	props, _ := m["properties"].(map[string]any)
	sub, _ := jsonptr.FollowRefs(root, props[name])
	sm, _ := sub.(map[string]any)
	v, ok := sm["const"]
	return v, ok
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
