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
them is 3.0.0. The new version starts as a copy of the target's schema, so the target is
usually the newest version in the family.

What else it carries depends on the bump and on the family's declaration:

- A patch or minor bump copies the schema only. Earlier documents in the major run on the
  new version as the family's scenario says, and copies would run the same checks twice, so
  add only the documents for what this version changes.
- A major bump also copies the target's pass/ and fail/ documents, as a starting point to
  edit, since some of them may legitimately stop holding.
- A family with no pairs of versions to run, whose scenario has neither side ahead or whose
  reader picks its schema from the document, copies the documents on every bump, because
  nothing else would test the new version.

Other files in the target's directory, such as compat-exceptions.yml, belong to that version
and are never copied.

A major bump in a family whose documents live indefinitely, and whose reader doesn't pick its
schema from the document, needs a migration statement in family.yml before validate passes.
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
			cmd.Println("The schema, and the pass and fail directories for its test documents, can be found here:")
			cmd.Printf("  %s\n\n", s.Path(schema.HomeDir))
		}
		decl, dErr := mgr.Registry().FamilyDeclaration(kNew)
		if releaseType == schema.ReleaseTypeMajor && dErr == nil && decl.Facts.NeedsMigrationStatement() {
			cmd.Printf("This family's old documents stay around, so a new major needs a migration statement, "+
				"which validate requires. %s.\n\n", schema.MigrationStatementHelp(kNew.Major()))
		}

		return nil
	}

	return cmd
}
