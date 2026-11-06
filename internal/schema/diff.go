package schema

import (
	"github.com/bitshepherds/json-schema-manager/internal/structdiff"
)

// Change is a structural change from one schema to another, with the newer schema's pass
// documents which use it.
type Change struct {
	Change structdiff.Change
	UsedBy []string // the paths of the newer schema's pass documents which use the change
}

// Diff lists the structural changes from older to newer, as rendered for production.
func Diff(older, newer *Schema) ([]Change, error) {
	changes, err := compareVersions(older, newer)
	if err != nil {
		return nil, err
	}
	passDocs, err := newer.TestDocuments(TestDocTypePass)
	if err != nil {
		return nil, err
	}

	out := make([]Change, len(changes))
	for i := range changes {
		out[i].Change = changes[i]
		for _, doc := range passDocs {
			if changes[i].UsedBy(doc.Unmarshalled, older.subschemaValidates()) {
				out[i].UsedBy = append(out[i].UsedBy, doc.Path)
			}
		}
	}
	return out, nil
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
