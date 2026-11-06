package schema

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/bitshepherds/json-schema-manager/internal/coverage"
	"github.com/bitshepherds/json-schema-manager/internal/validator"
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
}

// NewTester creates a new tester for the given registry.
func NewTester(r *Registry) *Tester {
	return &Tester{
		registry:         r,
		report:           NewTestReport(),
		stopOnFirstError: true,
		numWorkers:       runtime.GOMAXPROCS(0),
		ranPairs:         make(map[versionPair]bool),
	}
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

	ti, err := NewTestInfo(testPath)
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

	return t.report, finalErr
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

		if tErr := t.testVersion(ctx, k); tErr != nil {
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

// findUnexercisedChanges compares the schema with the version before it in its major family, and
// adds to the report each structural change which may make that version reject documents the
// schema accepts, and which none of the schema's pass documents uses.
func (t *Tester) findUnexercisedChanges(k Key) error {
	s, err := t.registry.GetSchemaByKey(k)
	if err != nil {
		return err
	}
	earlierKeys, err := s.MajorFamilyEarlierSchemas()
	if err != nil || len(earlierKeys) == 0 {
		return err
	}
	// The earlier versions are in version order, so the last is the one before s
	earlierKey := earlierKeys[len(earlierKeys)-1]
	earlier, err := t.registry.GetSchemaByKey(earlierKey)
	if err != nil {
		return err
	}

	changes, err := Diff(earlier, s)
	if err != nil {
		return err
	}
	for _, sc := range changes {
		if sc.Change.MayBreak() && len(sc.UsedBy) == 0 {
			t.report.AddUnexercisedChange(k, &UnexercisedChange{Earlier: earlierKey, Change: sc.Change})
		}
	}
	return nil
}

// inheritedDocuments counts the documents s inherits from the earlier versions in its major.
func (t *Tester) inheritedDocuments(s *Schema, decl *FamilyDeclaration) (pass, fail int, err error) {
	if !decl.Facts.InheritsDocuments() {
		return 0, 0, nil
	}
	earlierKeys, err := s.MajorFamilyEarlierSchemas()
	if err != nil {
		return 0, 0, err
	}
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
	validates := func(pointer string, v any) (bool, bool) {
		sub, cErr := t.registry.compiler.Compile(id + "#" + pointer)
		if cErr != nil {
			return false, false
		}
		return sub.Validate(v) == nil, true
	}
	docs := make([]any, len(passDocs))
	for i := range passDocs {
		docs[i] = passDocs[i].Unmarshalled
	}

	summary := &Summary{
		PassDocs:          len(passDocs),
		FailDocs:          len(failDocs),
		InheritedPassDocs: inheritedPass,
		InheritedFailDocs: inheritedFail,
		Coverage:          coverage.Measure(ri.Unmarshalled, docs, validates),
		FailKeywords:      make(map[string][]string),
	}
	for _, doc := range failDocs {
		if keywords := validator.FailedKeywords(ri.Validator.Validate(doc.Unmarshalled), id); keywords != nil {
			summary.FailKeywords[doc.Path] = keywords
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

	cross, err := t.pairSpecs(s, decl)
	if err != nil {
		return err
	}

	if rErr := t.runSpecs(ctx, append(own, cross...)); rErr != nil {
		return rErr
	}

	return t.findUnexercisedChanges(k)
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

// otherVersions returns the schemas of the other versions in s's major: those before it, and those after it.
func (t *Tester) otherVersions(s *Schema) (earlier, later []*Schema, err error) {
	earlierKeys, err := s.MajorFamilyEarlierSchemas()
	if err != nil {
		return nil, nil, err
	}
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

// pairSpecs returns the specs for the pairs s is part of, leaving out any this run has already taken
// on, so a pair two selected versions share runs once.
func (t *Tester) pairSpecs(s *Schema, decl *FamilyDeclaration) ([]Spec, error) {
	if decl.Facts.EffectiveAhead() == AheadNeither {
		return nil, nil
	}

	earlier, later, err := t.otherVersions(s)
	if err != nil {
		return nil, err
	}

	pairs := make([][2]*Schema, 0, len(earlier)+len(later))
	for _, e := range earlier {
		pairs = append(pairs, [2]*Schema{e, s})
	}
	for _, l := range later {
		pairs = append(pairs, [2]*Schema{s, l})
	}

	var specs []Spec
	for _, p := range pairs {
		if !t.claimPair(versionPair{p[0].Key(), p[1].Key()}) {
			continue
		}
		runs, pErr := pairRuns(p[0], p[1], decl)
		if pErr != nil {
			return nil, pErr
		}
		specs = append(specs, runs...)
	}
	return specs, nil
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

	earlier, later, err := t.otherVersions(s)
	if err != nil {
		return nil, err
	}

	var specs []Spec
	for _, run := range acrossVersionRuns {
		if !run.appliesTo(ahead) || run.docType != tt {
			continue
		}
		if run.fromLater {
			for _, e := range earlier {
				specs = append(specs, run.spec(e, s, doc, decl))
			}
		} else {
			for _, l := range later {
				specs = append(specs, run.spec(s, l, doc, decl))
			}
		}
	}
	return specs, nil
}

// LaterPassRejection is a pass document of a later version which the version under test rejects.
type LaterPassRejection struct {
	Later Key
	Path  string
	Err   error
}

// LaterPassRejections runs the pass documents of every later version in k's major on k, and returns
// those it rejects. It is what check-consumer asks, whatever the family's scenario.
func (t *Tester) LaterPassRejections(ctx context.Context, k Key) ([]LaterPassRejection, error) {
	s, err := t.registry.GetSchemaByKey(k)
	if err != nil {
		return nil, err
	}
	ri, err := s.Render(t.registry.config.ProductionEnvConfig())
	if err != nil {
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

	var rejections []LaterPassRejection
	for _, l := range later {
		docs, dErr := l.TestDocuments(TestDocTypePass)
		if dErr != nil {
			return nil, dErr
		}
		for _, doc := range docs {
			if cErr := ctx.Err(); cErr != nil {
				return nil, cErr
			}
			spec := NewSpec(s, doc, TestDocTypePass)
			if rErr := spec.Run(ri.Validator); rErr != nil {
				rejections = append(rejections, LaterPassRejection{Later: l.Key(), Path: doc.Path, Err: rErr})
			}
		}
	}
	return rejections, nil
}
