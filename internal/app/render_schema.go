package app

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

// NewRenderSchemaCmd returns a new cobra command for rendering a schema.
//
//nolint:gocognit // high complexity command setup
func NewRenderSchemaCmd(mgr Manager) *cobra.Command {
	var keyStr string
	var idStr string
	var envStr string
	var collapse bool

	cmd := &cobra.Command{
		Use:   "render-schema [target]",
		Short: "Output the rendered version of a schema",
		Args:  cobra.MaximumNArgs(1),
		Example: `
  jsm render-schema "domain_family_1_0_0"
  jsm render-schema -k "domain_family_1_0_0" --env dev
  jsm render-schema "https://js.myorg.com/domain_family_1_0_0.schema.json"
  jsm render-schema "domain_family_1_0_0" --collapse
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var targetArg string
			if len(args) > 0 {
				targetArg = args[0]
			}

			resolver := schema.NewTargetResolver(mgr.Registry(), targetArg)
			if keyStr != "" {
				resolver.SetKey(schema.Key(keyStr))
			}
			if idStr != "" {
				resolver.SetID(idStr)
			}

			target, err := resolver.Resolve()
			if err != nil {
				if targetArg == "" {
					return &schema.NoTargetArgumentError{}
				}
				return &schema.InvalidTargetArgumentError{Arg: targetArg}
			}

			if target.Key == nil {
				resolvedKey, sErr := resolver.ResolveScopeToSingleKey(cmd.Context(), *target.Scope, targetArg)
				if sErr != nil {
					return sErr
				}
				target.Key = &resolvedKey
			}

			rendered, err := mgr.RenderSchema(cmd.Context(), target, config.Env(envStr), collapse)
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(rendered))
			return nil
		},
	}

	cmd.Flags().StringVarP(&keyStr, "key", "k", "", "Identify a target schema by its key")
	cmd.Flags().StringVarP(&idStr, "id", "i", "", "Identify a target schema by its canonical ID")
	cmd.Flags().StringVarP(&envStr, "env", "e", "", "The environment to use for rendering (defaults to production)")
	cmd.Flags().BoolVarP(&collapse, "collapse", "C", false,
		"Inline external $ref dependencies into the schema's $defs block")

	return cmd
}
