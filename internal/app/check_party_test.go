package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/internal/schema"
	"github.com/bitshepherds/json-schema-manager/internal/validator"
)

func TestNewCheckPartyCmd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		args         []string
		wantManifest string
		wantFormat   string
		wantStrict   bool
	}{
		{"default manifest", nil, "jsm-party.yml", "text", false},
		{"named manifest, JSON and strict", []string{"m.yml", "-o", "json", "--strict"}, "m.yml", "json", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &MockManager{}
			m.On("CheckParty", context.Background(), tt.wantManifest, tt.wantFormat, true, tt.wantStrict).
				Return(nil).Once()
			cmd := NewCheckPartyCmd(m)
			cmd.Flags().Bool("nocolour", false, "")
			cmd.SetArgs(tt.args)
			require.NoError(t, cmd.ExecuteContext(context.Background()))
			m.AssertExpectations(t)
		})
	}
}

// partyFixture is a registry and a party manifest for a reader listing shop_order_1_0_0.
type partyFixture struct {
	registry *schema.Registry
	manifest string
}

// newPartyFixture returns a fixture holding the listed version with the given content.
func newPartyFixture(t *testing.T, schemaContent string) *partyFixture {
	t.Helper()
	regDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(regDir, config.JsmRegistryConfigFile), []byte(testConfig), 0o600))
	registry, err := schema.NewRegistry(
		regDir, validator.NewSanthoshCompiler(), fsh.NewPathResolver(), fsh.NewEnvProvider(),
	)
	require.NoError(t, err)
	f := &partyFixture{registry: registry, manifest: filepath.Join(t.TempDir(), "jsm-party.yml")}
	f.addVersion(t, "shop_order_1_0_0", schemaContent)
	require.NoError(t, os.WriteFile(f.manifest, []byte("role: reader\nschemas: [shop_order_1_0_0]\n"), 0o600))
	return f
}

// addVersion adds a version to the family, with empty pass and fail directories, and returns its directory.
func (f *partyFixture) addVersion(t *testing.T, k schema.Key, content string) string {
	t.Helper()
	s := schema.New(k, f.registry)
	for _, sub := range []string{"pass", "fail"} {
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(schema.HomeDir), sub), 0o755))
	}
	require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte(content), 0o600))
	declareFamily(t, s)
	return s.Path(schema.HomeDir)
}

func (f *partyFixture) declare(t *testing.T, declaration string) {
	t.Helper()
	require.NoError(t, os.WriteFile(f.registry.FamilyDeclarationPath("shop_order_1_0_0"), []byte(declaration), 0o600))
}

func (f *partyFixture) manager(w io.Writer) *CLIManager {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewCLIManager(logger, f.registry, schema.NewTester(f.registry), &MockGitter{}, nil, w)
}

// check runs the check, and returns its error and its output.
func (f *partyFixture) check(format string, strict bool) (string, error) {
	var out bytes.Buffer
	err := f.manager(&out).CheckParty(context.Background(), f.manifest, format, false, strict)
	return out.String(), err
}

func TestCLIManager_CheckParty(t *testing.T) {
	t.Parallel()

	t.Run("passes, in text and JSON", func(t *testing.T) {
		t.Parallel()
		f := newPartyFixture(t, `{}`)
		out, err := f.check("text", true)
		require.NoError(t, err)
		assert.Contains(t, out, "JSM PARTY CHECK: "+f.manifest+" (role: reader)")
		assert.Contains(t, out, "[PASS] shop_order_1_0_0 (0 document runs)")
		assert.Contains(t, out, "Party check summary: 1 passed, 0 failed, 0 warnings, 0 notices")
		out, err = f.check("json", true)
		require.NoError(t, err)
		assert.Contains(t, out, `"status": "pass"`)
		assert.Contains(t, out, `"role": "reader"`)
	})

	t.Run("manifest cannot be read", func(t *testing.T) {
		t.Parallel()
		f := newPartyFixture(t, `{}`)
		f.manifest = "missing.yml"
		_, err := f.check("text", false)
		require.Error(t, err)
	})

	t.Run("listed version does not render", func(t *testing.T) {
		t.Parallel()
		f := newPartyFixture(t, "{\"$ref\": \"{{ JSM `missing_1_0_0` }}\"}")
		_, err := f.check("text", false)
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrTestsFailed)
	})

	t.Run("the report cannot be written", func(t *testing.T) {
		t.Parallel()
		f := newPartyFixture(t, `{}`)
		err := f.manager(&failingWriter{}).CheckParty(context.Background(), f.manifest, "text", false, false)
		require.Error(t, err)
	})

	t.Run("a listed version which is missing", func(t *testing.T) {
		t.Parallel()
		f := newPartyFixture(t, `{}`)
		require.NoError(t, os.WriteFile(f.manifest, []byte("role: reader\nschemas: [shop_order_1_9_0]\n"), 0o600))
		out, err := f.check("text", false)
		require.ErrorIs(t, err, ErrTestsFailed)
		assert.Contains(t, out, "[FAIL] shop_order_1_9_0\n")
		assert.Contains(t, out, "not found in the registry")
	})
}

func TestCheckPartyFailureIsReportedAsTheRuleReportsIt(t *testing.T) {
	t.Parallel()

	f := newPartyFixture(t, `{"properties": {"status": {"enum": ["paid"]}}}`)
	later := f.addVersion(t, "shop_order_1_1_0", `{"properties": {"status": {"enum": ["paid", "refunded"]}}}`)
	doc := filepath.Join(later, "pass", "refunded.json")
	require.NoError(t, os.WriteFile(doc, []byte(`{"status": "refunded"}`), 0o600))

	out, err := f.check("text", false)
	require.ErrorIs(t, err, ErrTestsFailed)
	assert.Contains(t, out, "[FAIL] shop_order_1_0_0 (1 document run)")
	assert.Contains(t, out, "✗ shop_order_1_1_0: "+doc+
		" (no-loosening broken: pass document of 1.1.0 rejected by 1.0.0):")
	assert.Contains(t, out, "no-loosening broken between shop_order_1_0_0 and shop_order_1_1_0")
	assert.Contains(t, out,
		"a writer on shop_order_1_1_0 can send a reader still on shop_order_1_0_0 a document it rejects")

	out, err = f.check("json", false)
	require.ErrorIs(t, err, ErrTestsFailed)
	var res struct {
		Schemas []struct {
			Status string `json:"status"`
			Failed []struct {
				Later string `json:"later"`
				Rule  string `json:"rule"`
				Error string `json:"error"`
			} `json:"failed"`
		} `json:"schemas"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.Len(t, res.Schemas, 1)
	assert.Equal(t, "fail", res.Schemas[0].Status)
	require.Len(t, res.Schemas[0].Failed, 1)
	assert.Equal(t, "shop_order_1_1_0", res.Schemas[0].Failed[0].Later)
	assert.Equal(t, "no-loosening", res.Schemas[0].Failed[0].Rule)
	assert.Contains(t, res.Schemas[0].Failed[0].Error, "no-loosening broken")
}

func TestCheckPartyReportsAnExceptedRunWithItsReason(t *testing.T) {
	t.Parallel()

	f := newPartyFixture(t, `{"properties": {"status": {"enum": ["paid"]}}}`)
	later := f.addVersion(t, "shop_order_1_1_0", `{"properties": {"status": {"enum": ["paid", "refunded"]}}}`)
	doc := filepath.Join(later, "pass", "refunded.json")
	require.NoError(t, os.WriteFile(doc, []byte(`{"status": "refunded"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(later, schema.ExceptionsFile), []byte(`exceptions:
  - version: 1.0.0
    document: pass/refunded.json
    rule: no-loosening
    reason: every reader was upgraded first
`), 0o600))

	out, err := f.check("text", true)
	require.NoError(t, err)
	assert.Contains(t, out, "[PASS] shop_order_1_0_0")
	assert.Contains(t, out, "~ shop_order_1_1_0:")
	assert.Contains(t, out, "no-loosening excepted")
	assert.Contains(t, out, "reason: every reader was upgraded first")

	out, err = f.check("json", true)
	require.NoError(t, err)
	assert.Contains(t, out, `"status": "excepted"`)
	assert.Contains(t, out, `"reason": "every reader was upgraded first"`)
}

func TestCheckPartyNoticeExitsZeroEvenWhenStrict(t *testing.T) {
	t.Parallel()

	f := newPartyFixture(t, `{}`)
	f.declare(t, "scenario: lock-step\n")

	out, err := f.check("text", true)
	require.NoError(t, err)
	assert.Contains(t, out, "[NOTICE] shop_order_1_0_0\n")
	assert.Contains(t, out, "This family is declared lock-step: both sides move together, "+
		"so no later version can harm a reader on shop_order_1_0_0.")
	assert.Contains(t, out, "0 passed, 0 failed, 0 warnings, 1 notice\n")

	out, err = f.check("json", true)
	require.NoError(t, err)
	assert.Contains(t, out, `"status": "notice"`)
	assert.Contains(t, out, `"notices": 1`)
}

func TestCheckPartyStrictMatchesValidate(t *testing.T) {
	t.Parallel()

	// A family with a newer major is a warning here, and a version with no documents is one in validate.
	// Each exits 0 until --strict, which then fails with the same error, and so the same exit code.
	f := newPartyFixture(t, `{}`)
	f.addVersion(t, "shop_order_2_0_0", `{}`)
	key := schema.Key("shop_order_1_0_0")

	validate := func(strict bool) error {
		return f.manager(io.Discard).ValidateSchema(context.Background(), schema.ResolvedTarget{Key: &key},
			false, "text", false, true, strict)
	}

	require.NoError(t, validate(false))
	out, err := f.check("text", false)
	require.NoError(t, err)
	assert.Contains(t, out, "[WARN] shop_order_1_0_0")
	assert.Contains(t, out, "the family has a newer major version, shop_order_2_0_0, which this check doesn't cover: "+
		"move to it before writers stop sending version 1")

	require.ErrorIs(t, validate(true), ErrTestsFailed)
	_, err = f.check("text", true)
	require.ErrorIs(t, err, ErrTestsFailed)
	assert.Equal(t, ExitCode(validate(true)), ExitCode(err))
	assert.Equal(t, ExitCheckFailed, ExitCode(err))
}

func TestCheckPartyNewerMajorAdviceFollowsTheRole(t *testing.T) {
	t.Parallel()

	f := newPartyFixture(t, `{}`)
	f.addVersion(t, "shop_order_2_0_0", `{}`)
	f.declare(t, "scenario: owner-reads\n")
	require.NoError(t, os.WriteFile(f.manifest, []byte("role: writer\nschemas: [shop_order_1_0_0]\n"), 0o600))

	out, err := f.check("text", false)
	require.NoError(t, err)
	assert.Contains(t, out, "move to it before readers stop accepting version 1")
}
