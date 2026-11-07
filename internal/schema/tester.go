package schema

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/bitshepherds/json-schema-manager/v2/internal/config"
	"github.com/bitshepherds/json-schema-manager/v2/internal/coverage"
	"github.com/bitshepherds/json-schema-manager/v2/internal/structdiff"
	"github.com/bitshepherds/json-schema-manager/v2/internal/validator"
)

// ErrStopTesting is a sentinel error used to signal that further tests should be stopped and the report shown.
var ErrStopTesting = errors.New("stopping after first error")

// Tester is the type used to manage a run of tests for one or more schemas. It runs one rule, in
// rule.go: the family's declaration decides which documents run against which versions, however a
// version was selected.
type Tester struct {
	registry *Registry

	// Test run options
	stopOnFirstError bool
	numWorkers       int

	report *TestReport

	mu       sync.Mutex
	ranPairs map[versionPair]bool // the pairs this run has already taken on
	// comparedPairs are the pairs whose structural changes this run has already looked at.
	comparedPairs map[versionPair]bool
	// listed stands in for the exception files of these versions.
	listed map[Key][]Exception
	// finished are the versions whose runs all finished, in TestFoundSchemas.
	finished map[Key]bool
	// stopped says a run of TestFoundSchemas stopped at a failure.
	stopped bool
}

// NewTester creates a new tester for the given registry.
func NewTester(r *Registry) *Tester {
	return &Tester{
		registry:         r,
		report:           NewTestReport(),
		stopOnFirstError: true,
		numWorkers:       runtime.GOMAXPROCS(0),
		ranPairs:         make(map[versionPair]bool),
		comparedPairs:    make(map[versionPair]bool),
		finished:         make(map[Key]bool),
	}
}

// StaleAmong returns those of the entries which clear nothing as the registry stands, so which validate
// would report as stale if the exception file of the version with key k listed them. It runs what validate
// runs on that version, so the answer can't differ from validate's.
func StaleAmong(ctx context.Context, r *Registry, k Key, entries []Exception) ([]Exception, error) {
	t := NewTester(r)
	t.SetStopOnFirstError(false)
	t.listed = map[Key][]Exception{k: entries}
	report, err := t.TestSingleSchema(ctx, k)
	if err != nil {
		return nil, err
	}
	stale := make([]Exception, 0, len(report.StaleExceptions[k]))
	for _, s := range report.StaleExceptions[k] {
		stale = append(stale, s.Exception)
	}
	return stale, nil
}

// exceptions returns what the version's exception file lists, or what this run takes it to list.
func (t *Tester) exceptions(s *Schema) ([]Exception, error) {
	if entries, ok := t.listed[s.Key()]; ok {
		return entries, nil
	}
	return s.Exceptions()
}

// TestSingleSchema runs a schema's own documents, then the pairs it is part of, with every earlier
// and every later version in its major, as far as its family's declaration asks for them.
// The context can be used to cancel the test run early (e.g., on Ctrl+C).
func (t *Tester) TestSingleSchema(ctx context.Context, k Key) (*TestReport, error) {
	t.report.StartTime = time.Now()
	defer func() { t.report.EndTime = time.Now() }()

	if err := t.testVersion(ctx, k); err != nil && !errors.Is(err, ErrStopTesting) {
		return nil, err
	}
	return t.report, nil
}

// TestSpecificDocument runs one test document of a schema: on the schema itself, and wherever its
// family's declaration runs it on the other versions in the major.
func (t *Tester) TestSpecificDocument(ctx context.Context, k Key, testPath string) (*TestReport, error) {
	t.report.StartTime = time.Now()
	defer func() { t.report.EndTime = time.Now() }()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s, err := t.registry.GetSchemaByKey(k)
	if err != nil {
		return nil, err
	}

	// Ahead of running the tests, ensure the schema is valid by forcing a render.
	if _, err = s.Render(t.registry.config.ProductionEnvConfig()); err != nil {
		return nil, err
	}

	ti, err := NewTestInfo(testPath, t.registry.RootDirectory())
	if err != nil {
		return nil, err
	}

	// Determine test doc type from path
	dir := filepath.Base(filepath.Dir(testPath))
	var tt TestDocType
	switch dir {
	case string(TestDocTypePass):
		tt = TestDocTypePass
	case string(TestDocTypeFail):
		tt = TestDocTypeFail
	default:
		return nil, &InvalidTestDocumentDirectoryError{Path: testPath}
	}

	decl, err := t.registry.FamilyDeclaration(k)
	if err != nil {
		return nil, err
	}
	cross, err := t.documentAcrossVersions(s, decl, ti, tt)
	if err != nil {
		return nil, err
	}

	specs := append([]Spec{NewSpec(s, ti, tt)}, cross...)
	if err = t.runSpecs(ctx, specs); err != nil && !errors.Is(err, ErrStopTesting) {
		return nil, err
	}
	return t.report, nil
}

// SetStopOnFirstError controls whether the test run should stop on the first error.
// It defaults to true. If false, then all test results for all targeted schemas will be compiled in
// the test report.
func (t *Tester) SetStopOnFirstError(b bool) {
	t.stopOnFirstError = b
}

// SetNumWorkers controls the number of testing workers used to execute schema tests in parallel.
// It defaults to a sensible default. Only use this if you want to manually control the number of workers.
func (t *Tester) SetNumWorkers(n int) {
	t.numWorkers = n
}

// TestFoundSchemas searches for schemas matching the given search scope, and
// executes tests for each schema found.
// Each schema's documents, and the pairs it is part of, run as for TestSingleSchema. A pair two found
// schemas share runs once.
// Testing runs in parallel. Use SetNumWorkers to control the number of workers.
// The context can be used to cancel the test run early (e.g., on Ctrl+C).
func (t *Tester) TestFoundSchemas(ctx context.Context, ss SearchScope) (*TestReport, error) {
	searcher, err := NewSearcher(t.registry, ss)
	if err != nil {
		return nil, err
	}

	t.report.StartTime = time.Now()
	defer func() { t.report.EndTime = time.Now() }()

	// Create a sub-context to allow us to cancel workers/producer early
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	resultC := searcher.Schemas(runCtx)

	var wg sync.WaitGroup
	sem := make(chan struct{}, t.numWorkers)

	var finalErr error
	var errOnce sync.Once

	for res := range resultC {
		if stop := t.processSearchResult(runCtx, res, &wg, sem, &errOnce, &finalErr, cancelRun); stop {
			break
		}
	}

	// Wait for all workers currently in flight to finish
	wg.Wait()

	// If ctx was cancelled by the caller (not by stopOnFirstError), prioritise returning that error.
	if ctx.Err() != nil {
		return t.report, ctx.Err()
	}
	if finalErr != nil {
		return t.report, finalErr
	}
	if t.stopped {
		return t.report, t.dropUntested(ctx, searcher)
	}
	return t.report, nil
}

// dropUntested takes out of the report every schema whose runs didn't all finish when the run stopped
// at a failure, but for one with a failure, and counts those of the searcher's schemas it took out, or
// never reached, as not tested. A schema cut short would otherwise show as passed.
func (t *Tester) dropUntested(ctx context.Context, searcher *Searcher) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	keep := func(k Key) bool { return t.finished[k] || len(t.report.FailedTests[k]) > 0 }
	for res := range searcher.Schemas(ctx) {
		if res.Err != nil {
			return res.Err
		}
		if !keep(res.Key) {
			t.report.NotTested++
		}
	}
	t.report.dropUnless(keep)
	return nil
}

func (t *Tester) processSearchResult(
	ctx context.Context,
	res SearchResult,
	wg *sync.WaitGroup,
	sem chan struct{},
	errOnce *sync.Once,
	//nolint:gocritic // finalErr updated via pointer
	finalErr *error,
	cancelFunc context.CancelFunc,
) bool {
	if res.Err != nil {
		errOnce.Do(func() {
			*finalErr = res.Err
			cancelFunc()
		})
		return true
	}

	// Before acquiring a worker slot, check if we've been told to stop
	select {
	case <-ctx.Done():
		return true
	case sem <- struct{}{}: // Acquire worker slot
	}

	wg.Add(1)
	go func(k Key) {
		defer wg.Done()
		defer func() { <-sem }() // Release slot when finished

		tErr := t.testVersion(ctx, k)
		t.mu.Lock()
		t.finished[k] = tErr == nil
		t.stopped = t.stopped || errors.Is(tErr, ErrStopTesting)
		t.mu.Unlock()
		if tErr != nil {
			errOnce.Do(func() {
				if !errors.Is(tErr, ErrStopTesting) {
					*finalErr = tErr
				}
				cancelFunc() // Signal producer and other workers to stop
			})
		}
	}(res.Key)

	return false
}

// findUnexercisedChanges compares the pairs the schema is part of, as the rule's document runs do, and
// adds to the report each change in a direction its family protects with no document to show it. A
// family with no pairs protects nothing.
func (t *Tester) findUnexercisedChanges(k Key) error {
	s, err := t.registry.GetSchemaByKey(k)
	if err != nil {
		return err
	}
	decl, err := t.registry.FamilyDeclaration(k)
	if err != nil {
		return err
	}
	ahead := decl.Facts.EffectiveAhead()
	if ahead == AheadNeither {
		return nil
	}
	earlier, later, err := t.otherVersions(s, decl)
	if err != nil {
		return err
	}
	for _, p := range pairsOf(s, earlier, later) {
		if !t.claimComparison(versionPair{p[0].Key(), p[1].Key()}) {
			continue
		}
		if cErr := t.comparePair(p[0], p[1], ahead); cErr != nil {
			return cErr
		}
	}
	return nil
}

// comparePair adds to the report the unexercised changes between the pair, and each of the later
// version's exception entries for the pair which acknowledges a change that needs no acknowledging. A
// change the later version carries over from the version before it belongs to that version.
func (t *Tester) comparePair(earlier, later *Schema, ahead Ahead) error {
	changes, err := compareVersions(earlier, later)
	if err != nil {
		return err
	}
	between, err := t.versionsAfter(earlier, later)
	if err != nil {
		return err
	}
	carried, err := carriedChanges(earlier, between)
	if err != nil {
		return err
	}
	evidence, err := newChangeEvidence(earlier, later, between)
	if err != nil {
		return err
	}
	exceptions, err := t.exceptions(later)
	if err != nil {
		return err
	}

	acknowledged := make(map[string]bool)
	for i := range changes {
		if !slices.Contains(carried, carryKey(&changes[i])) {
			t.warnOfChange(&changes[i], earlier.Key(), later.Key(), evidence.missing(&changes[i], ahead), exceptions,
				acknowledged)
		}
	}

	for i := range exceptions {
		e := &exceptions[i]
		if e.Change != "" && e.Version == earlier.Key().Version() && !acknowledged[e.Change] {
			t.report.AddStaleException(later.Key(), e, fmt.Sprintf(
				"no change between %s and %s by that description needs acknowledging", earlier.Key(), later.Key()))
		}
	}
	return nil
}

// warnOfChange adds to the report a change no document shows in the directions missing, unless an
// exception acknowledges it, when it notes that in acknowledged.
func (t *Tester) warnOfChange(
	change *structdiff.Change,
	earlier, later Key,
	missing []structdiff.Effect,
	exceptions []Exception,
	acknowledged map[string]bool,
) {
	if len(missing) == 0 {
		return
	}
	if e := exceptionForChange(exceptions, earlier.Version(), change); e != nil {
		acknowledged[e.Change] = true
		t.report.AddAcknowledgedChange(later, &AcknowledgedChange{
			Earlier: earlier, Later: later, Change: *change, Reason: e.Reason,
		})
		return
	}
	for _, direction := range missing {
		t.report.AddUnexercisedChange(later, &UnexercisedChange{
			Earlier: earlier, Later: later, Direction: direction, Change: *change,
		})
	}
}

// exceptionForChange returns the entry which acknowledges the change from the earlier version, if any.
func exceptionForChange(exceptions []Exception, earlier SemVer, change *structdiff.Change) *Exception {
	i := slices.IndexFunc(exceptions, func(e Exception) bool {
		return e.Version == earlier && e.Change == change.String()
	})
	if i < 0 {
		return nil
	}
	return &exceptions[i]
}

// protectedDirections returns the directions in which a change with the given effect needs evidence,
// in a family with the given party ahead. A writer ahead of its reader is protected from widening, and
// a reader ahead of its writer from narrowing. A change which could go either way, needs evidence in
// whichever direction the family protects.
func protectedDirections(ahead Ahead, effect structdiff.Effect) []structdiff.Effect {
	var out []structdiff.Effect
	if (ahead == AheadWriter || ahead == AheadEither) && effect != structdiff.Narrows {
		out = append(out, structdiff.Widens)
	}
	if (ahead == AheadReader || ahead == AheadEither) && effect != structdiff.Widens {
		out = append(out, structdiff.Narrows)
	}
	return out
}

// changeEvidence is what can show a change between a pair: the documents of each version after the
// earlier one, up to the later one, since all of them run on the earlier version where the rule asks.
type changeEvidence struct {
	pass, fail   []TestInfo
	older, newer structdiff.Validates
}

func newChangeEvidence(earlier, later *Schema, between []*Schema) (*changeEvidence, error) {
	ev := &changeEvidence{older: earlier.subschemaValidates(), newer: later.subschemaValidates()}
	for _, s := range between {
		pass, err := s.TestDocuments(TestDocTypePass)
		if err != nil {
			return nil, err
		}
		fail, err := s.TestDocuments(TestDocTypeFail)
		if err != nil {
			return nil, err
		}
		ev.pass = append(ev.pass, pass...)
		ev.fail = append(ev.fail, fail...)
	}
	return ev, nil
}

// missing returns the directions the family protects in which no document shows the change.
func (ev *changeEvidence) missing(change *structdiff.Change, ahead Ahead) []structdiff.Effect {
	var out []structdiff.Effect
	for _, direction := range protectedDirections(ahead, change.Effect) {
		if len(ev.showing(change, direction)) == 0 {
			out = append(out, direction)
		}
	}
	return out
}

// showing returns the paths of the documents which show the change going in the direction.
func (ev *changeEvidence) showing(change *structdiff.Change, direction structdiff.Effect) []string {
	var paths []string
	if direction == structdiff.Narrows {
		for _, doc := range ev.fail {
			if change.NarrowedBy(doc.Unmarshalled, ev.older, ev.newer) {
				paths = append(paths, doc.Path)
			}
		}
		return paths
	}
	for _, doc := range ev.pass {
		if change.UsedBy(doc.Unmarshalled, ev.older) {
			paths = append(paths, doc.Path)
		}
	}
	return paths
}

// versionsAfter returns the versions in later's major after earlier, up to and including later, in
// version order.
func (t *Tester) versionsAfter(earlier, later *Schema) ([]*Schema, error) {
	keys, err := later.MajorFamilyEarlierSchemas()
	if err != nil {
		return nil, err
	}
	keys = keys[slices.Index(keys, earlier.Key())+1:]
	versions, err := t.schemas(keys)
	if err != nil {
		return nil, err
	}
	return append(versions, later), nil
}

// carriedChanges returns the changes from earlier to the version before the last of between, each as a
// carryKey. There are none if that version is earlier itself.
func carriedChanges(earlier *Schema, between []*Schema) ([]string, error) {
	if len(between) < 2 {
		return nil, nil
	}
	changes, err := compareVersions(earlier, between[len(between)-2])
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(changes))
	for i := range changes {
		keys[i] = carryKey(&changes[i])
	}
	return keys, nil
}

// carryKey says what makes a change the same one between two pairs.
func carryKey(change *structdiff.Change) string {
	return string(change.Effect) + " " + change.String()
}

// inheritedDocuments counts the documents s inherits from the earlier versions in its major. A live
// version inherits only from live ones, as nothing else still runs. One below oldestLive stays as it
// was, so moving oldestLive past it asks nothing new of it.
func (t *Tester) inheritedDocuments(s *Schema, decl *FamilyDeclaration) (pass, fail int, err error) {
	if !decl.Facts.InheritsDocuments() {
		return 0, 0, nil
	}
	earlierKeys, err := s.MajorFamilyEarlierSchemas()
	if err != nil {
		return 0, 0, err
	}
	sIsLive := decl.Live(s.Key().Version())
	earlierKeys = slices.DeleteFunc(earlierKeys, func(k Key) bool { return sIsLive && !decl.Live(k.Version()) })
	earlier, err := t.schemas(earlierKeys)
	if err != nil {
		return 0, 0, err
	}
	for _, e := range earlier {
		docs, dErr := ownSpecs(e)
		if dErr != nil {
			return 0, 0, dErr
		}
		for i := range docs {
			if docs[i].TestDocType == TestDocTypePass {
				pass++
			} else {
				fail++
			}
		}
	}
	return pass, fail, nil
}

// summarise adds to the report how well the schema's own test documents, in specs, cover it: how
// many there are, which points in the schema the pass documents reach, and which keywords reject
// each fail document. It also counts the documents the schema inherits.
func (t *Tester) summarise(s *Schema, decl *FamilyDeclaration, ri RenderInfo, specs []Spec) error {
	inheritedPass, inheritedFail, err := t.inheritedDocuments(s, decl)
	if err != nil {
		return err
	}

	var passDocs, failDocs []TestInfo
	for i := range specs {
		if specs[i].TestDocType == TestDocTypePass {
			passDocs = append(passDocs, specs[i].TestInfo)
		} else {
			failDocs = append(failDocs, specs[i].TestInfo)
		}
	}

	id := string(s.CanonicalID(t.registry.config.ProductionEnvConfig()))
	docs := make([]any, len(passDocs))
	for i := range passDocs {
		docs[i] = passDocs[i].Unmarshalled
	}

	summary := &Summary{
		PassDocs:          len(passDocs),
		FailDocs:          len(failDocs),
		InheritedPassDocs: inheritedPass,
		InheritedFailDocs: inheritedFail,
		Coverage:          coverage.Measure(ri.Unmarshalled, docs, s.subschemaValidates()),
		FailKeywords:      make(map[string][]string),
		SeveralReasons:    make(map[string][]string),
	}
	for _, doc := range failDocs {
		verr := ri.Validator.Validate(doc.Unmarshalled)
		if keywords := validator.FailedKeywords(verr, id); keywords != nil {
			summary.FailKeywords[doc.Path] = keywords
		}
		// Across versions, one reason can hide the loss of another. A family with no pairs has no such run.
		if faults := validator.FaultLocations(verr); len(faults) > 1 && decl.Facts.EffectiveAhead() != AheadNeither {
			summary.SeveralReasons[doc.Path] = faults
		}
	}
	t.report.AddSummary(s.Key(), summary)
	return nil
}

// testVersion runs a schema's own documents and the pairs it is part of, then looks for its
// unexercised changes. A failure goes in the report, and the run goes on unless stopOnFirstError, when
// it returns ErrStopTesting. Any other error means the tests could not run: the schema can't be loaded
// or rendered, its family has no valid declaration, or the context was cancelled.
func (t *Tester) testVersion(ctx context.Context, k Key) error {
	s, err := t.registry.GetSchemaByKey(k)
	if err != nil {
		return err
	}

	decl, err := t.registry.FamilyDeclaration(k)
	if err != nil {
		return err
	}

	// Ahead of running the tests, ensure the schema is valid by forcing a render.
	ri, err := s.Render(t.registry.config.ProductionEnvConfig())
	if err != nil {
		return err
	}

	own, err := ownSpecs(s)
	if err != nil {
		return err
	}
	if sErr := t.summarise(s, decl, ri, own); sErr != nil {
		return sErr
	}
	if mErr := t.checkMigrationStatement(s, decl); mErr != nil {
		return mErr
	}

	cross, err := t.pairSpecs(s, decl)
	if err != nil {
		return err
	}

	if rErr := t.runSpecs(ctx, append(own, cross...)); rErr != nil {
		return rErr
	}

	return t.findUnexercisedChanges(k)
}

// checkMigrationStatement adds to the report a major past the family's first which has no migration
// statement and needs one. It goes under the major's first version, once however many are selected.
// The earlier majors aren't affected, so it's found only when a version in that major is selected.
func (t *Tester) checkMigrationStatement(s *Schema, decl *FamilyDeclaration) error {
	major := s.Key().Major()
	if !decl.Facts.NeedsMigrationStatement() || decl.Migrations[major] != "" {
		return nil
	}
	majors, err := t.registry.pathResolver.GetUintSubdirectories(s.Path(FamilyDir))
	if err != nil {
		return err
	}
	if major == slices.Min(majors) {
		return nil
	}
	earlier, err := s.MajorFamilyEarlierSchemas()
	if err != nil {
		return err
	}
	first := s.Key()
	if len(earlier) > 0 {
		first = earlier[0]
	}
	t.report.AddMissingMigration(first, MissingMigration{
		Major: major,
		Why: fmt.Sprintf("major %d needs a migration statement: in %s %s family old documents stay around, "+
			"and a major bump doesn't make them vanish. %s.",
			major, decl.Scenario.Article(), decl.Scenario, MigrationStatementHelp(major)),
	})
	return nil
}

// runSpecs runs each spec on the version it names, and adds the result to the report, under the
// later version for a run across versions. A failure stops the run only if stopOnFirstError, when it
// returns ErrStopTesting.
func (t *Tester) runSpecs(ctx context.Context, specs []Spec) error {
	env := t.registry.config.ProductionEnvConfig()
	for i := range specs {
		if err := ctx.Err(); err != nil {
			return err
		}

		spec := &specs[i]
		ri, err := spec.Schema.Render(env)
		if err != nil {
			return err
		}

		if spec.Run(ri.Validator) == nil {
			t.report.AddPassedTest(spec.ReportKey(), spec)
			if spec.Exception != nil {
				t.report.AddStaleException(spec.ReportKey(), spec.Exception,
					"that run doesn't fail, so there's nothing to except")
			}
			continue
		}
		if spec.Exception != nil {
			t.report.AddExceptedTest(spec.ReportKey(), spec)
			continue
		}
		t.report.AddFailedTest(spec.ReportKey(), spec)
		if t.stopOnFirstError {
			return ErrStopTesting
		}
	}
	return nil
}

// ownSpecs returns the specs which run the schema's own documents on it.
func ownSpecs(s *Schema) ([]Spec, error) {
	var specs []Spec
	for _, tt := range []TestDocType{TestDocTypePass, TestDocTypeFail} {
		docs, err := s.TestDocuments(tt)
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			specs = append(specs, NewSpec(s, doc, tt))
		}
	}
	return specs, nil
}

// otherVersions returns the schemas of the other versions in s's major which it can be paired with:
// those before it, and those after it. A version below the family's oldestLive takes no part, so it
// has none, and none has it.
func (t *Tester) otherVersions(s *Schema, decl *FamilyDeclaration) (earlier, later []*Schema, err error) {
	if !decl.Live(s.Key().Version()) {
		return nil, nil, nil
	}
	earlierKeys, err := s.MajorFamilyEarlierSchemas()
	if err != nil {
		return nil, nil, err
	}
	earlierKeys = slices.DeleteFunc(earlierKeys, func(k Key) bool { return !decl.Live(k.Version()) })
	laterKeys, err := s.MajorFamilyFutureSchemas()
	if err != nil {
		return nil, nil, err
	}
	if earlier, err = t.schemas(earlierKeys); err != nil {
		return nil, nil, err
	}
	if later, err = t.schemas(laterKeys); err != nil {
		return nil, nil, err
	}
	return earlier, later, nil
}

func (t *Tester) schemas(keys []Key) ([]*Schema, error) {
	schemas := make([]*Schema, len(keys))
	for i, k := range keys {
		s, err := t.registry.GetSchemaByKey(k)
		if err != nil {
			return nil, err
		}
		schemas[i] = s
	}
	return schemas, nil
}

// pairsOf returns the pairs s is part of, given the other versions in its major.
func pairsOf(s *Schema, earlier, later []*Schema) [][2]*Schema {
	pairs := make([][2]*Schema, 0, len(earlier)+len(later))
	for _, e := range earlier {
		pairs = append(pairs, [2]*Schema{e, s})
	}
	for _, l := range later {
		pairs = append(pairs, [2]*Schema{s, l})
	}
	return pairs
}

// pairSpecs returns the specs for the pairs s is part of, leaving out any this run has already taken
// on, so a pair two selected versions share runs once. Each is marked if the later version's exception
// file names it, and an entry which names no run of the pair goes in the report as stale.
func (t *Tester) pairSpecs(s *Schema, decl *FamilyDeclaration) ([]Spec, error) {
	earlier, later, err := t.otherVersions(s, decl)
	if err != nil {
		return nil, err
	}
	if eErr := t.reportExceptionsWithoutPairs(s, decl, earlier); eErr != nil {
		return nil, eErr
	}
	if decl.Facts.EffectiveAhead() == AheadNeither {
		return nil, nil
	}

	var specs []Spec
	for _, p := range pairsOf(s, earlier, later) {
		if !t.claimPair(versionPair{p[0].Key(), p[1].Key()}) {
			continue
		}
		runs, pErr := pairRuns(p[0], p[1], decl)
		if pErr != nil {
			return nil, pErr
		}
		if eErr := t.applyExceptions(runs, p[0], p[1], decl); eErr != nil {
			return nil, eErr
		}
		specs = append(specs, runs...)
	}
	return specs, nil
}

// reportExceptionsWithoutPairs adds to the report the entries of s's exception file which can't name a
// run of any pair: every one if the family runs nothing across versions, and otherwise those which
// name anything but a live earlier version in its major. A version below oldestLive answers for none
// of its entries, since nothing about its pairs is checked any more.
func (t *Tester) reportExceptionsWithoutPairs(s *Schema, decl *FamilyDeclaration, earlier []*Schema) error {
	if !decl.Live(s.Key().Version()) {
		return nil
	}
	exceptions, err := t.exceptions(s)
	if err != nil {
		return err
	}
	for _, e := range exceptions {
		switch {
		case decl.Facts.EffectiveAhead() == AheadNeither:
			t.report.AddStaleException(s.Key(), &e, "this family runs no documents across versions")
		case !decl.Live(e.Version):
			t.report.AddStaleException(s.Key(), &e, fmt.Sprintf(
				"%s is below the family's oldestLive %s, so it takes no part in the checks",
				e.Version.String('.'), decl.OldestLive.String('.')))
		case !slices.ContainsFunc(earlier, func(o *Schema) bool { return o.Key().Version() == e.Version }):
			t.report.AddStaleException(s.Key(), &e, fmt.Sprintf("there is no earlier version %s in this major",
				e.Version.String('.')))
		}
	}
	return nil
}

// applyExceptions marks each of the pair's runs which the later version's exception file names, and
// adds to the report each entry for the pair which names none of them.
func (t *Tester) applyExceptions(runs []Spec, earlier, later *Schema, decl *FamilyDeclaration) error {
	exceptions, err := t.exceptions(later)
	if err != nil {
		return err
	}
	for _, e := range exceptions {
		if e.Version != earlier.Key().Version() || e.Change != "" {
			continue
		}
		i := slices.IndexFunc(runs, func(spec Spec) bool { return e.clears(&spec) })
		if i < 0 {
			t.report.AddStaleException(later.Key(), &e, e.whyNoRun(decl.Facts.EffectiveAhead(), later.Key()))
			continue
		}
		runs[i].Exception = &e
	}
	return nil
}

// pairRuns returns the specs which the family's declaration asks of the pair.
func pairRuns(earlier, later *Schema, decl *FamilyDeclaration) ([]Spec, error) {
	var specs []Spec
	for _, run := range acrossVersionRuns {
		if !run.appliesTo(decl.Facts.EffectiveAhead()) {
			continue
		}
		docs, err := run.source(earlier, later).TestDocuments(run.docType)
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			specs = append(specs, run.spec(earlier, later, doc, decl))
		}
	}
	return specs, nil
}

// claimComparison reports whether this run is the first to look at the pair's structural changes.
func (t *Tester) claimComparison(p versionPair) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.comparedPairs[p] {
		return false
	}
	t.comparedPairs[p] = true
	return true
}

// claimPair reports whether this run is the first to take on the pair.
func (t *Tester) claimPair(p versionPair) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ranPairs[p] {
		return false
	}
	t.ranPairs[p] = true
	return true
}

// documentAcrossVersions returns the specs which run one document of s, of the given type, on the
// other versions in its major.
func (t *Tester) documentAcrossVersions(
	s *Schema, decl *FamilyDeclaration, doc TestInfo, tt TestDocType,
) ([]Spec, error) {
	ahead := decl.Facts.EffectiveAhead()
	if ahead == AheadNeither {
		return nil, nil
	}

	earlier, later, err := t.otherVersions(s, decl)
	if err != nil {
		return nil, err
	}

	var specs []Spec
	for _, p := range pairsOf(s, earlier, later) {
		exceptions, eErr := p[1].Exceptions()
		if eErr != nil {
			return nil, eErr
		}
		for _, run := range acrossVersionRuns {
			if !run.appliesTo(ahead) || run.docType != tt || run.source(p[0], p[1]) != s {
				continue
			}
			spec := run.spec(p[0], p[1], doc, decl)
			if i := slices.IndexFunc(exceptions, func(e Exception) bool { return e.clears(&spec) }); i >= 0 {
				spec.Exception = &exceptions[i]
			}
			specs = append(specs, spec)
		}
	}
	return specs, nil
}

// RunsOnLaterVersions runs, for k and each later version in its major, the document runs of the pair
// which the family holds to the given rule, as validate does, whatever their outcome: a run which
// didn't give the right outcome has its Err set, and its Exception if the later version's file names it.
// It is what check-party asks. It leaves a stale exception file entry to validate, whose report it
// doesn't write.
func (t *Tester) RunsOnLaterVersions(ctx context.Context, k Key, rule Rule) ([]Spec, error) {
	s, err := t.registry.GetSchemaByKey(k)
	if err != nil {
		return nil, err
	}
	decl, err := t.registry.FamilyDeclaration(k)
	if err != nil {
		return nil, err
	}
	env := t.registry.config.ProductionEnvConfig()
	if _, err = s.Render(env); err != nil {
		return nil, err
	}
	laterKeys, err := s.MajorFamilyFutureSchemas()
	if err != nil {
		return nil, err
	}
	later, err := t.schemas(laterKeys)
	if err != nil {
		return nil, err
	}

	var ran []Spec
	for _, l := range later {
		runs, pErr := pairRuns(s, l, decl)
		if pErr != nil {
			return nil, pErr
		}
		if eErr := t.applyExceptions(runs, s, l, decl); eErr != nil {
			return nil, eErr
		}
		runs = slices.DeleteFunc(runs, func(spec Spec) bool { return spec.Rule != rule })
		if rErr := runForOutcome(ctx, runs, env); rErr != nil {
			return nil, rErr
		}
		ran = append(ran, runs...)
	}
	return ran, nil
}

// runForOutcome runs each spec on the version it names, leaving the outcome on the spec.
func runForOutcome(ctx context.Context, specs []Spec, env *config.EnvConfig) error {
	for i := range specs {
		if err := ctx.Err(); err != nil {
			return err
		}
		ri, err := specs[i].Schema.Render(env)
		if err != nil {
			return err
		}
		_ = specs[i].Run(ri.Validator)
	}
	return nil
}
