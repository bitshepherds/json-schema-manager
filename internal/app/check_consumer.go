package app

import (
	"github.com/spf13/cobra"

	"github.com/bitshepherds/json-schema-manager/internal/consumer"
)

// NewCheckConsumerCmd returns a new cobra command for checking a consumer's schema versions.
func NewCheckConsumerCmd(m Manager) *cobra.Command {
	var strict bool
	outputVal := newFormatValue(formatText, formatJSON)

	cmd := &cobra.Command{
		Use:   "check-consumer [manifest]",
		Short: "Check the schema versions a consuming service validates against",
		Long: `
Check the schema versions a consuming service validates documents against, listed in its consumer
manifest (` + consumer.ManifestFile + ` by default), against the registry. Run it in the consumer's CI, with the
registry checked out, or in the registry's CI for each consumer.

A manifest lists schema keys or canonical URLs:

  schemas:
    - shop_order_1_0_0
    - https://json-schemas.myorg.io/customer_1_2_0.schema.json

Providers move to new versions before their consumers do. So for each listed version, the check
fails if the version is not in the registry, or if a later version with the same major version has
a pass document which the listed version rejects: a provider sending that document would break the
consumer. It warns if the family has a newer major version, which providers will move to: plan to
move the consumer to it during the deprecation window, while providers send both.

Use --strict to fail on warnings too.
`,
		Args: cobra.MaximumNArgs(1),
		Example: `
  jsm check-consumer
  jsm check-consumer ./service/jsm-consumer.yml -o json
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			manifest := consumer.ManifestFile
			if len(args) > 0 {
				manifest = args[0]
			}
			noColour, _ := cmd.Flags().GetBool("nocolour")
			return m.CheckConsumer(cmd.Context(), manifest, outputVal.String(), !noColour, strict)
		},
	}

	cmd.Flags().VarP(outputVal, "output", "o", "Output format ("+outputVal.usage()+")")
	cmd.Flags().BoolVar(&strict, "strict", false, "Fail if the family of a listed version has a newer major version")
	return cmd
}
