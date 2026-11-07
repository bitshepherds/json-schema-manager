// Package report provides reporting functionality for JSM.
package report

import (
	"encoding/json"
	"io"
	"maps"
	"slices"
	"time"

	"github.com/bitshepherds/json-schema-manager/v2/internal/coverage"
	"github.com/bitshepherds/json-schema-manager/v2/internal/schema"
	"github.com/bitshepherds/json-schema-manager/v2/internal/structdiff"
)

// JSONReporter implements schema.Reporter for JSON output.
type JSONReporter struct{}

// statusExcepted is the status of a run which gave the wrong outcome, and which an exception names.
const statusExcepted = "excepted"

type jsonSpec struct {
	Path         string      `json:"path"`
	TestDocType  string      `json:"type"`
	ValidatedBy  schema.Key  `json:"validatedBy,omitempty"`  // the schema the document was tested against
	Rule         schema.Rule `json:"rule,omitempty"`         // what the run proves
	DocumentFrom schema.Key  `json:"documentFrom,omitempty"` // the version the document belongs to
	Earlier      schema.Key  `json:"earlier,omitempty"`      // for a run across versions, the earlier version
	Later        schema.Key  `json:"later,omitempty"`        // for a run across versions, the later version
	Status       string      `json:"status"`                 // passed, failed or excepted
	Reason       string      `json:"reason,omitempty"`       // for an excepted run, the reason its exception gives
	Error        string      `json:"error,omitempty"`
}

// newJSONSpec returns the JSON form of s, with the given status.
func newJSONSpec(s *schema.Spec, status string) jsonSpec {
	js := jsonSpec{
		Status:       status,
		Path:         s.TestInfo.Path,
		TestDocType:  string(s.TestDocType),
		Rule:         s.Rule,
		DocumentFrom: s.DocumentFrom,
		Earlier:      s.Earlier,
		Later:        s.Later,
	}
	if s.Schema != nil {
		js.ValidatedBy = s.Schema.Key()
	}
	if s.Err != nil {
		js.Error = s.Err.Error()
	}
	if status == statusExcepted {
		js.Reason = s.Exception.Reason
	}
	return js
}

type jsonSchemaResults struct {
	Passed []jsonSpec `json:"passed"`
	Failed []jsonSpec `json:"failed"`
	// Excepted lists runs which gave the wrong outcome, and which an exception names. They aren't failures.
	Excepted []jsonSpec   `json:"excepted"`
	Summary  *jsonSummary `json:"summary,omitempty"` // set for each schema tested against its own documents
}

// jsonSummary describes how well a schema's test documents cover it.
type jsonSummary struct {
	PassDocuments          int                 `json:"passDocuments"`
	FailDocuments          int                 `json:"failDocuments"`
	InheritedPassDocuments int                 `json:"inheritedPassDocuments"`
	InheritedFailDocuments int                 `json:"inheritedFailDocuments"`
	MissingDocuments       bool                `json:"missingDocuments"` // no pass or no fail document, own or inherited
	Covered                int                 `json:"covered"`
	Total                  int                 `json:"total"`
	Points                 []coverage.Point    `json:"points"`
	FailKeywords           map[string][]string `json:"failKeywords"` // fail document path: the keywords which reject it
	// SeveralReasons gives, for a fail document's path, where its independent reasons are.
	SeveralReasons map[string][]string `json:"severalReasons,omitempty"`
}

func newJSONSummary(s *schema.Summary) *jsonSummary {
	points := s.Coverage
	if points == nil {
		points = []coverage.Point{}
	}
	return &jsonSummary{
		PassDocuments:          s.PassDocs,
		FailDocuments:          s.FailDocs,
		InheritedPassDocuments: s.InheritedPassDocs,
		InheritedFailDocuments: s.InheritedFailDocs,
		MissingDocuments:       s.MissingDocuments(),
		Covered:                s.Covered(),
		Total:                  len(s.Coverage),
		Points:                 points,
		FailKeywords:           s.FailKeywords,
		SeveralReasons:         s.SeveralReasons,
	}
}

// jsonUnexercisedChange is a change which no pass document of the schema uses.
type jsonUnexercisedChange struct {
	Schema   schema.Key        `json:"schema"`
	Earlier  schema.Key        `json:"earlier"`
	Later    schema.Key        `json:"later"`
	Location string            `json:"location"`
	Keyword  string            `json:"keyword,omitempty"`
	Effect   structdiff.Effect `json:"effect"`
	// Direction is the way which needs evidence: widens needs a pass document, and narrows a fail document.
	Direction structdiff.Effect `json:"direction"`
	Detail    string            `json:"detail"`
}

// jsonAcknowledgedChange is a change a schema's exception file acknowledges, as no document can show it.
type jsonAcknowledgedChange struct {
	Schema   schema.Key        `json:"schema"`
	Earlier  schema.Key        `json:"earlier"`
	Later    schema.Key        `json:"later"`
	Location string            `json:"location"`
	Keyword  string            `json:"keyword,omitempty"`
	Effect   structdiff.Effect `json:"effect"`
	Detail   string            `json:"detail"`
	Reason   string            `json:"reason"`
}

// jsonMissingMigration is a major which needs a migration statement and has none, and so fails the run.
type jsonMissingMigration struct {
	Schema schema.Key `json:"schema"` // the major's first version
	Major  uint64     `json:"major"`
	Why    string     `json:"why"`
}

// jsonStaleException is an entry of a schema's exception file which clears nothing, and so fails the run.
type jsonStaleException struct {
	Schema   schema.Key  `json:"schema"` // the later version, whose file lists it
	Version  string      `json:"version"`
	Document string      `json:"document,omitempty"`
	Rule     schema.Rule `json:"rule,omitempty"`
	Change   string      `json:"change,omitempty"`
	Reason   string      `json:"reason"`
	Why      string      `json:"why"` // why it's stale
}

type jsonOutput struct {
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
	Duration  string `json:"duration"`
	Stats     struct {
		TotalPassed      int `json:"totalPassed"`
		TotalFailed      int `json:"totalFailed"`
		TotalExcepted    int `json:"totalExcepted"`
		TotalStale       int `json:"totalStaleExceptions"`
		TotalMigrations  int `json:"totalMissingMigrations"`
		TotalUnexercised int `json:"totalUnexercised"`
		TotalMissingDocs int `json:"totalMissingDocuments"`
		TotalNotTested   int `json:"totalNotTested"` // schemas not tested, as the run stopped at the first failure
	} `json:"stats"`
	// StoppedEarly says the run stopped at the first failure, so results leaves out the schemas it didn't test.
	StoppedEarly        bool                             `json:"stoppedEarly"`
	Results             map[schema.Key]jsonSchemaResults `json:"results"`
	UnexercisedChanges  []jsonUnexercisedChange          `json:"unexercisedChanges"`
	AcknowledgedChanges []jsonAcknowledgedChange         `json:"acknowledgedChanges"`
	StaleExceptions     []jsonStaleException             `json:"staleExceptions"`
	MissingMigrations   []jsonMissingMigration           `json:"missingMigrations"`
}

func (jr *JSONReporter) Write(w io.Writer, r *schema.TestReport) error {
	out := jsonOutput{
		StartTime: r.StartTime.Format(time.RFC3339),
		EndTime:   r.EndTime.Format(time.RFC3339),
		Duration:  r.EndTime.Sub(r.StartTime).String(),
		Results:   make(map[schema.Key]jsonSchemaResults),
	}

	out.Stats.TotalPassed = addRuns(out.Results, r.PassedTests, "passed",
		func(res *jsonSchemaResults, js jsonSpec) { res.Passed = append(res.Passed, js) })
	out.Stats.TotalFailed = addRuns(out.Results, r.FailedTests, "failed",
		func(res *jsonSchemaResults, js jsonSpec) { res.Failed = append(res.Failed, js) })
	out.Stats.TotalExcepted = addRuns(out.Results, r.ExceptedTests, statusExcepted,
		func(res *jsonSchemaResults, js jsonSpec) { res.Excepted = append(res.Excepted, js) })

	for k, s := range r.Summaries {
		res := out.Results[k]
		res.Summary = newJSONSummary(s)
		out.Results[k] = res
	}
	out.Stats.TotalMissingDocs = r.MissingDocuments()
	out.Stats.TotalNotTested = r.NotTested
	out.StoppedEarly = r.StoppedEarly()

	out.UnexercisedChanges = unexercisedChanges(r)
	out.AcknowledgedChanges = acknowledgedChanges(r)
	out.Stats.TotalUnexercised = len(out.UnexercisedChanges)

	out.StaleExceptions = staleExceptions(r)
	out.Stats.TotalStale = len(out.StaleExceptions)
	out.MissingMigrations = missingMigrations(r)
	out.Stats.TotalMigrations = len(out.MissingMigrations)

	// Write empty result lists as [], not null
	for k, res := range out.Results {
		if res.Passed == nil {
			res.Passed = []jsonSpec{}
		}
		if res.Failed == nil {
			res.Failed = []jsonSpec{}
		}
		if res.Excepted == nil {
			res.Excepted = []jsonSpec{}
		}
		out.Results[k] = res
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// addRuns adds each run in the log to its schema's results, and returns how many there are.
func addRuns(
	results map[schema.Key]jsonSchemaResults,
	log schema.TestLog,
	status string,
	add func(*jsonSchemaResults, jsonSpec),
) int {
	total := 0
	for k, specs := range log {
		res := results[k]
		for i := range specs {
			add(&res, newJSONSpec(&specs[i], status))
		}
		results[k] = res
		total += len(specs)
	}
	return total
}

// missingMigrations lists the report's missing migration statements, ordered by schema.
func missingMigrations(r *schema.TestReport) []jsonMissingMigration {
	out := make([]jsonMissingMigration, 0, len(r.MissingMigrations))
	for _, k := range slices.Sorted(maps.Keys(r.MissingMigrations)) {
		m := r.MissingMigrations[k]
		out = append(out, jsonMissingMigration{Schema: k, Major: m.Major, Why: m.Why})
	}
	return out
}

// staleExceptions lists the report's stale exceptions, ordered by schema.
func staleExceptions(r *schema.TestReport) []jsonStaleException {
	out := []jsonStaleException{}
	for _, k := range slices.Sorted(maps.Keys(r.StaleExceptions)) {
		for _, s := range r.StaleExceptions[k] {
			out = append(out, jsonStaleException{
				Schema:   k,
				Version:  s.Exception.Version.String('.'),
				Document: s.Exception.Document,
				Rule:     s.Exception.Rule,
				Change:   s.Exception.Change,
				Reason:   s.Exception.Reason,
				Why:      s.Why,
			})
		}
	}
	return out
}

// unexercisedChanges lists the report's unexercised changes, ordered by schema.
func unexercisedChanges(r *schema.TestReport) []jsonUnexercisedChange {
	keys := make([]schema.Key, 0, len(r.UnexercisedChanges))
	for k := range r.UnexercisedChanges {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	out := []jsonUnexercisedChange{}
	for _, k := range keys {
		ucs := r.UnexercisedChanges[k]
		for i := range ucs {
			uc := &ucs[i]
			out = append(out, jsonUnexercisedChange{
				Schema:    k,
				Earlier:   uc.Earlier,
				Later:     uc.Later,
				Location:  uc.Change.Location,
				Keyword:   uc.Change.Keyword,
				Effect:    uc.Change.Effect,
				Direction: uc.Direction,
				Detail:    uc.Change.Detail,
			})
		}
	}
	return out
}

// acknowledgedChanges lists the changes exception files acknowledge, ordered by schema.
func acknowledgedChanges(r *schema.TestReport) []jsonAcknowledgedChange {
	out := []jsonAcknowledgedChange{}
	for _, k := range slices.Sorted(maps.Keys(r.AcknowledgedChanges)) {
		acs := r.AcknowledgedChanges[k]
		for i := range acs {
			ac := &acs[i]
			out = append(out, jsonAcknowledgedChange{
				Schema:   k,
				Earlier:  ac.Earlier,
				Later:    ac.Later,
				Location: ac.Change.Location,
				Keyword:  ac.Change.Keyword,
				Effect:   ac.Change.Effect,
				Detail:   ac.Change.Detail,
				Reason:   ac.Reason,
			})
		}
	}
	return out
}
