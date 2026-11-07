package config

import (
	"fmt"

	"github.com/bitshepherds/json-schema-manager/v2/internal/validator"
)

// MustHaveExactlyOneProductionEnvironmentError is returned when the configuration doesn't have exactly one
// production environment.
type MustHaveExactlyOneProductionEnvironmentError struct{}

func (e *MustHaveExactlyOneProductionEnvironmentError) Error() string {
	return "json-schema-manager-config.yml must have exactly one environment marked with isProduction: true"
}

// MissingConfigError is returned when the configuration file is missing.
type MissingConfigError struct {
	Path string
}

func (e *MissingConfigError) Error() string {
	return fmt.Sprintf("json-schema-manager-config.yml missing in: %s", e.Path)
}

// InvalidYAMLError is returned when the configuration file contains invalid YAML.
type InvalidYAMLError struct {
	Wrapped error
}

func (e *InvalidYAMLError) Error() string {
	return fmt.Sprintf("json-schema-manager-config.yml is not a valid yaml document: %v", e.Wrapped)
}

// MissingPropertyError is returned when a required property is missing from the configuration.
type MissingPropertyError struct {
	Property string
}

func (e *MissingPropertyError) Error() string {
	return fmt.Sprintf("json-schema-manager-config.yml is missing required property: %s", e.Property)
}

// UnknownEnvironmentError is returned when an environment is requested that is not defined in the configuration.
type UnknownEnvironmentError struct {
	Env Env
}

func (e *UnknownEnvironmentError) Error() string {
	return fmt.Sprintf(
		"json-schema-manager-config.yml does not define environment '%s'",
		e.Env,
	)
}

// InvalidURLError is returned when a property in the configuration contains an invalid URL.
type InvalidURLError struct {
	Wrapped  error
	Property string
	Value    string
}

func (e *InvalidURLError) Error() string {
	return fmt.Sprintf(
		"json-schema-manager-config.yml property %s has invalid URL '%s': %v",
		e.Property,
		e.Value,
		e.Wrapped,
	)
}

// InvalidDefaultJSONSchemaVersionError is returned when the default JSON schema version in the configuration is
// invalid.
type InvalidDefaultJSONSchemaVersionError struct {
	Value     string
	Supported []validator.Draft
}

func (e *InvalidDefaultJSONSchemaVersionError) Error() string {
	return fmt.Sprintf(
		"json-schema-manager-config.yml property defaultJsonSchemaVersion has invalid value '%s'. "+
			"Supported versions are: %v",
		e.Value,
		e.Supported,
	)
}
