package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

func TestCreateSchemaCmd(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T) (*MockManager, *cobra.Command) {
		t.Helper()
		reg := setupTestRegistry(t)
		mgr := &MockManager{
			registry: reg,
		}
		cmd := NewCreateSchemaCmd(mgr)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		return mgr, cmd
	}

	t.Run("successful execution", func(t *testing.T) {
		t.Parallel()
		mgr, cmd := setup(t)
		domainAndFamily := "test-domain/test-family"
		key := schema.Key("test-domain_test-family_1_0_0")

		// Create the schema file in the correct nested directory structure
		s := schema.New(key, mgr.Registry())
		require.NoError(t, os.MkdirAll(s.Path(schema.HomeDir), 0o755))
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte("{}"), 0o600))

		mgr.On("CreateSchema", domainAndFamily, schema.FamilySpec{Scenario: schema.ScenarioOwnerWrites}).
			Return(key, nil).Once()

		cmd.SetArgs([]string{domainAndFamily, "--scenario", "owner-writes"})
		err := cmd.ExecuteContext(context.Background())
		require.NoError(t, err)
		mgr.AssertExpectations(t)
	})

	t.Run("missing args errors", func(t *testing.T) {
		t.Parallel()
		_, cmd := setup(t)
		cmd.SetArgs([]string{})
		err := cmd.ExecuteContext(context.Background())
		require.Error(t, err)
	})

	t.Run("too many args errors", func(t *testing.T) {
		t.Parallel()
		_, cmd := setup(t)
		cmd.SetArgs([]string{"arg1", "arg2"})
		err := cmd.ExecuteContext(context.Background())
		require.Error(t, err)
	})

	t.Run("manager error propagates", func(t *testing.T) {
		t.Parallel()
		mgr, cmd := setup(t)
		domainAndFamily := "test-domain/test-family"
		mgr.On("CreateSchema", domainAndFamily, schema.FamilySpec{Scenario: schema.ScenarioOwnerWrites}).
			Return(schema.Key(""), errors.New("manager error")).Once()

		cmd.SetArgs([]string{domainAndFamily, "--scenario", "owner-writes"})
		err := cmd.ExecuteContext(context.Background())
		require.Error(t, err)
		assert.Equal(t, "manager error", err.Error())
		mgr.AssertExpectations(t)
	})
}

func TestCreateSchemaWritesDeclaration(t *testing.T) {
	t.Parallel()

	// run creates domain/family through the real manager, answering any questions with stdin.
	run := func(t *testing.T, stdin string, args ...string) (*schema.FamilyDeclaration, string, error) {
		t.Helper()
		reg := setupTestRegistry(t)
		mgr := NewCLIManager(slog.New(slog.DiscardHandler), reg, schema.NewTester(reg), &MockGitter{}, nil, io.Discard)
		cmd := NewCreateSchemaCmd(mgr)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(strings.NewReader(stdin))
		cmd.SetArgs(append([]string{"domain/family"}, args...))
		err := cmd.ExecuteContext(context.Background())
		if err != nil {
			assert.NoFileExists(t, reg.FamilyDeclarationPath("domain_family_1_0_0"), "nothing is written on a refusal")
			return nil, out.String(), err
		}
		d, dErr := reg.FamilyDeclaration("domain_family_1_0_0")
		require.NoError(t, dErr)
		return d, out.String(), nil
	}

	t.Run("takes the scenario as a flag, without asking", func(t *testing.T) {
		t.Parallel()
		d, out, err := run(t, "", "--scenario", "config-file")
		require.NoError(t, err)
		assert.Equal(t, schema.ScenarioConfigFile, d.Scenario)
		assert.NotContains(t, out, "Which scenario")
		assert.Contains(t, out, schema.FamilyDeclarationFile)
	})

	t.Run("asks for the scenario, by number", func(t *testing.T) {
		t.Parallel()
		d, out, err := run(t, "8\n")
		require.NoError(t, err)
		assert.Equal(t, schema.ScenarioLockStep, d.Scenario)
		assert.Contains(t, out, "Which scenario")
		assert.Contains(t, out, "scenarios.md")
		assert.Contains(t, out, "1. owner-writes")
		assert.Contains(t, out, "9. third-party-format")
	})

	t.Run("asks for the scenario, by name", func(t *testing.T) {
		t.Parallel()
		d, _, err := run(t, "owner-reads\n")
		require.NoError(t, err)
		assert.Equal(t, schema.ScenarioOwnerReads, d.Scenario)
	})

	t.Run("create-schema offers owner-writes and owner-reads, and not the old names", func(t *testing.T) {
		t.Parallel()
		_, out, err := run(t, "1\n")
		require.NoError(t, err)
		assert.Contains(t, out, "1. owner-writes: Owner writes:")
		assert.Contains(t, out, "2. owner-reads: Owner reads:")

		help := NewCreateSchemaCmd(nil)
		var usage bytes.Buffer
		help.SetOut(&usage)
		require.NoError(t, help.Usage())
		assert.Contains(t, help.Long, "owner-writes")
		assert.Contains(t, usage.String(), "owner-writes, owner-reads")

		for _, old := range []string{"provider-first", "consumer-first"} {
			assert.NotContains(t, out, old)
			assert.NotContains(t, help.Long, old)
			assert.NotContains(t, usage.String(), old)

			_, _, oErr := run(t, "", "--scenario", old)
			require.ErrorContains(t, oErr, "scenario "+old+" is now owner-", "--scenario refuses the old name")
			_, _, aErr := run(t, old+"\n")
			require.ErrorContains(t, aErr, "scenario "+old+" is now owner-", "the prompt refuses the old name")
		}
	})

	t.Run("a number off the menu is refused", func(t *testing.T) {
		t.Parallel()
		_, _, err := run(t, "10\n")
		require.ErrorContains(t, err, "not one of")
	})

	t.Run("with nobody to ask, says which flag to use", func(t *testing.T) {
		t.Parallel()
		_, _, err := run(t, "")
		require.ErrorContains(t, err, "--scenario")
	})

	t.Run("where documents live varies, takes it as a flag", func(t *testing.T) {
		t.Parallel()
		d, out, err := run(t, "", "--scenario", "uncoordinated", "--documents-live", "moment")
		require.NoError(t, err)
		assert.Equal(t, schema.DocumentsLiveMoment, d.Facts.DocumentsLive)
		assert.NotContains(t, out, "varies in scenario")
	})

	t.Run("where documents live varies, asks, and offers indefinitely for not knowing", func(t *testing.T) {
		t.Parallel()
		d, out, err := run(t, "9\nindefinitely\n")
		require.NoError(t, err)
		assert.Equal(t, schema.ScenarioThirdPartyFormat, d.Scenario)
		assert.Equal(t, schema.DocumentsLiveIndefinitely, d.Facts.DocumentsLive)
		assert.Contains(t, out, "If you don't know, choose indefinitely")
	})

	t.Run("where documents live varies, with nobody to ask, says what to do", func(t *testing.T) {
		t.Parallel()
		_, _, err := run(t, "", "--scenario", "third-party-format")
		require.ErrorContains(t, err, "--documents-live")
		require.ErrorContains(t, err, "choose indefinitely")
	})

	t.Run("a retention family starts with its first version as the oldest live", func(t *testing.T) {
		t.Parallel()
		d, _, err := run(t, "", "--scenario", "retained-stream")
		require.NoError(t, err)
		assert.Equal(t, schema.SemVer{1, 0, 0}, d.OldestLive)
	})
}
