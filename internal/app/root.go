package app

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/bitshepherds/json-schema-manager/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/internal/repo"
	"github.com/bitshepherds/json-schema-manager/internal/schema"
	"github.com/bitshepherds/json-schema-manager/internal/validator"
)

// Version is the current version of jsm, set at build time.
var Version = "dev"

// CreateRegistryCmdName is the name of the command to create a new registry.
const CreateRegistryCmdName = "create-registry"

// Banner with colour codes and escaped backticks.
var Banner = "\033[32m" + `
       _______ ____  _   __                 
      / / ___// __ \/ | / /                 
 __  / /\__ \/ / / /  |/ /                  
/ /_/ /___/ / /_/ / /|  /                   
\____//____/\____/_/ |_/                    
                                            
   _____      __                            
  / ___/_____/ /_  ___  ____ ___  ____ _    
  \__ \/ ___/ __ \/ _ \/ __ ` + "`" + `__ \/ __ ` + "`" + `/    
 ___/ / /__/ / / /  __/ / / / / / /_/ /     
/____/\___/_/ / /_/\___/_/ /_/\__,_/      
                                            
    __  ___                                 
   /  |/  /___ _____  ____ _____ ____  _____
  / /|_/ / __ ` + "`" + `/ __ \/ __ ` + "`" + `/ __ ` + "`" + `/ _ \/ ___/
 / /  / / /_/ / / / / /_/ / /_/ /  __/ /    
/_/  /_/\__,_/_/ /_/\__,_/\__, /\___/_/     
                         /____/             
` + "\033[0m"

// LongDescription is the long description of the application.
var LongDescription = `
jsm is a CLI tool for developing and testing JSON Schemas which act as bulletproof
data contracts between services in an organisation. 
Use it to craft data contracts ahead of service implementation and validate that
changes in data contracts will not break existing services.

Exit codes:
  0  success: every test and check passed
  1  a test or check failed (validate, build-dist, check-changes, check-consumer)
  2  jsm could not run (usage, configuration, schema or git error)
  130, 143  jsm was stopped by SIGINT or SIGTERM before it finished
`

// NewRootCmd creates the root command and wires up dependencies.
func NewRootCmd(
	lazy *LazyManager,
	ll *slog.LevelVar,
	stdout, stderr io.Writer,
	envProvider fsh.EnvProvider,
) *cobra.Command {
	var debug bool
	var noColour bool
	var registryPath string

	rootCmd := &cobra.Command{
		Use:           "jsm",
		Short:         "A professional tool for managing JSON schemas",
		Version:       Version,
		SilenceErrors: true,
		SilenceUsage:  true,
		Long:          Banner + "\n" + LongDescription,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// Skip initialization for help, completion and create-registry commands
			if cmd.Name() == "help" || isCompletionCommand(cmd) || cmd.Name() == CreateRegistryCmdName {
				return nil
			}
			// Skip if already initialised (e.g., in tests)
			if lazy.HasInner() {
				if debug {
					ll.Set(slog.LevelDebug)
				}
				return nil
			}

			// 1. Setup Logging
			if debug {
				ll.Set(slog.LevelDebug)
			}

			// 2. Build Dependencies
			compiler := validator.NewSanthoshCompiler()
			pathResolver := fsh.NewPathResolver()

			registry, err := schema.NewRegistry(registryPath, compiler, pathResolver, envProvider)
			if err != nil {
				return fmt.Errorf("registry initialisation failed: %w", err)
			}

			logger, _, err := setupLogger(stderr, ll, registry.RootDirectory(), "", envProvider)
			if err != nil {
				logger.Warn("logging to file disabled", "error", err)
			}

			tester := schema.NewTester(registry)
			cfg, _ := registry.Config()
			// Run git in the registry's repository, wherever jsm is run from
			gitter := repo.NewCLIGitter(cfg, pathResolver, registry.RootDirectory())
			distBuilder := schema.NewFSDistBuilder(registry, cfg, gitter, "dist")

			// 3. Hydrate the Lazy Wrapper
			realMgr := NewCLIManager(logger, registry, tester, gitter, distBuilder, stdout)
			lazy.SetInner(realMgr)

			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&registryPath, "registry", "r", "",
		"path to the registry root directory (default: $"+schema.RootDirEnvVar+", or else the current directory)")
	rootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "Enable debug logging")

	rootCmd.PersistentFlags().BoolVarP(&noColour, "nocolour", "c", false, "Disable colour in output")
	// Support alternate spellings
	rootCmd.PersistentFlags().BoolVar(&noColour, "nocolor", false, "")
	rootCmd.PersistentFlags().BoolVar(&noColour, "noColor", false, "")
	rootCmd.PersistentFlags().BoolVar(&noColour, "noColour", false, "")
	_ = rootCmd.PersistentFlags().MarkHidden("nocolor")
	_ = rootCmd.PersistentFlags().MarkHidden("noColor")
	_ = rootCmd.PersistentFlags().MarkHidden("noColour")

	// Subcommands
	pathResolver := fsh.NewPathResolver()
	rootCmd.AddCommand(NewCreateRegistryCmd(pathResolver))
	rootCmd.AddCommand(NewValidateCmd(lazy))
	rootCmd.AddCommand(NewCreateSchemaCmd(lazy))
	rootCmd.AddCommand(NewCreateSchemaVersionCmd(lazy))
	rootCmd.AddCommand(NewRenderSchemaCmd(lazy))
	rootCmd.AddCommand(NewCheckChangesCmd(lazy))
	rootCmd.AddCommand(NewTagDeploymentCmd(lazy))
	rootCmd.AddCommand(NewBuildDistCmd(lazy))
	rootCmd.AddCommand(NewDiffCmd(lazy))
	rootCmd.AddCommand(NewCheckConsumerCmd(lazy))

	return rootCmd
}

// isCompletionCommand returns true if the command or any of its parents is the "completion" command.
func isCompletionCommand(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "completion" {
			return true
		}
	}
	return false
}
