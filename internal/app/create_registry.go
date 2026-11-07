package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

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
			cmd.Printf("%s", addEnvironmentVariableInstructions(pathResolver, dirpath))
			cmd.Println("\nTo create your first schema, use: the create-schema command. For details:")
			cmd.Printf("  jsm create-schema -h\n")

			return nil
		},
	}

	return cmd
}

func addEnvironmentVariableInstructions(pathResolver fsh.PathResolver, dirpath string) string {
	return addEnvironmentVariableInstructionsForOS(pathResolver, dirpath, runtime.GOOS)
}

func addEnvironmentVariableInstructionsForOS(pathResolver fsh.PathResolver, dirpath, goos string) string {
	abs, err := pathResolver.Abs(dirpath)
	if err != nil {
		abs = dirpath
	}

	envVar := schema.RootDirEnvVar
	instructions := "To use this registry by default, we recommend you set an environment variable. Run:\n"

	switch goos {
	case "windows":
		instructions += fmt.Sprintf("\n  setx %s %q && set %q\n", envVar, abs, envVar+"="+abs)
	case "darwin":
		instructions += fmt.Sprintf("\n  echo 'export %s=%q' >> ~/.zshrc && source ~/.zshrc\n", envVar, abs)
	default:
		instructions += fmt.Sprintf("\n  echo 'export %s=%q' >> ~/.bashrc && source ~/.bashrc\n", envVar, abs)
	}

	return instructions
}
