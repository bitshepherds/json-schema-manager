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

	// UnexercisedChanges lists, for each schema, the changes which none of its pass documents use.
	UnexercisedChanges map[Key][]UnexercisedChange

	// Summaries describe, for each schema tested against its own documents, how well they cover it.
	Summaries map[Key]*Summary
}

// Summary describes how well a schema's test documents cover it.
type Summary struct {
	PassDocs     int
	FailDocs     int
	Coverage     []coverage.Point    // the points in the schema, and whether its pass documents reach each
	FailKeywords map[string][]string // for each fail document's path, the keywords which reject it
}

// MissingDocuments reports whether the schema has no pass document, or no fail document.
func (s *Summary) MissingDocuments() bool {
	return s.PassDocs == 0 || s.FailDocs == 0
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

// NewTestReport creates a new TestReport.
func NewTestReport() *TestReport {
	return &TestReport{
		FailedTests:        make(TestLog),
		PassedTests:        make(TestLog),
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
