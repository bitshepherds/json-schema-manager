package app

import (
	"github.com/spf13/cobra"

	"github.com/bitshepherds/json-schema-manager/v2/internal/schema"
)

// NewValidateCmd creates a new validate command.
func NewValidateCmd(m Manager) *cobra.Command {
	var verbose bool
	var continueOnError bool
	var strict bool
	var keyStr string
	var idStr string
	var scopeStr string

	cmd := &cobra.Command{
		Use:   "validate [target]",
		Short: "Validate one or more JSON schemas",
		Long: `
Validate one or more JSON schemas against their test documents: each document in a schema's pass/
directory must be accepted, and each in its fail/ directory rejected.

The family's declaration, family.yml, then decides which documents run across versions, between each
schema and every earlier and later version with the same major version:

  who can be ahead  what runs across versions
  writer            later pass documents on earlier versions, earlier fail documents on later versions
  reader            earlier pass documents on later versions, later fail documents on earlier versions
  either            all four
  neither           none, and so too when the reader picks its schema from the document

The same pairs run however a schema is selected, so validating an earlier version also runs its pairs
with later versions. A failure appears under the later version, since that's what changed, and says
which rule broke, the two versions, the document, and what that means for the family's scenario. See
user-docs/scenarios.md.

The report counts runs, not documents. A run is one document checked against one schema version. A
version's own documents give one run each. A run across versions is counted under the later version of
the pair, and happens only in the directions the table above gives for the family's scenario, so the
count depends on the family: a version can show more runs than it has documents.

A version's compat-exceptions.yml can list a deliberate break of the rule, with its reason. The report
shows that run as excepted, and it doesn't fail. An entry which clears nothing is stale, and fails.

A family whose documents live indefinitely, and whose reader doesn't pick its schema from the document,
needs a migration statement in family.yml for each major past its first. Without one, validate fails,
under that major's first version.

These checks are only as strong as the documents, so validate also compares the same pairs of
versions structurally, and warns about each change in a direction the family protects which no
document shows. A writer-ahead family needs a pass document for a change which widens what's accepted,
such as an enum value added. A reader-ahead family needs a fail document for one which narrows it, a
family with either ahead needs both, and a family with neither ahead needs none. Where structdiff can't
tell which way a change goes, it needs the documents for the direction the family protects. The earlier
version's own documents show a change too, by the later version's verdict on them: a pass document the
later version rejects shows a narrowing, and a fail document it accepts shows a widening, whether that
run fails or is excepted as a deliberate break. A change no document can show is acknowledged in
compat-exceptions.yml. 'jsm diff' lists every structural change between two versions.

It also warns about each schema with no pass document, or no fail document, and each fail document
rejected for several independent reasons, where its family runs documents across versions. Documents
inherited from an earlier version in the major count as a schema's own here, though not all of them run on
it: in a writer-ahead family an inherited pass document doesn't run on the later version, and in a
reader-ahead family nor does an inherited fail document. With --verbose, the
report shows how much of each schema its pass documents reach (its properties, enum values, types,
anyOf and oneOf subschemas, and the paths through each if), and which keywords reject each fail
document. The JSON report always includes these.

Use --strict to fail on any warning.
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
	outputVal := newFormatValue(formatText, formatJSON, formatJUnit)
	cmd.Flags().VarP(outputVal, "output", "o", "Output format ("+outputVal.usage()+")")
	cmd.Flags().BoolVarP(&continueOnError, "continue-on-error", "C", false,
		"Continue testing even if a schema test fails (default is to stop on first error)")

	cmd.Flags().StringVarP(&keyStr, "key", "k", "", "Validate a schema identified by its key")
	cmd.Flags().StringVarP(&idStr, "id", "i", "", "Validate a schema identified by its canonical ID")
	cmd.Flags().StringVarP(&scopeStr, "search-scope", "s", "", "Validate all schemas identified by JSM Search Scope")
	cmd.Flags().BoolVar(&strict, "strict", false,
		"Fail on warnings: a schema with no pass or no fail document, a fail document rejected for several "+
			"reasons, or a change in a direction the family protects which no document shows")
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

		target, err := resolver.Resolve()
		if err != nil {
			return err
		}

		noColour, _ := cmd.Flags().GetBool("nocolour")
		useColour := !noColour

		if watch {
			return m.WatchValidation(cmd.Context(), target, verbose, outputVal.String(),
				useColour, continueOnError, strict, nil)
		}

		return m.ValidateSchema(cmd.Context(), target, verbose, outputVal.String(),
			useColour, continueOnError, strict)
	}

	return cmd
}
