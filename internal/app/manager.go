// Package app provides the core application logic for JSM.
package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/repo"
	"github.com/bitshepherds/json-schema-manager/internal/report"
	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

// Manager defines the business logic for JSON schema operations.
type Manager interface {
	ValidateSchema(ctx context.Context, target schema.ResolvedTarget, verbose bool, format string,
		useColour bool, continueOnError bool, testScope schema.TestScope, skipCompatible bool) error
	WatchValidation(ctx context.Context, target schema.ResolvedTarget, verbose bool, format string,
		useColour bool, continueOnError bool, testScope schema.TestScope, skipCompatible bool,
		readyChan chan<- struct{}) error
	Registry() *schema.Registry
	CreateSchema(domainAndFamilyName string) (schema.Key, error)
	CreateSchemaVersion(k schema.Key, rt schema.ReleaseType) (schema.Key, error)
	RenderSchema(ctx context.Context, target schema.ResolvedTarget, env config.Env) ([]byte, error)
	CheckChanges(ctx context.Context, envName config.Env) error
	TagDeployment(ctx context.Context, envName config.Env) error
	BuildDist(ctx context.Context, envName config.Env, all bool) error
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
) error {
	return l.check().ValidateSchema(ctx, target, verbose, format, useColour, continueOnError, testScope, skipCompatible)
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
func (l *LazyManager) RenderSchema(ctx context.Context, target schema.ResolvedTarget, env config.Env) ([]byte, error) {
	return l.check().RenderSchema(ctx, target, env)
}

// CheckChanges implements the Manager interface.
func (l *LazyManager) CheckChanges(ctx context.Context, envName config.Env) error {
	return l.check().CheckChanges(ctx, envName)
}

// TagDeployment implements the Manager interface.
func (l *LazyManager) TagDeployment(ctx context.Context, envName config.Env) error {
	return l.check().TagDeployment(ctx, envName)
}

// BuildDist implements the Manager interface.
func (l *LazyManager) BuildDist(ctx context.Context, envName config.Env, all bool) error {
	return l.check().BuildDist(ctx, envName, all)
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
) error {
	m.logger.Debug("validating schema", "target", target, "verbose", verbose, "format", format,
		"useColour", useColour, "continueOnError", continueOnError, "skipCompatible", skipCompatible)

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

	return reporter.Write(m.reporterWriter, tr)
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

	return watcher.Watch(ctx, callback)
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
func (m *CLIManager) RenderSchema(_ context.Context, target schema.ResolvedTarget, env config.Env) ([]byte, error) {
	m.logger.Debug("rendering schema", "target", target, "env", env)

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

	return ri.Rendered, nil
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

	changes, err := m.gitter.GetSchemaChanges(ctx, anchor, m.registry.RootDirectory(), schema.SchemaSuffix)
	if err != nil {
		return err
	}

	if !envCfg.AllowSchemaMutation {
		var modifiedPaths []string
		for _, change := range changes {
			if !change.IsNew && !change.IsDeleted {
				modifiedPaths = append(modifiedPaths, change.Path)
			}
		}

		if len(modifiedPaths) > 0 {
			return &schema.ChangedDeployedSchemasError{Paths: modifiedPaths}
		}
	}

	_, _ = fmt.Fprintln(m.reporterWriter, "All changes are valid")
	return nil
}

// TagDeployment ensures that a successful deployment is tagged in git.
func (m *CLIManager) TagDeployment(ctx context.Context, envName config.Env) error {
	m.logger.Debug("tagging deployment", "env", envName)

	cfg, err := m.registry.Config()
	if err != nil {
		return err
	}

	if _, envErr := cfg.EnvConfig(envName); envErr != nil {
		return envErr
	}

	tagName, err := m.gitter.TagDeploymentSuccess(ctx, envName)
	if err != nil {
		if tagName != "" {
			m.logger.Warn("tag was created but could not be pushed", "tag", tagName, "error", err)
			_, _ = fmt.Fprintf(m.reporterWriter, "🏷️  Tag created locally but failed to push: %s\n", tagName)
			return nil // Consider it success if tag created? Or error?
		}
		return err
	}

	_, _ = fmt.Fprintf(m.reporterWriter, "🏷️  Successfully tagged and pushed deployment: %s\n", tagName)
	return nil
}

// BuildDist builds a distribution directory for the given environment.
func (m *CLIManager) BuildDist(ctx context.Context, envName config.Env, all bool) error {
	m.logger.Debug("building distribution", "env", envName, "all", all)

	var count int
	var err error
	if all {
		count, err = m.distBuilder.BuildAll(ctx, envName)
	} else {
		// Check for mutations first
		if ccErr := m.CheckChanges(ctx, envName); ccErr != nil {
			return ccErr
		}

		anchor, anchorErr := m.gitter.GetLatestAnchor(ctx, envName)
		if anchorErr != nil {
			return anchorErr
		}

		count, err = m.distBuilder.BuildChanged(ctx, envName, anchor)
	}

	if err != nil {
		return err
	}

	if count == 0 {
		_, _ = fmt.Fprintln(m.reporterWriter, "No schemas to build")
		return nil
	}

	_, _ = fmt.Fprintf(m.reporterWriter, "📂 Successfully built %d schemas to distribution directory\n", count)
	return nil
}
