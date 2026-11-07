package app

import (
	"context"

	"github.com/stretchr/testify/mock"

	"github.com/bitshepherds/json-schema-manager/v2/internal/config"
	"github.com/bitshepherds/json-schema-manager/v2/internal/repo"
	"github.com/bitshepherds/json-schema-manager/v2/internal/schema"
)

const testConfig = `
environments:
  prod:
    privateUrlRoot: "https://json-schemas.internal.myorg.io/"
    publicUrlRoot: "https://json-schemas.myorg.io/"
    isProduction: true
`

const simpleTestConfig = "environments: " +
	"{prod: {publicUrlRoot: 'https://p', privateUrlRoot: 'https://pr', isProduction: true}}"

type MockManager struct {
	mock.Mock
	registry *schema.Registry
}

func (m *MockManager) Registry() *schema.Registry {
	return m.registry
}

func (m *MockManager) ValidateSchema(ctx context.Context, target schema.ResolvedTarget, verbose bool,
	format string, useColour bool, continueOnError bool, strict bool,
) error {
	args := m.Called(ctx, target, verbose, format, useColour, continueOnError, strict)
	return args.Error(0)
}

func (m *MockManager) WatchValidation(ctx context.Context, target schema.ResolvedTarget, verbose bool,
	format string, useColour bool, continueOnError bool, strict bool, readyChan chan<- struct{},
) error {
	args := m.Called(ctx, target, verbose, format, useColour, continueOnError, strict, readyChan)
	return args.Error(0)
}

func (m *MockManager) CreateSchema(domainAndFamilyName string, spec schema.FamilySpec) (schema.Key, error) {
	args := m.Called(domainAndFamilyName, spec)
	k, _ := args.Get(0).(schema.Key)
	return k, args.Error(1)
}

func (m *MockManager) CreateSchemaVersion(k schema.Key, rt schema.ReleaseType) (schema.Key, error) {
	args := m.Called(k, rt)
	kNew, _ := args.Get(0).(schema.Key)
	return kNew, args.Error(1)
}

func (m *MockManager) RenderSchema(
	ctx context.Context,
	target schema.ResolvedTarget,
	env config.Env,
	collapse bool,
) ([]byte, error) {
	args := m.Called(ctx, target, env, collapse)
	res, _ := args.Get(0).([]byte)
	return res, args.Error(1)
}

func (m *MockManager) DiffSchemas(older, newer schema.Key) ([]schema.Change, error) {
	args := m.Called(older, newer)
	res, _ := args.Get(0).([]schema.Change)
	return res, args.Error(1)
}

func (m *MockManager) CheckParty(
	ctx context.Context, manifestPath, format string, useColour, strict bool,
) error {
	args := m.Called(ctx, manifestPath, format, useColour, strict)
	return args.Error(0)
}

func (m *MockManager) CheckChanges(ctx context.Context, envName config.Env) error {
	args := m.Called(ctx, envName)
	return args.Error(0)
}

func (m *MockManager) TagDeployment(ctx context.Context, envName config.Env, _ bool) error {
	args := m.Called(ctx, envName)
	return args.Error(0)
}

func (m *MockManager) BuildDist(ctx context.Context, envName config.Env, all, collapse, skipTests, strict bool) error {
	args := m.Called(ctx, envName, all, collapse, skipTests, strict)
	return args.Error(0)
}

// MockGitter is a test mock for the repo.Gitter interface.
type MockGitter struct {
	GetLatestAnchorFunc  func(ctx context.Context, env config.Env) (repo.Revision, error)
	TagDeploymentFunc    func(ctx context.Context, env config.Env, push bool) (string, error)
	GetSchemaChangesFunc func(ctx context.Context, anchor repo.Revision, sourceDir, suffix string) ([]repo.Change, error)
	ShowFileFunc         func(ctx context.Context, rev repo.Revision, path string) ([]byte, error)
	UntrackedFilesFunc   func(ctx context.Context, sourceDir, suffix string) ([]string, error)
	ListFilesFunc        func(ctx context.Context, rev repo.Revision, dir, suffix string) ([]string, error)
}

func (m *MockGitter) ListFiles(ctx context.Context, rev repo.Revision, dir, suffix string) ([]string, error) {
	if m.ListFilesFunc != nil {
		return m.ListFilesFunc(ctx, rev, dir, suffix)
	}
	return nil, nil
}

func (m *MockGitter) UntrackedFiles(ctx context.Context, sourceDir, suffix string) ([]string, error) {
	if m.UntrackedFilesFunc != nil {
		return m.UntrackedFilesFunc(ctx, sourceDir, suffix)
	}
	return nil, nil
}

func (m *MockGitter) ShowFile(ctx context.Context, rev repo.Revision, path string) ([]byte, error) {
	if m.ShowFileFunc != nil {
		return m.ShowFileFunc(ctx, rev, path)
	}
	return nil, nil
}

func (m *MockGitter) GetLatestAnchor(ctx context.Context, env config.Env) (repo.Revision, error) {
	if m.GetLatestAnchorFunc != nil {
		return m.GetLatestAnchorFunc(ctx, env)
	}
	return "HEAD", nil
}

func (m *MockGitter) TagDeploymentSuccess(ctx context.Context, env config.Env, push bool) (string, error) {
	if m.TagDeploymentFunc != nil {
		return m.TagDeploymentFunc(ctx, env, push)
	}
	return "jsm-deploy/prod/20260130-120000", nil
}

func (m *MockGitter) GetSchemaChanges(
	ctx context.Context,
	anchor repo.Revision,
	sourceDir, suffix string,
) ([]repo.Change, error) {
	if m.GetSchemaChangesFunc != nil {
		return m.GetSchemaChangesFunc(ctx, anchor, sourceDir, suffix)
	}
	return nil, nil
}
