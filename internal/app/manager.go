// Package app provides the core application logic for JSM.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/consumer"
	"github.com/bitshepherds/json-schema-manager/internal/repo"
	"github.com/bitshepherds/json-schema-manager/internal/report"
	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

// ErrTestsFailed is returned by ValidateSchema, after the report is written, when any test failed,
// or when strict and the report has warnings.
// The report already describes the failures, so Run does not print it again.
var ErrTestsFailed = errors.New("one or more tests failed")

// Manager defines the business logic for JSON schema operations.
type Manager interface {
	ValidateSchema(ctx context.Context, target schema.ResolvedTarget, verbose bool, format string,
		useColour bool, continueOnError bool, testScope schema.TestScope, skipCompatible bool,
		strict bool) error
	WatchValidation(ctx context.Context, target schema.ResolvedTarget, verbose bool, format string,
		useColour bool, continueOnError bool, testScope schema.TestScope, skipCompatible bool,
		readyChan chan<- struct{}) error
	Registry() *schema.Registry
	CreateSchema(domainAndFamilyName string) (schema.Key, error)
	CreateSchemaVersion(k schema.Key, rt schema.ReleaseType) (schema.Key, error)
	RenderSchema(ctx context.Context, target schema.ResolvedTarget, env config.Env, collapse bool) ([]byte, error)
	DiffSchemas(older, newer schema.Key) ([]schema.Change, error)
	CheckConsumer(ctx context.Context, manifestPath, format string, useColour, strict bool) error
	CheckChanges(ctx context.Context, envName config.Env) error
	TagDeployment(ctx context.Context, envName config.Env, push bool) error
	BuildDist(ctx context.Context, envName config.Env, all, collapse, skipTests bool) error
}

// Ensure the interface is satisfied.
var _ Manager = (*LazyManager)(nil)

// LazyManager acts as a placeholder for a real Manager implementation, allowing
// for deferred initialization of dependencies.
type LazyManager struct {
	inner Manager
}

// SetInner sets the inner manager implementation.
func (l *LazyManager) SetInner(m Manager) {
	l.inner = m
}

// HasInner returns true if the inner manager has been set.
// This is used by PersistentPreRunE to skip initialization if already configured (e.g., in tests).
func (l *LazyManager) HasInner() bool {
	return l.inner != nil
}

func (l *LazyManager) check() Manager {
	if l.inner == nil {
		panic("LazyManager accessed before initialization; check command wiring.")
	}
	return l.inner
}

// ValidateSchema implements the Manager interface.
func (l *LazyManager) ValidateSchema(ctx context.Context, target schema.ResolvedTarget, verbose bool,
	format string, useColour bool, continueOnError bool, testScope schema.TestScope, skipCompatible bool,
	strict bool,
) error {
	return l.check().ValidateSchema(ctx, target, verbose, format, useColour, continueOnError, testScope,
		skipCompatible, strict)
}

// WatchValidation implements the Manager interface.
func (l *LazyManager) WatchValidation(ctx context.Context, target schema.ResolvedTarget, verbose bool,
	format string, useColour bool, continueOnError bool, testScope schema.TestScope, skipCompatible bool,
	readyChan chan<- struct{},
) error {
	return l.check().WatchValidation(ctx, target, verbose, format, useColour, continueOnError,
		testScope, skipCompatible, readyChan)
}

// Registry implements the Manager interface.
func (l *LazyManager) Registry() *schema.Registry {
	return l.check().Registry()
}

// CreateSchema implements the Manager interface.
func (l *LazyManager) CreateSchema(domainAndFamilyName string) (schema.Key, error) {
	return l.check().CreateSchema(domainAndFamilyName)
}

// CreateSchemaVersion implements the Manager interface.
func (l *LazyManager) CreateSchemaVersion(k schema.Key, rt schema.ReleaseType) (schema.Key, error) {
	return l.check().CreateSchemaVersion(k, rt)
}

// RenderSchema implements the Manager interface.
func (l *LazyManager) RenderSchema(
	ctx context.Context,
	target schema.ResolvedTarget,
	env config.Env,
	collapse bool,
) ([]byte, error) {
	return l.check().RenderSchema(ctx, target, env, collapse)
}

// DiffSchemas implements the Manager interface.
func (l *LazyManager) DiffSchemas(older, newer schema.Key) ([]schema.Change, error) {
	return l.check().DiffSchemas(older, newer)
}

// CheckConsumer implements the Manager interface.
func (l *LazyManager) CheckConsumer(ctx context.Context, manifestPath, format string, useColour, strict bool) error {
	return l.check().CheckConsumer(ctx, manifestPath, format, useColour, strict)
}

// CheckChanges implements the Manager interface.
func (l *LazyManager) CheckChanges(ctx context.Context, envName config.Env) error {
	return l.check().CheckChanges(ctx, envName)
}

// TagDeployment implements the Manager interface.
func (l *LazyManager) TagDeployment(ctx context.Context, envName config.Env, push bool) error {
	return l.check().TagDeployment(ctx, envName, push)
}

// BuildDist implements the Manager interface.
func (l *LazyManager) BuildDist(ctx context.Context, envName config.Env, all, collapse, skipTests bool) error {
	return l.check().BuildDist(ctx, envName, all, collapse, skipTests)
}

// Ensure the interface is satisfied.
var _ Manager = (*CLIManager)(nil)

// CLIManager is the concrete implementation of the Manager interface.
type CLIManager struct {
	logger         *slog.Logger
	registry       *schema.Registry
	tester         *schema.Tester
	gitter         repo.Gitter
	distBuilder    schema.DistBuilder
	reporterWriter io.Writer
}

// NewCLIManager creates a new concrete Manager implementation.
func NewCLIManager(
	l *slog.Logger,
	r *schema.Registry,
	t *schema.Tester,
	g repo.Gitter,
	db schema.DistBuilder,
	rw io.Writer,
) *CLIManager {
	return &CLIManager{
		logger:         l,
		registry:       r,
		tester:         t,
		gitter:         g,
		distBuilder:    db,
		reporterWriter: rw,
	}
}

// Registry returns the underlying schema registry.
func (m *CLIManager) Registry() *schema.Registry {
	return m.registry
}

// CreateSchema creates a new schema with the given name.
func (m *CLIManager) CreateSchema(domainAndFamilyName string) (schema.Key, error) {
	s, err := m.registry.CreateSchema(domainAndFamilyName)
	if err != nil {
		return schema.Key(""), err
	}
	return s.Key(), nil
}

// CreateSchemaVersion increments the version of an existing schema.
func (m *CLIManager) CreateSchemaVersion(k schema.Key, rt schema.ReleaseType) (schema.Key, error) {
	m.logger.Debug("creating schema version", "key", k, "releaseType", rt)
	s, err := m.registry.CreateSchemaVersion(k, rt)
	if err != nil {
		return schema.Key(""), err
	}
	return s.Key(), nil
}

// ValidateSchema runs tests on the target schema(s).
func (m *CLIManager) ValidateSchema(ctx context.Context, target schema.ResolvedTarget, verbose bool,
	format string, useColour bool, continueOnError bool, testScope schema.TestScope, skipCompatible bool,
	strict bool,
) error {
	m.logger.Debug("validating schema", "target", target, "verbose", verbose, "format", format,
		"useColour", useColour, "continueOnError", continueOnError, "skipCompatible", skipCompatible,
		"strict", strict)

	m.tester.SetStopOnFirstError(!continueOnError)
	m.tester.SetScope(testScope)
	m.tester.SetSkipCompatible(skipCompatible)

	var tr *schema.TestReport
	var err error

	switch {
	case target.Key != nil:
		tr, err = m.tester.TestSingleSchema(ctx, *target.Key)
	case target.Scope != nil:
		tr, err = m.tester.TestFoundSchemas(ctx, *target.Scope)
	default:
		return &schema.NoSchemaTargetsError{}
	}

	if err != nil {
		return err
	}

	var reporter schema.Reporter
	switch format {
	case "json":
		reporter = &report.JSONReporter{}
	default:
		reporter = &report.TextReporter{Verbose: verbose, UseColour: useColour}
	}

	if wErr := reporter.Write(m.reporterWriter, tr); wErr != nil {
		return wErr
	}

	warned := len(tr.UnexercisedChanges) > 0 || tr.MissingDocuments() > 0
	if len(tr.FailedTests) > 0 || strict && warned {
		return ErrTestsFailed
	}

	return nil
}

// WatchValidation watches for changes in the registry and triggers validation.
// If you want to know when the watcher is ready to start listening to changes,
// pass a non-nil readyChan to be notified.
func (m *CLIManager) WatchValidation(ctx context.Context, target schema.ResolvedTarget, verbose bool,
	format string, useColour bool, continueOnError bool, testScope schema.TestScope, skipCompatible bool,
	readyChan chan<- struct{},
) error {
	m.logger.Debug("watching validation", "target", target, "verbose", verbose, "format", format,
		"useColour", useColour, "continueOnError", continueOnError, "skipCompatible", skipCompatible)

	if target.Key == nil && target.Scope == nil {
		return &schema.NoSchemaTargetsError{}
	}

	watcher := schema.NewWatcher(m.registry, m.logger)

	callback := func(event schema.WatchEvent) {
		m.handleWatchEvent(ctx, event, target, verbose, format, useColour, continueOnError, testScope, skipCompatible)
	}

	// Forward watcher Ready signal if caller wants notification
	if readyChan != nil {
		go func() {
			<-watcher.Ready
			readyChan <- struct{}{}
		}()
	}

	return watcher.Watch(ctx, target, callback)
}

func (m *CLIManager) handleWatchEvent(ctx context.Context, event schema.WatchEvent, target schema.ResolvedTarget,
	verbose bool, format string, useColour bool, continueOnError bool,
	testScope schema.TestScope, skipCompatible bool,
) {
	// Filter events based on target
	if target.Key != nil && event.Key != *target.Key {
		return
	}
	if target.Scope != nil && !event.Key.InScope(*target.Scope) {
		return
	}

	// Create a new tester for each event to ensure fresh reporting
	tester := schema.NewTester(m.registry)
	tester.SetStopOnFirstError(!continueOnError)
	tester.SetScope(testScope)
	tester.SetSkipCompatible(skipCompatible)

	var tr *schema.TestReport
	var err error

	if event.TestPath != "" {
		m.logger.Info("Test changed:", "schema", event.Key, "test", event.TestPath)
		tr, err = tester.TestSpecificDocument(ctx, event.Key, event.TestPath)
	} else {
		m.logger.Info("Schema changed:", "schema", event.Key)
		m.registry.Reset()
		tr, err = tester.TestSingleSchema(ctx, event.Key)
	}

	if err != nil {
		m.logger.Error("Validation failed", "error", err)
		return
	}

	var reporter schema.Reporter
	switch format {
	case "json":
		reporter = &report.JSONReporter{}
	default:
		reporter = &report.TextReporter{Verbose: verbose, UseColour: useColour}
	}

	if rErr := reporter.Write(m.reporterWriter, tr); rErr != nil {
		m.logger.Error("Failed to write report", "error", rErr)
	}
}

// RenderSchema renders a schema for a specific environment.
func (m *CLIManager) RenderSchema(
	_ context.Context,
	target schema.ResolvedTarget,
	env config.Env,
	collapse bool,
) ([]byte, error) {
	m.logger.Debug("rendering schema", "target", target, "env", env, "collapse", collapse)

	if target.Key == nil {
		return nil, &schema.NoSchemaTargetsError{}
	}

	cfg, err := m.registry.Config()
	if err != nil {
		return nil, err
	}

	var envCfg *config.EnvConfig
	if env == "" {
		envCfg = cfg.ProductionEnvConfig()
	} else {
		var envErr error
		envCfg, envErr = cfg.EnvConfig(env)
		if envErr != nil {
			// Introspect environments to provide a helpful error message
			var validEnvs []string
			for e := range cfg.Environments {
				validEnvs = append(validEnvs, string(e))
			}
			slices.Sort(validEnvs)
			return nil, fmt.Errorf("invalid environment: '%s'. Valid environments are: '%s'",
				env, strings.Join(validEnvs, "', '"))
		}
	}

	s, err := m.registry.GetSchemaByKey(*target.Key)
	if err != nil {
		return nil, err
	}

	ri, err := m.registry.CoordinateRender(s, envCfg)
	if err != nil {
		return nil, err
	}

	if collapse {
		inliner := schema.NewInliner(m.registry, envCfg)
		collapsed, cErr := inliner.Collapse(ri.Rendered)
		if cErr != nil {
			return nil, cErr
		}
		if vErr := inliner.Verify(s, ri.Validator, collapsed); vErr != nil {
			return nil, vErr
		}
		return collapsed, nil
	}

	return ri.Rendered, nil
}

// DiffSchemas lists the structural changes from the schema older to the schema newer, as rendered
// for production.
func (m *CLIManager) DiffSchemas(older, newer schema.Key) ([]schema.Change, error) {
	m.logger.Debug("comparing schemas", "older", older, "newer", newer)

	os, err := m.registry.GetSchemaByKey(older)
	if err != nil {
		return nil, err
	}
	ns, err := m.registry.GetSchemaByKey(newer)
	if err != nil {
		return nil, err
	}
	return schema.Diff(os, ns)
}

// CheckConsumer checks the schema versions in the consumer manifest at manifestPath against the
// registry, and writes the result. It returns ErrTestsFailed if any check failed, or if strict and
// any gave a warning.
func (m *CLIManager) CheckConsumer(ctx context.Context, manifestPath, format string, useColour, strict bool) error {
	m.logger.Debug("checking consumer", "manifest", manifestPath, "format", format, "strict", strict)

	manifest, err := consumer.LoadManifest(manifestPath)
	if err != nil {
		return err
	}
	res, err := consumer.Check(ctx, m.registry, manifest)
	if err != nil {
		return err
	}

	if format == formatJSON {
		err = (&report.JSONReporter{}).WriteConsumerCheck(m.reporterWriter, res)
	} else {
		err = (&report.TextReporter{UseColour: useColour}).WriteConsumerCheck(m.reporterWriter, res)
	}
	if err != nil {
		return err
	}

	if res.Count(consumer.Fail) > 0 || strict && res.Count(consumer.Warn) > 0 {
		return ErrTestsFailed
	}
	return nil
}

// CheckChanges determines whether there are any changes to previously-deployed schemas for an environment which
// does not permit schema mutation. If so, it returns an error.
func (m *CLIManager) CheckChanges(ctx context.Context, envName config.Env) error {
	m.logger.Debug("checking changes", "env", envName)

	cfg, err := m.registry.Config()
	if err != nil {
		return err
	}

	envCfg, err := cfg.EnvConfig(envName)
	if err != nil {
		return err
	}

	anchor, err := m.gitter.GetLatestAnchor(ctx, envName)
	if err != nil {
		return err
	}
	if anchor.IsEmptyTree() {
		_, _ = fmt.Fprintf(m.reporterWriter, "No deployment to %s found: treating every schema git tracks as new. "+
			"If %s has been deployed, fetch the %s/%s/* tags.\n", envName, envName, repo.JSMDeployTagPrefix, envName)
	}

	// Every changed file in the registry, to cover test documents and the config file too
	changes, err := m.gitter.GetSchemaChanges(ctx, anchor, m.registry.RootDirectory(), "")
	if err != nil {
		return err
	}

	if !envCfg.AllowSchemaMutation {
		urlRoots, uErr := m.changedURLRoots(ctx, anchor, envCfg, changes)
		if uErr != nil {
			return uErr
		}

		if modifiedPaths := deployedMutations(changes); len(modifiedPaths) > 0 || len(urlRoots) > 0 {
			return &schema.ChangedDeployedSchemasError{Paths: modifiedPaths, URLRoots: urlRoots}
		}
	}

	_, _ = fmt.Fprintln(m.reporterWriter, "All changes are valid")
	return nil
}

// deployedMutations returns the paths of the deployed schema files and test documents which the
// changes modify or delete. New schemas and test documents may be added, and a whole version,
// including its schema file, may be deleted.
func deployedMutations(changes []repo.Change) []string {
	deletedVersions := make(map[string]bool)
	for _, c := range changes {
		if c.IsDeleted && strings.HasSuffix(c.Path, schema.SchemaSuffix) {
			deletedVersions[filepath.Dir(c.Path)] = true
		}
	}

	var paths []string
	for _, c := range changes {
		if isDeployedMutation(c, deletedVersions) {
			paths = append(paths, c.Path)
		}
	}
	return paths
}

// isDeployedMutation reports whether c modifies or deletes a deployed schema file or test document,
// other than by deleting a whole version, whose home directory is in deletedVersions.
func isDeployedMutation(c repo.Change, deletedVersions map[string]bool) bool {
	if c.IsNew {
		return false
	}
	if strings.HasSuffix(c.Path, schema.SchemaSuffix) {
		return !c.IsDeleted
	}

	docDir := filepath.Dir(c.Path)
	docType := schema.TestDocType(filepath.Base(docDir))
	if filepath.Ext(c.Path) != ".json" || (docType != schema.TestDocTypePass && docType != schema.TestDocTypeFail) {
		return false
	}
	return !c.IsDeleted || !deletedVersions[filepath.Dir(docDir)]
}

// changedURLRoots describes each change to envCfg's URL roots since the anchor, which would change
// the IDs of the schemas deployed to it.
func (m *CLIManager) changedURLRoots(
	ctx context.Context,
	anchor repo.Revision,
	envCfg *config.EnvConfig,
	changes []repo.Change,
) ([]string, error) {
	i := slices.IndexFunc(changes, func(c repo.Change) bool {
		return !c.IsNew && !c.IsDeleted && filepath.Base(c.Path) == config.JsmRegistryConfigFile
	})
	if i < 0 {
		return nil, nil
	}

	data, err := m.gitter.ShowFile(ctx, anchor, changes[i].Path)
	if err != nil {
		return nil, err
	}
	deployed, err := config.Parse(data)
	if err != nil {
		return nil, err
	}
	was := deployed.Environments[envCfg.Env]
	if was == nil {
		// The environment was added since the anchor
		return nil, nil
	}

	var roots []string
	for _, r := range []struct{ name, was, is string }{
		{"privateUrlRoot", was.PrivateURLRoot, envCfg.PrivateURLRoot},
		{"publicUrlRoot", was.PublicURLRoot, envCfg.PublicURLRoot},
	} {
		if r.was != r.is {
			roots = append(roots, fmt.Sprintf("%s of %s changed from %q to %q", r.name, envCfg.Env, r.was, r.is))
		}
	}
	return roots, nil
}

// TagDeployment ensures that a successful deployment is tagged in git.
func (m *CLIManager) TagDeployment(ctx context.Context, envName config.Env, push bool) error {
	m.logger.Debug("tagging deployment", "env", envName, "push", push)

	cfg, err := m.registry.Config()
	if err != nil {
		return err
	}

	if _, envErr := cfg.EnvConfig(envName); envErr != nil {
		return envErr
	}

	tagName, err := m.gitter.TagDeploymentSuccess(ctx, envName, push)
	if err != nil {
		if tagName != "" {
			// The next check-changes run looks for the tag on origin, so an unpushed tag is a failed deployment record
			return fmt.Errorf("tag %s was created locally but could not be pushed: %w. "+
				"Push it with 'git push origin %s'", tagName, err, tagName)
		}
		return err
	}

	if !push {
		_, _ = fmt.Fprintf(m.reporterWriter, "🏷️  Tagged deployment locally (not pushed): %s\n", tagName)
		return nil
	}

	_, _ = fmt.Fprintf(m.reporterWriter, "🏷️  Successfully tagged and pushed deployment: %s\n", tagName)
	return nil
}

// BuildDist builds a distribution directory for the given environment.
// Unless skipTests is set, it first runs every schema's tests and compatibility check, as
// validate all does, and builds nothing if any fail.
func (m *CLIManager) BuildDist(ctx context.Context, envName config.Env, all, collapse, skipTests bool) error {
	m.logger.Debug("building distribution", "env", envName, "all", all, "collapse", collapse,
		"skipTests", skipTests)

	var count int
	var err error
	if all {
		if !skipTests {
			if tErr := m.testBeforeBuild(ctx); tErr != nil {
				return tErr
			}
		}
		count, err = m.distBuilder.BuildAll(ctx, envName, collapse)
	} else {
		count, err = m.buildChanged(ctx, envName, collapse, skipTests)
	}

	if err != nil {
		return err
	}

	if count == 0 {
		_, _ = fmt.Fprintln(m.reporterWriter, "No schemas to build")
	} else {
		_, _ = fmt.Fprintf(m.reporterWriter, "📂 Successfully built %d schemas to distribution directory\n", count)
	}

	if all {
		return nil
	}
	return m.reportUntrackedSchemas(ctx)
}

// buildChanged checks that no deployed schema has changed, runs the tests unless skipTests, then
// builds the schemas changed since the last deployment, and returns how many it built.
func (m *CLIManager) buildChanged(ctx context.Context, envName config.Env, collapse, skipTests bool) (int, error) {
	if err := m.CheckChanges(ctx, envName); err != nil {
		return 0, err
	}

	if !skipTests {
		if err := m.testBeforeBuild(ctx); err != nil {
			return 0, err
		}
	}

	anchor, err := m.gitter.GetLatestAnchor(ctx, envName)
	if err != nil {
		return 0, err
	}

	return m.distBuilder.BuildChanged(ctx, envName, anchor, collapse)
}

// reportUntrackedSchemas lists the registry's schemas which git does not track. An incremental build
// finds the schemas to build with git, so it leaves them out.
func (m *CLIManager) reportUntrackedSchemas(ctx context.Context) error {
	rootDir := m.registry.RootDirectory()
	paths, err := m.gitter.UntrackedFiles(ctx, rootDir, schema.SchemaSuffix)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return nil
	}

	// git gives paths with symlinks resolved, so resolve the root too
	if resolved, rErr := filepath.EvalSymlinks(rootDir); rErr == nil {
		rootDir = resolved
	}
	_, _ = fmt.Fprintln(m.reporterWriter, "Not built, because git does not track them yet:")
	for _, p := range paths {
		if rel, rErr := filepath.Rel(rootDir, p); rErr == nil {
			p = rel
		}
		_, _ = fmt.Fprintf(m.reporterWriter, "  %s\n", p)
	}
	_, _ = fmt.Fprintln(m.reporterWriter, "Commit them to include them, or use --all to build every schema "+
		"in the registry.")
	return nil
}

// testBeforeBuild runs every schema's tests and compatibility check. If any fail, it writes
// the test report and returns ErrTestsFailed.
func (m *CLIManager) testBeforeBuild(ctx context.Context) error {
	m.tester.SetStopOnFirstError(false)
	m.tester.SetScope(schema.TestScopeLocal)
	m.tester.SetSkipCompatible(false)

	tr, err := m.tester.TestFoundSchemas(ctx, "")
	if err != nil {
		return err
	}
	if len(tr.FailedTests) == 0 {
		return nil
	}

	if wErr := (&report.TextReporter{}).Write(m.reporterWriter, tr); wErr != nil {
		return wErr
	}
	_, _ = fmt.Fprintln(m.reporterWriter, "Not building: fix the failing tests, or use --skip-tests")
	return ErrTestsFailed
}
