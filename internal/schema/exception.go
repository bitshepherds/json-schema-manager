package schema

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// ExceptionsFile is the name of the file, in a version's directory, which lists the deliberate breaks
// of the rule between that version and earlier ones. It belongs to that one version.
const ExceptionsFile = "compat-exceptions.yml"

// Exception clears one document run across versions: the run of Document, between the earlier version
// Version and the version whose exception file lists it, under Rule. Its directory says which way the
// document runs, as the rule table has only one run for each rule and type of document.
type Exception struct {
	Version  SemVer
	Document string // relative to the directory of the version it belongs to, such as fail/too-long.json
	Rule     Rule
	Reason   string
}

func (e Exception) String() string {
	return fmt.Sprintf("version %s, %s, %s", e.Version.String('.'), e.Document, e.Rule)
}

// run returns the kind of run which the exception clears.
func (e Exception) run() acrossVersionRun {
	docType := TestDocType(filepath.Dir(filepath.FromSlash(e.Document)))
	// As in the rule table: no-loosening runs later pass and earlier fail documents, and no-tightening
	// runs earlier pass and later fail documents.
	return acrossVersionRun{
		fromLater: (e.Rule == RuleNoLoosening) == (docType == TestDocTypePass),
		docType:   docType,
		rule:      e.Rule,
	}
}

// clears reports whether the exception names the run of the spec, which is one between Version and a
// later version.
func (e Exception) clears(spec *Spec) bool {
	v := spec.Earlier.Version()
	dir := filepath.Base(filepath.Dir(spec.TestInfo.Path))
	return e.Version == v && e.Rule == spec.Rule && e.Document == dir+"/"+filepath.Base(spec.TestInfo.Path)
}

// whyNoRun says why the exception names no run of the pair of its version and the later version: the
// family's rule doesn't ask for it, or the document isn't there.
func (e Exception) whyNoRun(ahead Ahead, later Key) string {
	run := e.run()
	if !run.appliesTo(ahead) {
		return fmt.Sprintf("this family doesn't hold its versions to %s", e.Rule)
	}
	owner := e.Version.String('.')
	if run.fromLater {
		v := later.Version()
		owner = v.String('.')
	}
	return fmt.Sprintf("%s has no document %s", owner, e.Document)
}

// TargetGone reports whether what the exception names has gone from disk: its earlier version, or its
// document, given the directory of the later version whose file lists it. Such an entry is stale, and
// nothing can bring it back, so it may be removed even once it's deployed.
func (e Exception) TargetGone(laterDir string) bool {
	familyDir := filepath.Dir(filepath.Dir(filepath.Dir(laterDir)))
	earlierDir := filepath.Join(familyDir, e.Version.String(filepath.Separator))

	if schemas, _ := filepath.Glob(filepath.Join(earlierDir, "*"+SchemaSuffix)); len(schemas) == 0 {
		return true
	}
	owner := earlierDir
	if e.run().fromLater {
		owner = laterDir
	}
	_, err := os.Stat(filepath.Join(owner, filepath.FromSlash(e.Document)))
	return err != nil
}

// exceptionsFile is the content of a compat-exceptions.yml file.
type exceptionsFile struct {
	Exceptions []exceptionEntry `yaml:"exceptions"`
}

type exceptionEntry struct {
	Version  string `yaml:"version"`
	Document string `yaml:"document"`
	Rule     Rule   `yaml:"rule"`
	Reason   string `yaml:"reason"`
}

// exception checks the entry, and returns the exception it states.
func (entry exceptionEntry) exception() (Exception, error) {
	v, err := parseVersion("version", entry.Version)
	if err != nil {
		return Exception{}, err
	}
	if !validExceptionDocument(entry.Document) {
		return Exception{}, fmt.Errorf("document %q must be a %s file directly in pass or fail, such as "+
			"fail/too-long.json", entry.Document, TestDocSuffix)
	}
	if entry.Rule != RuleNoLoosening && entry.Rule != RuleNoTightening {
		return Exception{}, fmt.Errorf("rule %q must be %s or %s", entry.Rule, RuleNoLoosening, RuleNoTightening)
	}
	if strings.TrimSpace(entry.Reason) == "" {
		return Exception{}, errors.New("it must give a reason")
	}
	return Exception{Version: v, Document: entry.Document, Rule: entry.Rule, Reason: entry.Reason}, nil
}

func validExceptionDocument(document string) bool {
	dir, name, found := strings.Cut(document, "/")
	return found && (dir == string(TestDocTypePass) || dir == string(TestDocTypeFail)) &&
		!strings.Contains(name, "/") && filepath.Ext(name) == TestDocSuffix
}

// ParseExceptions reads the exceptions from the content of a compat-exceptions.yml file. Each needs
// every field, and an exception may be listed only once.
func ParseExceptions(data []byte) ([]Exception, error) {
	var f exceptionsFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	// A file with nothing in it is EOF, and excepts nothing
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	exceptions := make([]Exception, 0, len(f.Exceptions))
	for i, entry := range f.Exceptions {
		e, err := entry.exception()
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", i+1, err)
		}
		if slices.ContainsFunc(exceptions, func(o Exception) bool {
			return o.Version == e.Version && o.Document == e.Document && o.Rule == e.Rule
		}) {
			return nil, fmt.Errorf("entry %d: it names %s twice", i+1, e)
		}
		exceptions = append(exceptions, e)
	}
	return exceptions, nil
}

// Exceptions reads the version's exception file. A version with no such file has no exceptions.
func (s *Schema) Exceptions() ([]Exception, error) {
	path := filepath.Join(s.Path(HomeDir), ExceptionsFile)
	//nolint:gosec // Path is constructed from internal registry logic
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	var exceptions []Exception
	if err == nil {
		exceptions, err = ParseExceptions(data)
	}
	if err != nil {
		return nil, &InvalidExceptionsError{Path: path, Wrapped: err}
	}
	return exceptions, nil
}
