package app

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

// NewValidateCmd creates a new validate command.
func NewValidateCmd(m Manager) *cobra.Command {
	var verbose bool
	var continueOnError bool
	var skipCompatible bool
	var failUnexercised bool
	var keyStr string
	var idStr string
	var scopeStr string
	var testScopeStr string

	cmd := &cobra.Command{
		Use:   "validate [target]",
		Short: "Validate one or more JSON schemas",
		Long: `
Validate one or more JSON schemas against their test documents: each document in a schema's pass/
directory must be accepted, and each in its fail/ directory rejected.

Each schema's pass documents must also be accepted by every earlier version with the same major
version, so that a provider moving to the schema does not break consumers still on an earlier one.
Use --skip-compatible to leave this check out.

That check is only as strong as the pass documents, so validate also compares each schema with the
version before it, and warns about each structural change which may break consumers of that version
and which none of the schema's pass documents uses: for example, an enum value added with no
document using it. Add a pass document which uses the change, or use --fail-unexercised to fail
on these warnings. 'jsm diff' lists every structural change between two versions.
`,
		Args: cobra.MaximumNArgs(1),
		Example: `
IDENTIFYING A SINGLE SCHEMA
- By Key:
  jsm validate -k "domain_family_1_0_0" 
  jsm validate "domain_family_1_0_0" 
- By Canonical ID (i.e. its eventual production URL):
  jsm validate -i "https://js.myorg.com/domain_family_1_0_0.schema.json" 
  jsm validate "https://js.myorg.com/domain_family_1_0_0.schema.json" 
- By File Path:
  jsm validate "./path/to/domain_family_1_0_0.schema.json"

IDENTIFYING MULTIPLE SCHEMAS
- By File Path:
  jsm validate "./path/to/domain/family" - targets all schema versions in the given family
- By JSM Search Scope: (a string of elements separated by '/')
  jsm validate "domain/family" - targets all schemas within the given family

ALL SCHEMAS
  jsm validate all`,
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed test results")
	outputVal := formatValue("text")
	cmd.Flags().VarP(&outputVal, "output", "o", "Output format (text, json)")
	cmd.Flags().BoolVarP(&continueOnError, "continue-on-error", "C", false,
		"Continue testing even if a schema test fails (default is to stop on first error)")

	cmd.Flags().StringVarP(&keyStr, "key", "k", "", "Validate a schema identified by its key")
	cmd.Flags().StringVarP(&idStr, "id", "i", "", "Validate a schema identified by its canonical ID")
	cmd.Flags().StringVarP(&scopeStr, "search-scope", "s", "", "Validate all schemas identified by JSM Search Scope")
	cmd.Flags().StringVarP(&testScopeStr, "test-scope", "t", "local",
		fmt.Sprintf("Test Docs Selection (%s, %s, %s, %s, %s)",
			schema.TestScopeLocal,
			schema.TestScopePass,
			schema.TestScopeFail,
			schema.TestScopeConsumerBreaking,
			schema.TestScopeAll,
		))
	cmd.Flags().BoolVar(&skipCompatible, "skip-compatible", false,
		"Skip checking each schema's pass documents against earlier versions in its major family")
	cmd.Flags().BoolVar(&failUnexercised, "fail-unexercised", false,
		"Fail when a schema has a change which may break consumers of the version before it, "+
			"and none of its pass documents uses it")
	var watch bool
	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "Watch for changes and rerun tests")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		var arg string
		if len(args) > 0 {
			arg = args[0]
		}

		resolver := schema.NewTargetResolver(m.Registry(), arg)
		if keyStr != "" {
			resolver.SetKey(schema.Key(keyStr))
		}
		if idStr != "" {
			resolver.SetID(idStr)
		}
		if scopeStr != "" {
			resolver.SetScope(schema.SearchScope(scopeStr))
		}

		testScope, err := schema.NewTestScope(testScopeStr)
		if err != nil {
			return err
		}

		target, err := resolver.Resolve()
		if err != nil {
			return err
		}

		noColour, _ := cmd.Flags().GetBool("nocolour")
		useColour := !noColour

		if watch {
			return m.WatchValidation(cmd.Context(), target, verbose, string(outputVal),
				useColour, continueOnError, testScope, skipCompatible, nil)
		}

		return m.ValidateSchema(cmd.Context(), target, verbose, string(outputVal),
			useColour, continueOnError, testScope, skipCompatible, failUnexercised)
	}

	return cmd
}
