package report

import (
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

// TextReporter implements schema.Reporter for plain text output.
type TextReporter struct {
	Verbose   bool
	UseColour bool
}

const (
	colReset     = "\033[0m"
	colRed       = "\033[31m"
	colYellow    = "\033[33m"
	colGreen     = "\033[32m"
	colGrey      = "\033[90m"
	colWhite     = "\033[37m"
	colBoldRed   = "\033[1;31m"
	colBoldGreen = "\033[1;32m"
	colBoldWhite = "\033[1;37m"
)

// cs returns a string which will render with the given colour
// if colourisation is enabled.
func (tr *TextReporter) cs(c, s string) string {
	if !tr.UseColour {
		return s
	}
	return c + s + colReset
}

// Write implements the schema.Reporter interface.
//
//nolint:gocognit // high complexity report rendering logic
func (tr *TextReporter) Write(w io.Writer, r *schema.TestReport) error {
	divider := strings.Repeat("-", 40)

	_, _ = fmt.Fprintf(w, "%s\n", divider)
	_, _ = fmt.Fprint(w, tr.cs(colBoldWhite, "JSM TEST REPORT\n\n"))
	_, _ = fmt.Fprintf(w, "%s %s\n", tr.cs(colGrey, "Started: "), tr.cs(colWhite, r.StartTime.Format("15:04:05")))
	_, _ = fmt.Fprintf(w, "%s %s\n", tr.cs(colGrey, "Duration:"), tr.cs(colWhite, r.EndTime.Sub(r.StartTime).String()))
	_, _ = fmt.Fprintf(w, "%s\n", divider)

	// Collect all keys to ensure sorted output
	keysMap := make(map[schema.Key]bool)
	for k := range r.PassedTests {
		keysMap[k] = true
	}
	for k := range r.FailedTests {
		keysMap[k] = true
	}

	keys := make([]schema.Key, 0, len(keysMap))
	for k := range keysMap {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})

	totalPassed := 0
	totalFailed := 0

	for _, k := range keys {
		keyCol := colWhite
		passed := r.PassedTests[k]
		failed := r.FailedTests[k]
		totalPassed += len(passed)
		totalFailed += len(failed)

		statusText := "PASS"
		statusCol := colGreen
		if len(failed) > 0 {
			statusText = "FAIL"
			statusCol = colRed
			keyCol = colRed
		}

		status := tr.cs(statusCol, "["+statusText+"]")
		keyStr := tr.cs(keyCol, string(k)+schema.SchemaSuffix)
		suffix := fmt.Sprintf("(pass: %d, fail: %d)", len(passed), len(failed))

		_, _ = fmt.Fprintf(w, "%s %s %s\n", status, keyStr, tr.cs(statusCol, suffix))

		if tr.Verbose {
			// Show for each schema, the tests that passed and failed, along with any errors
			for _, spec := range passed {
				_, _ = fmt.Fprintf(w, "  %s %s (%s)\n",
					tr.cs(colGreen, "✓"),
					tr.cs(colGrey, spec.TestInfo.Path),
					tr.cs(colGreen, spec.ResultLabel()))
			}

			for _, spec := range failed {
				_, _ = fmt.Fprintf(w, "  %s %s (%s):\n",
					tr.cs(colRed, "✗"),
					tr.cs(colGrey, spec.TestInfo.Path),
					tr.cs(colRed, spec.ResultLabel()))
				_, _ = fmt.Fprintf(w, "    %v\n", spec.Err)
			}
		} else {
			// Just show the failures
			for _, spec := range failed {
				_, _ = fmt.Fprintf(w, "  %s %s (%s):\n",
					tr.cs(colRed, "✗"),
					tr.cs(colGrey, spec.TestInfo.Path),
					tr.cs(colRed, spec.ResultLabel()))
				_, _ = fmt.Fprintf(w, "    %v\n", spec.Err)
			}
		}
	}

	totalUnexercised := tr.writeUnexercised(w, r, divider)

	_, _ = fmt.Fprintf(w, "%s\n", divider)
	summaryLabel := tr.cs(colBoldWhite, "Test summary: ")
	summaryStats := fmt.Sprintf("%d passed, %d failed", totalPassed, totalFailed)
	if totalUnexercised > 0 {
		summaryStats += fmt.Sprintf(", %d unexercised %s", totalUnexercised, plural(totalUnexercised, "change"))
	}
	statsColor := colBoldGreen
	if totalFailed > 0 {
		statsColor = colBoldRed
	}
	_, err := fmt.Fprintf(w, "%s%s\n", summaryLabel, tr.cs(statsColor, summaryStats))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", divider)
	return err
}

// writeUnexercised writes the changes which no pass document uses, and returns how many there are.
func (tr *TextReporter) writeUnexercised(w io.Writer, r *schema.TestReport, divider string) int {
	keys := make([]schema.Key, 0, len(r.UnexercisedChanges))
	for k := range r.UnexercisedChanges {
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return 0
	}
	slices.Sort(keys)

	_, _ = fmt.Fprintf(w, "%s\n", divider)
	_, _ = fmt.Fprintln(w, tr.cs(colYellow, "Unexercised changes: no pass document uses these, so the compatibility "+
		"check cannot tell whether they break consumers of the version before."))

	total := 0
	for _, k := range keys {
		ucs := r.UnexercisedChanges[k]
		total += len(ucs)
		_, _ = fmt.Fprintf(w, "%s %s since %s:\n",
			tr.cs(colYellow, "[WARN]"), tr.cs(colWhite, string(k)+schema.SchemaSuffix), ucs[0].Earlier)
		for _, uc := range ucs {
			_, _ = fmt.Fprintf(w, "  %s %s (%s)\n", tr.cs(colYellow, "!"), uc.Change.String(), uc.Change.Effect)
		}
	}
	_, _ = fmt.Fprintln(w, "Add a pass document which uses each change. If the earlier version rejects it, "+
		"the change breaks its consumers and needs a new major version.")
	return total
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
