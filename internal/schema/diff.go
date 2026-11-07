package schema

import (
	"slices"

	"github.com/bitshepherds/json-schema-manager/internal/structdiff"
)

// Change is a structural change from one schema to another, with the documents which show it, and
// the directions the family protects in which none does.
type Change struct {
	Change  structdiff.Change
	UsedBy  []string            // the paths of the pass documents which use a change that may widen the schema
	ShownBy []string            // the paths of the fail documents which show a change that may narrow it
	Missing []structdiff.Effect // the directions the family protects in which no document shows the change
}

// Diff lists the structural changes from older to newer, as rendered for production. For a pair
// validate compares, it looks for each change's documents as validate does, and says where its family
// protects a direction with none to show it. For any other pair, such as two majors, it looks only in
// newer's documents, and nothing is protected.
func Diff(older, newer *Schema) ([]Change, error) {
	changes, err := compareVersions(older, newer)
	if err != nil {
		return nil, err
	}
	ahead, between, err := comparedAs(older, newer)
	if err != nil {
		return nil, err
	}
	evidence, err := newChangeEvidence(older, newer, between)
	if err != nil {
		return nil, err
	}

	out := make([]Change, len(changes))
	for i := range changes {
		c := &changes[i]
		out[i] = Change{Change: *c, Missing: evidence.missing(c, ahead)}
		// A family which protects both directions asks about every way the change can go
		for _, direction := range protectedDirections(AheadEither, c.Effect) {
			if direction == structdiff.Narrows {
				out[i].ShownBy = evidence.showing(c, direction)
			} else {
				out[i].UsedBy = evidence.showing(c, direction)
			}
		}
	}
	return out, nil
}

// comparedAs returns who can be ahead in the pair, as validate sees it, and the versions whose documents
// can show a change between the two. Validate doesn't compare a pair outside newer's live earlier
// versions, so such a pair has nobody ahead, and only newer's documents.
func comparedAs(older, newer *Schema) (Ahead, []*Schema, error) {
	decl, err := newer.registry.FamilyDeclaration(newer.Key())
	if err != nil {
		return "", nil, err
	}
	t := NewTester(newer.registry)
	earlier, _, err := t.otherVersions(newer, decl)
	if err != nil {
		return "", nil, err
	}
	if !slices.ContainsFunc(earlier, func(s *Schema) bool { return s.Key() == older.Key() }) {
		return AheadNeither, []*Schema{newer}, nil
	}
	between, err := t.versionsAfter(older, newer)
	return decl.Facts.EffectiveAhead(), between, err
}

// compareVersions lists the structural changes from older to newer, as rendered for production.
func compareVersions(older, newer *Schema) ([]structdiff.Change, error) {
	cfg := newer.registry.config
	ec := cfg.ProductionEnvConfig()
	ori, err := older.Render(ec)
	if err != nil {
		return nil, err
	}
	nri, err := newer.Render(ec)
	if err != nil {
		return nil, err
	}
	return structdiff.Compare(ori.Unmarshalled, nri.Unmarshalled, structdiff.AssertFormat(cfg.AssertFormat)), nil
}

// subschemaValidates returns a function which says whether the subschema at a pointer in the schema, as
// rendered for production, accepts a value. It says it can't tell if the subschema doesn't compile.
func (s *Schema) subschemaValidates() func(pointer string, v any) (accepted, ok bool) {
	id := string(s.CanonicalID(s.registry.config.ProductionEnvConfig()))
	return func(pointer string, v any) (bool, bool) {
		sub, err := s.registry.compiler.Compile(id + "#" + pointer)
		if err != nil {
			return false, false
		}
		return sub.Validate(v) == nil, true
	}
}
