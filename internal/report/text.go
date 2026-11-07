package report

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/bitshepherds/json-schema-manager/v2/internal/schema"
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

	keys := reportKeys(r)
	totalPassed := 0
	totalFailed := 0
	totalExcepted := 0
	totalStale := 0
	totalAcknowledged := 0

	for _, k := range keys {
		keyCol := colWhite
		passed := r.PassedTests[k]
		failed := r.FailedTests[k]
		totalPassed += len(passed)
		totalFailed += len(failed)
		excepted := r.ExceptedTests[k]
		stale := r.StaleExceptions[k]
		totalExcepted += len(excepted)
		totalStale += len(stale)

		summary := r.Summaries[k]
		statusText := "PASS"
		statusCol := colGreen
		switch {
		case len(failed) > 0 || len(stale) > 0 || hasMissingMigration(r, k):
			statusText = "FAIL"
			statusCol = colRed
			keyCol = colRed
		case summary != nil && summary.HasWarnings():
			statusText = "WARN"
			statusCol = colYellow
		}

		status := tr.cs(statusCol, "["+statusText+"]")
		keyStr := tr.cs(keyCol, string(k)+schema.SchemaSuffix)
		suffix := countsSuffix(len(passed), len(failed), len(excepted))

		_, _ = fmt.Fprintf(w, "%s %s %s\n", status, keyStr, tr.cs(statusCol, suffix))

		if tr.Verbose {
			// Show for each schema, the tests that passed and failed, along with any errors
			for i := range passed {
				spec := &passed[i]
				_, _ = fmt.Fprintf(w, "  %s %s (%s)\n",
					tr.cs(colGreen, "✓"),
					tr.cs(colGrey, spec.TestInfo.Path),
					tr.cs(colGreen, spec.ResultLabel()))
			}

			for i := range failed {
				spec := &failed[i]
				_, _ = fmt.Fprintf(w, "  %s %s (%s):\n",
					tr.cs(colRed, "✗"),
					tr.cs(colGrey, spec.TestInfo.Path),
					tr.cs(colRed, spec.ResultLabel()))
				_, _ = fmt.Fprintf(w, "    %v\n", spec.Err)
			}
		} else {
			// Just show the failures
			for i := range failed {
				spec := &failed[i]
				_, _ = fmt.Fprintf(w, "  %s %s (%s):\n",
					tr.cs(colRed, "✗"),
					tr.cs(colGrey, spec.TestInfo.Path),
					tr.cs(colRed, spec.ResultLabel()))
				_, _ = fmt.Fprintf(w, "    %v\n", spec.Err)
			}
		}
		tr.writeMissingMigration(w, r, k)
		tr.writeExceptions(w, excepted, stale)
		tr.writeAcknowledged(w, r.AcknowledgedChanges[k])
		totalAcknowledged += len(r.AcknowledgedChanges[k])
		tr.writeSummary(w, summary)
	}

	totalUnexercised := tr.writeUnexercised(w, r, divider)

	_, _ = fmt.Fprintf(w, "%s\n", divider)
	summaryLabel := tr.cs(colBoldWhite, "Test summary: ")
	summaryStats := fmt.Sprintf("%d passed, %d failed", totalPassed, totalFailed)
	for _, part := range []struct {
		n      int
		format string
	}{
		{totalExcepted, ", %d excepted"},
		{totalAcknowledged, ", %d acknowledged"},
		{totalStale, ", %d stale " + plural(totalStale, "exception")},
		{len(r.MissingMigrations), ", %d missing migration " + plural(len(r.MissingMigrations), "statement")},
		{totalUnexercised, ", %d unexercised " + plural(totalUnexercised, "change")},
		{r.MissingDocuments(), ", %d " + plural(r.MissingDocuments(), "schema") + " without pass or fail documents"},
	} {
		if part.n > 0 {
			summaryStats += fmt.Sprintf(part.format, part.n)
		}
	}
	statsColor := colBoldGreen
	if r.HasFailures() {
		statsColor = colBoldRed
	}
	_, err := fmt.Fprintf(w, "%s%s\n", summaryLabel, tr.cs(statsColor, summaryStats))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", divider)
	return err
}

// countsSuffix says how a schema's runs went, leaving out excepted runs if there are none.
func countsSuffix(passed, failed, excepted int) string {
	if excepted > 0 {
		return fmt.Sprintf("(pass: %d, fail: %d, excepted: %d)", passed, failed, excepted)
	}
	return fmt.Sprintf("(pass: %d, fail: %d)", passed, failed)
}

// reportKeys returns the keys of every schema in the report, sorted.
func reportKeys(r *schema.TestReport) []schema.Key {
	keysMap := make(map[schema.Key]bool)
	for k := range r.PassedTests {
		keysMap[k] = true
	}
	for k := range r.FailedTests {
		keysMap[k] = true
	}
	for k := range r.Summaries {
		keysMap[k] = true
	}
	for k := range r.ExceptedTests {
		keysMap[k] = true
	}
	for k := range r.StaleExceptions {
		keysMap[k] = true
	}
	for k := range r.MissingMigrations {
		keysMap[k] = true
	}
	for k := range r.AcknowledgedChanges {
		keysMap[k] = true
	}
	return slices.Sorted(maps.Keys(keysMap))
}

func hasMissingMigration(r *schema.TestReport, k schema.Key) bool {
	_, missing := r.MissingMigrations[k]
	return missing
}

// writeMissingMigration writes why the major whose first version is k needs a migration statement, if it does.
func (tr *TextReporter) writeMissingMigration(w io.Writer, r *schema.TestReport, k schema.Key) {
	if m, missing := r.MissingMigrations[k]; missing {
		_, _ = fmt.Fprintf(w, "  %s %s\n", tr.cs(colRed, "✗"), m.Why)
	}
}

// writeExceptions writes each run a schema's exception file excepts, with its reason, and each entry
// of the file which is stale.
func (tr *TextReporter) writeExceptions(w io.Writer, excepted []schema.Spec, stale []schema.StaleException) {
	for i := range excepted {
		spec := &excepted[i]
		_, _ = fmt.Fprintf(w, "  %s %s (%s):\n",
			tr.cs(colYellow, "~"),
			tr.cs(colGrey, spec.TestInfo.Path),
			tr.cs(colYellow, spec.ResultLabel()))
		_, _ = fmt.Fprintf(w, "    reason: %s\n", spec.Exception.Reason)
	}
	for _, s := range stale {
		_, _ = fmt.Fprintf(w, "  %s %s:\n", tr.cs(colRed, "✗"), tr.cs(colRed, "stale exception: "+s.Exception.String()))
		_, _ = fmt.Fprintf(w, "    %s. Remove it from %s.\n", s.Why, schema.ExceptionsFile)
	}
}

// writeSummary writes a warning if the schema has no pass or no fail documents, of its own or
// inherited, and, when verbose, what it inherits and how well its own documents cover it.
func (tr *TextReporter) writeSummary(w io.Writer, s *schema.Summary) {
	if s == nil {
		return
	}
	var missing []string
	if s.MissingPassDocuments() {
		missing = append(missing, "pass")
	}
	if s.MissingFailDocuments() {
		missing = append(missing, "fail")
	}
	if len(missing) > 0 {
		_, _ = fmt.Fprintf(w, "  %s no %s documents\n", tr.cs(colYellow, "!"), strings.Join(missing, " or "))
	}
	for _, path := range slices.Sorted(maps.Keys(s.SeveralReasons)) {
		_, _ = fmt.Fprintf(w, "  %s %s\n", tr.cs(colYellow, "!"), severalReasonsWarning(path, s.SeveralReasons[path]))
	}
	if !tr.Verbose {
		return
	}

	if s.InheritedPassDocs+s.InheritedFailDocs > 0 {
		_, _ = fmt.Fprintf(w, "  %s %d pass and %d fail documents from earlier versions in its major\n",
			tr.cs(colGrey, "inherited:"), s.InheritedPassDocs, s.InheritedFailDocs)
	}
	_, _ = fmt.Fprintf(w, "  %s %d of %d points reached by pass documents\n",
		tr.cs(colGrey, "coverage:"), s.Covered(), len(s.Coverage))
	for _, p := range s.Coverage {
		if !p.Covered {
			_, _ = fmt.Fprintf(w, "    %s %s: %s %s\n", tr.cs(colYellow, "-"), p.Location, p.Kind, p.Detail)
		}
	}
	for _, path := range slices.Sorted(maps.Keys(s.FailKeywords)) {
		_, _ = fmt.Fprintf(w, "  %s %s\n", tr.cs(colGrey, path+" is rejected by"),
			strings.Join(s.FailKeywords[path], ", "))
	}
}

// severalReasonsWarning says that the fail document at path is rejected for reasons at several locations.
func severalReasonsWarning(path string, locations []string) string {
	return fmt.Sprintf(
		"%s is rejected for several reasons, at %s: split it, as one reason can hide a change to another",
		path,
		strings.Join(locations, ", "),
	)
}

// writeUnexercised writes the changes which need a document, and returns how many there are.
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
	_, _ = fmt.Fprintln(w, tr.cs(colYellow, "Unexercised changes: no document shows these in the direction this "+
		"family protects, so the checks cannot tell whether they break anyone."))

	total := 0
	for _, k := range keys {
		ucs := r.UnexercisedChanges[k]
		total += len(ucs)
		var earlier schema.Key
		for i := range ucs {
			uc := &ucs[i]
			if uc.Earlier != earlier {
				earlier = uc.Earlier
				_, _ = fmt.Fprintf(w, "%s %s since %s:\n",
					tr.cs(colYellow, "[WARN]"), tr.cs(colWhite, string(k)+schema.SchemaSuffix), earlier)
			}
			_, _ = fmt.Fprintf(w, "  %s %s (%s): needs a %s document\n",
				tr.cs(colYellow, "!"), uc.Change.String(), uc.Change.Effect, uc.NeededDocument())
		}
	}
	_, _ = fmt.Fprintf(
		w,
		"Add a document which uses each change: if the rule then fails, its failure says what to do. "+
			"If no document can show a change, acknowledge it in %s.\n",
		schema.ExceptionsFile,
	)
	return total
}

// writeAcknowledged writes the changes a schema's exception file acknowledges.
func (tr *TextReporter) writeAcknowledged(w io.Writer, acknowledged []schema.AcknowledgedChange) {
	for i := range acknowledged {
		ac := &acknowledged[i]
		_, _ = fmt.Fprintf(w, "  %s %s since %s (acknowledged):\n",
			tr.cs(colYellow, "~"), ac.Change.String(), ac.Earlier)
		_, _ = fmt.Fprintf(w, "    reason: %s\n", ac.Reason)
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
