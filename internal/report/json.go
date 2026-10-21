// Package report provides reporting functionality for JSM.
package report

import (
	"encoding/json"
	"io"
	"time"

	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

// JSONReporter implements schema.Reporter for JSON output.
type JSONReporter struct{}

type jsonSpec struct {
	Path           string     `json:"path"`
	TestDocType    string     `json:"type"`
	ValidatedBy    schema.Key `json:"validatedBy,omitempty"`    // the schema the document was tested against
	ForwardVersion string     `json:"forwardVersion,omitempty"` // set if the document came from a later version
	Error          string     `json:"error,omitempty"`
}

// newJSONSpec returns the JSON form of s, without its error.
func newJSONSpec(s *schema.Spec) jsonSpec {
	js := jsonSpec{
		Path:        s.TestInfo.Path,
		TestDocType: string(s.TestDocType),
	}
	if s.Schema != nil {
		js.ValidatedBy = s.Schema.Key()
	}
	if s.ForwardVersion != nil {
		js.ForwardVersion = s.ForwardVersion.String('.')
	}
	return js
}

type jsonSchemaResults struct {
	Passed []jsonSpec `json:"passed"`
	Failed []jsonSpec `json:"failed"`
}

type jsonOutput struct {
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
	Duration  string `json:"duration"`
	Stats     struct {
		TotalPassed int `json:"totalPassed"`
		TotalFailed int `json:"totalFailed"`
	} `json:"stats"`
	Results map[schema.Key]jsonSchemaResults `json:"results"`
}

func (jr *JSONReporter) Write(w io.Writer, r *schema.TestReport) error {
	out := jsonOutput{
		StartTime: r.StartTime.Format(time.RFC3339),
		EndTime:   r.EndTime.Format(time.RFC3339),
		Duration:  r.EndTime.Sub(r.StartTime).String(),
		Results:   make(map[schema.Key]jsonSchemaResults),
	}

	for k, specs := range r.PassedTests {
		res := out.Results[k]
		for i := range specs {
			res.Passed = append(res.Passed, newJSONSpec(&specs[i]))
		}
		out.Results[k] = res
		out.Stats.TotalPassed += len(specs)
	}

	for k, specs := range r.FailedTests {
		res := out.Results[k]
		for i := range specs {
			js := newJSONSpec(&specs[i])
			if specs[i].Err != nil {
				js.Error = specs[i].Err.Error()
			}
			res.Failed = append(res.Failed, js)
		}
		out.Results[k] = res
		out.Stats.TotalFailed += len(specs)
	}

	// Write empty result lists as [], not null
	for k, res := range out.Results {
		if res.Passed == nil {
			res.Passed = []jsonSpec{}
		}
		if res.Failed == nil {
			res.Failed = []jsonSpec{}
		}
		out.Results[k] = res
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
