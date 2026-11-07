package app

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bitshepherds/json-schema-manager/v2/internal/schema"
	"github.com/bitshepherds/json-schema-manager/v2/internal/structdiff"
)

// NewDiffCmd returns a new cobra command for listing the structural changes between two schemas.
func NewDiffCmd(mgr Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "diff [older] [newer]",
		Short: "List the structural changes from one schema version to another",
		Long: `
List the structural changes from one schema version to another, such as enum values added,
types allowed, properties added, and constraints relaxed or tightened. Every change is listed,
whatever the family's scenario. Each is marked:

  widens   the newer version accepts documents which the older one rejects
  narrows  the newer version rejects documents which the older one accepts
  alters   JSM cannot tell which way the change goes: it may widen, narrow, or do both

Notes beside a change name the documents which show it, from any version after the older one up to
the newer one, and flag a direction the family's scenario protects which no document shows:

  used by pass/x.json        this pass document has a value there which the older version rejects
  shown by fail/y.json       this fail document has a value there which the older version accepts,
                             and the newer one rejects
  no pass document uses it   flagged in a writer-ahead or either family, for a change which widens
                             or alters
  no fail document shows it  flagged in a reader-ahead or either family, for a change which narrows
                             or alters

A family with neither side ahead flags nothing. Nor does a pair which validate doesn't compare, such
as two majors or a version below oldestLive, and only the newer version's documents are named there.

Where a change has a precise test, such as an enum value added, that test decides whether a pass
document uses it. Otherwise the older version's subschema at that location decides.

'jsm validate' warns about what these notes flag, under the version which made the change. This
command doesn't read compat-exceptions.yml, so it still flags a change an entry there acknowledges,
which validate no longer warns about: what it says stays true, that no document shows the change.

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
