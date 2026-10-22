package schema

import (
	"bytes"
	"text/template"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/validator"
)

// Renderer is a go template renderer which converts a source schema into
// a rendered schema targeting a specific environment.
type Renderer struct {
	s  *Schema           // The schema to render
	ec *config.EnvConfig // The target environment for which the schema is being rendered.
}

// NewRenderer creates a new renderer for the given schema and environment configuration.
func NewRenderer(s *Schema, ec *config.EnvConfig) *Renderer {
	return &Renderer{
		s:  s,
		ec: ec,
	}
}

// Render renders the schema. Note that if the {{ JSM <Key> }} template is used, then
// rendering will cause the referenced schema to be loaded and rendered too if necessary, and
// this will continue recursively until all dependencies are resolved.
// On success, it returns the rendered schema bytes and the unmarshalled validator.JSONSchema.
func (r *Renderer) Render() ([]byte, validator.JSONSchema, error) {
	fp := r.s.Path(FilePath)

	// Clone the master so we are thread safe, and can provide a 'global' funcMap which
	// lets us omit use of the leading . (e.g. {{ ID }} instead of {{ .ID }})
	tmpl, err := r.s.tmpl.Clone()
	if err != nil {
		return nil, nil, err
	}

	tmpl.Funcs(template.FuncMap{
		"ID":  r.ID,
		"JSM": r.JSM,
	})

	var buf bytes.Buffer
	if err = tmpl.Execute(&buf, nil); err != nil {
		return nil, nil, &TemplateExecutionFailedError{Path: fp, Wrapped: err}
	}

	renderedBytes := buf.Bytes()
	data, err := jsonschema.UnmarshalJSON(bytes.NewReader(renderedBytes))
	if err != nil {
		return nil, nil, &InvalidJSONError{Path: fp, Wrapped: err}
	}
	// The $id must come from {{ ID }}, so that it follows the schema's place in the registry
	if m, ok := data.(map[string]any); ok {
		if id, has := m["$id"]; has && id != string(r.s.CanonicalID(r.ec)) {
			return nil, nil, &HardCodedIDError{Path: fp, ID: id}
		}
	}
	return renderedBytes, validator.JSONSchema(data), nil
}

// ID is a template function which returns the canonical ID of the schema.
func (r *Renderer) ID() (ID, error) {
	return r.s.CanonicalID(r.ec), nil
}

// JSM is a template function which returns the canonical ID of the referenced schema.
func (r *Renderer) JSM(arg string) (ID, error) {
	c, err := NewCoreFromString(arg, KeySeparator)
	if err != nil {
		return "", &JSMArgInvalidKeyError{Arg: arg}
	}
	key := c.Key()

	s, err := r.s.registry.GetSchemaByKey(key)
	if err != nil {
		return "", &JSMArgNotFoundError{Key: key, Wrapped: err}
	}

	// We also need to force a compilation of the schema to ensure that it is valid.
	// This will render and compile the schema if it hasn't been rendered yet.
	if _, err = s.Render(r.ec); err != nil {
		return "", err
	}

	return s.CanonicalID(r.ec), nil
}
