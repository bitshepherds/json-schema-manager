package app

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/v2/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/v2/internal/schema"
)

func TestNewCreateSchemaVersionCmd(t *testing.T) {
	t.Parallel()

	baseKey := schema.Key("domain_family_1_0_0")
	newKey := schema.Key("domain_family_1_1_0")

	tests := []struct {
		name        string
		args        []string
		setupMock   func(m *MockManager)
		wantErr     bool
		wantErrType interface{}
		wantOutput  string
	}{
		{
			name: "Target and release type as positional arguments",
			args: []string{"domain_family_1_0_0", "minor"},
			setupMock: func(m *MockManager) {
				m.On("CreateSchemaVersion", baseKey, schema.ReleaseTypeMinor).Return(newKey, nil)
			},
			wantOutput: fmt.Sprintf("Successfully created new schema with key: %s\n\n", newKey),
		},
		{
			name: "Release type as positional argument with -k flag",
			args: []string{"patch", "-k", "domain_family_1_0_0"},
			setupMock: func(m *MockManager) {
				m.On("CreateSchemaVersion", baseKey, schema.ReleaseTypePatch).
					Return(schema.Key("domain_family_1_0_1"), nil)
			},
			wantOutput: "Successfully created new schema with key: domain_family_1_0_1\n\n",
		},
		{
			name: "Release type as positional argument with -i flag",
			args: []string{"major", "-i", "https://p/domain_family_1_0_0.schema.json"},
			setupMock: func(m *MockManager) {
				m.On("CreateSchemaVersion", baseKey, schema.ReleaseTypeMajor).
					Return(schema.Key("domain_family_2_0_0"), nil)
			},
			wantOutput: "Successfully created new schema with key: domain_family_2_0_0\n\n",
		},
		{
			name: "Success with path output",
			args: []string{"domain_family_1_0_0", "major"},
			setupMock: func(m *MockManager) {
				kMajor := schema.Key("domain_family_2_0_0")
				m.On("CreateSchemaVersion", baseKey, schema.ReleaseTypeMajor).Return(kMajor, nil)

				// Create the version dir/file in registry so GetSchemaByKey succeeds
				s := schema.New(kMajor, m.Registry())
				_ = os.MkdirAll(s.Path(schema.HomeDir), 0o755)
				_ = os.WriteFile(s.Path(schema.FilePath), []byte("{}"), 0o600)
			},
			wantOutput: "Successfully created new schema with key: domain_family_2_0_0\n\n" +
				"The schema, and the pass and fail directories for its test documents, can be found here:",
		},
		{
			name:        "Invalid release type",
			args:        []string{"domain_family_1_0_0", "invalid"},
			wantErr:     true,
			wantErrType: &schema.InvalidReleaseTypeError{},
		},
		{
			name:        "Missing target",
			args:        []string{"major"},
			wantErr:     true,
			wantErrType: &schema.NoTargetArgumentError{},
		},
		{
			name:        "Target resolution fails (invalid characters)",
			args:        []string{"INVALID_TARGET", "major"},
			wantErr:     true,
			wantErrType: &schema.InvalidTargetArgumentError{},
		},
		{
			name: "Target scope resolves to single schema",
			args: []string{"domain/family", "major"},
			setupMock: func(m *MockManager) {
				m.On("CreateSchemaVersion", baseKey, schema.ReleaseTypeMajor).
					Return(schema.Key("domain_family_2_0_0"), nil)
			},
			wantOutput: "Successfully created new schema with key: domain_family_2_0_0\n\n",
		},
		{
			name:        "Target scope resolves to zero schemas",
			args:        []string{"nonexistent", "major"},
			wantErr:     true,
			wantErrType: &schema.NotFoundError{},
		},
		{
			name: "Manager error",
			args: []string{"domain_family_1_0_0", "major"},
			setupMock: func(m *MockManager) {
				m.On("CreateSchemaVersion", baseKey, schema.ReleaseTypeMajor).Return(schema.Key(""), fmt.Errorf("boom"))
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

			cmd := NewCreateSchemaVersionCmd(m)

			// Capture stdout
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
				assert.Contains(t, out.String(), tt.wantOutput)
			}
			m.AssertExpectations(t)
		})
	}
}
