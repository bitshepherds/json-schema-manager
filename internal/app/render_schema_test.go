package app

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

func TestNewRenderSchemaCmd(t *testing.T) {
	t.Parallel()

	baseKey := schema.Key("domain_family_1_0_0")
	renderedBytes := []byte(`{"type": "object"}`)

	tests := []struct {
		name        string
		args        []string
		setupMock   func(m *MockManager)
		wantErr     bool
		wantErrType interface{}
		wantOutput  string
	}{
		{
			name: "Render by positional key",
			args: []string{"domain_family_1_0_0"},
			setupMock: func(m *MockManager) {
				m.On("RenderSchema", mock.Anything, mock.MatchedBy(func(rt schema.ResolvedTarget) bool {
					return *rt.Key == baseKey
				}), config.Env(""), false).Return(renderedBytes, nil)
			},
			wantOutput: string(renderedBytes),
		},
		{
			name: "Render by flag -k and --env",
			args: []string{"-k", "domain_family_1_0_0", "--env", "prod"},
			setupMock: func(m *MockManager) {
				m.On("RenderSchema", mock.Anything, mock.MatchedBy(func(rt schema.ResolvedTarget) bool {
					return *rt.Key == baseKey
				}), config.Env("prod"), false).Return(renderedBytes, nil)
			},
			wantOutput: string(renderedBytes),
		},
		{
			name: "Render by flag -i",
			args: []string{"-i", "https://p/domain_family_1_0_0.schema.json"},
			setupMock: func(m *MockManager) {
				m.On("RenderSchema", mock.Anything, mock.MatchedBy(func(rt schema.ResolvedTarget) bool {
					return *rt.Key == baseKey
				}), config.Env(""), false).Return(renderedBytes, nil)
			},
			wantOutput: string(renderedBytes),
		},
		{
			name: "Render with --collapse",
			args: []string{"-k", "domain_family_1_0_0", "--collapse"},
			setupMock: func(m *MockManager) {
				m.On("RenderSchema", mock.Anything, mock.MatchedBy(func(rt schema.ResolvedTarget) bool {
					return *rt.Key == baseKey
				}), config.Env(""), true).Return([]byte(`{"collapsed":true}`), nil)
			},
			wantOutput: `{"collapsed":true}`,
		},
		{
			name:        "Invalid target",
			args:        []string{"!!"},
			wantErr:     true,
			wantErrType: &schema.InvalidTargetArgumentError{},
		},
		{
			name:        "Missing target (empty arg)",
			args:        []string{""},
			wantErr:     true,
			wantErrType: &schema.NoTargetArgumentError{},
		},
		{
			name:        "Missing target (no arg)",
			args:        []string{},
			wantErr:     true,
			wantErrType: &schema.NoTargetArgumentError{},
		},
		{
			name: "Scope resolves to single schema",
			args: []string{"domain/family"},
			setupMock: func(m *MockManager) {
				m.On("RenderSchema", mock.Anything, mock.MatchedBy(func(rt schema.ResolvedTarget) bool {
					return rt.Key != nil && *rt.Key == baseKey
				}), config.Env(""), false).Return(renderedBytes, nil)
			},
			wantOutput: string(renderedBytes),
		},
		{
			name:        "Scope resolves to zero schemas",
			args:        []string{"nonexistent"},
			wantErr:     true,
			wantErrType: &schema.NotFoundError{},
		},
		{
			name: "Manager error",
			args: []string{"domain_family_1_0_0"},
			setupMock: func(m *MockManager) {
				m.On("RenderSchema", mock.Anything, mock.Anything, mock.Anything, false).Return(nil, fmt.Errorf("boom"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup an isolated temporary registry root for this subtest
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "json-schema-manager-config.yml")
			require.NoError(t, os.WriteFile(configPath, []byte(simpleTestConfig), 0o600))

			reg, rErr := schema.NewRegistry(tmpDir, &mockCompiler{}, fsh.NewPathResolver(), fsh.NewEnvProvider())
			require.NoError(t, rErr)

			// Create a dummy schema file for resolution to work
			familyDir := filepath.Join(tmpDir, "domain", "family")
			versionDir := filepath.Join(familyDir, "1", "0", "0")
			require.NoError(t, os.MkdirAll(versionDir, 0o755))
			schemaFile := filepath.Join(versionDir, "domain_family_1_0_0.schema.json")
			require.NoError(t, os.WriteFile(schemaFile, []byte("{}"), 0o600))

			m := &MockManager{registry: reg}
			if tt.setupMock != nil {
				tt.setupMock(m)
			}

			cmd := NewRenderSchemaCmd(m)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrType != nil {
					//nolint:testifylint // IsType is appropriate for table-driven tests with interface{}
					assert.IsType(t, tt.wantErrType, err)
				}
				return
			}

			require.NoError(t, err)
			if tt.wantOutput != "" {
				assert.Equal(t, tt.wantOutput+"\n", out.String())
			}
			m.AssertExpectations(t)
		})
	}
}
