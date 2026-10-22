package app

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

// NewRenderSchemaCmd returns a new cobra command for rendering a schema.
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

			key, err := resolveOneSchema(cmd.Context(), mgr.Registry(), targetArg, keyStr, idStr)
			if err != nil {
				return err
			}

			rendered, err := mgr.RenderSchema(cmd.Context(), schema.ResolvedTarget{Key: &key}, config.Env(envStr),
				collapse)
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
		"Inline external $ref dependencies into the schema's $defs block, failing if the result "+
			"does not compile or its test documents give different results")

	return cmd
}

// resolveOneSchema resolves a target argument, or the key or ID given by a flag, to one schema.
func resolveOneSchema(ctx context.Context, r *schema.Registry, targetArg, keyStr, idStr string) (schema.Key, error) {
	resolver := schema.NewTargetResolver(r, targetArg)
	if keyStr != "" {
		resolver.SetKey(schema.Key(keyStr))
	}
	if idStr != "" {
		resolver.SetID(idStr)
	}

	target, err := resolver.Resolve()
	if err != nil {
		if targetArg == "" {
			return "", &schema.NoTargetArgumentError{}
		}
		return "", &schema.InvalidTargetArgumentError{Arg: targetArg}
	}

	if target.Key != nil {
		return *target.Key, nil
	}
	return resolver.ResolveScopeToSingleKey(ctx, *target.Scope, targetArg)
}
