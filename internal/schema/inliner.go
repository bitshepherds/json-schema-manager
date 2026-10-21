package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/validator"
)

// Inliner collapses external $ref references to JSM-managed schemas into internal
// #/$defs/<key> references, collecting the referenced schema bodies into a $defs block.
// This process is recursive: if a referenced schema itself references other schemas,
// those are also inlined. The result is a fully self-contained JSON Schema.
type Inliner struct {
	registry    *Registry
	envCfg      *config.EnvConfig
	newCompiler func() validator.Compiler
}

// NewInliner creates a new Inliner for the given registry and environment configuration.
func NewInliner(r *Registry, ec *config.EnvConfig) *Inliner {
	return &Inliner{
		registry:    r,
		envCfg:      ec,
		newCompiler: validator.NewSanthoshCompiler,
	}
}

// Collapse takes a rendered JSON Schema and returns a version with all external $ref
// references to JSM-managed schemas resolved into #/$defs/<key> references.
// Referenced schema bodies are collected in a top-level $defs block.
// The $id and $schema properties are stripped from each inlined schema, as the schema key
// serves as the unique identifier in the $defs block, and only the root may declare $schema.
// JSON pointer references within an inlined schema, such as #/definitions/x, are rebased to
// its place in the $defs block.
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

	base, fragment, _ := strings.Cut(ref, "#")
	key, matched := inl.matchJSMRef(base)
	if !matched {
		return nil
	}

	if fragment == "" || strings.HasPrefix(fragment, "/") {
		obj["$ref"] = "#/$defs/" + string(key) + fragment
	} else {
		// A named anchor stays valid, as the inlined schema keeps its $anchor
		obj["$ref"] = "#" + fragment
	}
	if visited[key] {
		return nil
	}

	visited[key] = true
	defBody, err := inl.renderDef(key)
	if err != nil {
		return err
	}
	rebaseLocalRefs(defBody, "#/$defs/"+string(key))
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

// rebaseLocalRefs rewrites the JSON pointer $refs within an inlined schema body, such as
// #/definitions/x, to point into the body's new place at prefix in the collapsed schema.
func rebaseLocalRefs(node any, prefix string) {
	switch v := node.(type) {
	case map[string]any:
		if ref, ok := v["$ref"].(string); ok && (ref == "#" || strings.HasPrefix(ref, "#/")) {
			v["$ref"] = prefix + ref[1:]
		}
		for _, val := range v {
			rebaseLocalRefs(val, prefix)
		}
	case []any:
		for _, item := range v {
			rebaseLocalRefs(item, prefix)
		}
	}
}

// renderDef renders the referenced schema and returns its body as a map, with
// the $id and $schema properties stripped.
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
	delete(body, "$schema")

	return body, nil
}

// Verify compiles the collapsed form of s on its own, and checks that each of s's pass and fail
// test documents gives the same result against it as against original, the validator for s.
func (inl *Inliner) Verify(s *Schema, original validator.Validator, collapsed []byte) error {
	k := s.Key()
	data, err := jsonschema.UnmarshalJSON(bytes.NewReader(collapsed))
	if err != nil {
		return &CollapsedSchemaError{Key: k, Wrapped: err}
	}

	c := inl.newCompiler()
	id := string(s.CanonicalID(inl.envCfg))
	if err = c.AddSchema(id, data); err != nil {
		return &CollapsedSchemaError{Key: k, Wrapped: err}
	}
	v, err := c.Compile(id)
	if err != nil {
		return &CollapsedSchemaError{Key: k, Wrapped: fmt.Errorf("it does not compile: %w", err)}
	}

	for _, tt := range []TestDocType{TestDocTypePass, TestDocTypeFail} {
		docs, dErr := s.TestDocuments(tt)
		var missing *TestDirMissingConfigError
		if errors.As(dErr, &missing) {
			continue
		}
		if dErr != nil {
			return dErr
		}
		for _, doc := range docs {
			wasValid := original.Validate(doc.Unmarshalled) == nil
			if isValid := v.Validate(doc.Unmarshalled) == nil; isValid != wasValid {
				return &CollapsedSchemaError{Key: k, Wrapped: fmt.Errorf(
					"test document %s is %s by the original but %s by the collapsed form",
					doc.Path, validity(wasValid), validity(isValid))}
			}
		}
	}
	return nil
}

func validity(valid bool) string {
	if valid {
		return "accepted"
	}
	return "rejected"
}
