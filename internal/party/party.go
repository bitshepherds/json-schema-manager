// Package party checks the schema versions a party to a family's documents uses, listed in its party
// manifest, against the registry: that each exists, that no later version in its major harms a party
// in its role, as the family's declaration says, and whether its family has a newer major version.
package party

import (
	"context"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

// ManifestFile is the default name of a party manifest.
const ManifestFile = "jsm-party.yml"

// Role says which side of a family's documents a party is on.
type Role string

// The roles a party can state.
const (
	RoleReader Role = "reader"
	RoleWriter Role = "writer"
)

// rule returns the rule whose break harms a party in this role, while it stays on a version: a reader
// is harmed by a later version accepting what it rejects, and a writer by one rejecting what it writes.
func (role Role) rule() schema.Rule {
	if role == RoleReader {
		return schema.RuleNoLoosening
	}
	return schema.RuleNoTightening
}

// Manifest lists the schema versions a party uses, and states its role for them.
type Manifest struct {
	Path    string   `yaml:"-"`
	Role    Role     `yaml:"role"`
	Schemas []string `yaml:"schemas"` // each a schema key or canonical URL
}

// ManifestError is returned when a party manifest cannot be read.
type ManifestError struct {
	Path   string
	Reason string
}

func (e *ManifestError) Error() string {
	return fmt.Sprintf("party manifest %s: %s", e.Path, e.Reason)
}

// LoadManifest reads the party manifest at path.
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
	switch m.Role {
	case RoleReader, RoleWriter:
	case "":
		return nil, &ManifestError{Path: path, Reason: "must state its role, reader or writer"}
	default:
		return nil, &ManifestError{Path: path, Reason: fmt.Sprintf("role %q must be reader or writer", m.Role)}
	}
	if len(m.Schemas) == 0 {
		return nil, &ManifestError{Path: path, Reason: "lists no schemas"}
	}
	m.Path = path
	return m, nil
}

// Status is the outcome of checking one schema version a party uses.
type Status string

const (
	// Pass means the version exists, and no document run shows a later version in its major harming the party.
	Pass Status = "pass"
	// Notice means the version exists, and nothing ran, since no later version can harm the party.
	Notice Status = "notice"
	// Warn means the version passes or has a notice, but its family has a newer major version, or it is
	// below its family's oldestLive, so nothing checks it any more.
	Warn Status = "warn"
	// Fail means the version does not exist, or a document run shows a later version harming the party.
	Fail Status = "fail"
)

// SchemaResult is the result of checking one entry of a party manifest.
type SchemaResult struct {
	Entry      string        // as written in the manifest
	Key        schema.Key    // empty if the entry is not a schema key or canonical URL
	Problem    string        // why the entry could not be checked
	Notice     string        // why no later version can harm the party, if none can
	Retired    string        // why nothing checks the version, if it is below its family's oldestLive
	Runs       []schema.Spec // the document runs with later versions, each with its outcome
	NewerMajor schema.Key    // the newest version of the family, if it has a newer major version
}

// Failures returns the runs which show a later version harming the party, and no exception names.
func (r *SchemaResult) Failures() []schema.Spec {
	return r.runsWhere(func(spec *schema.Spec) bool { return spec.Err != nil && spec.Exception == nil })
}

// Excepted returns the runs which show a later version harming the party, and an exception names.
func (r *SchemaResult) Excepted() []schema.Spec {
	return r.runsWhere(func(spec *schema.Spec) bool { return spec.Err != nil && spec.Exception != nil })
}

func (r *SchemaResult) runsWhere(keep func(*schema.Spec) bool) []schema.Spec {
	var specs []schema.Spec
	for i := range r.Runs {
		if keep(&r.Runs[i]) {
			specs = append(specs, r.Runs[i])
		}
	}
	return specs
}

// Status returns the outcome of the check.
func (r *SchemaResult) Status() Status {
	switch {
	case r.Problem != "" || len(r.Failures()) > 0:
		return Fail
	case r.NewerMajor != "" || r.Retired != "":
		return Warn
	case r.Notice != "":
		return Notice
	default:
		return Pass
	}
}

// Result is the result of checking a party manifest.
type Result struct {
	Manifest string
	Role     Role
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
	res := &Result{Manifest: m.Path, Role: m.Role}
	for _, entry := range m.Schemas {
		sr, err := checkEntry(ctx, r, m.Role, entry)
		if err != nil {
			return nil, err
		}
		res.Schemas = append(res.Schemas, sr)
	}
	return res, nil
}

func checkEntry(ctx context.Context, r *schema.Registry, role Role, entry string) (SchemaResult, error) {
	sr := SchemaResult{Entry: entry}
	var s *schema.Schema
	sr.Key, s, sr.Problem = listedSchema(r, entry)
	if sr.Problem != "" {
		return sr, nil
	}
	decl, err := r.FamilyDeclaration(sr.Key)
	if err != nil {
		return sr, err
	}

	switch {
	case !decl.Live(sr.Key.Version()):
		// The rule's pairs leave this version out, so a later version could harm the party unseen.
		sr.Retired = fmt.Sprintf("%s is below this family's oldestLive %s, so no check holds later versions to it, "+
			"and a later version can harm a %s still on it: move to a version the family still checks.",
			sr.Key, decl.OldestLive.String('.'), role)
	case decl.Facts.EffectiveAhead().Holds(role.rule()):
		sr.Runs, err = schema.NewTester(r).RunsOnLaterVersions(ctx, sr.Key, role.rule())
		if err != nil {
			return sr, err
		}
	default:
		sr.Notice = whyNoHarm(decl, role, sr.Key)
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

// whyNoHarm says why no later version can harm a party in the role, in a family which doesn't hold its
// versions to the rule that protects that role.
func whyNoHarm(decl *schema.FamilyDeclaration, role Role, k schema.Key) string {
	var why string
	switch {
	case decl.Facts.ReaderPicksSchema:
		why = "a reader validates each document against the version the document names"
	case decl.Facts.Ahead == schema.AheadNeither:
		why = "both sides move together"
	case role == RoleReader:
		why = "readers move to a new version before writers do"
	default:
		why = "writers move to a new version before readers do"
	}
	return fmt.Sprintf("This family is declared %s: %s, so no later version can harm a %s on %s.",
		decl.Scenario, why, role, k)
}

// listedSchema resolves a manifest entry to its key and schema, or returns why it cannot.
func listedSchema(r *schema.Registry, entry string) (key schema.Key, s *schema.Schema, problem string) {
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
