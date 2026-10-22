package validator

import (
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// NewSanthoshCompiler returns a concrete implementation of Compiler.
// Using the santhosh-tekuri/jsonschema/v6 package.
func NewSanthoshCompiler() Compiler {
	return &santhoshCompiler{c: newJSONSchemaCompiler(false)}
}

// newJSONSchemaCompiler returns a compiler which refuses external $refs, and which asserts the
// format keyword if assertFormat is true.
func newJSONSchemaCompiler(assertFormat bool) *jsonschema.Compiler {
	c := jsonschema.NewCompiler()
	c.UseLoader(jsonschema.SchemeURLLoader{
		"file":  jsonschema.FileLoader{},
		"http":  externalRefLoader{},
		"https": externalRefLoader{},
	})
	if assertFormat {
		c.AssertFormat()
	}
	return c
}

// ExternalRefError is returned when a schema refers to a URL which is not a schema in the registry.
type ExternalRefError struct {
	URL string
}

func (e *ExternalRefError) Error() string {
	return fmt.Sprintf("$ref %s is external, and external $refs are not supported: refer to a schema in the "+
		"registry with {{ JSM `key` }}, or copy the external schema into the registry", e.URL)
}

// externalRefLoader refuses to load a schema from the network, so that a registry is closed:
// every schema it uses is in the registry, and tested there.
type externalRefLoader struct{}

func (externalRefLoader) Load(url string) (any, error) {
	return nil, &ExternalRefError{URL: url}
}

// santhoshValidator wraps jsonschema.Schema to implement Validator.
type santhoshValidator struct {
	v *jsonschema.Schema
}

// Validate adapts jsonschema.Schema.Validate to match the Validator interface.
func (sv *santhoshValidator) Validate(doc JSONDocument) error {
	return sv.v.Validate(doc)
}

// santhoshCompiler wraps jsonschema.Compiler to implement Compiler.
type santhoshCompiler struct {
	mu           sync.Mutex
	c            *jsonschema.Compiler
	assertFormat bool
}

// AssertFormat makes the compiler treat the format keyword as an assertion, which the 2019-09
// and 2020-12 drafts otherwise treat as an annotation.
func (s *santhoshCompiler) AssertFormat() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assertFormat = true
	s.c.AssertFormat()
}

func (s *santhoshCompiler) AddSchema(id string, schemaData JSONSchema) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.c.AddResource(id, schemaData)
}

func (s *santhoshCompiler) Compile(id string) (Validator, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.c.Compile(id)
	if err != nil {
		return nil, err
	}
	return &santhoshValidator{v: v}, nil
}

func (s *santhoshCompiler) SupportedSchemaVersions() []Draft {
	return []Draft{
		Draft4,
		Draft6,
		Draft7,
		Draft2019_09,
		Draft2020_12,
	}
}

func (s *santhoshCompiler) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.c = newJSONSchemaCompiler(s.assertFormat)
}
