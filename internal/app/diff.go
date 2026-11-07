package app

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bitshepherds/json-schema-manager/internal/schema"
	"github.com/bitshepherds/json-schema-manager/internal/structdiff"
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

// writeChanges writes the changes from older to newer, one per line, each with the documents which
// show it, and what its family protects that none does.
func writeChanges(w io.Writer, older, newer schema.Key, changes []schema.Change) {
	if len(changes) == 0 {
		_, _ = fmt.Fprintf(w, "No structural changes from %s to %s\n", older, newer)
		return
	}

	_, _ = fmt.Fprintf(w, "Changes from %s to %s:\n", older, newer)
	for i := range changes {
		c := &changes[i]
		line := fmt.Sprintf("  %-8s %s", c.Change.Effect, c.Change.String())
		if notes := changeNotes(c); len(notes) > 0 {
			line += " (" + strings.Join(notes, "; ") + ")"
		}
		_, _ = fmt.Fprintln(w, line)
	}
}

// changeNotes says which documents show the change, and in which directions the family needs one.
func changeNotes(c *schema.Change) []string {
	var notes []string
	if len(c.UsedBy) > 0 {
		notes = append(notes, "used by "+documentNames(c.UsedBy))
	}
	if len(c.ShownBy) > 0 {
		notes = append(notes, "shown by "+documentNames(c.ShownBy))
	}
	for _, direction := range c.Missing {
		if direction == structdiff.Narrows {
			notes = append(notes, "no fail document shows it")
		} else {
			notes = append(notes, "no pass document uses it")
		}
	}
	return notes
}

// documentNames gives each document's path by its directory and name.
func documentNames(paths []string) string {
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = filepath.Join(filepath.Base(filepath.Dir(p)), filepath.Base(p))
	}
	return strings.Join(names, " ")
}
