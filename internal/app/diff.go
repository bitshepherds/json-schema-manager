package app

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

// NewDiffCmd returns a new cobra command for listing the structural changes between two schemas.
func NewDiffCmd(mgr Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "diff [older] [newer]",
		Short: "List the structural changes from one schema version to another",
		Long: `
List the structural changes from one schema version to another, such as enum values added,
types allowed, properties added, and constraints relaxed or tightened. Each change is marked:

  widens   the newer version accepts documents which the older one rejects
  narrows  the newer version rejects documents which the older one accepts
  alters   the change may widen what the schema accepts, and JSM cannot tell

A change which widens or alters a schema may break consumers of the older version. For each, the
newer version's pass documents which use it are listed: those with a value there which the older
version rejects or, for a change where JSM cannot tell which values those are, any value there.
'jsm validate' warns about such a change in a version when none of its pass documents uses it,
since its compatibility check cannot then tell whether the change breaks consumers.

The comparison is structural: it follows local $refs, and compares other schemas' $refs by URL.
`,
		Args: cobra.ExactArgs(2),
		Example: `
  jsm diff shop_order_1_0_0 shop_order_1_1_0
  jsm diff "./shop/order/1/0/0/shop_order_1_0_0.schema.json" "./shop/order/1/1/0/shop_order_1_1_0.schema.json"
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			older, err := resolveOneSchema(cmd.Context(), mgr.Registry(), args[0], "", "")
			if err != nil {
				return err
			}
			newer, err := resolveOneSchema(cmd.Context(), mgr.Registry(), args[1], "", "")
			if err != nil {
				return err
			}

			changes, err := mgr.DiffSchemas(older, newer)
			if err != nil {
				return err
			}

			writeChanges(cmd.OutOrStdout(), older, newer, changes)
			return nil
		},
	}
}

// writeChanges writes the changes from older to newer, one per line.
func writeChanges(w io.Writer, older, newer schema.Key, changes []schema.Change) {
	if len(changes) == 0 {
		_, _ = fmt.Fprintf(w, "No structural changes from %s to %s\n", older, newer)
		return
	}

	_, _ = fmt.Fprintf(w, "Changes from %s to %s:\n", older, newer)
	for i := range changes {
		c := &changes[i].Change
		line := fmt.Sprintf("  %-8s %s", c.Effect, c.String())
		switch {
		case !c.MayBreak():
		case len(changes[i].UsedBy) == 0:
			line += " (no pass document uses it)"
		default:
			line += " (used by"
			for _, p := range changes[i].UsedBy {
				line += " " + filepath.Join(filepath.Base(filepath.Dir(p)), filepath.Base(p))
			}
			line += ")"
		}
		_, _ = fmt.Fprintln(w, line)
	}
}
