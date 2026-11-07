package app

import (
	"github.com/spf13/cobra"

	"github.com/bitshepherds/json-schema-manager/v2/internal/config"
)

// deploymentHistoryHelp explains the git history that check-changes and build-dist need.
const deploymentHistoryHelp = `
The latest deployment anchor is the newest jsm-deploy/[env]/* tag in the history of HEAD, created by
'jsm tag-deployment'. This needs the history and tags back to that deployment: in GitHub Actions, check out
with 'fetch-depth: 0'. In a shallow clone without the tag, the command fails rather than guess. With no
deployment tag at all, every schema is treated as new.`

// NewCheckChangesCmd creates a new check-changes command.
func NewCheckChangesCmd(m Manager) *cobra.Command {
	var env string

	cmd := &cobra.Command{
		Use:   "check-changes [environment]",
		Short: "Check for schema changes against the latest deployment anchor for the given environment",
		Long: `
Check for schema changes against the latest deployment anchor for the given environment.
Use this command in a CI/CD pipeline to prevent deploying a schema change to an environment which does not permit
schema mutation. Your production environment should never permit schema mutation, but it is recommended to allow 
mutations in dev environments to allow teams to iterate on a new schema together before the contract is locked down.

In an environment which does not permit schema mutation, these changes since the deployment fail the check:
  - modifying a deployed schema file
  - modifying or deleting a pass/ or fail/ test document of a deployed version
  - changing the environment's privateUrlRoot or publicUrlRoot, which give deployed schemas their IDs
  - changing or removing the family.yml of a family with a deployed version, other than moving its
    oldestLive forward or changing its migration statements: a changed scenario means a new family
  - editing an entry of a compat-exceptions.yml which was there at the deployment, or removing one
    while it clears something: an entry which clears nothing, as validate judges it, may go
  - deleting a deployed version when documents written under it may still exist: any version if the
    family's documentsLive is indefinitely, or a version at or above oldestLive if it is retention
Adding new schemas and test documents is allowed, and so is deleting a whole version if its documents
can't still exist: any version if documentsLive is moment, or one below oldestLive if it is retention.
A family with no family.yml may gain one. A family's versions going all at once are judged as above, by the
family.yml they were deployed with. A deployed version may gain a compat-exceptions.yml, or new entries in
one, and a family.yml may gain or change migration statements.

In every environment, the check fails if any family has no family.yml, or an invalid one.
` + deploymentHistoryHelp,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				env = args[0]
			}
			return m.CheckChanges(cmd.Context(), config.Env(env))
		},
	}

	return cmd
}
