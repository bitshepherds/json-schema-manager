// Package schema provides types and functions for managing JSON schemas.
package schema

import (
	"fmt"
)

// NoDomainError is returned when a schema does not have a domain.
type NoDomainError struct{}

func (e *NoDomainError) Error() string {
	return "At least one domain is required"
}

// InvalidDomainError is returned when a domain contains invalid characters.
type InvalidDomainError struct {
	d []string
}

func (e *InvalidDomainError) Error() string {
	return fmt.Sprintf("domain %s must contain only [a-z], [0-9], and '-'", e.d)
}

// InvalidFamilyNameError is returned when a family name contains invalid characters.
type InvalidFamilyNameError struct {
	fn string
}

func (e *InvalidFamilyNameError) Error() string {
	return fmt.Sprintf("family name %s must contain only [a-z], [0-9], and '-'", e.fn)
}

// InvalidSchemaKeyCharactersError is returned when a schema key contains invalid characters.
type InvalidSchemaKeyCharactersError struct {
	Key Key
}

func (e *InvalidSchemaKeyCharactersError) Error() string {
	return fmt.Sprintf("%s contains invalid characters - must contain only [a-z], [0-9],'-', and '_'", e.Key)
}

// InvalidMajorVersionError is returned when a major version is not a valid number.
type InvalidMajorVersionError struct {
	v string
}

func (e *InvalidMajorVersionError) Error() string {
	return fmt.Sprintf("%s is not a valid major version", e.v)
}

// InvalidMinorVersionError is returned when a minor version is not a valid number.
type InvalidMinorVersionError struct {
	v string
}

func (e *InvalidMinorVersionError) Error() string {
	return fmt.Sprintf("%s is not a valid minor version", e.v)
}

// InvalidPatchVersionError is returned when a patch version is not a valid number.
type InvalidPatchVersionError struct {
	v string
}

func (e *InvalidPatchVersionError) Error() string {
	return fmt.Sprintf("%s is not a valid patch version", e.v)
}

// InvalidKeyStringError is returned when a schema key string is not in the correct format.
type InvalidKeyStringError struct {
	ks string
}

func (e *InvalidKeyStringError) Error() string {
	return fmt.Sprintf("%s is not a valid schema key. Example: 'domain_subdomain_family_1_0_0'", e.ks)
}

// LocationOutsideRootDirectoryError is returned when a location is outside the root directory.
type LocationOutsideRootDirectoryError struct {
	Location      string
	RootDirectory string
}

func (e *LocationOutsideRootDirectoryError) Error() string {
	return fmt.Sprintf("location %s is outside root directory %s", e.Location, e.RootDirectory)
}

// NotASchemaFileError is returned when a file is not a JSON schema file.
type NotASchemaFileError struct {
	Path string
}

func (e *NotASchemaFileError) Error() string {
	return fmt.Sprintf("%s is not a JSON schema file", e.Path)
}

// AlreadyExistsError is returned when a schema already exists.
type AlreadyExistsError struct {
	K Key
}

func (e *AlreadyExistsError) Error() string {
	return fmt.Sprintf("schema %s already exists", e.K)
}

// InvalidJSONError is returned when a file is not valid JSON.
type InvalidJSONError struct {
	Path    string
	Wrapped error
}

func (e InvalidJSONError) Error() string {
	return fmt.Sprintf("%s is not valid JSON: %s", e.Path, e.Wrapped)
}

// InvalidJSONSchemaError is returned when a file is not a valid JSON schema.
type InvalidJSONSchemaError struct {
	Path    string
	Wrapped error
}

func (e InvalidJSONSchemaError) Error() string {
	return fmt.Sprintf("%s is not a valid JSON Schema: %s", e.Path, e.Wrapped)
}

// CannotReadXPublicError is returned when the x-public property cannot be read.
type CannotReadXPublicError struct {
	Path string
}

func (e CannotReadXPublicError) Error() string {
	return fmt.Sprintf("%s contains invalid x-public property - if present, it must be a boolean", e.Path)
}

// TemplateFormatInvalidError is returned when a schema template has a syntax error.
type TemplateFormatInvalidError struct {
	Path    string
	Wrapped error
}

func (e TemplateFormatInvalidError) Error() string {
	return fmt.Sprintf("%s has a template syntax error. Check {{ID }} and {{JSM `<schema key>`}} for "+
		"validity. Error: %v", e.Path, e.Wrapped)
}

// TemplateExecutionFailedError is returned when a schema template cannot be rendered.
type TemplateExecutionFailedError struct {
	Path    string
	Wrapped error
}

func (e TemplateExecutionFailedError) Error() string {
	return fmt.Sprintf("%s cannot be rendered. Check {{ID }} and {{JSM `<schema key>`}} for validity. "+
		"Error: %v", e.Path, e.Wrapped)
}

// RegistryInitError is returned when the registry cannot be initialised.
type RegistryInitError struct {
	Path string
	Err  error
}

func (e *RegistryInitError) Error() string {
	return fmt.Sprintf("Registry could not be initialised. Path: %s, Error: %s", e.Path, e.Err)
}

// RegistryRootNotFolderError is returned when the registry root is not a directory.
type RegistryRootNotFolderError struct {
	Path string
}

func (e *RegistryRootNotFolderError) Error() string {
	return fmt.Sprintf("Registry could not be initialised. Path: %s is not a directory", e.Path)
}

// JSMArgInvalidKeyError is returned when a JSM template argument has an invalid key.
type JSMArgInvalidKeyError struct {
	Arg string
}

func (e *JSMArgInvalidKeyError) Error() string {
	return fmt.Sprintf("A $ref to a JSM schema ({{ JSM `<schema key>` }}) has an invalid <schema key>: %s", e.Arg)
}

// JSMArgNotFoundError is returned when a JSM template argument cannot be resolved.
type JSMArgNotFoundError struct {
	Key     Key
	Wrapped error
}

func (e *JSMArgNotFoundError) Error() string {
	return fmt.Sprintf("A $ref to a JSM schema ({{ JSM `%s` }}) could not be loaded. Error: %s", e.Key, e.Wrapped)
}

// NotFoundError is returned when a schema is not found.
type NotFoundError struct {
	Path string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("schema not found: %s", e.Path)
}

// InvalidSearchScopeError is returned when a search scope is not in the correct format.
type InvalidSearchScopeError struct {
	spec string
}

func (e *InvalidSearchScopeError) Error() string {
	return fmt.Sprintf("`%s` is not a valid JSM Search Scope. Valid Example: 'domain/family'", e.spec)
}

// InvalidSchemaFilenameError is returned when a schema file has an invalid filename structure.
type InvalidSchemaFilenameError struct {
	Path string
}

func (e *InvalidSchemaFilenameError) Error() string {
	return fmt.Sprintf("schema file %s has an invalid filename structure", e.Path)
}

// FailTestPassedError is returned when a fail test document unexpectedly passes validation.
type FailTestPassedError struct {
	SchemaPath  string
	TestDocPath string
}

func (e FailTestPassedError) Error() string {
	return fmt.Sprintf("Fail Test document %s did not fail validation as expected for schema %s:",
		e.TestDocPath, e.SchemaPath)
}

// PassTestFailedError is returned when a pass test document unexpectedly fails validation.
type PassTestFailedError struct {
	SchemaPath  string
	TestDocPath string
	Wrapped     error
}

func (e PassTestFailedError) Error() string {
	return fmt.Sprintf("Pass Test document %s did not pass validation as expected for schema %s, error: %s",
		e.TestDocPath, e.SchemaPath, e.Wrapped)
}

// CannotReadTestDocumentError is returned when a test document cannot be read.
type CannotReadTestDocumentError struct {
	Path string
}

func (e CannotReadTestDocumentError) Error() string {
	return fmt.Sprintf("test document %s could not be read", e.Path)
}

// InvalidTestDocumentError is returned when a test document is not valid JSON.
type InvalidTestDocumentError struct {
	Path string
}

func (e InvalidTestDocumentError) Error() string {
	return fmt.Sprintf("test document %s is not valid JSON", e.Path)
}

// TestDirMissingConfigError is returned when a test directory is missing.
type TestDirMissingConfigError struct {
	Path string
	Type TestDocType
}

func (e *TestDirMissingConfigError) Error() string {
	return fmt.Sprintf("%s directory missing: %s", e.Type, e.Path)
}

// InvalidTestDocumentDirectoryError is returned when a test document is not in a 'pass' or 'fail' directory.
type InvalidTestDocumentDirectoryError struct {
	Path string
}

func (e *InvalidTestDocumentDirectoryError) Error() string {
	return fmt.Sprintf("test document must be in a 'pass' or 'fail' directory: %s", e.Path)
}

// NoSchemaTargetsError is returned when no schema targets are specified for validation.
type NoSchemaTargetsError struct{}

func (e *NoSchemaTargetsError) Error() string {
	return "must specify a schema to validate via positional argument or flags (--key, --id, --search-scope)"
}

// InvalidCreateSchemaArgError is returned when a create schema argument is not in the correct format.
type InvalidCreateSchemaArgError struct {
	Arg string
}

func (e *InvalidCreateSchemaArgError) Error() string {
	return fmt.Sprintf("Invalid create schema argument: %s - "+
		"you must provide a chain of one or more domains and a family name, "+
		"separated by '/' e.g. 'domain/subdomain/family'", e.Arg)
}

// InvalidTestScopeError is returned when a test scope is not in the correct format.
type InvalidTestScopeError struct {
	Scope string
}

func (e *InvalidTestScopeError) Error() string {
	return fmt.Sprintf("Invalid test scope: %s - valid scopes are: %s, %s, %s, %s, %s",
		e.Scope, TestScopeLocal, TestScopePass, TestScopeFail, TestScopeConsumerBreaking, TestScopeAll)
}

// InvalidTargetArgumentError is returned when a target argument is not in the correct format.
type InvalidTargetArgumentError struct {
	Arg string
}

func (e *InvalidTargetArgumentError) Error() string {
	return fmt.Sprintf("Invalid schema target: %s", e.Arg)
}

// TargetArgumentTargetsMultipleSchemasError is returned when a target argument targets multiple schemas.
type TargetArgumentTargetsMultipleSchemasError struct {
	Arg string
}

func (e *TargetArgumentTargetsMultipleSchemasError) Error() string {
	return fmt.Sprintf("Schema target %s targets multiple schemas", e.Arg)
}

// InvalidReleaseTypeError is returned when a release type is not in the correct format.
type InvalidReleaseTypeError struct {
	Value string
}

func (e *InvalidReleaseTypeError) Error() string {
	return fmt.Sprintf("Invalid release type: %s - valid release types are: %s, %s, %s",
		e.Value, ReleaseTypeMajor, ReleaseTypeMinor, ReleaseTypePatch)
}

// NoTargetArgumentError is returned when no target argument is provided.
type NoTargetArgumentError struct{}

func (e *NoTargetArgumentError) Error() string {
	return "No target has been provided"
}

// ChangedDeployedSchemasError is returned when deployed schemas are modified.
type ChangedDeployedSchemasError struct {
	Paths []string
}

func (e *ChangedDeployedSchemasError) Error() string {
	return fmt.Sprintf("cannot modify deployed schemas: %v", e.Paths)
}

// CollapsedSchemaError is returned when a collapsed schema does not compile, or does not give the
// same result as the original schema for one of its test documents.
type CollapsedSchemaError struct {
	Key     Key
	Wrapped error
}

func (e *CollapsedSchemaError) Error() string {
	return fmt.Sprintf("the collapsed form of %s is not equivalent to the original: %s", e.Key, e.Wrapped)
}

func (e *CollapsedSchemaError) Unwrap() error {
	return e.Wrapped
}
