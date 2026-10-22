package app

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/internal/schema"
	"github.com/bitshepherds/json-schema-manager/internal/structdiff"
)

func TestNewDiffCmd(t *testing.T) {
	t.Parallel()

	older := schema.Key("domain_family_1_0_0")
	newer := schema.Key("domain_family_1_1_0")
	changes := []schema.Change{
		{
			Change: structdiff.Change{
				Location: "/status",
				Keyword:  "enum",
				Effect:   structdiff.Widens,
				Detail:   `adds "x"`,
			},
		},
		{
			Change: structdiff.Change{Location: "/n", Keyword: "maximum", Effect: structdiff.Widens, Detail: "relaxes"},
			UsedBy: []string{"/reg/domain/family/1/1/0/pass/a.json", "/reg/domain/family/1/1/0/pass/b.json"},
		},
		{
			Change: structdiff.Change{
				Location: "/",
				Keyword:  "required",
				Effect:   structdiff.Narrows,
				Detail:   `requires "a"`,
			},
		},
	}

	tests := []struct {
		name       string
		args       []string
		changes    []schema.Change
		mgrErr     error
		wantErr    bool
		wantOutput []string
	}{
		{
			name:    "changes",
			args:    []string{"domain_family_1_0_0", "domain_family_1_1_0"},
			changes: changes,
			wantOutput: []string{
				"Changes from domain_family_1_0_0 to domain_family_1_1_0:\n",
				"  widens   /status: enum adds \"x\" (no pass document uses it)\n",
				"  widens   /n: maximum relaxes (used by pass/a.json pass/b.json)\n",
				"  narrows  /: required requires \"a\"\n",
			},
		},
		{
			name:       "no changes",
			args:       []string{"domain_family_1_0_0", "domain_family_1_1_0"},
			wantOutput: []string{"No structural changes from domain_family_1_0_0 to domain_family_1_1_0\n"},
		},
		{name: "older invalid", args: []string{"!!", "domain_family_1_1_0"}, wantErr: true},
		{name: "newer invalid", args: []string{"domain_family_1_0_0", "!!"}, wantErr: true},
		{
			name:    "manager error",
			args:    []string{"domain_family_1_0_0", "domain_family_1_1_0"},
			mgrErr:  errors.New("boom"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "json-schema-manager-config.yml"),
				[]byte(simpleTestConfig), 0o600))
			reg, err := schema.NewRegistry(tmpDir, &mockCompiler{}, fsh.NewPathResolver(), fsh.NewEnvProvider())
			require.NoError(t, err)

			m := &MockManager{registry: reg}
			m.On("DiffSchemas", older, newer).Return(tt.changes, tt.mgrErr).Maybe()

			cmd := NewDiffCmd(m)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tt.args)
			err = cmd.Execute()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, joinLines(tt.wantOutput), out.String())
		})
	}
}

func joinLines(lines []string) string {
	var b bytes.Buffer
	for _, l := range lines {
		b.WriteString(l)
	}
	return b.String()
}

func TestCLIManager_DiffSchemas(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	registry := setupTestRegistry(t)
	for k, content := range map[schema.Key]string{
		"d_f_1_0_0": `{"maxLength": 1}`,
		"d_f_1_1_0": `{"maxLength": 2}`,
	} {
		s := schema.New(k, registry)
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(schema.HomeDir), "pass"), 0o755))
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte(content), 0o600))
	}
	mgr := NewCLIManager(logger, registry, schema.NewTester(registry), &MockGitter{}, nil, io.Discard)

	changes, err := mgr.DiffSchemas("d_f_1_0_0", "d_f_1_1_0")
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, "maxLength", changes[0].Change.Keyword)

	_, err = mgr.DiffSchemas("missing_1_0_0", "d_f_1_1_0")
	require.Error(t, err)
	_, err = mgr.DiffSchemas("d_f_1_0_0", "missing_1_0_0")
	require.Error(t, err)
}
