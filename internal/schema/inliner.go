package schema

import (
	"encoding/json"
	"strings"

	"github.com/bitshepherds/json-schema-manager/internal/config"
)

// Inliner collapses external $ref references to JSM-managed schemas into internal
// #/$defs/<key> references, collecting the referenced schema bodies into a $defs block.
// This process is recursive: if a referenced schema itself references other schemas,
// those are also inlined. The result is a fully self-contained JSON Schema.
type Inliner struct {
	registry *Registry
	envCfg   *config.EnvConfig
}

// NewInliner creates a new Inliner for the given registry and environment configuration.
func NewInliner(r *Registry, ec *config.EnvConfig) *Inliner {
	return &Inliner{
		registry: r,
		envCfg:   ec,
	}
}

// Collapse takes a rendered JSON Schema and returns a version with all external $ref
// references to JSM-managed schemas resolved into #/$defs/<key> references.
// Referenced schema bodies are collected in a top-level $defs block.
// The $id property is stripped from each inlined schema, as the schema key
// serves as the unique identifier in the $defs block.
func (inl *Inliner) Collapse(rendered []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(rendered, &doc); err != nil {
		return nil, err
	}

	defs := make(map[string]any)
	visited := make(map[Key]bool)

	if err := inl.rewriteRefs(doc, defs, visited); err != nil {
		return nil, err
	}

	if len(defs) > 0 {
		existing, _ := doc["$defs"].(map[string]any)
		if existing == nil {
			existing = make(map[string]any)
		}
		for k, v := range defs {
			existing[k] = v
		}
		doc["$defs"] = existing
	}

	return json.MarshalIndent(doc, "", "  ")
}

// rewriteRefs walks the JSON tree, replacing $ref values that match JSM-managed
// schema URLs with #/$defs/<key> references. For each matched reference, the
// referenced schema body is rendered and added to the defs accumulator.
func (inl *Inliner) rewriteRefs(node any, defs map[string]any, visited map[Key]bool) error {
	switch v := node.(type) {
	case map[string]any:
		if err := inl.processObject(v, defs, visited); err != nil {
			return err
		}
	case []any:
		for _, item := range v {
			if err := inl.rewriteRefs(item, defs, visited); err != nil {
				return err
			}
		}
	}
	return nil
}

// processObject handles a single JSON object node, checking for $ref and recursing
// into child values.
func (inl *Inliner) processObject(obj, defs map[string]any, visited map[Key]bool) error {
	if err := inl.handleRef(obj, defs, visited); err != nil {
		return err
	}

	for k, val := range obj {
		if k == "$ref" {
			continue
		}
		if err := inl.rewriteRefs(val, defs, visited); err != nil {
			return err
		}
	}
	return nil
}

// handleRef checks for a $ref in the object, and if it's a JSM-managed schema,
// inlines it into the defs accumulator.
func (inl *Inliner) handleRef(obj, defs map[string]any, visited map[Key]bool) error {
	ref, ok := obj["$ref"].(string)
	if !ok {
		return nil
	}

	key, matched := inl.matchJSMRef(ref)
	if !matched {
		return nil
	}

	obj["$ref"] = "#/$defs/" + string(key)
	if visited[key] {
		return nil
	}

	visited[key] = true
	defBody, err := inl.renderDef(key)
	if err != nil {
		return err
	}
	defs[string(key)] = defBody

	// Recursively process the inlined definition for its own refs
	return inl.rewriteRefs(defBody, defs, visited)
}

// matchJSMRef checks whether a $ref string is a canonical URL for a JSM-managed schema.
// It attempts to match against both the public and private URL roots for the current
// environment. Returns the schema Key and true if matched, or empty and false otherwise.
func (inl *Inliner) matchJSMRef(ref string) (Key, bool) {
	for _, urlRoot := range []string{inl.envCfg.PublicURLRoot, inl.envCfg.PrivateURLRoot} {
		base := urlRoot
		if !strings.HasSuffix(base, "/") {
			base += "/"
		}
		if strings.HasPrefix(ref, base) && strings.HasSuffix(ref, SchemaSuffix) {
			stem := ref[len(base) : len(ref)-len(SchemaSuffix)]
			if k, err := NewKey(stem); err == nil {
				return k, true
			}
		}
	}
	return "", false
}

// renderDef renders the referenced schema and returns its body as a map, with
// the $id property stripped.
func (inl *Inliner) renderDef(k Key) (map[string]any, error) {
	s, err := inl.registry.GetSchemaByKey(k)
	if err != nil {
		return nil, err
	}

	ri, err := inl.registry.CoordinateRender(s, inl.envCfg)
	if err != nil {
		return nil, err
	}

	var body map[string]any
	if err := json.Unmarshal(ri.Rendered, &body); err != nil {
		return nil, err
	}

	// Strip $id as the schema key serves as the unique identifier in $defs
	delete(body, "$id")

	return body, nil
}
