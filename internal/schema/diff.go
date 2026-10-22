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
	ec := newer.registry.config.ProductionEnvConfig()
	ori, err := older.Render(ec)
	if err != nil {
		return nil, err
	}
	nri, err := newer.Render(ec)
	if err != nil {
		return nil, err
	}
	passDocs, err := newer.TestDocuments(TestDocTypePass)
	if err != nil {
		return nil, err
	}

	changes := structdiff.Compare(ori.Unmarshalled, nri.Unmarshalled)
	out := make([]Change, len(changes))
	for i := range changes {
		out[i].Change = changes[i]
		for _, doc := range passDocs {
			if changes[i].UsedBy(doc.Unmarshalled) {
				out[i].UsedBy = append(out[i].UsedBy, doc.Path)
			}
		}
	}
	return out, nil
}
