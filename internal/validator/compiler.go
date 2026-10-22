// Package validator provides interfaces and types for JSON Schema validation.
package validator

// Draft represents a JSON Schema draft version.
type Draft string

const (
	// Draft4 represents JSON Schema Draft 4.
	Draft4 Draft = "http://json-schema.org/draft-04/schema#"
	// Draft6 represents JSON Schema Draft 6.
	Draft6 Draft = "http://json-schema.org/draft-06/schema#"
	// Draft7 represents JSON Schema Draft 7.
	Draft7 Draft = "http://json-schema.org/draft-07/schema#"
	// Draft2019_09 represents JSON Schema Draft 2019-09.
	Draft2019_09 Draft = "https://json-schema.org/draft/2019-09/schema"
	// Draft2020_12 represents JSON Schema Draft 2020-12.
	Draft2020_12 Draft = "https://json-schema.org/draft/2020-12/schema"
)

// A JSONDocument is a valid parsed JSON Document - i.e. the result of json.Unmarshal().
type JSONDocument interface{}

// A JSONSchema is a valid parsed JSON Document representing a JSON Schema.
// Note that a Compiler must compile the JSONSchema before use which will identify any JSON Schema issues.
type JSONSchema JSONDocument

// Validator represents something which can be used to validate a JSON document.
type Validator interface {
	// Validate validates JSON document.
	Validate(v JSONDocument) error
}

// FormatAsserter is a Compiler which can treat the format keyword as an assertion.
type FormatAsserter interface {
	AssertFormat()
}

// Compiler defines a JSON Schema compiler. Because JSON schemas can, and often do reference
// other sub-schemas via the $ref property, a Compiler first must register all the
// JSON Schemas that it will need to compile.
type Compiler interface {
	// AddSchema registers a JSONSchema with the compiler.
	// Note that if a schema references another from JSM, then the referenced schema must be added first.
	// An error is produced if the JSONSchema cannot be added.
	AddSchema(id string, data JSONSchema) error

	// Compile creates a Validator from the JSONSchema previously added with the given ID.
	// An error is produced if the JSONSchema cannot be compiled.
	Compile(id string) (Validator, error)

	// SupportedSchemaVersions returns a slice of Draft representing the supported schema versions.
	SupportedSchemaVersions() []Draft

	// Clear resets the compiler state, removing all registered schemas.
	Clear()
}
