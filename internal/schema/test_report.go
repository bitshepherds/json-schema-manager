package schema

import (
	"io"
	"sync"
	"time"

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
	}
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
