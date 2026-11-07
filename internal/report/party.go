package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/bitshepherds/json-schema-manager/v2/internal/party"
	"github.com/bitshepherds/json-schema-manager/v2/internal/schema"
)

// WritePartyCheck writes the result of checking a party manifest.
func (tr *TextReporter) WritePartyCheck(w io.Writer, res *party.Result) error {
	divider := strings.Repeat("-", 40)
	_, _ = fmt.Fprintf(w, "%s\n%s %s (role: %s)\n%s\n",
		divider, tr.cs(colBoldWhite, "JSM PARTY CHECK:"), res.Manifest, res.Role, divider)

	for i := range res.Schemas {
		sr := &res.Schemas[i]
		statusCol := map[party.Status]string{
			party.Pass: colGreen, party.Notice: colGrey, party.Warn: colYellow, party.Fail: colRed,
		}[sr.Status()]
		_, _ = fmt.Fprintf(w, "%s %s", tr.cs(statusCol, "["+strings.ToUpper(string(sr.Status()))+"]"), sr.Entry)
		if sr.Problem == "" && sr.Notice == "" && sr.Retired == "" {
			_, _ = fmt.Fprintf(w, " %s", tr.cs(statusCol, fmt.Sprintf("(%d document %s)", len(sr.Runs),
				plural(len(sr.Runs), "run"))))
		}
		_, _ = fmt.Fprintln(w)

		if sr.Problem != "" {
			_, _ = fmt.Fprintf(w, "  %s %s\n", tr.cs(colRed, "✗"), sr.Problem)
		}
		tr.writePartyRuns(w, sr)
		if sr.Notice != "" {
			_, _ = fmt.Fprintf(w, "  %s %s\n", tr.cs(colGrey, "i"), sr.Notice)
		}
		if sr.Retired != "" {
			_, _ = fmt.Fprintf(w, "  %s %s\n", tr.cs(colYellow, "!"), sr.Retired)
		}
		if sr.NewerMajor != "" {
			_, _ = fmt.Fprintf(w, "  %s the family has a newer major version, %s, which this check doesn't cover: %s\n",
				tr.cs(colYellow, "!"), sr.NewerMajor, newerMajorAdvice(res.Role, sr.Key))
		}
	}

	failed, warned, notices := res.Count(party.Fail), res.Count(party.Warn), res.Count(party.Notice)
	statsCol := colBoldGreen
	if failed > 0 {
		statsCol = colBoldRed
	}
	stats := fmt.Sprintf("%d passed, %d failed, %d %s, %d %s", res.Count(party.Pass), failed, warned,
		plural(warned, "warning"), notices, plural(notices, "notice"))
	_, err := fmt.Fprintf(w, "%s\n%s%s\n%s\n", divider, tr.cs(colBoldWhite, "Party check summary: "),
		tr.cs(statsCol, stats), divider)
	return err
}

// writePartyRuns writes each run which failed, and each an exception names, under its later version.
func (tr *TextReporter) writePartyRuns(w io.Writer, sr *party.SchemaResult) {
	failures := sr.Failures()
	for i := range failures {
		spec := &failures[i]
		_, _ = fmt.Fprintf(w, "  %s %s: %s (%s):\n", tr.cs(colRed, "✗"), spec.ReportKey(),
			tr.cs(colGrey, spec.TestInfo.Path), tr.cs(colRed, spec.ResultLabel()))
		_, _ = fmt.Fprintf(w, "    %v\n", spec.Err)
	}
	excepted := sr.Excepted()
	for i := range excepted {
		spec := &excepted[i]
		_, _ = fmt.Fprintf(w, "  %s %s: %s (%s):\n", tr.cs(colYellow, "~"), spec.ReportKey(),
			tr.cs(colGrey, spec.TestInfo.Path), tr.cs(colYellow, spec.ResultLabel()))
		_, _ = fmt.Fprintf(w, "    reason: %s\n", spec.Exception.Reason)
	}
}

// newerMajorAdvice says what a party in the role should do about a newer major version.
func newerMajorAdvice(role party.Role, listed schema.Key) string {
	if role == party.RoleWriter {
		return fmt.Sprintf("move to it before readers stop accepting version %d", listed.Major())
	}
	return fmt.Sprintf("move to it before writers stop sending version %d", listed.Major())
}

type jsonPartySchema struct {
	Entry        string       `json:"entry"`
	Key          schema.Key   `json:"key,omitempty"`
	Status       party.Status `json:"status"`
	Problem      string       `json:"problem,omitempty"`
	Notice       string       `json:"notice,omitempty"`
	Retired      string       `json:"retired,omitempty"`
	DocumentRuns int          `json:"documentRuns"`
	Failed       []jsonSpec   `json:"failed"`
	Excepted     []jsonSpec   `json:"excepted"`
	NewerMajor   schema.Key   `json:"newerMajor,omitempty"`
}

type jsonPartyCheck struct {
	Manifest string     `json:"manifest"`
	Role     party.Role `json:"role"`
	Stats    struct {
		Passed   int `json:"passed"`
		Failed   int `json:"failed"`
		Warnings int `json:"warnings"`
		Notices  int `json:"notices"`
	} `json:"stats"`
	Schemas []jsonPartySchema `json:"schemas"`
}

// WritePartyCheck writes the result of checking a party manifest.
func (jr *JSONReporter) WritePartyCheck(w io.Writer, res *party.Result) error {
	out := jsonPartyCheck{Manifest: res.Manifest, Role: res.Role, Schemas: []jsonPartySchema{}}
	out.Stats.Passed = res.Count(party.Pass)
	out.Stats.Failed = res.Count(party.Fail)
	out.Stats.Warnings = res.Count(party.Warn)
	out.Stats.Notices = res.Count(party.Notice)

	for i := range res.Schemas {
		sr := &res.Schemas[i]
		out.Schemas = append(out.Schemas, jsonPartySchema{
			Entry:        sr.Entry,
			Key:          sr.Key,
			Status:       sr.Status(),
			Problem:      sr.Problem,
			Notice:       sr.Notice,
			Retired:      sr.Retired,
			DocumentRuns: len(sr.Runs),
			Failed:       jsonSpecs(sr.Failures(), "failed"),
			Excepted:     jsonSpecs(sr.Excepted(), statusExcepted),
			NewerMajor:   sr.NewerMajor,
		})
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// jsonSpecs returns the JSON form of the runs, with the given status.
func jsonSpecs(specs []schema.Spec, status string) []jsonSpec {
	out := make([]jsonSpec, len(specs))
	for i := range specs {
		out[i] = newJSONSpec(&specs[i], status)
	}
	return out
}
