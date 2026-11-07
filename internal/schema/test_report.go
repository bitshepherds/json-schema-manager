package schema

import (
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bitshepherds/json-schema-manager/v2/internal/coverage"
	"github.com/bitshepherds/json-schema-manager/v2/internal/structdiff"
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

	// MissingMigrations lists, under the first version of each major, a major which needs a migration
	// statement and has none.
	MissingMigrations map[Key]MissingMigration

	// UnexercisedChanges lists, for each schema, the changes which none of its pass documents use.
	UnexercisedChanges map[Key][]UnexercisedChange
	// AcknowledgedChanges lists, for each schema, the changes its exception file acknowledges.
	AcknowledgedChanges map[Key][]AcknowledgedChange

	// Summaries describe, for each schema tested against its own documents, how well they cover it.
	Summaries map[Key]*Summary

	// NotTested counts the schemas in the run's scope which it didn't test, because it stopped at the
	// first failure. The report holds no runs or summary for them, only what a schema whose checks
	// finished found under one: a stale exception, a missing migration statement or an unexercised change.
	// notTested names those.
	NotTested int
	// notTested are the schemas the run didn't test, of those NotTested counts, which the report holds a
	// finding for all the same, found by a schema whose checks finished.
	notTested map[Key]bool
}

// MarkNotTested notes that the run didn't test the schema with the given key, though the report holds a
// finding for it which another schema's checks found.
func (r *TestReport) MarkNotTested(k Key) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.markNotTested(k)
}

func (r *TestReport) markNotTested(k Key) {
	if r.notTested == nil {
		r.notTested = make(map[Key]bool)
	}
	r.notTested[k] = true
}

// Tested reports whether the run tested the schema with the given key. One it stopped before testing
// can still hold a finding, such as a stale exception, which another schema's checks found.
func (r *TestReport) Tested(k Key) bool {
	return !r.notTested[k]
}

// StoppedEarly reports whether the run stopped at the first failure before testing every schema.
func (r *TestReport) StoppedEarly() bool {
	return r.NotTested > 0
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
	// SeveralReasons gives, for each fail document rejected for several independent reasons, where they
	// are. One of them can hide a change to another when the document runs on another version.
	SeveralReasons map[string][]string
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

// HasWarnings reports whether the summary holds anything --strict fails on.
func (s *Summary) HasWarnings() bool {
	return s.MissingDocuments() || len(s.SeveralReasons) > 0
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

// UnexercisedChange is a structural change between two versions in a major which may go in a direction
// the family protects, and which no document shows going that way. So the rule's document runs cannot
// tell whether the change breaks anyone.
type UnexercisedChange struct {
	Earlier   Key
	Later     Key
	Direction structdiff.Effect // Widens or Narrows: the direction which needs evidence
	Change    structdiff.Change

	foundBy Key // the version whose checks found it, if not Later
}

// NeededDocument returns the kind of document which would show the change.
func (uc *UnexercisedChange) NeededDocument() TestDocType {
	if uc.Direction == structdiff.Narrows {
		return TestDocTypeFail
	}
	return TestDocTypePass
}

// AcknowledgedChange is an unexercised change which the later version's exception file acknowledges,
// because no document could show it.
type AcknowledgedChange struct {
	Earlier Key
	Later   Key
	Change  structdiff.Change
	Reason  string
}

// StaleException is an entry of a version's exception file which clears nothing: what it names isn't
// there, or the run doesn't fail. It fails the run, so they don't pile up.
type StaleException struct {
	Exception Exception
	Why       string

	foundBy Key // the version whose checks found it, if not the one whose file lists it
}

// MissingMigration is a major which needs a migration statement in its family's declaration, and has none.
type MissingMigration struct {
	Major uint64
	Why   string

	foundBy []Key // the versions whose checks found it, if not the major's first
}

// NewTestReport creates a new TestReport.
func NewTestReport() *TestReport {
	return &TestReport{
		FailedTests:         make(TestLog),
		PassedTests:         make(TestLog),
		ExceptedTests:       make(TestLog),
		StaleExceptions:     make(map[Key][]StaleException),
		MissingMigrations:   make(map[Key]MissingMigration),
		UnexercisedChanges:  make(map[Key][]UnexercisedChange),
		AcknowledgedChanges: make(map[Key][]AcknowledgedChange),
		Summaries:           make(map[Key]*Summary),
	}
}

// dropUnless takes out of the report everything it holds for each schema for which keep is false, but
// for a stale exception, missing migration statement or unexercised change which a version for which
// finished is true found. A schema which keeps one is marked as not tested.
func (r *TestReport) dropUnless(keep, finished func(Key) bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	drop := func(k Key) bool { return !keep(k) }
	maps.DeleteFunc(r.PassedTests, func(k Key, _ []Spec) bool { return drop(k) })
	maps.DeleteFunc(r.FailedTests, func(k Key, _ []Spec) bool { return drop(k) })
	maps.DeleteFunc(r.ExceptedTests, func(k Key, _ []Spec) bool { return drop(k) })
	maps.DeleteFunc(r.AcknowledgedChanges, func(k Key, _ []AcknowledgedChange) bool { return drop(k) })
	maps.DeleteFunc(r.Summaries, func(k Key, _ *Summary) bool { return drop(k) })

	// lost reports whether what goes under the schema was found by none of the versions for which
	// finished is true, nor by that schema itself if no version is given.
	lost := func(under Key, by ...Key) bool {
		if keep(under) {
			return false
		}
		if len(by) == 0 || by[0] == "" {
			return !finished(under)
		}
		return !slices.ContainsFunc(by, finished)
	}
	for k, stale := range r.StaleExceptions {
		r.StaleExceptions[k] = slices.DeleteFunc(stale, func(s StaleException) bool { return lost(k, s.foundBy) })
	}
	for k, ucs := range r.UnexercisedChanges {
		r.UnexercisedChanges[k] = slices.DeleteFunc(ucs, func(uc UnexercisedChange) bool { return lost(k, uc.foundBy) })
	}
	maps.DeleteFunc(r.StaleExceptions, func(_ Key, stale []StaleException) bool { return len(stale) == 0 })
	maps.DeleteFunc(r.UnexercisedChanges, func(_ Key, ucs []UnexercisedChange) bool { return len(ucs) == 0 })
	maps.DeleteFunc(r.MissingMigrations, func(k Key, m MissingMigration) bool { return lost(k, m.foundBy...) })

	for _, under := range []func(yield func(Key) bool){
		maps.Keys(r.StaleExceptions), maps.Keys(r.UnexercisedChanges), maps.Keys(r.MissingMigrations),
	} {
		for k := range under {
			if drop(k) {
				r.markNotTested(k)
			}
		}
	}
}

// AddSummary adds the summary of the schema with the given key's test documents.
func (r *TestReport) AddSummary(key Key, s *Summary) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Summaries[key] = s
}

// FailDocumentsWithSeveralReasons returns how many fail documents are rejected for several independent reasons.
func (r *TestReport) FailDocumentsWithSeveralReasons() int {
	n := 0
	for _, s := range r.Summaries {
		n += len(s.SeveralReasons)
	}
	return n
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
// fail document, a fail document with several reasons, or a change which no document shows.
func (r *TestReport) HasWarnings() bool {
	return len(r.UnexercisedChanges) > 0 || r.MissingDocuments() > 0 || r.FailDocumentsWithSeveralReasons() > 0
}

// HasFailures reports whether the report has anything which fails the run: a test which failed, a
// stale exception, or a missing migration statement.
func (r *TestReport) HasFailures() bool {
	return len(r.FailedTests) > 0 || len(r.StaleExceptions) > 0 || len(r.MissingMigrations) > 0
}

// AddMissingMigration records, under the first version of its major, a major with no migration
// statement. Each version in the major finds it, so it is kept once, noting each that found it.
func (r *TestReport) AddMissingMigration(key Key, m MissingMigration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if found, ok := r.MissingMigrations[key]; ok {
		m.foundBy = append(found.foundBy, m.foundBy...)
	}
	r.MissingMigrations[key] = m
}

// AddExceptedTest adds a test which gave the wrong outcome, and which an exception names.
func (r *TestReport) AddExceptedTest(key Key, spec *Spec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ExceptedTests[key] = append(r.ExceptedTests[key], *spec)
}

// AddStaleException adds an entry of the exception file of the version with the given key which clears nothing.
func (r *TestReport) AddStaleException(key Key, e *Exception, why string) {
	r.addStaleException(key, key, e, why)
}

// addStaleException adds a stale entry of the exception file of the version with the given key, which
// the checks of the version with key by found.
func (r *TestReport) addStaleException(key, by Key, e *Exception, why string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.StaleExceptions[key] = append(r.StaleExceptions[key], StaleException{Exception: *e, Why: why, foundBy: by})
}

// AddUnexercisedChange adds a change in the schema with the given key which no document shows. Workers
// reach a later version's pairs in any order, so each schema's are kept in order of the earlier version.
func (r *TestReport) AddUnexercisedChange(key Key, uc *UnexercisedChange) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.UnexercisedChanges[key] = append(r.UnexercisedChanges[key], *uc)
	slices.SortStableFunc(r.UnexercisedChanges[key], func(a, b UnexercisedChange) int {
		return strings.Compare(string(a.Earlier), string(b.Earlier))
	})
}

// AddAcknowledgedChange adds a change in the schema with the given key which its exception file acknowledges.
func (r *TestReport) AddAcknowledgedChange(key Key, ac *AcknowledgedChange) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.AcknowledgedChanges[key] = append(r.AcknowledgedChanges[key], *ac)
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
