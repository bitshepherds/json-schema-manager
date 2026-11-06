package schema

import (
	"io"
	"sync"
	"time"

	"github.com/bitshepherds/json-schema-manager/internal/coverage"
	"github.com/bitshepherds/json-schema-manager/internal/structdiff"
)

// Reporter defines the interface for creating formatted test reports.
type Reporter interface {
	Write(w io.Writer, report *TestReport) error
}

// TestLog is a map of schema keys to a list of Specs.
// It is used to collect test outcomes for a schema identified by Key.
type TestLog map[Key][]Spec

// NewTestLog creates a new TestLog.
func NewTestLog() TestLog {
	return make(TestLog)
}

// AddTest adds a test to the TestLog.
func (l TestLog) AddTest(key Key, spec *Spec) {
	l[key] = append(l[key], *spec)
}

// TestReport represents the results of a test run.
type TestReport struct {
	mu sync.Mutex

	StartTime time.Time
	EndTime   time.Time
	// Fields for future implementation when test execution is added
	// numTests    int     // The number of tests that were run
	// numFailures int     // The number of tests which identified a problem.
	FailedTests TestLog // tests exposing a problem - i.e. a pass test that failed or a fail test that passed
	PassedTests TestLog // tests that passed as expected - i.e a pass test that passed and a fail test that failed

	// ExceptedTests are runs across versions which gave the wrong outcome, and which the later version's
	// exception file names, so don't count as failures.
	ExceptedTests TestLog

	// StaleExceptions lists, for each later version, the entries of its exception file which clear nothing.
	StaleExceptions map[Key][]StaleException

	// UnexercisedChanges lists, for each schema, the changes which none of its pass documents use.
	UnexercisedChanges map[Key][]UnexercisedChange

	// Summaries describe, for each schema tested against its own documents, how well they cover it.
	Summaries map[Key]*Summary
}

// Summary describes how well a schema's test documents cover it.
type Summary struct {
	PassDocs int
	FailDocs int
	// InheritedPassDocs and InheritedFailDocs count the documents of earlier versions in the major, if
	// the family inherits documents. Which of them run on this schema is for the family's rule to say.
	InheritedPassDocs int
	InheritedFailDocs int
	Coverage          []coverage.Point    // the points in the schema, and whether its pass documents reach each
	FailKeywords      map[string][]string // for each fail document's path, the keywords which reject it
}

// MissingPassDocuments reports whether the schema has no pass document, of its own or inherited.
func (s *Summary) MissingPassDocuments() bool {
	return s.PassDocs+s.InheritedPassDocs == 0
}

// MissingFailDocuments reports whether the schema has no fail document, of its own or inherited.
func (s *Summary) MissingFailDocuments() bool {
	return s.FailDocs+s.InheritedFailDocs == 0
}

// MissingDocuments reports whether the schema has no pass document, or no fail document, of its own or inherited.
func (s *Summary) MissingDocuments() bool {
	return s.MissingPassDocuments() || s.MissingFailDocuments()
}

// Covered returns how many of the schema's coverage points its pass documents reach.
func (s *Summary) Covered() int {
	n := 0
	for _, p := range s.Coverage {
		if p.Covered {
			n++
		}
	}
	return n
}

// UnexercisedChange is a structural change in a schema since the version before it in its major
// family, which may make that earlier version reject documents the schema accepts, and which none
// of the schema's pass documents uses. So the compatibility check cannot tell whether the change
// breaks consumers of the earlier version.
type UnexercisedChange struct {
	Earlier Key // the version before the schema
	Change  structdiff.Change
}

// StaleException is an entry of a version's exception file which clears nothing: what it names isn't
// there, or the run doesn't fail. It fails the run, so they don't pile up.
type StaleException struct {
	Exception Exception
	Why       string
}

// NewTestReport creates a new TestReport.
func NewTestReport() *TestReport {
	return &TestReport{
		FailedTests:        make(TestLog),
		PassedTests:        make(TestLog),
		ExceptedTests:      make(TestLog),
		StaleExceptions:    make(map[Key][]StaleException),
		UnexercisedChanges: make(map[Key][]UnexercisedChange),
		Summaries:          make(map[Key]*Summary),
	}
}

// AddSummary adds the summary of the schema with the given key's test documents.
func (r *TestReport) AddSummary(key Key, s *Summary) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Summaries[key] = s
}

// MissingDocuments returns how many schemas have no pass document, or no fail document.
func (r *TestReport) MissingDocuments() int {
	n := 0
	for _, s := range r.Summaries {
		if s.MissingDocuments() {
			n++
		}
	}
	return n
}

// HasWarnings reports whether the report has anything --strict fails on: a schema with no pass or no
// fail document, or a change which no pass document uses.
func (r *TestReport) HasWarnings() bool {
	return len(r.UnexercisedChanges) > 0 || r.MissingDocuments() > 0
}

// HasFailures reports whether the report has anything which fails the run: a test which failed, or a
// stale exception.
func (r *TestReport) HasFailures() bool {
	return len(r.FailedTests) > 0 || len(r.StaleExceptions) > 0
}

// AddExceptedTest adds a test which gave the wrong outcome, and which an exception names.
func (r *TestReport) AddExceptedTest(key Key, spec *Spec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ExceptedTests[key] = append(r.ExceptedTests[key], *spec)
}

// AddStaleException adds an entry of the exception file of the version with the given key which clears nothing.
func (r *TestReport) AddStaleException(key Key, e Exception, why string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.StaleExceptions[key] = append(r.StaleExceptions[key], StaleException{Exception: e, Why: why})
}

// AddUnexercisedChange adds a change in the schema with the given key which no pass document uses.
func (r *TestReport) AddUnexercisedChange(key Key, uc *UnexercisedChange) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.UnexercisedChanges[key] = append(r.UnexercisedChanges[key], *uc)
}

// AddFailedTest adds a failed test to the report.
func (r *TestReport) AddFailedTest(key Key, spec *Spec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.FailedTests[key] = append(r.FailedTests[key], *spec)
}

// AddPassedTest adds a passed test to the report.
func (r *TestReport) AddPassedTest(key Key, spec *Spec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.PassedTests[key] = append(r.PassedTests[key], *spec)
}
