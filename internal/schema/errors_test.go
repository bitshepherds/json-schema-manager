package schema

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		contains []string
	}{
		{
			name:     "NoDomainError",
			err:      &NoDomainError{},
			contains: []string{"At least one domain is required"},
		},
		{
			name:     "InvalidDomainError",
			err:      &InvalidDomainError{d: []string{"INVALID"}},
			contains: []string{"domain [INVALID] must contain only"},
		},
		{
			name:     "InvalidFamilyNameError",
			err:      &InvalidFamilyNameError{fn: "INVALID"},
			contains: []string{"family name INVALID must contain only"},
		},
		{
			name:     "InvalidSchemaKeyCharactersError",
			err:      &InvalidSchemaKeyCharactersError{Key: "invalid_key"},
			contains: []string{"invalid_key"},
		},
		{
			name:     "InvalidMajorVersionError",
			err:      &InvalidMajorVersionError{v: "v1"},
			contains: []string{"v1 is not a valid major version"},
		},
		{
			name:     "InvalidMinorVersionError",
			err:      &InvalidMinorVersionError{v: "v1"},
			contains: []string{"v1 is not a valid minor version"},
		},
		{
			name:     "InvalidPatchVersionError",
			err:      &InvalidPatchVersionError{v: "v1"},
			contains: []string{"v1 is not a valid patch version"},
		},

		{
			name:     "InvalidKeyStringError",
			err:      &InvalidKeyStringError{ks: "invalid"},
			contains: []string{"invalid is not a valid schema key"},
		},
		{
			name: "LocationOutsideRootDirectoryError",
			err: &LocationOutsideRootDirectoryError{
				Location:      "/outside",
				RootDirectory: "/root",
			},
			contains: []string{"/outside", "/root"},
		},
		{
			name:     "NotASchemaFileError",
			err:      &NotASchemaFileError{Path: "/path/to/file"},
			contains: []string{"/path/to/file"},
		},
		{
			name:     "AlreadyExistsError",
			err:      &AlreadyExistsError{K: "domain_family_1_0_0"},
			contains: []string{"domain_family_1_0_0"},
		},
		{
			name:     "InvalidJSONError",
			err:      InvalidJSONError{Path: "/path/to/invalid.json", Wrapped: errors.New("json error")},
			contains: []string{"/path/to/invalid.json", "json error"},
		},
		{
			name:     "InvalidJSONSchemaError",
			err:      InvalidJSONSchemaError{Path: "/path/to/schema.json", Wrapped: errors.New("schema error")},
			contains: []string{"/path/to/schema.json", "schema error"},
		},
		{
			name:     "CannotReadXPublicError",
			err:      CannotReadXPublicError{Path: "/path/to/schema.json"},
			contains: []string{"/path/to/schema.json"},
		},
		{
			name: "TemplateFormatInvalidError",
			err: TemplateFormatInvalidError{
				Path:    "/path/to/template.json",
				Wrapped: errors.New("template error"),
			},
			contains: []string{"/path/to/template.json", "template error"},
		},
		{
			name: "TemplateExecutionFailedError",
			err: TemplateExecutionFailedError{
				Path:    "/path/to/template.json",
				Wrapped: errors.New("execution error"),
			},
			contains: []string{"/path/to/template.json", "execution error"},
		},
		{
			name:     "RegistryInitError",
			err:      &RegistryInitError{Path: "/nonexistent", Err: errors.New("test error")},
			contains: []string{"/nonexistent", "test error", "Registry could not be initialised"},
		},
		{
			name:     "RegistryRootNotFolderError",
			err:      &RegistryRootNotFolderError{Path: "/not_a_folder"},
			contains: []string{"/not_a_folder"},
		},
		{
			name:     "JSMArgInvalidKeyError",
			err:      &JSMArgInvalidKeyError{Arg: "invalid_arg"},
			contains: []string{"invalid_arg"},
		},
		{
			name: "JSMArgNotFoundError",
			err: &JSMArgNotFoundError{
				Key:     "domain_family_1_0_0",
				Wrapped: errors.New("schema not found"),
			},
			contains: []string{"domain_family_1_0_0", "schema not found"},
		},
		{
			name:     "NotFoundError",
			err:      &NotFoundError{Path: "/missing"},
			contains: []string{"schema not found: /missing"},
		},
		{
			name:     "InvalidSearchScopeError",
			err:      &InvalidSearchScopeError{spec: "invalid"},
			contains: []string{"`invalid` is not a valid JSM Search Scope"},
		},
		{
			name:     "InvalidSchemaFilenameError",
			err:      &InvalidSchemaFilenameError{Path: "/invalid.json"},
			contains: []string{"schema file /invalid.json has an invalid filename structure"},
		},

		{
			name: "FailTestPassedError",
			err: FailTestPassedError{
				SchemaPath:  "/schema.json",
				TestDocPath: "/test.json",
			},
			contains: []string{
				"Fail Test document /test.json was accepted by schema /schema.json, but was expected to fail validation",
			},
		},
		{
			name: "PassTestFailedError",
			err: PassTestFailedError{
				SchemaPath:  "/schema.json",
				TestDocPath: "/test.json",
				Wrapped:     errors.New("validation error"),
			},
			contains: []string{"Pass Test document /test.json did not pass validation as expected", "validation error"},
		},
		{
			name:     "CannotReadTestDocumentError",
			err:      CannotReadTestDocumentError{Path: "/test.json"},
			contains: []string{"test document /test.json could not be read"},
		},
		{
			name:     "InvalidTestDocumentError",
			err:      InvalidTestDocumentError{Path: "/test.json"},
			contains: []string{"test document /test.json is not valid JSON"},
		},
		{
			name: "TestDirMissingConfigError",
			err: &TestDirMissingConfigError{
				Path: "/path/to/pass",
				Type: TestDocTypePass,
			},
			contains: []string{"pass directory missing: /path/to/pass"},
		},
		{
			name:     "InvalidTargetArgumentError",
			err:      &InvalidTargetArgumentError{Arg: "invalid"},
			contains: []string{"Invalid schema target: invalid"},
		},
		{
			name:     "TargetArgumentTargetsMultipleSchemasError",
			err:      &TargetArgumentTargetsMultipleSchemasError{Arg: "multiple"},
			contains: []string{"Schema target multiple targets multiple schemas"},
		},
		{
			name:     "InvalidReleaseTypeError",
			err:      &InvalidReleaseTypeError{Value: "invalid"},
			contains: []string{"Invalid release type: invalid"},
		},
		{
			name:     "NoTargetArgumentError",
			err:      &NoTargetArgumentError{},
			contains: []string{"No target has been provided"},
		},
		{
			name:     "ChangedDeployedSchemasError",
			err:      &ChangedDeployedSchemasError{Paths: []string{"f1.json"}},
			contains: []string{"cannot modify deployed schemas", "f1.json"},
		},
		{
			name:     "NoSchemaTargetsError",
			err:      &NoSchemaTargetsError{},
			contains: []string{"must specify a schema to validate"},
		},
		{
			name:     "InvalidTestDocumentDirectoryError",
			err:      &InvalidTestDocumentDirectoryError{Path: "/invalid/test.json"},
			contains: []string{"test document must be in a 'pass' or 'fail' directory"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			msg := tt.err.Error()
			for _, c := range tt.contains {
				assert.Contains(t, msg, c)
			}
		})
	}
}
