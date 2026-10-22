// Package config provides configuration management for JSM.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/bitshepherds/json-schema-manager/internal/validator"
)

// JsmRegistryConfigFile is the name of the JSM registry configuration file.
const JsmRegistryConfigFile = "json-schema-manager-config.yml"

// DefaultConfigContent is the default content for a new JSM registry configuration file.
const DefaultConfigContent = `# Public Schema Registry Configuration

# DEFAULT JSON SCHEMA VERSION
#
# There are a number of different versions of JSON Schema. More recent versions
# offer additional features and capabilities, but you should choose a version 
# that is supported by all of your organisation's systems. The default below is
# draft-07, which is supported by most systems, but JSON Schema Manager supports
# the following versions:
# - http://json-schema.org/draft-04/schema#
# - http://json-schema.org/draft-06/schema#
# - http://json-schema.org/draft-07/schema# (Default)
# - https://json-schema.org/draft/2019-09/schema
# - https://json-schema.org/draft/2020-12/schema
defaultJsonSchemaVersion: "http://json-schema.org/draft-07/schema#"

# FORMAT ASSERTION
#
# The format keyword, such as "format": "email", rejects invalid values under draft-07 and
# earlier, but is only an annotation under 2019-09 and 2020-12. Set assertFormat to true to make
# it reject invalid values under every version, so that moving a family to a newer version does
# not weaken its validation.
assertFormat: false

# ENVIRONMENT CONFIGURATION
#
# This section defines properties relating to the publication of schemas to an environment.
# Your source code schema files should set $id to "{{ ID }}". On building a distribution, 
# JSM will replace this with the canonical id for the schema in the target environment.
# (See jsm build-dist for more information)
#
# By default, schemas are rendered for your private configuration. To identify a schema
# as a public schema for consumption by third parties, set "x-public": true in the schema.
# This will cause JSM to use your publicUrlRoot instead of the privateUrlRoot.

environments:
  dev:
    privateUrlRoot: "https://dev.json-schemas.internal.example.com/"
    publicUrlRoot: "https://dev.json-schemas.example.com/"
    # You can also use a path for UrlRoots - e.g. "https://internal.example.com/schemas/"

    allowSchemaMutation: true # Permits developers to change schemas after publication: DO NOT DO THIS IN PRODUCTION!
  prod:
    privateUrlRoot: "https://json-schemas.internal.example.com/"
    publicUrlRoot: "https://json-schemas.example.com/"
    isProduction: true # This environment is the production environment.
`

// Env represents a JSM environment name.
type Env string

// EnvConfig contains configuration for a specific JSM environment.
type EnvConfig struct {
	PublicURLRoot       string `yaml:"publicUrlRoot"`
	PrivateURLRoot      string `yaml:"privateUrlRoot"`
	AllowSchemaMutation bool   `yaml:"allowSchemaMutation"`
	IsProduction        bool   `yaml:"isProduction"`
	Env                 Env    // this is set for convenience when the environments are read in.
}

// Config represents the root JSM configuration.
type Config struct {
	Environments             map[Env]*EnvConfig `yaml:"environments"`
	DefaultJSONSchemaVersion validator.Draft    `yaml:"defaultJsonSchemaVersion"`
	AssertFormat             bool               `yaml:"assertFormat"`
	ProductionEnv            Env                // this is set for convenience when the environments are read in.
}

// Validate validates an EnvConfig.
func (e *EnvConfig) Validate(pathPrefix string) error {
	if e.PublicURLRoot == "" {
		return &MissingPropertyError{Property: fmt.Sprintf("%s.publicUrlRoot", pathPrefix)}
	}
	if err := validateHTTPURL(fmt.Sprintf("%s.publicUrlRoot", pathPrefix), e.PublicURLRoot); err != nil {
		return err
	}

	if e.PrivateURLRoot == "" {
		return &MissingPropertyError{Property: fmt.Sprintf("%s.privateUrlRoot", pathPrefix)}
	}
	if err := validateHTTPURL(fmt.Sprintf("%s.privateUrlRoot", pathPrefix), e.PrivateURLRoot); err != nil {
		return err
	}
	return nil
}

// URLRoot returns the URL root for the environment.
func (e *EnvConfig) URLRoot(isPublic bool) string {
	if isPublic {
		return e.PublicURLRoot
	}
	return e.PrivateURLRoot
}

// New creates a new Config by reading from the registry root directory.
func New(registryRootDir string, compiler validator.Compiler) (*Config, error) {
	configPath := filepath.Join(registryRootDir, JsmRegistryConfigFile)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, &MissingConfigError{Path: registryRootDir}
	}

	//nolint:gosec // Path is constructed from internal registry logic
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	config, err := Parse(data)
	if err != nil {
		return nil, err
	}

	if vErr := config.Validate(compiler); vErr != nil {
		return nil, vErr
	}

	return config, nil
}

// Parse reads a Config from the content of a registry config file, without validating it.
func Parse(data []byte) (*Config, error) {
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		// return a wrapped error or the raw yaml error depending on preference.
		// For now, we wrap it to match previous behaviour partially, but strictly speaking
		// the yaml library returns nice errors on its own.
		return nil, &InvalidYAMLError{Wrapped: err}
	}

	for envName, ec := range config.Environments {
		if ec != nil {
			ec.Env = envName
		}
	}

	return &config, nil
}

// ProductionEnvConfig returns the production environment configuration.
func (c *Config) ProductionEnvConfig() *EnvConfig {
	return c.Environments[c.ProductionEnv]
}

// EnvConfig returns the configuration for the given environment.
func (c *Config) EnvConfig(env Env) (*EnvConfig, error) {
	ec, ok := c.Environments[env]
	if !ok {
		return nil, &UnknownEnvironmentError{Env: env}
	}
	return ec, nil
}

// Validate validates the Config.
func (c *Config) Validate(compiler validator.Compiler) error {
	if c.DefaultJSONSchemaVersion == "" {
		c.DefaultJSONSchemaVersion = validator.Draft7
	}

	supported := compiler.SupportedSchemaVersions()
	if !slices.Contains(supported, c.DefaultJSONSchemaVersion) {
		return &InvalidDefaultJSONSchemaVersionError{
			Value:     string(c.DefaultJSONSchemaVersion),
			Supported: supported,
		}
	}

	if len(c.Environments) == 0 {
		return &MissingPropertyError{Property: "environments"}
	}

	prodCount := 0
	for envName, envCfg := range c.Environments {
		if err := envCfg.Validate(fmt.Sprintf("environments.%s", envName)); err != nil {
			return err
		}
		if envCfg.IsProduction {
			c.ProductionEnv = envName
			prodCount++
		}
	}

	if prodCount != 1 {
		return &MustHaveExactlyOneProductionEnvironmentError{}
	}

	return nil
}

func validateHTTPURL(prop, val string) error {
	u, pErr := url.Parse(val)
	if pErr != nil {
		return &InvalidURLError{Property: prop, Value: val, Wrapped: pErr}
	}
	if u.Scheme != "https" {
		return &InvalidURLError{
			Property: prop,
			Value:    val,
			Wrapped:  fmt.Errorf("scheme must be https"),
		}
	}
	return nil
}
