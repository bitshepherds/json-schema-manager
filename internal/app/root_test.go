package app

import (
	"bytes"
	"io"
	"log/slog"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

func TestRootCmd(t *testing.T) {
	t.Parallel()

	setup := func() (*slog.LevelVar, *cobra.Command) {
		mgr := &MockManager{registry: &schema.Registry{}}
		lazy := &LazyManager{inner: mgr}
		logLevel := &slog.LevelVar{}
		var stdout, stderr bytes.Buffer
		rootCmd := NewRootCmd(lazy, logLevel, &stdout, &stderr, fsh.NewEnvProvider())
		return logLevel, rootCmd
	}

	t.Run("execute help", func(t *testing.T) {
		t.Parallel()
		_, rootCmd := setup()
		rootCmd.SetArgs([]string{"--help"})
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	t.Run("test version flag", func(t *testing.T) {
		t.Parallel()
		_, rootCmd := setup()
		rootCmd.SetArgs([]string{"--version"})
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	t.Run("test debug flag", func(t *testing.T) {
		t.Parallel()
		logLevel, rootCmd := setup()
		rootCmd.SetArgs([]string{"--debug"})
		// Execute a command that exists to trigger PersistentPreRunE
		// Since root has a RunE, Execute() should trigger it.
		err := rootCmd.Execute()
		require.NoError(t, err)
		assert.Equal(t, slog.LevelDebug, logLevel.Level())
	})

	t.Run("test root command execution", func(t *testing.T) {
		t.Parallel()
		_, rootCmd := setup()
		rootCmd.SetArgs([]string{})
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	t.Run("test completion command", func(t *testing.T) {
		t.Parallel()
		_, rootCmd := setup()
		rootCmd.SetArgs([]string{"completion", "bash"})
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	t.Run("test completion subcommand skips registry init", func(t *testing.T) {
		t.Parallel()
		lazy := &LazyManager{} // Empty lazy manager, no inner manager
		logLevel := &slog.LevelVar{}
		var stdout, stderr bytes.Buffer
		rootCmd := NewRootCmd(lazy, logLevel, &stdout, &stderr, fsh.NewEnvProvider())

		rootCmd.SetArgs([]string{"completion", "zsh"})
		// This should not fail even though registryPath is empty and lazy has no inner,
		// because PersistentPreRunE should skip initialization for completion.
		err := rootCmd.Execute()
		require.NoError(t, err)
		assert.False(t, lazy.HasInner(), "Registry should not have been initialised")
	})

	t.Run("test alternate flag spellings", func(t *testing.T) {
		t.Parallel()
		// Test that alternate spellings don't cause "unknown flag" errors
		variants := []string{"--nocolor", "--noColor", "--noColour"}
		for _, variant := range variants {
			t.Run(variant, func(t *testing.T) {
				t.Parallel()
				_, rootCmd := setup()
				// Use help to avoid registry init, but include the flag
				// flags are processed before PersistentPreRunE
				rootCmd.SetArgs([]string{"help", variant})
				err := rootCmd.Execute()
				require.NoError(t, err, "Flag %s should be recognised", variant)
			})
		}
	})

	t.Run("test help command", func(t *testing.T) {
		t.Parallel()
		_, rootCmd := setup()
		rootCmd.SetArgs([]string{"help"})
		err := rootCmd.Execute()
		require.NoError(t, err)
	})
}

// TestShortFlagsMeanOneThing checks that each short flag means the same on every command, apart
// from the root's -v for --version.
func TestShortFlagsMeanOneThing(t *testing.T) {
	t.Parallel()

	root := NewRootCmd(&LazyManager{}, new(slog.LevelVar), io.Discard, io.Discard, fsh.NewEnvProvider())
	meanings := make(map[string]map[string]bool)
	for _, cmd := range root.Commands() {
		cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if f.Shorthand == "" || f.Name == "help" {
				return
			}
			if meanings[f.Shorthand] == nil {
				meanings[f.Shorthand] = make(map[string]bool)
			}
			meanings[f.Shorthand][f.Name] = true
		})
	}
	for short, names := range meanings {
		assert.Len(t, names, 1, "-%s means %v", short, names)
	}

	csv, _, err := root.Find([]string{"create-schema-version"})
	require.NoError(t, err)
	assert.Nil(t, csv.Flags().Lookup("verbose"), "create-schema-version has no verbose output")
}
