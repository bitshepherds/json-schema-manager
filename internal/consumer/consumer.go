// Package consumer checks the schema versions a consuming service validates against, listed in
// its consumer manifest, against the registry: that each exists, that the later versions in its
// major family send nothing it rejects, and whether its family has a newer major version.
package consumer

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

// ManifestFile is the default name of a consumer manifest.
const ManifestFile = "jsm-consumer.yml"

// Manifest lists the schema versions a consuming service validates documents against.
type Manifest struct {
	Path    string   `yaml:"-"`
	Schemas []string `yaml:"schemas"` // each a schema key or canonical URL
}

// ManifestError is returned when a consumer manifest cannot be read.
type ManifestError struct {
	Path   string
	Reason string
}

func (e *ManifestError) Error() string {
	return fmt.Sprintf("consumer manifest %s: %s", e.Path, e.Reason)
}

// LoadManifest reads the consumer manifest at path.
func LoadManifest(path string) (*Manifest, error) {
	//nolint:gosec // the path is the manifest the user names
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &ManifestError{Path: path, Reason: "cannot be read: " + err.Error()}
	}
	m := &Manifest{}
	if err = yaml.Unmarshal(data, m); err != nil {
		return nil, &ManifestError{Path: path, Reason: "is not valid YAML: " + err.Error()}
	}
	if len(m.Schemas) == 0 {
		return nil, &ManifestError{Path: path, Reason: "lists no schemas"}
	}
	m.Path = path
	return m, nil
}

// Status is the outcome of checking one schema version a consumer validates against.
type Status string

const (
	// Pass means the version exists, and the later versions in its major family send nothing it rejects.
	Pass Status = "pass"
	// Warn means the version passes, but its family has a newer major version.
	Warn Status = "warn"
	// Fail means the version does not exist, or a later version sends a document it rejects.
	Fail Status = "fail"
)

// Rejection is a pass document of a later version which the consumer's version rejects.
type Rejection struct {
	Later schema.Key // the later version the document belongs to
	Path  string
	Err   error
}

// SchemaResult is the result of checking one entry of a consumer manifest.
type SchemaResult struct {
	Entry      string     // as written in the manifest
	Key        schema.Key // empty if the entry is not a schema key or canonical URL
	Problem    string     // why the entry could not be checked
	Rejections []Rejection
	NewerMajor schema.Key // the newest version of the family, if it has a newer major version
}

// Status returns the outcome of the check.
func (r *SchemaResult) Status() Status {
	switch {
	case r.Problem != "" || len(r.Rejections) > 0:
		return Fail
	case r.NewerMajor != "":
		return Warn
	default:
		return Pass
	}
}

// Result is the result of checking a consumer manifest.
type Result struct {
	Manifest string
	Schemas  []SchemaResult
}

// Count returns how many of the manifest's entries have the given status.
func (r *Result) Count(s Status) int {
	n := 0
	for i := range r.Schemas {
		if r.Schemas[i].Status() == s {
			n++
		}
	}
	return n
}

// Check checks each schema version the manifest lists against the registry.
func Check(ctx context.Context, r *schema.Registry, m *Manifest) (*Result, error) {
	res := &Result{Manifest: m.Path}
	for _, entry := range m.Schemas {
		sr, err := checkEntry(ctx, r, entry)
		if err != nil {
			return nil, err
		}
		res.Schemas = append(res.Schemas, sr)
	}
	return res, nil
}

func checkEntry(ctx context.Context, r *schema.Registry, entry string) (SchemaResult, error) {
	sr := SchemaResult{Entry: entry}
	var s *schema.Schema
	sr.Key, s, sr.Problem = pinnedSchema(r, entry)
	if sr.Problem != "" {
		return sr, nil
	}

	// Run the later versions' pass documents, in the same major family, against this version
	tester := schema.NewTester(r)
	tester.SetScope(schema.TestScopeConsumerBreaking)
	tester.SetSkipCompatible(true)
	tester.SetStopOnFirstError(false)
	report, err := tester.TestSingleSchema(ctx, sr.Key)
	if err != nil {
		return sr, err
	}
	for _, spec := range report.FailedTests[sr.Key] {
		sr.Rejections = append(sr.Rejections, Rejection{
			Later: withVersion(sr.Key, spec.ForwardVersion),
			Path:  spec.TestInfo.Path,
			Err:   spec.Err,
		})
	}

	newest, err := s.NewestInFamily()
	if err != nil {
		return sr, err
	}
	if newest.Major() > sr.Key.Major() {
		sr.NewerMajor = newest
	}
	return sr, nil
}

// pinnedSchema resolves a manifest entry to its key and schema, or returns why it cannot.
func pinnedSchema(r *schema.Registry, entry string) (key schema.Key, s *schema.Schema, problem string) {
	target, err := schema.NewTargetResolver(r, entry).Resolve()
	if err != nil || target.Key == nil {
		return "", nil, "not a schema key or canonical URL"
	}
	s, err = r.GetSchemaByKey(*target.Key)
	if err != nil {
		return *target.Key, nil, "not found in the registry"
	}
	return *target.Key, s, ""
}

// withVersion returns the key of the version v in the family of k.
func withVersion(k schema.Key, v *schema.SemVer) schema.Key {
	parts := strings.Split(string(k), schema.KeySeparatorString)
	lp := len(parts)
	parts[lp-3] = strconv.FormatUint(v.Major(), 10)
	parts[lp-2] = strconv.FormatUint(v.Minor(), 10)
	parts[lp-1] = strconv.FormatUint(v.Patch(), 10)
	return schema.Key(strings.Join(parts, schema.KeySeparatorString))
}
