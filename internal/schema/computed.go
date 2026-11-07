package schema

import (
	"encoding/json"
	"os"

	"github.com/bitshepherds/json-schema-manager/v2/internal/config"
	"github.com/bitshepherds/json-schema-manager/v2/internal/validator"
)

// RenderInfo holds the rendered artefacts for a schema.
type RenderInfo struct {
	ID           ID                   // The canonical ID of the JSON schema
	Rendered     []byte               // The template-substituted document prior to unmarshalling
	Unmarshalled validator.JSONSchema // The UnmarshalJSON unmarshalled version of the rendered schema.
	Validator    validator.Validator  // The compiled validator which can be used to validate JSON documents.
}

// RenderCache is the type used to store rendered information for a schema in memory.
type RenderCache map[config.Env]RenderInfo

// IDCache is the type used to store canonical IDs for a schema in memory.
type IDCache map[config.Env]ID

// TestInfo holds the artefacts for a test document.
type TestInfo struct {
	Path         string                 // The path of the test document, from the registry's root
	SrcDoc       []byte                 // The source document
	Unmarshalled validator.JSONDocument // The UnmarshalJSON unmarshalled document
}

// NewTestInfo attempts to read in and parse the test JSON document at filePath. It names the document by
// its path from rootDir, the registry's root, so that whatever reports it reads the same on every machine.
func NewTestInfo(filePath, rootDir string) (TestInfo, error) {
	path := relativeTo(rootDir, filePath)
	//nolint:gosec // Path is constructed from internal registry logic
	data, err := os.ReadFile(filePath)
	if err != nil {
		return TestInfo{}, CannotReadTestDocumentError{Path: path}
	}

	var unmarshalled validator.JSONDocument
	if err = json.Unmarshal(data, &unmarshalled); err != nil {
		return TestInfo{}, InvalidTestDocumentError{Path: path}
	}

	return TestInfo{
		Path:         path,
		SrcDoc:       data,
		Unmarshalled: unmarshalled,
	}, nil
}

// TestCache is a cache of the suffix-free filenames of test documents for a schema version
// e.g. for /path/to/pass/mytest.json, an entry 'mytest' would be stored in the [TestTypePass] slice of the TestCache.
type TestCache map[TestDocType][]TestInfo

// Computed is a collection of information about a schema which is calculated after the schema has been loaded.
// Access to Computed fields must be protected by Schema.mu.
type Computed struct {
	filePath  string      // The path of the schema file on disk
	familyDir string      // The directory of the schema family
	homeDir   string      // The directory of the schema home
	key       Key         // The key to use for this schema in a cache.
	renders   RenderCache // Rendered versions of the schema in each environment
	ids       IDCache     // Canonical IDs for the schema in each environment
	tests     TestCache   // Test document information
}

// StoreRenderInfo stores a rendered schema for an environment.
// Caller must hold Schema.mu.
func (c *Computed) StoreRenderInfo(env config.Env, ri RenderInfo) {
	if c.renders == nil {
		c.renders = make(RenderCache)
	}
	c.renders[env] = ri
}

// RenderInfo returns the rendered schema for an environment.
// Caller must hold Schema.mu.
func (c *Computed) RenderInfo(env config.Env) RenderInfo {
	return c.renders[env]
}

// StoreID stores a canonical ID for an environment.
// Caller must hold Schema.mu.
func (c *Computed) StoreID(env config.Env, id ID) {
	if c.ids == nil {
		c.ids = make(IDCache)
	}
	c.ids[env] = id
}

// ID returns the canonical ID for an environment.
// Caller must hold Schema.mu.
func (c *Computed) ID(env config.Env) ID {
	return c.ids[env]
}

// StoreTests stores the given test documents in the cache.
// Note that the paths and .json suffixes are removed.
// Caller must hold Schema.mu.
func (c *Computed) StoreTests(tt TestDocType, tests []TestInfo) {
	if c.tests == nil {
		c.tests = make(TestCache, 2)
	}

	c.tests[tt] = tests
}

// Tests returns the absolute paths of the test documents for the given test type.
func (c *Computed) Tests(tt TestDocType) []TestInfo {
	if c.tests == nil || c.tests[tt] == nil {
		return nil
	}
	return c.tests[tt]
}
