package app

import (
	"context"
	"io"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

func TestValidateCmd(t *testing.T) {
	t.Parallel()

	setup := func() (*MockManager, *cobra.Command) {
		mgr := &MockManager{
			registry: &schema.Registry{},
		}
		cmd := NewValidateCmd(mgr)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		// Add the persistent flags that NewValidateCmd expects from root
		cmd.Flags().Bool("nocolour", false, "")
		return mgr, cmd
	}

	t.Run("successful execution", func(t *testing.T) {
		t.Parallel()
		mgr, cmd := setup()
		path := "domain_family_1_0_0" // use a valid key
		mgr.On("ValidateSchema", mock.Anything, mock.AnythingOfType("schema.ResolvedTarget"),
			false, "text", true, false, schema.TestScopeLocal, false, false).Return(nil).Once()

		cmd.SetArgs([]string{path})
		err := cmd.ExecuteContext(context.Background())
		require.NoError(t, err)
		mgr.AssertExpectations(t)
	})

	t.Run("no args errors", func(t *testing.T) {
		t.Parallel()
		_, cmd := setup()
		cmd.SetArgs([]string{})
		err := cmd.ExecuteContext(context.Background())
		require.Error(t, err)
	})

	t.Run("validate all", func(t *testing.T) {
		t.Parallel()
		mgr, cmd := setup()
		scope := schema.SearchScope("")
		target := schema.ResolvedTarget{Scope: &scope}
		mgr.On("ValidateSchema", mock.Anything, target, false, "text", true, false, schema.TestScopeLocal, false, false).
			Return(nil).
			Once()
		cmd.SetArgs([]string{"all"})
		err := cmd.ExecuteContext(context.Background())
		require.NoError(t, err)
		mgr.AssertExpectations(t)
	})

	t.Run("too many args", func(t *testing.T) {
		t.Parallel()
		_, cmd := setup()
		cmd.SetArgs([]string{"s1.json", "s2.json"})
		err := cmd.ExecuteContext(context.Background())
		require.Error(t, err)
	})

	t.Run("validate by key", func(t *testing.T) {
		t.Parallel()
		mgr, cmd := setup()
		key := schema.Key("domain_family_1_0_0")
		target := schema.ResolvedTarget{Key: &key}
		mgr.On("ValidateSchema", mock.Anything, target, false, "text", true, false, schema.TestScopeLocal, false, false).
			Return(nil).
			Once()
		cmd.SetArgs([]string{"--key", "domain_family_1_0_0"})
		err := cmd.ExecuteContext(context.Background())
		require.NoError(t, err)
		mgr.AssertExpectations(t)
	})

	t.Run("validate by id", func(t *testing.T) {
		t.Parallel()
		mgr, cmd := setup()
		key := schema.Key("domain_family_1_0_0")
		target := schema.ResolvedTarget{Key: &key}
		mgr.On("ValidateSchema", mock.Anything, target, false, "text", true, false, schema.TestScopeLocal, false, false).
			Return(nil).
			Once()
		cmd.SetArgs([]string{"--id", "https://example.com/domain_family_1_0_0.schema.json"})
		err := cmd.ExecuteContext(context.Background())
		require.NoError(t, err)
		mgr.AssertExpectations(t)
	})

	t.Run("validate by scope", func(t *testing.T) {
		t.Parallel()
		mgr, cmd := setup()
		scope := schema.SearchScope("test/scope")
		target := schema.ResolvedTarget{Scope: &scope}
		mgr.On("ValidateSchema", mock.Anything, target, false, "text", true, false, schema.TestScopeLocal, false, false).
			Return(nil).
			Once()
		cmd.SetArgs([]string{"--search-scope", "test/scope"})
		err := cmd.ExecuteContext(context.Background())
		require.NoError(t, err)
		mgr.AssertExpectations(t)
	})

	t.Run("key flag overrides positional arg", func(t *testing.T) {
		t.Parallel()
		mgr, cmd := setup()
		key := schema.Key("domain_family_1_0_0")
		target := schema.ResolvedTarget{Key: &key}
		mgr.On("ValidateSchema", mock.Anything, target, false, "text", true, false, schema.TestScopeLocal, false, false).
			Return(nil).
			Once()
		cmd.SetArgs([]string{"pos_arg", "--key", "domain_family_1_0_0"})
		err := cmd.ExecuteContext(context.Background())
		require.NoError(t, err)
		mgr.AssertExpectations(t)
	})

	t.Run("resolver error", func(t *testing.T) {
		t.Parallel()
		_, cmd := setup()
		cmd.SetArgs([]string{"--id", "https://example.com/bad.txt"})
		err := cmd.ExecuteContext(context.Background())
		require.Error(t, err)
	})

	t.Run("invalid key flag errors", func(t *testing.T) {
		t.Parallel()
		_, cmd := setup()
		cmd.SetArgs([]string{"--key", "domain-a"})
		err := cmd.ExecuteContext(context.Background())
		require.Error(t, err)
	})

	t.Run("test-scope flag", func(t *testing.T) {
		t.Parallel()
		mgr, cmd := setup()
		key := schema.Key("domain_family_1_0_0")
		target := schema.ResolvedTarget{Key: &key}
		mgr.On("ValidateSchema", mock.Anything, target, false, "text", true, false, schema.TestScopeConsumerBreaking,
			false, false).
			Return(nil).
			Once()
		cmd.SetArgs([]string{"domain_family_1_0_0", "--test-scope", "consumer-breaking"})
		err := cmd.ExecuteContext(context.Background())
		require.NoError(t, err)
		mgr.AssertExpectations(t)
	})

	t.Run("invalid test-scope flag", func(t *testing.T) {
		t.Parallel()
		_, cmd := setup()
		cmd.SetArgs([]string{"domain_family_1_0_0", "--test-scope", "invalid"})
		err := cmd.ExecuteContext(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Invalid test scope")
	})

	t.Run("skip-compatible flag", func(t *testing.T) {
		t.Parallel()
		mgr, cmd := setup()
		key := schema.Key("domain_family_1_0_0")
		target := schema.ResolvedTarget{Key: &key}
		mgr.On("ValidateSchema", mock.Anything, target, false, "text", true, false, schema.TestScopeLocal, true, false).
			Return(nil).Once()
		cmd.SetArgs([]string{"domain_family_1_0_0", "--skip-compatible"})
		err := cmd.ExecuteContext(context.Background())
		require.NoError(t, err)
		mgr.AssertExpectations(t)
	})

	t.Run("nocolour flag", func(t *testing.T) {
		t.Parallel()
		mgr, cmd := setup()
		key := schema.Key("domain_family_1_0_0")
		target := schema.ResolvedTarget{Key: &key}
		mgr.On("ValidateSchema", mock.Anything, target, false, "text", false, false, schema.TestScopeLocal, false, false).
			Return(nil).
			Once()
		cmd.SetArgs([]string{"domain_family_1_0_0", "--nocolour"})
		err := cmd.ExecuteContext(context.Background())
		require.NoError(t, err)
		mgr.AssertExpectations(t)
	})

	t.Run("watch flag", func(t *testing.T) {
		t.Parallel()
		mgr, cmd := setup()
		key := schema.Key("domain_family_1_0_0")
		target := schema.ResolvedTarget{Key: &key}
		mgr.On("WatchValidation", mock.Anything, target, false, "text", true, false,
			schema.TestScopeLocal, false, (chan<- struct{})(nil)).
			Return(nil).Once()
		cmd.SetArgs([]string{"domain_family_1_0_0", "--watch"})
		err := cmd.ExecuteContext(context.Background())
		require.NoError(t, err)
		mgr.AssertExpectations(t)
	})
}
