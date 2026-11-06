package app

import (
	"github.com/spf13/cobra"

	"github.com/bitshepherds/json-schema-manager/internal/party"
)

// NewCheckPartyCmd returns a new cobra command for checking a party's schema versions.
func NewCheckPartyCmd(m Manager) *cobra.Command {
	var strict bool
	outputVal := newFormatValue(formatText, formatJSON)

	cmd := &cobra.Command{
		Use:   "check-party [manifest]",
		Short: "Check whether later schema versions can harm a party on the versions it uses",
		Long: `
Check the schema versions a party uses, listed in its party manifest (` + party.ManifestFile + ` by default),
against the registry. A party is a service or tool which reads or writes a family's documents, and
may lag behind later versions. Run it in the party's CI, with the registry checked out, or in the
registry's CI for each party.

A manifest states the party's role, reader or writer, and lists schema keys or canonical URLs:

  role: reader
  schemas:
    - shop_order_1_0_0
    - https://json-schemas.myorg.io/customer_1_2_0.schema.json

The family's declared scenario decides what can harm the party. For each listed version, the check
fails if the version is not in the registry, and otherwise runs the documents the family's rule runs
between that version and each later version in its major, for the party's side:

  A reader in a writer-ahead or either family: later pass documents on the listed version, and the
  listed version's fail documents on later versions. A failure means a writer on the later version
  can send the reader a document it rejects.

  A writer in a reader-ahead or either family: the listed version's pass documents on later versions,
  and later fail documents on the listed version. A failure means a reader on the later version
  rejects a document the writer sends.

  Any other combination, such as a reader in a reader-ahead family, or any party in a neither family:
  no later version can harm this party, so nothing runs, and the check gives a notice saying why.

A failure is reported as validate reports it, under the later version, and a deliberate exception in
that version's compat-exceptions.yml applies here too. The check warns if the family has a newer
major version: it doesn't look across majors, so plan the party's move to it.

Use --strict to fail on warnings too, as on validate.
`,
		Args: cobra.MaximumNArgs(1),
		Example: `
  jsm check-party
  jsm check-party ./service/jsm-party.yml -o json
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			manifest := party.ManifestFile
			if len(args) > 0 {
				manifest = args[0]
			}
			noColour, _ := cmd.Flags().GetBool("nocolour")
			return m.CheckParty(cmd.Context(), manifest, outputVal.String(), !noColour, strict)
		},
	}

	cmd.Flags().VarP(outputVal, "output", "o", "Output format ("+outputVal.usage()+")")
	cmd.Flags().BoolVar(&strict, "strict", false, "Fail on warnings too, such as a family's newer major version")
	return cmd
}
