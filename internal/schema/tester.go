package schema

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// ErrStopTesting is a sentinel error used to signal that further tests should be stopped and the report shown.
var ErrStopTesting = errors.New("stopping after first error")

// TestScope describes which test documents are used.
type TestScope string

// NewTestScope creates a new TestScope from a string.
func NewTestScope(s string) (TestScope, error) {
	switch s {
	case "local":
		return TestScopeLocal, nil
	case "pass-only":
		return TestScopePass, nil
	case "fail-only":
		return TestScopeFail, nil
	case "consumer-breaking":
		return TestScopeConsumerBreaking, nil
	case "all":
		return TestScopeAll, nil
	default:
		return "", &InvalidTestScopeError{Scope: s}
	}
}

const (
	// TestScopeLocal selects a schema's local test documents (in the pass and fail folders next it).
	TestScopeLocal TestScope = "local"

	// TestScopePass selects a schema's local passing test documents (in the pass folder next to it).
	TestScopePass TestScope = "pass-only"

	// TestScopeFail selects a schema's local failing test documents (in the fail folder next to it).
	TestScopeFail TestScope = "fail-only"

	// TestScopeConsumerBreaking selects passing test documents for LATER versions of the schema sharing the same
	// major version. This identifies whether supposedly later 'non-breaking' changes actually are breaking.
	TestScopeConsumerBreaking TestScope = "consumer-breaking"

	// TestScopeAll means for each schema in the test, identify all relevant tests to run.
	// This combines TestScopeLocal and TestScopeConsumerBreaking.
	TestScopeAll TestScope = "all"
)

// Tester is the type used to manage a run of tests for one or more schemas.
type Tester struct {
	registry *Registry

	// Test run options
	stopOnFirstError bool
	skipCompatible   bool
	numWorkers       int
	scope            TestScope

	report *TestReport
}

// NewTester creates a new tester for the given registry.
func NewTester(r *Registry) *Tester {
	return &Tester{
		registry:         r,
		report:           NewTestReport(),
		scope:            TestScopeLocal,
		stopOnFirstError: true,
		numWorkers:       runtime.GOMAXPROCS(0),
	}
}

// TestSingleSchema executes tests for a schema identified by a relative or absolute filepath.
// The context can be used to cancel the test run early (e.g., on Ctrl+C).
// After local tests pass, this will also run provider compatibility checks against earlier
// versions in the same major family (unless skipCompatible is set).
func (t *Tester) TestSingleSchema(ctx context.Context, k Key) (*TestReport, error) {
	t.report.StartTime = time.Now()
	defer func() { t.report.EndTime = time.Now() }()

	localFailed, err := t.testSchema(ctx, k)
	if err != nil && !errors.Is(err, ErrStopTesting) {
		return nil, err
	}

	// If local tests had failures, don't run compatibility checks
	if localFailed {
		return t.report, nil
	}

	// Run provider compatibility check against earlier versions
	if !t.skipCompatible {
		if err := t.testSchemaCompatibleWithEarlierVersions(ctx, k); err != nil && !errors.Is(err, ErrStopTesting) {
			return nil, err
		}
	}

	return t.report, nil
}

// TestSpecificDocument executes a single test document for a schema.
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
	ri, err := s.Render(t.registry.config.ProductionEnvConfig())
	if err != nil {
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

	spec := NewSpec(s, ti, tt, nil)
	err = spec.Run(ri.Validator)
	if err != nil {
		t.report.AddFailedTest(k, &spec)
	} else {
		t.report.AddPassedTest(k, &spec)
	}

	return t.report, nil
}

// SetScope controls the testing scope. This adjusts which tests are identified for each specific
// schema. It defaults to TestScopeLocal.
func (t *Tester) SetScope(s TestScope) {
	t.scope = s
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

// SetSkipCompatible controls whether provider compatibility checking is skipped.
// It defaults to false, meaning compatibility checks run after local tests pass.
func (t *Tester) SetSkipCompatible(b bool) {
	t.skipCompatible = b
}

// TestFoundSchemas searches for schemas matching the given search scope, and
// executes tests for each schema found.
// As with TestSingleSchema, each schema whose local tests pass is then checked for provider
// compatibility against earlier versions in its major family (unless skipCompatible is set).
// The consumer-breaking and all scopes already test each schema against its later versions'
// pass documents, which covers the same pairs, so they skip this check to avoid running them twice.
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

		if tErr := t.testFoundSchema(ctx, k); tErr != nil {
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

// testFoundSchema runs a found schema's tests, then its provider compatibility check if that applies.
func (t *Tester) testFoundSchema(ctx context.Context, k Key) error {
	localFailed, err := t.testSchema(ctx, k)
	if err != nil || localFailed {
		return err
	}

	if t.skipCompatible || t.scope == TestScopeConsumerBreaking || t.scope == TestScopeAll {
		return nil
	}

	return t.testSchemaCompatibleWithEarlierVersions(ctx, k)
}

// testSchema identifies the specs to run for the given schema, and executes them.
// It returns when all specs have been run, or when context is cancelled.
// If a test error is encountered, it is added to the report, but the function
// continues to run the remaining tests (or return without error if stopOnFirstError is true).
// An error is only returned if we can't load the schema or identify the specs to run, or if
// we received a context cancellation error.
// The localFailed result reports whether any of this schema's tests failed.
func (t *Tester) testSchema(ctx context.Context, key Key) (localFailed bool, err error) {
	s, err := t.registry.GetSchemaByKey(key)
	if err != nil {
		return false, err
	}

	// Ahead of running the tests, ensure the schema is valid by forcing a render.
	ri, err := s.Render(t.registry.config.ProductionEnvConfig())
	if err != nil {
		return false, err
	}

	specs, err := t.getSpecsForSchema(s)
	if err != nil {
		return false, err
	}

	// execute tests
	for _, spec := range specs {
		if ce := ctx.Err(); ce != nil {
			return localFailed, ce
		}

		err = spec.Run(ri.Validator)

		if err != nil {
			localFailed = true
			t.report.AddFailedTest(key, &spec)
			if t.stopOnFirstError {
				// Signal to the producer to stop other testing activity
				return localFailed, ErrStopTesting
			}
		} else {
			t.report.AddPassedTest(key, &spec)
		}
	}

	return localFailed, nil
}

// getSpecsForSchema identifies the tests to run for the given schema.
func (t *Tester) getSpecsForSchema(s *Schema) ([]Spec, error) {
	var specs []Spec
	var err error

	// Pass Tests?
	if t.scope != TestScopeFail && t.scope != TestScopeConsumerBreaking {
		specs, err = t.appendSpecsByTestDocType(s, TestDocTypePass, false, specs)
		if err != nil {
			return nil, err
		}
	}

	// Fail Tests?
	if t.scope != TestScopePass && t.scope != TestScopeConsumerBreaking {
		specs, err = t.appendSpecsByTestDocType(s, TestDocTypeFail, false, specs)
		if err != nil {
			return nil, err
		}
	}

	// Breaking Tests?
	if t.scope == TestScopeConsumerBreaking || t.scope == TestScopeAll {
		specs, err = t.appendBreakingSpecs(s, specs)
		if err != nil {
			return nil, err
		}
	}

	return specs, nil
}

// appendBreakingSpecs appends to the specs slice the breaking specs which are pass specs
// for schemas which share the same major family version as s, and which are a later version
// in the family.
// E.g. If s is version 1.2.3, and we also have in the family version:
// 1.2.4, 1.3.0, 1.3.1, 2.0.0, then we will append specs which use passing test docs for
// 1.2.4, 1.3.0, 1.3.1, but NOT 2.0.0.
func (t *Tester) appendBreakingSpecs(s *Schema, specs []Spec) ([]Spec, error) {
	fsKeys, err := s.MajorFamilyFutureSchemas()
	if err != nil {
		return nil, err
	}
	for _, fsKey := range fsKeys {
		fs, err2 := t.registry.GetSchemaByKey(fsKey)
		if err2 != nil {
			return nil, err2
		}
		specs, err = t.appendSpecsByTestDocType(fs, TestDocTypePass, true, specs)
		if err != nil {
			return nil, err
		}
	}

	return specs, nil
}

// appendSpecsByTestDocType appends the specs for the given test document type to the given slice,
// optimising allocations where possible.
// If isBreakingTest is true, it indicates that the specs should set the ForwardVersion field, as
// the tests are being appended to test an earlier version in the family sharing the same major version.
func (t *Tester) appendSpecsByTestDocType(
	s *Schema,
	testDocType TestDocType,
	isBreakingTest bool,
	specs []Spec,
) ([]Spec, error) {
	tests, err := s.TestDocuments(testDocType)
	if err != nil {
		return nil, err
	}

	// optimise allocations:
	if newlen := len(specs) + len(tests); cap(specs) < newlen {
		newSpecs := make([]Spec, len(specs), newlen)
		copy(newSpecs, specs)
		specs = newSpecs
	}

	var forwardVersion *SemVer

	if isBreakingTest {
		forwardVersion = &s.core.version
	}

	for _, test := range tests {
		specs = append(specs, NewSpec(s, test, testDocType, forwardVersion))
	}

	return specs, nil
}

// testSchemaCompatibleWithEarlierVersions checks that the target schema's pass tests
// also validate against earlier versions in the same major family.
// This ensures a provider using the new schema won't break consumers on older versions.
func (t *Tester) testSchemaCompatibleWithEarlierVersions(ctx context.Context, targetKey Key) error {
	targetSchema, err := t.registry.GetSchemaByKey(targetKey)
	if err != nil {
		return err
	}

	passTests, err := targetSchema.TestDocuments(TestDocTypePass)
	if err != nil {
		return err
	}

	earlierKeys, err := targetSchema.MajorFamilyEarlierSchemas()
	if err != nil {
		return err
	}

	if len(passTests) == 0 || len(earlierKeys) == 0 {
		return nil
	}

	return t.runCompatibilityTests(ctx, targetSchema, passTests, earlierKeys)
}

func (t *Tester) runCompatibilityTests(
	ctx context.Context,
	targetSchema *Schema,
	passTests []TestInfo,
	earlierKeys []Key,
) error {
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	var wg sync.WaitGroup
	sem := make(chan struct{}, t.numWorkers)

	var finalErr error
	var errOnce sync.Once

	for _, earlierKey := range earlierKeys {
		if stop := t.processCompatibilityTest(runCtx, earlierKey, targetSchema, passTests, &wg, sem, &errOnce,
			&finalErr, cancelRun); stop {
			break
		}
	}

	wg.Wait()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	return finalErr
}

func (t *Tester) processCompatibilityTest(
	ctx context.Context,
	earlierKey Key,
	targetSchema *Schema,
	passTests []TestInfo,
	wg *sync.WaitGroup,
	sem chan struct{},
	errOnce *sync.Once,
	//nolint:gocritic // finalErr updated via pointer
	finalErr *error,
	cancelFunc context.CancelFunc,
) bool {
	select {
	case <-ctx.Done():
		return true
	case sem <- struct{}{}: // Acquire worker slot
	}

	wg.Add(1)
	go func(ek Key) {
		defer wg.Done()
		defer func() { <-sem }()

		if tErr := t.testEarlierSchemaWithTargetTests(ctx, ek, targetSchema, passTests); tErr != nil {
			errOnce.Do(func() {
				if !errors.Is(tErr, ErrStopTesting) {
					*finalErr = tErr
				}
				cancelFunc()
			})
		}
	}(earlierKey)

	return false
}

// testEarlierSchemaWithTargetTests runs the target schema's pass tests against an earlier schema.
func (t *Tester) testEarlierSchemaWithTargetTests(
	ctx context.Context,
	earlierKey Key,
	targetSchema *Schema,
	passTests []TestInfo,
) error {
	earlierSchema, err := t.registry.GetSchemaByKey(earlierKey)
	if err != nil {
		return err
	}

	// Render the earlier schema to get its validator
	ri, err := earlierSchema.Render(t.registry.config.ProductionEnvConfig())
	if err != nil {
		return err
	}

	// The ForwardVersion is set to the target schema's version,
	// as these tests are from a "forward" (newer) version
	forwardVersion := &targetSchema.core.version

	// Run each pass test from the target schema against the earlier schema
	for _, testInfo := range passTests {
		if ce := ctx.Err(); ce != nil {
			return ce
		}

		spec := NewSpec(earlierSchema, testInfo, TestDocTypePass, forwardVersion)
		err = spec.Run(ri.Validator)

		if err != nil {
			t.report.AddFailedTest(earlierKey, &spec)
			if t.stopOnFirstError {
				return ErrStopTesting
			}
		} else {
			t.report.AddPassedTest(earlierKey, &spec)
		}
	}

	return nil
}
