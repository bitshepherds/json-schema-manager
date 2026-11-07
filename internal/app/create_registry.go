package app

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bitshepherds/json-schema-manager/v2/internal/config"
	"github.com/bitshepherds/json-schema-manager/v2/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/v2/internal/schema"
)

// NewCreateRegistryCmd returns a new cobra command for creating a registry.
func NewCreateRegistryCmd(pathResolver fsh.PathResolver) *cobra.Command {
	cmd := &cobra.Command{
		Use:   CreateRegistryCmdName + " [dirpath]",
		Short: "Create a new JSON schema registry",
		Long:  `Create a new directory and initialise it with a default JSON schema manager configuration file.`,
		Args:  cobra.ExactArgs(1),
		Example: `
jsm create-registry ./my-registry
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dirpath := args[0]

			// 1. Create directory if it doesn't exist
			if err := os.MkdirAll(dirpath, 0o750); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

			configPath := filepath.Join(dirpath, config.JsmRegistryConfigFile)

			// 2. Check if config file already exists
			if _, err := os.Stat(configPath); err == nil {
				return fmt.Errorf("registry already exists: %s", configPath)
			}

			// 3. Write default config
			if err := os.WriteFile(configPath, []byte(config.DefaultConfigContent), 0o600); err != nil {
				return fmt.Errorf("failed to write configuration file: %w", err)
			}

			cmd.Printf("Successfully created new registry at: %s\n", dirpath)
			cmd.Printf("%s", registryUsageAdvice(pathResolver, dirpath))
			cmd.Println("\nTo create your first schema, use: the create-schema command. For details:")
			cmd.Printf("  jsm create-schema -h\n")

			return nil
		},
	}

	return cmd
}

// registryUsageAdvice says how to point jsm at the new registry: run it from the registry's
// folder, or give the folder with -r. It doesn't advise setting JSM_REGISTRY_ROOT_DIR for good,
// as a variable which follows a person everywhere sends jsm to that registry from any folder
// without a registry of its own.
func registryUsageAdvice(pathResolver fsh.PathResolver, dirpath string) string {
	abs, err := pathResolver.Abs(dirpath)
	if err != nil {
		abs = dirpath
	}
	abs = shellQuote(abs, runtime.GOOS)

	return fmt.Sprintf(`
jsm uses the registry in the folder it runs from. To use this one, run jsm from its folder:

  cd %[1]s

or give its path with -r from anywhere:

  jsm validate all -r %[1]s

$%[2]s names a registry for jsm to use when the current folder holds none. It suits a
pipeline, or one project through a per-project tool such as mise or direnv.
`, abs, schema.RootDirEnvVar)
}

// safeInShell matches a path a POSIX shell reads as one word, unquoted; safeInPowerShell, one
// that PowerShell does.
var (
	safeInShell      = regexp.MustCompile(`^[A-Za-z0-9_@+=:,./-]+$`)
	safeInPowerShell = regexp.MustCompile(`^[A-Za-z0-9_+=:./\\-]+$`)
)

// shellQuote quotes path so that a command advising it can be pasted, as one word with nothing
// expanded, into the default shell on goos: PowerShell on windows, and a POSIX shell such as sh,
// bash or zsh elsewhere. Both take single quotes literally; inside them, PowerShell writes a
// quote as two, and a POSIX shell closes the quotes around an escaped one. A path the shell
// reads as one word is left bare. On windows, the advice is for PowerShell, not cmd.
func shellQuote(path, goos string) string {
	if goos == "windows" {
		if safeInPowerShell.MatchString(path) {
			return path
		}
		return "'" + strings.ReplaceAll(path, "'", "''") + "'"
	}
	if safeInShell.MatchString(path) {
		return path
	}
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}
