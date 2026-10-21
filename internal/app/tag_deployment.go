package app

import (
	"github.com/spf13/cobra"

	"github.com/bitshepherds/json-schema-manager/internal/config"
)

// NewTagDeploymentCmd returns a new cobra command for tagging a deployment.
func NewTagDeploymentCmd(mgr Manager) *cobra.Command {
	var env string
	var noPush bool

	cmd := &cobra.Command{
		Use:   "tag-deployment [environment]",
		Short: "Record a successful deployment by creating a JSM deployment tag",
		Long: `
Create and push an environment-specific git tag to mark a successful deployment of schemas. 
This tag will be used as the anchor for future 'check-changes' runs to verify that 
already-deployed schemas are not modified in environments where mutations are forbidden.

If the tag cannot be pushed to origin, the command fails, since later runs elsewhere will not find it.
Use --no-push to create the tag locally only.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				env = args[0]
			}
			return mgr.TagDeployment(cmd.Context(), config.Env(env), !noPush)
		},
	}

	cmd.Flags().BoolVar(&noPush, "no-push", false, "Create the deployment tag locally without pushing it to origin")

	return cmd
}
