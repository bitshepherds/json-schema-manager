// Package structdiff compares two versions of a JSON Schema, and lists the structural changes
// between them, such as enum values added, constraints raised or lowered, and types allowed.
//
// The comparison is structural: it walks both schemas side by side, following local $refs, and
// compares the keywords at each location. It does not decide whether two schemas accept the same
// documents. Where the walk can't tell which way a change goes, it says so with Alters, and such a
// change may in fact make no difference.
package structdiff

import (
	"net/url"
	"reflect"
	"slices"
	"strings"

	"github.com/bitshepherds/json-schema-manager/internal/jsonptr"
)

// Effect says how a change affects the documents a schema accepts.
type Effect string

const (
	// Widens means the newer schema accepts documents which the older one rejects, so a reader on
	// the older version may reject documents written to the newer one.
	Widens Effect = "widens"
	// Narrows means the newer schema rejects documents which the older one accepts, so a reader on
	// the newer version may reject documents written to the older one.
	Narrows Effect = "narrows"
	// Alters means structdiff cannot tell: the change may widen what the schema accepts, narrow it,
	// or both.
	Alters Effect = "alters"
)

// Validates reports whether the subschema at a pointer in a schema accepts v. The pointer is a JSON
// Pointer written as a URL fragment. It reports ok as false if it can't tell.
type Validates func(pointer string, v any) (accepted, ok bool)

// noSubschema is the pointer to a subschema which isn't there, which accepts every value.
const noSubschema = "\x00"

// Change is one structural change between two versions of a schema.
type Change struct {
	Location string // where in a document the change applies, as a JSON Pointer; * stands for any item or property
	Keyword  string // the keyword which changed, or empty when a whole subschema changed
	Effect   Effect
	Detail   string // what changed, e.g. `adds "refunded"`

	// path is where to find the values which can use the change. It differs from Location for a change
	// inside a subschema which isn't simply applied, such as an anyOf arm: there, what the arm does to
	// a value says nothing about what the whole combinator does, so the values are those at the combinator.
	path []jsonptr.Segment
	// older and newer point to the subschemas in each version which apply to those values.
	older, newer string
	// uses reports whether a value at path uses the change, so the older schema rejects it. It is nil
	// when structdiff cannot tell, and the older subschema then decides.
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

// UsedBy reports whether doc has a value which uses the change, so a document which accepts it shows
// the older version rejecting something the newer one accepts. Where the change has no precise test,
// as with an anyOf alternative added, a value uses it only if the older subschema at that location
// rejects it, which older decides.
func (c *Change) UsedBy(doc any, older Validates) bool {
	return slices.ContainsFunc(jsonptr.ValuesAt(doc, c.path), func(v any) bool {
		if c.uses != nil {
			return c.uses(v)
		}
		return rejects(older, c.older, v)
	})
}

// NarrowedBy reports whether doc has a value which shows the change narrowing the schema: the older
// subschema at that location accepts it, and the newer one rejects it.
func (c *Change) NarrowedBy(doc any, older, newer Validates) bool {
	return slices.ContainsFunc(jsonptr.ValuesAt(doc, c.path), func(v any) bool {
		return accepts(older, c.older, v) && rejects(newer, c.newer, v)
	})
}

func rejects(validates Validates, pointer string, v any) bool {
	if pointer == noSubschema {
		return false
	}
	accepted, ok := validates(pointer, v)
	return ok && !accepted
}

func accepts(validates Validates, pointer string, v any) bool {
	if pointer == noSubschema {
		return true
	}
	accepted, ok := validates(pointer, v)
	return ok && accepted
}

// Option changes what Compare looks at.
type Option func(*comparer)

// AssertFormat says whether the validator is set to treat the format keyword as an assertion, so a
// change to it changes what's accepted. Drafts before 2019-09 assert it regardless.
func AssertFormat(assert bool) Option {
	return func(c *comparer) { c.assertFormat = assert }
}

// Compare lists the structural changes from older to newer, which are unmarshalled JSON Schemas.
func Compare(older, newer any, opts ...Option) []Change {
	c := &comparer{
		oldRoot: older,
		newRoot: newer,
		// A $ref to the root of both schemas recurses
		seen: map[[2]string]bool{{"#", "#"}: true},
	}
	for _, opt := range opts {
		opt(c)
	}
	c.node(&location{}, "", "", older, newer)
	return c.changes
}

type comparer struct {
	oldRoot, newRoot any
	assertFormat     bool
	seen             map[[2]string]bool // the local $ref pairs being compared, to stop at recursive schemas
	changes          []Change
}

// polarity says how a change to a subschema bears on the schema it's within.
type polarity int

const (
	keeps    polarity = iota // as in allOf and anyOf
	flips                    // as in not
	unknowns                 // as in oneOf and if, where structdiff can't tell
)

// within returns the polarity of a subschema with polarity p, inside one with polarity outer.
func (p polarity) within(outer polarity) polarity {
	switch {
	case p == unknowns || outer == unknowns:
		return unknowns
	case p == outer:
		return keeps
	default:
		return flips
	}
}

func (p polarity) apply(e Effect) Effect {
	switch {
	case p == unknowns:
		return Alters
	case p == flips && e == Widens:
		return Narrows
	case p == flips && e == Narrows:
		return Widens
	default:
		return e
	}
}

// location is where the walk has reached.
type location struct {
	path []jsonptr.Segment // where a change applies

	// testPath, older and newer say which values can use a change here, and which subschemas decide.
	testPath     []jsonptr.Segment
	older, newer string

	polarity polarity
	// conditional means the walk is inside a subschema which isn't simply applied, such as an anyOf
	// arm: testPath, older and newer stay at that subschema's parent, and no precise test holds.
	conditional bool
}

// descend returns the location of a child value, whose subschemas are at the given pointers.
func (l *location) descend(s jsonptr.Segment, older, newer string) *location {
	child := *l
	child.path = append(slices.Clip(l.path), s)
	if !child.conditional {
		child.testPath, child.older, child.newer = child.path, older, newer
	}
	return &child
}

// condition returns the location inside a subschema, of the given polarity, which isn't simply applied.
func (l *location) condition(p polarity) *location {
	inner := *l
	inner.polarity = p.within(l.polarity)
	inner.conditional = true
	return &inner
}

func (c *comparer) add(l *location, keyword string, effect Effect, detail string, uses func(any) bool) {
	if l.conditional {
		uses = nil
	}
	c.changes = append(c.changes, Change{
		Location: jsonptr.Format(l.path),
		Keyword:  keyword,
		Effect:   l.polarity.apply(effect),
		Detail:   detail,
		path:     slices.Clone(l.testPath),
		older:    l.older,
		newer:    l.newer,
		uses:     uses,
	})
}

// join returns the pointer to a child of the subschema at ptr, given the tokens which lead to it.
func join(ptr string, tokens ...string) string {
	var b strings.Builder
	b.WriteString(ptr)
	for _, t := range tokens {
		b.WriteString("/")
		b.WriteString(url.PathEscape(jsonptr.Escape(t)))
	}
	return b.String()
}

// node compares the subschemas o and n, at the pointers oPtr and nPtr, which apply at l.
func (c *comparer) node(l *location, oPtr, nPtr string, oRaw, nRaw any) {
	o, oRef := jsonptr.FollowRefs(c.oldRoot, oRaw)
	n, nRef := jsonptr.FollowRefs(c.newRoot, nRaw)
	if c.refSiblings(l, oPtr, nPtr, oRaw, nRaw, oRef, nRef) {
		return
	}
	if oRef != "" || nRef != "" {
		pair := [2]string{oRef, nRef}
		if c.seen[pair] {
			return
		}
		c.seen[pair] = true
		defer delete(c.seen, pair)
		oPtr, nPtr = targetPointer(oPtr, oRef), targetPointer(nPtr, nRef)
	}

	om, oIsMap := o.(map[string]any)
	nm, nIsMap := n.(map[string]any)
	if !oIsMap || !nIsMap {
		c.wholeSchema(l, "", o, n, nil)
		return
	}
	c.subschema(l, oPtr, nPtr, om, nm)
}

// subschema compares every keyword of om and nm.
func (c *comparer) subschema(l *location, oPtr, nPtr string, om, nm map[string]any) {
	c.keywords(l, om, nm)
	c.subschemas(l, oPtr, nPtr, om, nm)
	c.otherKeywords(l, om, nm)
}

// targetPointer returns the pointer to what the $ref ref leads to, or ptr if there's no $ref.
func targetPointer(ptr, ref string) string {
	if ref == "" {
		return ptr
	}
	return strings.TrimPrefix(ref, "#")
}

// refSiblings compares the keywords beside a local $ref, which apply as well as what it leads to from
// draft 2019-09 on. It reports whether there's nothing more to compare at l, as a subschema which
// moves between a $ref with keywords beside it and no $ref can't be compared keyword by keyword.
func (c *comparer) refSiblings(l *location, oPtr, nPtr string, oRaw, nRaw any, oRef, nRef string) bool {
	os := refSiblings(c.oldRoot, oRaw, oRef)
	ns := refSiblings(c.newRoot, nRaw, nRef)
	switch {
	case len(os) == 0 && len(ns) == 0:
		return false
	case oRef == "" || nRef == "":
		c.add(l, "$ref", Alters, "changes between a $ref with keywords beside it and no $ref", nil)
		return true
	default:
		c.subschema(l, oPtr, nPtr, os, ns)
		return false
	}
}

// refSiblings returns the keywords of raw beside its local $ref ref, which constrain a value alongside it.
func refSiblings(root, raw any, ref string) map[string]any {
	if ref == "" || earlyDraft(root) {
		return nil
	}
	m, _ := raw.(map[string]any)
	var out map[string]any
	for k, v := range m {
		if k == "$ref" || k == "$schema" || slices.Contains(ignoredKeywords, k) {
			continue
		}
		if out == nil {
			out = make(map[string]any)
		}
		out[k] = v
	}
	return out
}

// earlyDraft reports whether the schema's draft is one before 2019-09, which ignores keywords beside a
// $ref and asserts format whatever the config says. A schema which names no draft is validated under
// the latest.
func earlyDraft(root any) bool {
	m, _ := root.(map[string]any)
	draft, _ := m["$schema"].(string)
	return strings.Contains(draft, "draft-0")
}

// wholeSchema compares subschemas o and n where either is a boolean schema. An absent subschema
// counts as true. When o is false, the function usesOldFalse reports whether a value uses the
// change; if it is nil, any value does.
func (c *comparer) wholeSchema(l *location, keyword string, o, n any, usesOldFalse func(any) bool) {
	if reflect.DeepEqual(o, n) {
		return
	}
	switch {
	case o == false:
		if usesOldFalse == nil {
			usesOldFalse = func(any) bool { return true }
		}
		c.add(l, keyword, Widens, "accepts values where the earlier version accepted none", usesOldFalse)
	case n == false:
		c.add(l, keyword, Narrows, "accepts no values", nil)
	case o == true:
		c.add(l, keyword, Narrows, "adds constraints where the earlier version had none", nil)
	default:
		c.add(l, keyword, Widens, "removes every constraint", nil)
	}
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
	"$id", "$anchor", "$comment", "$defs", "definitions",
	"title", "description", "examples", "default", "deprecated", "readOnly", "writeOnly",
	"contentEncoding", "contentMediaType", "contentSchema",
}

// walkedKeywords are compared by subschemas when both versions have them.
var walkedKeywords = []string{"not", "if", "then", "else"}

// otherKeywords reports a change to any keyword which structdiff does not otherwise compare.
// Adding one usually adds a constraint, and removing one removes it.
func (c *comparer) otherKeywords(l *location, om, nm map[string]any) {
	for _, k := range unionKeys(om, nm) {
		ov, oHas := om[k]
		nv, nHas := nm[k]
		bothHave := oHas && nHas
		switch {
		case slices.Contains(handledKeywords, k), slices.Contains(ignoredKeywords, k):
		case bothHave && slices.Contains(walkedKeywords, k):
		case k == "$ref" && (jsonptr.IsLocalRef(ov) || jsonptr.IsLocalRef(nv)): // compared by following it
		case k == "format" && !c.formatAsserted(): // an annotation
		case k == "$schema":
			c.draft(l, ov, nv)
		case k == "if" && !bothHave:
			c.ifAppears(l, oHas)
		default:
			c.otherKeyword(l, k, ov, nv, oHas, nHas)
		}
	}
}

func (c *comparer) formatAsserted() bool {
	return c.assertFormat || earlyDraft(c.oldRoot) || earlyDraft(c.newRoot)
}

// ifAppears reports an if appearing or going, given whether the older version has it. It changes which
// values then and else apply to, so it can go either way.
func (c *comparer) ifAppears(l *location, oHas bool) {
	if oHas {
		c.add(l, "if", Alters, "removes if", nil)
		return
	}
	c.add(l, "if", Alters, "adds if", nil)
}

func (c *comparer) otherKeyword(l *location, k string, ov, nv any, oHas, nHas bool) {
	switch {
	case !oHas:
		c.add(l, k, Narrows, "adds "+k, nil)
	case !nHas:
		c.add(l, k, Widens, "removes "+k, nil)
	case !reflect.DeepEqual(ov, nv):
		c.add(l, k, Alters, "changes "+k, nil)
	}
}

// draft reports a change of draft, which can change what every other keyword means. A trailing # on
// the draft's URL is no change.
func (c *comparer) draft(l *location, ov, nv any) {
	normal := func(v any) string {
		s, _ := v.(string)
		return strings.TrimSuffix(s, "#")
	}
	if normal(ov) != normal(nv) {
		c.add(l, "$schema", Alters, "changes the draft from "+quote(ov)+" to "+quote(nv), nil)
	}
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
