// Package app provides the core application logic for JSM.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
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
		useColour bool, continueOnError bool, strict bool) error
	WatchValidation(ctx context.Context, target schema.ResolvedTarget, verbose bool, format string,
		useColour bool, continueOnError bool, strict bool, readyChan chan<- struct{}) error
	Registry() *schema.Registry
	CreateSchema(domainAndFamilyName string, spec schema.FamilySpec) (schema.Key, error)
	CreateSchemaVersion(k schema.Key, rt schema.ReleaseType) (schema.Key, error)
	RenderSchema(ctx context.Context, target schema.ResolvedTarget, env config.Env, collapse bool) ([]byte, error)
	DiffSchemas(older, newer schema.Key) ([]schema.Change, error)
	CheckConsumer(ctx context.Context, manifestPath, format string, useColour, strict bool) error
	CheckChanges(ctx context.Context, envName config.Env) error
	TagDeployment(ctx context.Context, envName config.Env, push bool) error
	BuildDist(ctx context.Context, envName config.Env, all, collapse, skipTests, strict bool) error
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
	format string, useColour bool, continueOnError bool, strict bool,
) error {
	return l.check().ValidateSchema(ctx, target, verbose, format, useColour, continueOnError, strict)
}

// WatchValidation implements the Manager interface.
func (l *LazyManager) WatchValidation(ctx context.Context, target schema.ResolvedTarget, verbose bool,
	format string, useColour bool, continueOnError bool, strict bool, readyChan chan<- struct{},
) error {
	return l.check().WatchValidation(ctx, target, verbose, format, useColour, continueOnError, strict, readyChan)
}

// Registry implements the Manager interface.
func (l *LazyManager) Registry() *schema.Registry {
	return l.check().Registry()
}

// CreateSchema implements the Manager interface.
func (l *LazyManager) CreateSchema(domainAndFamilyName string, spec schema.FamilySpec) (schema.Key, error) {
	return l.check().CreateSchema(domainAndFamilyName, spec)
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
func (l *LazyManager) BuildDist(ctx context.Context, envName config.Env, all, collapse, skipTests, strict bool) error {
	return l.check().BuildDist(ctx, envName, all, collapse, skipTests, strict)
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
func (m *CLIManager) CreateSchema(domainAndFamilyName string, spec schema.FamilySpec) (schema.Key, error) {
	s, err := m.registry.CreateSchema(domainAndFamilyName, spec)
	if err != nil {
		return schema.Key(""), err
	}
	return s.Key(), nil
}

// CreateSchemaVersion increments the version of an existing schema.
func (m *CLIManager) CreateSchemaVersion(k schema.Key, rt schema.ReleaseType) (schema.Key, error) {
	m.logger.Debug("creating schema version", "key", k, "releaseType", rt)
	if err := m.registry.RequireFamilyDeclared(k); err != nil {
		return schema.Key(""), err
	}
	s, err := m.registry.CreateSchemaVersion(k, rt)
	if err != nil {
		return schema.Key(""), err
	}
	return s.Key(), nil
}

// ValidateSchema runs tests on the target schema(s).
func (m *CLIManager) ValidateSchema(ctx context.Context, target schema.ResolvedTarget, verbose bool,
	format string, useColour bool, continueOnError bool, strict bool,
) error {
	m.logger.Debug("validating schema", "target", target, "verbose", verbose, "format", format,
		"useColour", useColour, "continueOnError", continueOnError, "strict", strict)

	if err := m.requireDeclarations(ctx, target); err != nil {
		return err
	}

	m.tester.SetStopOnFirstError(!continueOnError)

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

	if wErr := newReporter(format, verbose, useColour, strict).Write(m.reporterWriter, tr); wErr != nil {
		return wErr
	}

	if tr.HasFailures() || strict && tr.HasWarnings() {
		return ErrTestsFailed
	}

	return nil
}

// requireDeclarations returns an error if a family in the target has no valid declaration.
func (m *CLIManager) requireDeclarations(ctx context.Context, target schema.ResolvedTarget) error {
	switch {
	case target.Key != nil:
		return m.registry.RequireFamilyDeclared(*target.Key)
	case target.Scope != nil:
		return m.registry.RequireFamilyDeclarations(ctx, *target.Scope)
	default:
		return nil
	}
}

// newReporter returns the reporter for the given output format.
func newReporter(format string, verbose, useColour, strict bool) schema.Reporter {
	switch format {
	case formatJSON:
		return &report.JSONReporter{}
	case formatJUnit:
		return &report.JUnitReporter{Strict: strict}
	default:
		return &report.TextReporter{Verbose: verbose, UseColour: useColour}
	}
}

// WatchValidation watches for changes in the registry and triggers validation.
// If you want to know when the watcher is ready to start listening to changes,
// pass a non-nil readyChan to be notified.
func (m *CLIManager) WatchValidation(ctx context.Context, target schema.ResolvedTarget, verbose bool,
	format string, useColour bool, continueOnError bool, strict bool, readyChan chan<- struct{},
) error {
	m.logger.Debug("watching validation", "target", target, "verbose", verbose, "format", format,
		"useColour", useColour, "continueOnError", continueOnError, "strict", strict)

	if target.Key == nil && target.Scope == nil {
		return &schema.NoSchemaTargetsError{}
	}
	if err := m.requireDeclarations(ctx, target); err != nil {
		return err
	}

	watcher := schema.NewWatcher(m.registry, m.logger)

	callback := func(event schema.WatchEvent) {
		m.handleWatchEvent(ctx, event, target, verbose, format, useColour, continueOnError, strict)
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
	verbose bool, format string, useColour bool, continueOnError bool, strict bool,
) {
	run := watchRunFor(event, target)
	if run == nil {
		return
	}

	// Create a new tester for each event to ensure fresh reporting
	tester := schema.NewTester(m.registry)
	tester.SetStopOnFirstError(!continueOnError)

	tr, err := m.runForWatch(ctx, tester, event, *run)
	if err != nil {
		m.logger.Error("Validation failed", "error", err)
		return
	}

	if rErr := newReporter(format, verbose, useColour, strict).Write(m.reporterWriter, tr); rErr != nil {
		m.logger.Error("Failed to write report", "error", rErr)
	}
	if strict && tr.HasWarnings() {
		m.logger.Error("Validation failed: there are warnings, and --strict is set")
	}
}

// watchRunFor returns what to test for a change, or nil if the target doesn't depend on what changed:
// a whole family, the key of the version watched, or the version changed. A watched version depends on
// its family's declaration and on the other versions in its major, as its pairs include them.
func watchRunFor(event schema.WatchEvent, target schema.ResolvedTarget) *schema.ResolvedTarget {
	switch {
	case target.Key != nil && event.Family != "":
		if target.Key.InScope(event.Family) {
			return &schema.ResolvedTarget{Key: target.Key}
		}
	case target.Key != nil:
		if event.Key.SameMajorFamily(*target.Key) {
			return &schema.ResolvedTarget{Key: target.Key}
		}
	case target.Scope != nil && event.Family != "":
		if target.Scope.Contains(event.Family) {
			return &schema.ResolvedTarget{Scope: &event.Family}
		}
	case target.Scope != nil:
		if event.Key.InScope(*target.Scope) {
			return &schema.ResolvedTarget{Key: &event.Key}
		}
	}
	return nil
}

// runForWatch tests what watchRunFor chose. A changed document of the version being tested runs just
// that document; anything else may have changed what every run gives, so reloads the registry first.
func (m *CLIManager) runForWatch(ctx context.Context, tester *schema.Tester, event schema.WatchEvent,
	run schema.ResolvedTarget,
) (*schema.TestReport, error) {
	if err := m.requireDeclarations(ctx, run); err != nil {
		return nil, err
	}

	if event.TestPath != "" && run.Key != nil && event.Key == *run.Key {
		m.logger.Info("Test changed:", "schema", event.Key, "test", event.TestPath)
		return tester.TestSpecificDocument(ctx, event.Key, event.TestPath)
	}

	m.registry.Reset()
	if run.Scope != nil {
		m.logger.Info("Declaration changed:", "family", *run.Scope)
		return tester.TestFoundSchemas(ctx, *run.Scope)
	}
	m.logger.Info("Schema changed:", "schema", *run.Key)
	return tester.TestSingleSchema(ctx, *run.Key)
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

	envCfg, err := renderEnvConfig(cfg, env)
	if err != nil {
		return nil, err
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

// renderEnvConfig returns the config of the environment to render for: the given one, or production if none
// is given. An unknown environment's error lists the valid ones.
func renderEnvConfig(cfg *config.Config, env config.Env) (*config.EnvConfig, error) {
	if env == "" {
		return cfg.ProductionEnvConfig(), nil
	}

	envCfg, err := cfg.EnvConfig(env)
	if err != nil {
		var validEnvs []string
		for e := range cfg.Environments {
			validEnvs = append(validEnvs, string(e))
		}
		slices.Sort(validEnvs)
		return nil, fmt.Errorf("invalid environment: '%s'. Valid environments are: '%s'",
			env, strings.Join(validEnvs, "', '"))
	}
	return envCfg, nil
}

// DiffSchemas lists the structural changes from the schema older to the schema newer, as rendered
// for production.
func (m *CLIManager) DiffSchemas(older, newer schema.Key) ([]schema.Change, error) {
	m.logger.Debug("comparing schemas", "older", older, "newer", newer)

	olderSchema, err := m.registry.GetSchemaByKey(older)
	if err != nil {
		return nil, err
	}
	newerSchema, err := m.registry.GetSchemaByKey(newer)
	if err != nil {
		return nil, err
	}
	for _, k := range []schema.Key{older, newer} {
		if dErr := m.registry.RequireFamilyDeclared(k); dErr != nil {
			return nil, dErr
		}
	}
	return schema.Diff(olderSchema, newerSchema)
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
		if mErr := m.checkNoDeployedChange(ctx, anchor, envCfg, changes); mErr != nil {
			return mErr
		}
	}

	if dErr := m.registry.RequireFamilyDeclarations(ctx, ""); dErr != nil {
		return dErr
	}

	_, _ = fmt.Fprintln(m.reporterWriter, "All changes are valid")
	return nil
}

// checkNoDeployedChange returns an error describing each of the changes which a deployment fixes in place.
func (m *CLIManager) checkNoDeployedChange(
	ctx context.Context,
	anchor repo.Revision,
	envCfg *config.EnvConfig,
	changes []repo.Change,
) error {
	urlRoots, err := m.changedURLRoots(ctx, anchor, envCfg, changes)
	if err != nil {
		return err
	}
	declarations, err := m.changedDeclarations(ctx, anchor, changes)
	if err != nil {
		return err
	}
	exceptions, err := m.changedExceptions(ctx, anchor, changes)
	if err != nil {
		return err
	}

	modifiedPaths := deployedMutations(changes)
	if len(modifiedPaths) > 0 || len(urlRoots) > 0 || len(declarations) > 0 || len(exceptions) > 0 {
		return &schema.ChangedDeployedSchemasError{
			Paths: modifiedPaths, URLRoots: urlRoots, Declarations: declarations, Exceptions: exceptions,
		}
	}
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

// changedDeclarations describes each change since the anchor to the declaration of a family with a
// deployed version. A family with no declaration at the anchor may gain one, and a declaration may go
// along with the whole family. Whether the new declaration is valid is checked on its own, so an
// invalid one isn't reported here. One which was invalid at the anchor declared nothing to protect.
func (m *CLIManager) changedDeclarations(
	ctx context.Context,
	anchor repo.Revision,
	changes []repo.Change,
) ([]string, error) {
	var refused []string
	for _, c := range changes {
		if c.IsNew || filepath.Base(c.Path) != schema.FamilyDeclarationFile {
			continue
		}
		familyDir := filepath.Dir(c.Path)
		deployed, err := m.gitter.ListFiles(ctx, anchor, familyDir, schema.SchemaSuffix)
		if err != nil {
			return nil, err
		}
		if !hasVersion(familyDir, deployed) {
			continue
		}

		reason, err := m.declarationChange(ctx, anchor, c.Path)
		if err != nil {
			return nil, err
		}
		if reason != "" {
			refused = append(refused, fmt.Sprintf("%s: %s", c.Path, reason))
		}
	}
	return refused, nil
}

// hasVersion reports whether any of the schema files is a version of the family in familyDir, at
// <major>/<minor>/<patch>/<schema file>.
func hasVersion(familyDir string, schemaFiles []string) bool {
	return slices.ContainsFunc(schemaFiles, func(p string) bool {
		rel := strings.TrimPrefix(p, familyDir+string(filepath.Separator))
		return strings.Count(rel, string(filepath.Separator)) == 3
	})
}

// declarationChange says why the declaration at path may not differ from the one at the anchor, or
// returns "" if it may.
func (m *CLIManager) declarationChange(ctx context.Context, anchor repo.Revision, path string) (string, error) {
	//nolint:gosec // The path comes from git, within the registry
	current, err := os.ReadFile(path)
	if err != nil {
		// git can report a removed declaration as a rename, so go by what's there, not by its status
		if familyHasVersions(filepath.Dir(path)) {
			return "it cannot be removed while the family has versions", nil
		}
		return "", nil
	}

	data, err := m.gitter.ShowFile(ctx, anchor, path)
	if err != nil {
		return "", err
	}
	was, err := schema.ParseFamilyDeclaration(data)
	if err != nil {
		return "", nil //nolint:nilerr // see changedDeclarations
	}
	is, err := schema.ParseFamilyDeclaration(current)
	if err != nil {
		return "", nil //nolint:nilerr // see changedDeclarations
	}
	return is.ChangeFrom(was), nil
}

// familyHasVersions reports whether any schema file remains in the family's directory.
func familyHasVersions(familyDir string) bool {
	matches, _ := filepath.Glob(filepath.Join(familyDir, "*", "*", "*", "*"+schema.SchemaSuffix))
	return len(matches) > 0
}

// changedExceptions describes each change since the anchor to an exception file which was there at the
// anchor. A file may be added, and entries added to it, as that's the way out of a check which fails on
// deployed versions, but what it said at the anchor is locked. A file goes along with its whole version,
// an entry may go once what it names has gone, and a file invalid at the anchor had nothing to protect.
func (m *CLIManager) changedExceptions(
	ctx context.Context,
	anchor repo.Revision,
	changes []repo.Change,
) ([]string, error) {
	var refused []string
	for _, c := range changes {
		if c.IsNew || filepath.Base(c.Path) != schema.ExceptionsFile {
			continue
		}
		data, err := m.gitter.ShowFile(ctx, anchor, c.Path)
		if err != nil {
			return nil, err
		}
		deployed, err := schema.ParseExceptions(data)
		if err != nil {
			continue
		}
		refused = append(refused, lockedExceptionChanges(c.Path, deployed)...)
	}
	return refused, nil
}

// lockedExceptionChanges describes each of the deployed exceptions which the file at path no longer
// holds as it was.
func lockedExceptionChanges(path string, deployed []schema.Exception) []string {
	versionDir := filepath.Dir(path)
	// git can report a removed file as a rename, so go by what's there, not by its status
	//nolint:gosec // The path comes from git, within the registry
	data, err := os.ReadFile(path)
	if err != nil {
		matches, _ := filepath.Glob(filepath.Join(versionDir, "*"+schema.SchemaSuffix))
		if len(matches) == 0 {
			return nil
		}
		data = nil
	}
	current, err := schema.ParseExceptions(data)
	if err != nil {
		return []string{fmt.Sprintf("%s: it is no longer valid: %s", path, err)}
	}

	var refused []string
	for _, e := range deployed {
		if !slices.Contains(current, e) && !e.TargetGone(versionDir) {
			refused = append(refused, fmt.Sprintf("%s: the entry for %s can't be edited or removed", path, e))
		}
	}
	return refused
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
// Unless skipTests is set, it first runs every schema's tests, and the rule across versions, as
// validate all does, and builds nothing if any fail.
func (m *CLIManager) BuildDist(ctx context.Context, envName config.Env, all, collapse, skipTests, strict bool) error {
	m.logger.Debug("building distribution", "env", envName, "all", all, "collapse", collapse,
		"skipTests", skipTests, "strict", strict)

	if dErr := m.registry.RequireFamilyDeclarations(ctx, ""); dErr != nil {
		return dErr
	}

	var count int
	var err error
	if all {
		if !skipTests {
			if tErr := m.testBeforeBuild(ctx, strict); tErr != nil {
				return tErr
			}
		}
		count, err = m.distBuilder.BuildAll(ctx, envName, collapse)
	} else {
		count, err = m.buildChanged(ctx, envName, collapse, skipTests, strict)
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
func (m *CLIManager) buildChanged(ctx context.Context, envName config.Env, collapse, skipTests, strict bool) (
	int, error,
) {
	if err := m.CheckChanges(ctx, envName); err != nil {
		return 0, err
	}

	if !skipTests {
		if err := m.testBeforeBuild(ctx, strict); err != nil {
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

// testBeforeBuild runs every schema's tests, as validate all does, and writes the report if it has
// failures or warnings. It returns ErrTestsFailed if any test failed, or if strict and it has warnings.
func (m *CLIManager) testBeforeBuild(ctx context.Context, strict bool) error {
	m.tester.SetStopOnFirstError(false)

	tr, err := m.tester.TestFoundSchemas(ctx, "")
	if err != nil {
		return err
	}
	if !tr.HasFailures() && !tr.HasWarnings() {
		return nil
	}

	if wErr := (&report.TextReporter{}).Write(m.reporterWriter, tr); wErr != nil {
		return wErr
	}
	if !tr.HasFailures() && !strict {
		return nil
	}
	_, _ = fmt.Fprintln(m.reporterWriter, "Not building: fix the failing tests, or use --skip-tests")
	return ErrTestsFailed
}
