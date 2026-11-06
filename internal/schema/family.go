package schema

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// FamilyDeclarationFile is the name of the file, in a family's directory, which declares how the family is used.
const FamilyDeclarationFile = "family.yml"

// ScenariosDoc is where the scenarios, and the facts each one implies, are described.
const ScenariosDoc = "user-docs/scenarios.md"

// Scenario names one of the common ways a schema family is used.
type Scenario string

// Ahead says who can be on a newer version than the other side.
type Ahead string

// DocumentsLive says how long documents outlive their writer.
type DocumentsLive string

// Role says whether the schema sets the rules, or records a format someone else owns.
type Role string

// The scenarios a family can declare.
const (
	ScenarioProviderFirst    Scenario = "provider-first"
	ScenarioConsumerFirst    Scenario = "consumer-first"
	ScenarioConfigFile       Scenario = "config-file"
	ScenarioStoredOneSchema  Scenario = "stored-one-schema"
	ScenarioStoredSelfNaming Scenario = "stored-self-naming"
	ScenarioRetainedStream   Scenario = "retained-stream"
	ScenarioUncoordinated    Scenario = "uncoordinated"
	ScenarioLockStep         Scenario = "lock-step"
	ScenarioThirdPartyFormat Scenario = "third-party-format"
)

// The values of a family's facts.
const (
	AheadWriter  Ahead = "writer"
	AheadReader  Ahead = "reader"
	AheadEither  Ahead = "either"
	AheadNeither Ahead = "neither"

	DocumentsLiveMoment       DocumentsLive = "moment"
	DocumentsLiveRetention    DocumentsLive = "retention"
	DocumentsLiveIndefinitely DocumentsLive = "indefinitely"

	SchemaPrescribes Role = "prescribes"
	SchemaDescribes  Role = "describes"
)

// FamilyFacts are the four facts which decide what can break a family.
type FamilyFacts struct {
	Ahead             Ahead
	DocumentsLive     DocumentsLive // empty only for a scenario where it varies, until the family states it
	ReaderPicksSchema bool
	Schema            Role
}

// EffectiveAhead returns who the rule treats as able to be ahead. A reader that picks its schema from
// the document only ever validates a document against the document's own version, so no two versions
// meet, whoever is ahead.
func (f FamilyFacts) EffectiveAhead() Ahead {
	if f.ReaderPicksSchema {
		return AheadNeither
	}
	return f.Ahead
}

// InheritsDocuments reports whether a version counts the documents of earlier versions in its major as
// well as its own, because the family's rule runs earlier documents across versions. Every family but
// one with neither side ahead does: writer-ahead runs earlier fail documents on later versions,
// reader-ahead runs earlier pass documents, and either runs both.
func (f FamilyFacts) InheritsDocuments() bool {
	return f.EffectiveAhead() != AheadNeither
}

// NeedsMigrationStatement reports whether a new major must say how old documents move to it: they stay
// around for good, and each is read through whatever version the reader is on, not one it names itself.
func (f FamilyFacts) NeedsMigrationStatement() bool {
	return f.DocumentsLive == DocumentsLiveIndefinitely && !f.ReaderPicksSchema
}

// ScenarioInfo describes one scenario: its name, what it is called in the docs, and the facts it implies.
type ScenarioInfo struct {
	Name  Scenario
	Title string
	Facts FamilyFacts
}

// scenarios lists the scenarios in the docs' order. The facts are the table in ScenariosDoc.
var scenarios = []ScenarioInfo{
	{
		ScenarioProviderFirst, "Provider moves first: events from a service, API responses",
		FamilyFacts{AheadWriter, DocumentsLiveMoment, false, SchemaPrescribes},
	},
	{
		ScenarioConsumerFirst, "Consumer moves first: API request bodies, partners submitting to one service",
		FamilyFacts{AheadReader, DocumentsLiveMoment, false, SchemaPrescribes},
	},
	{
		ScenarioConfigFile, "Config files: a tool's settings file",
		FamilyFacts{AheadReader, DocumentsLiveIndefinitely, false, SchemaPrescribes},
	},
	{
		ScenarioStoredOneSchema, "Stored data, one schema for all: a database of mixed versions, validated on read",
		FamilyFacts{AheadReader, DocumentsLiveIndefinitely, false, SchemaPrescribes},
	},
	{
		ScenarioStoredSelfNaming, "Stored data, each document names its version: documents carrying a version tag",
		FamilyFacts{AheadNeither, DocumentsLiveIndefinitely, true, SchemaPrescribes},
	},
	{
		ScenarioRetainedStream, "Retained streams: replayable event logs, dead-letter queues",
		FamilyFacts{AheadEither, DocumentsLiveRetention, false, SchemaPrescribes},
	},
	{
		ScenarioUncoordinated, "Uncoordinated parties: several providers on a bus, syncing mobile clients",
		FamilyFacts{AheadEither, "", false, SchemaPrescribes},
	},
	{
		ScenarioLockStep, "Lock-step: both sides released together",
		FamilyFacts{AheadNeither, DocumentsLiveMoment, false, SchemaPrescribes},
	},
	{
		ScenarioThirdPartyFormat, "A format someone else owns: a third party's API response or file format",
		FamilyFacts{AheadReader, "", false, SchemaDescribes},
	},
}

// Scenarios returns the scenarios a family can declare, in the docs' order.
func Scenarios() []ScenarioInfo {
	return slices.Clone(scenarios)
}

// DocumentsLiveVaries reports whether the scenario leaves it to the family to state how long its documents live.
func (s Scenario) DocumentsLiveVaries() bool {
	info, ok := scenarioInfo(s)
	return ok && info.Facts.DocumentsLive == ""
}

func scenarioInfo(s Scenario) (ScenarioInfo, bool) {
	i := slices.IndexFunc(scenarios, func(info ScenarioInfo) bool { return info.Name == s })
	if i < 0 {
		return ScenarioInfo{}, false
	}
	return scenarios[i], true
}

// ScenarioNames returns the names a family can declare, joined for a message.
func ScenarioNames() string {
	names := make([]string, len(scenarios))
	for i, info := range scenarios {
		names[i] = string(info.Name)
	}
	return strings.Join(names, ", ")
}

// FamilyDeclaration is a family's parsed family.yml: its scenario, and the facts that gives it once
// any overrides are applied.
type FamilyDeclaration struct {
	Scenario   Scenario
	Facts      FamilyFacts
	OldestLive SemVer            // the zero SemVer unless the family's documents live for a retention period
	Migrations map[uint64]string // for a major, a sentence or link saying how old documents move to it
}

// Live reports whether the version takes part in the family's checks: documents written under it may
// still exist. Every version does, unless the family's documents live for a retention period.
func (d *FamilyDeclaration) Live(v SemVer) bool {
	return slices.Compare(v[:], d.OldestLive[:]) >= 0
}

// MigrationStatementHelp says what a family must add to its declaration for a new major.
func MigrationStatementHelp(major uint64) string {
	return fmt.Sprintf("Add a sentence or link to %s saying how old documents move to it, with "+
		"\"migrations:\" and under it \"%d: <how they move>\". A migration is your own code: JSM only checks "+
		"the statement is there", FamilyDeclarationFile, major)
}

// familyFile is the content of a family.yml file. An absent optional fact is left empty.
type familyFile struct {
	Scenario          Scenario      `yaml:"scenario"`
	Ahead             Ahead         `yaml:"ahead"`
	DocumentsLive     DocumentsLive `yaml:"documentsLive"`
	ReaderPicksSchema *bool         `yaml:"readerPicksSchema"`
	Schema            Role          `yaml:"schema"`
	OldestLive        string        `yaml:"oldestLive"`

	Migrations map[uint64]string `yaml:"migrations"`
}

// oneOf returns a problem if value is set, and is not among allowed.
func oneOf[T ~string](field string, value T, allowed ...T) error {
	if value == "" || slices.Contains(allowed, value) {
		return nil
	}
	names := make([]string, len(allowed))
	for i, a := range allowed {
		names[i] = string(a)
	}
	return fmt.Errorf("%s %q is not one of: %s", field, value, strings.Join(names, ", "))
}

// ParseFamilyDeclaration reads a family declaration from the content of a family.yml file, expands
// its scenario to the facts it implies, applies any overrides, and checks the result.
func ParseFamilyDeclaration(data []byte) (*FamilyDeclaration, error) {
	var f familyFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	// A file with nothing in it is EOF, and is refused below for naming no scenario
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	if f.Scenario == "" {
		return nil, fmt.Errorf("it must name a scenario, such as \"scenario: %s\". The scenarios are: %s",
			ScenarioProviderFirst, ScenarioNames())
	}
	info, ok := scenarioInfo(f.Scenario)
	if !ok {
		return nil, fmt.Errorf("scenario %q is not one of: %s", f.Scenario, ScenarioNames())
	}
	if err := errors.Join(
		oneOf("ahead", f.Ahead, AheadWriter, AheadReader, AheadEither, AheadNeither),
		oneOf("documentsLive", f.DocumentsLive, DocumentsLiveMoment, DocumentsLiveRetention, DocumentsLiveIndefinitely),
		oneOf("schema", f.Schema, SchemaPrescribes, SchemaDescribes),
	); err != nil {
		return nil, err
	}

	d := &FamilyDeclaration{Scenario: f.Scenario, Facts: info.Facts}
	f.applyOverrides(&d.Facts)

	if d.Facts.DocumentsLive == "" {
		return nil, fmt.Errorf("how long documents live varies in scenario %s, so the family must state it, with "+
			"\"documentsLive: moment\", \"documentsLive: retention\" or \"documentsLive: indefinitely\". "+
			"If you don't know how long they live, choose indefinitely", f.Scenario)
	}

	var err error
	d.OldestLive, err = parseOldestLive(f.OldestLive, d.Facts.DocumentsLive)
	if err != nil {
		return nil, err
	}
	for _, major := range slices.Sorted(maps.Keys(f.Migrations)) {
		if strings.TrimSpace(f.Migrations[major]) == "" {
			return nil, fmt.Errorf("the migration statement for major %d is empty: give a sentence or link", major)
		}
	}
	d.Migrations = f.Migrations
	return d, nil
}

func (f *familyFile) applyOverrides(facts *FamilyFacts) {
	if f.Ahead != "" {
		facts.Ahead = f.Ahead
	}
	if f.DocumentsLive != "" {
		facts.DocumentsLive = f.DocumentsLive
	}
	if f.ReaderPicksSchema != nil {
		facts.ReaderPicksSchema = *f.ReaderPicksSchema
	}
	if f.Schema != "" {
		facts.Schema = f.Schema
	}
}

// parseOldestLive checks oldestLive, which a family has if, and only if, its documents live for a retention period.
func parseOldestLive(s string, live DocumentsLive) (SemVer, error) {
	if live != DocumentsLiveRetention {
		if s != "" {
			return SemVer{}, fmt.Errorf("oldestLive only applies when documentsLive is %s, and here it is %s",
				DocumentsLiveRetention, live)
		}
		return SemVer{}, nil
	}
	if s == "" {
		return SemVer{}, fmt.Errorf("documentsLive is %s, so it must give the family's oldest live version, "+
			"such as \"oldestLive: 1.0.0\"", DocumentsLiveRetention)
	}
	return parseVersion("oldestLive", s)
}

// parseVersion reads a version given in a file, such as 1.0.0, for the field of that name.
func parseVersion(field, s string) (SemVer, error) {
	parts := strings.Split(s, ".")
	if len(parts) != len(SemVer{}) {
		return SemVer{}, fmt.Errorf("%s %q is not a version, such as 1.0.0", field, s)
	}
	v, err := NewSemVer(parts[0], parts[1], parts[2])
	if err != nil {
		return SemVer{}, fmt.Errorf("%s %q is not a version, such as 1.0.0: %w", field, s, err)
	}
	return v, nil
}

// ChangeFrom says why this declaration may not replace the declaration a deployed family had, or
// returns "" if it may. Only moving oldestLive forward changes what the family declares. Migration
// statements are records, not declarations: a deployed family needs to gain one for each new major.
func (d *FamilyDeclaration) ChangeFrom(deployed *FamilyDeclaration) string {
	if d.Scenario != deployed.Scenario || d.Facts != deployed.Facts {
		return "its scenario and facts cannot change, as a changed scenario means a new family"
	}
	if slices.Compare(d.OldestLive[:], deployed.OldestLive[:]) < 0 {
		return fmt.Sprintf("oldestLive cannot move back, from %s to %s",
			deployed.OldestLive.String('.'), d.OldestLive.String('.'))
	}
	return ""
}

// FamilySpec is what an author chooses about a new family.
type FamilySpec struct {
	Scenario      Scenario
	DocumentsLive DocumentsLive // needed only if the scenario leaves it to the family
}

// content returns the family.yml of a new family, whose first version is 1.0.0.
func (fs FamilySpec) content() ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# How this family is used, which decides what JSM checks. See %s.\n", ScenariosDoc)
	fmt.Fprintf(&b, "scenario: %s\n", fs.Scenario)

	info, _ := scenarioInfo(fs.Scenario)
	live := info.Facts.DocumentsLive
	if fs.DocumentsLive != "" {
		live = fs.DocumentsLive
		fmt.Fprintf(&b, "documentsLive: %s\n", live)
	}
	if live == DocumentsLiveRetention {
		b.WriteString("oldestLive: 1.0.0\n")
	}

	data := []byte(b.String())
	if _, err := ParseFamilyDeclaration(data); err != nil {
		return nil, err
	}
	return data, nil
}

// FamilyDeclarationPath returns where the declaration of the key's family lives.
func (r *Registry) FamilyDeclarationPath(k Key) string {
	return filepath.Join(New(k, r).Path(FamilyDir), FamilyDeclarationFile)
}

// FamilyDeclaration reads the declaration of the key's family. A family with no declaration, or an
// invalid one, is an error which says how to declare it.
func (r *Registry) FamilyDeclaration(k Key) (*FamilyDeclaration, error) {
	path := r.FamilyDeclarationPath(k)
	//nolint:gosec // Path is constructed from internal registry logic
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, &MissingFamilyDeclarationError{Path: path}
	}
	if err != nil {
		return nil, err
	}
	d, err := ParseFamilyDeclaration(data)
	if err != nil {
		return nil, &InvalidFamilyDeclarationError{Path: path, Wrapped: err}
	}
	if d.Facts.DocumentsLive == DocumentsLiveRetention {
		versionDir := filepath.Join(filepath.Dir(path), d.OldestLive.String(filepath.Separator))
		if found, _ := filepath.Glob(filepath.Join(versionDir, "*"+SchemaSuffix)); len(found) == 0 {
			return nil, &InvalidFamilyDeclarationError{Path: path, Wrapped: fmt.Errorf(
				"oldestLive %s is not a version of this family. It must name the oldest version whose documents "+
					"may still exist", d.OldestLive.String('.'))}
		}
	}
	return d, nil
}

// declaredOldestLive returns the oldestLive of the family in familyDir: the zero SemVer if it has none,
// or no valid declaration, which is reported elsewhere.
func declaredOldestLive(familyDir string) SemVer {
	//nolint:gosec // Path is constructed from internal registry logic
	data, _ := os.ReadFile(filepath.Join(familyDir, FamilyDeclarationFile))
	d, err := ParseFamilyDeclaration(data)
	if err != nil {
		return SemVer{}
	}
	return d.OldestLive
}

// RequireFamilyDeclared returns an error if the key's schema exists, and its family has no valid
// declaration. For a schema which doesn't exist it returns nil, as the command has a better error to give.
func (r *Registry) RequireFamilyDeclared(k Key) error {
	if _, err := os.Stat(New(k, r).Path(FilePath)); err != nil {
		return nil //nolint:nilerr // see above
	}
	_, err := r.FamilyDeclaration(k)
	return err
}

// RequireFamilyDeclarations returns an error naming every family in the scope with no valid declaration.
func (r *Registry) RequireFamilyDeclarations(ctx context.Context, scope SearchScope) error {
	searcher, err := NewSearcher(r, scope)
	if err != nil {
		return err
	}

	seen := make(map[string]bool)
	var problems []error
	for res := range searcher.Schemas(ctx) {
		if res.Err != nil {
			return res.Err
		}
		path := r.FamilyDeclarationPath(res.Key)
		if seen[path] {
			continue
		}
		seen[path] = true
		if _, dErr := r.FamilyDeclaration(res.Key); dErr != nil {
			problems = append(problems, dErr)
		}
	}
	return errors.Join(problems...)
}
