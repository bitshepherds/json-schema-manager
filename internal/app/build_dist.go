package app

import (
	"github.com/spf13/cobra"

	"github.com/bitshepherds/json-schema-manager/internal/config"
)

// NewBuildDistCmd creates a new build-dist command.
func NewBuildDistCmd(m Manager) *cobra.Command {
	var all bool
	var collapse bool
	var skipTests bool
	var strict bool

	cmd := &cobra.Command{
		Use:   "build-dist [environment]",
		Short: "Build a distribution directory of rendered schemas",
		Long: `
Build rendered schemas for the specified environment and write them to dist/[env], next to
the registry root. So the registry must be in a subdirectory of its git repository, which
keeps dist inside the repository. Other commands work with the registry at the git root.

Before building, it runs every schema's tests, and the documents its family's declaration
runs across versions, as 'jsm validate all' does, and a stale entry in a compat-exceptions.yml
counts as a failure. If any fail, it prints the report, builds nothing and exits with code 1.
It also prints any warnings, and --strict fails on them, as
it does for validate. Use --skip-tests only if an earlier step of your pipeline has already
run validate: it is not a way past a failure, which is what a compat-exceptions.yml entry is for.

By default, this command only builds schemas that have been added or modified since 
the last successful deployment to that environment. A mutation check is performed 
first to ensure that existing schemas haven't been modified in environments where 
mutations are forbidden.

It finds them with git, so it leaves out schemas which git does not track yet, and
lists them: commit them to include them. In CI, every schema is committed.

If the --all (-a) flag is used, all schemas in the registry are rendered and written 
to the dist directory, skipping the mutation check.

If the --collapse flag is used, external $ref references to other JSM-managed
schemas in the same registry are resolved and inlined into a $defs block within each
schema. This produces self-contained schemas that can be used without loading their
dependencies. The collapse is recursive: transitive dependencies are also inlined.
Each collapsed schema is compiled on its own, and its pass and fail test documents
must give the same results as with the original, or the command fails.

WARNING: Using the --all (-a) flag is NOT recommended in a deployment pipeline, as it 
bypasses safety checks and may deploy unintended changes. It is primarily intended 
for local troubleshooting or manual overrides.
` + deploymentHistoryHelp,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env := args[0]
			if err := m.BuildDist(cmd.Context(), config.Env(env), all, collapse, skipTests, strict); err != nil {
				return err
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&all, "all", "a", false, "Render and build all schemas, skipping mutation checks")
	cmd.Flags().BoolVar(&collapse, "collapse", false,
		"Inline external $ref dependencies into each schema's $defs block")
	cmd.Flags().BoolVar(&skipTests, "skip-tests", false,
		"Build without first running the schemas' tests, for a pipeline which has already run validate")
	cmd.Flags().BoolVar(&strict, "strict", false,
		"Fail on warnings: a schema with no pass or no fail document, a fail document rejected for several "+
			"reasons, or a change in a direction the family protects which no document shows")
	cmd.MarkFlagsMutuallyExclusive("skip-tests", "strict")

	return cmd
}
