package app

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

// NewCreateSchemaVersionCmd returns a new cobra command for creating a schema version.
//
//nolint:gocognit // high complexity command setup
func NewCreateSchemaVersionCmd(mgr Manager) *cobra.Command {
	var verbose bool
	var keyStr string
	var idStr string

	releaseTypesStr := fmt.Sprintf("%s, %s, or %s",
		schema.ReleaseTypeMajor,
		schema.ReleaseTypeMinor,
		schema.ReleaseTypePatch,
	)

	cmd := &cobra.Command{
		Use:   "create-schema-version [target] [release-type]",
		Short: "Create the next " + releaseTypesStr + " version of the given schema family",
		Long: `
Create the next ` + releaseTypesStr + ` version of the given schema family.
The new version is numbered after the highest existing version: for example, if 1.0.0,
1.1.0 and 2.0.0 exist, a minor version of 1.0.0 is 1.2.0, and a major version of any of
them is 3.0.0. The new version starts as a copy of the target, with its pass/ and fail/
test documents, so the target is usually the newest version in the family.
`,
		Args: cobra.RangeArgs(1, 2),
		Example: `
IDENTIFYING A TARGET SCHEMA
- By Key:
  jsm create-schema-version -k "domain_family_1_0_0" major
  jsm create-schema-version "domain_family_1_0_0" major
- By Canonical ID (i.e. its eventual production URL):
  jsm create-schema-version -i "https://js.myorg.com/domain_family_1_0_0.schema.json" minor
  jsm create-schema-version "https://js.myorg.com/domain_family_1_0_0.schema.json" minor
- By File Path:
  jsm create-schema-version "./path/to/domain_family_1_0_0.schema.json" major
`,
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show details of the new version")
	cmd.Flags().StringVarP(&keyStr, "key", "k", "", "Identify a target schema by its key")
	cmd.Flags().StringVarP(&idStr, "id", "i", "", "Identify a target schema by its canonical ID")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		var targetArg string
		var releaseTypeArg string
		if len(args) == 1 {
			releaseTypeArg = args[0]
		} else {
			targetArg = args[0]
			releaseTypeArg = args[1]
		}

		releaseType, err := schema.NewReleaseType(releaseTypeArg)
		if err != nil {
			return err
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

		k := target.Key
		// For this command, we need the resolver to resolve to a single schema.
		// If we got a scope instead of a key, try to resolve it to a single schema.
		if target.Key == nil {
			resolvedKey, sErr := resolver.ResolveScopeToSingleKey(cmd.Context(), *target.Scope, targetArg)
			if sErr != nil {
				return sErr
			}
			k = &resolvedKey
		}

		kNew, cErr := mgr.CreateSchemaVersion(*k, releaseType)
		if cErr != nil {
			return cErr
		}

		cmd.Printf("Successfully created new schema with key: %s\n\n", kNew)
		s, err := mgr.Registry().GetSchemaByKey(kNew)
		if err == nil {
			cmd.Println("The schema and its test documents can be found here:")
			cmd.Printf("  %s\n\n", s.Path(schema.HomeDir))
		}

		return nil
	}

	return cmd
}
