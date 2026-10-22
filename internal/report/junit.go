package report

import (
	"cmp"
	"encoding/xml"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

// JUnitReporter implements schema.Reporter for JUnit XML output, which CI systems show as test results.
// Each schema is a test suite, and each test document run against it a test case.
//
// JUnit has no warnings. A schema's warnings (no pass or no fail document, and changes which no pass
// document uses) are written to its suite's system-out, or, if Strict, as failing test cases, so the
// report fails when jsm validate --strict does.
type JUnitReporter struct {
	Strict bool
}

type junitTestSuites struct {
	XMLName  xml.Name         `xml:"testsuites"`
	Name     string           `xml:"name,attr"`
	Tests    int              `xml:"tests,attr"`
	Failures int              `xml:"failures,attr"`
	Time     string           `xml:"time,attr"`
	Suites   []junitTestSuite `xml:"testsuite"`
}

type junitTestSuite struct {
	Name      string          `xml:"name,attr"`
	Tests     int             `xml:"tests,attr"`
	Failures  int             `xml:"failures,attr"`
	Cases     []junitTestCase `xml:"testcase"`
	SystemOut string          `xml:"system-out,omitempty"`
}

type junitTestCase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Failure   *junitFailure `xml:"failure"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

// Write implements the schema.Reporter interface.
func (jr *JUnitReporter) Write(w io.Writer, r *schema.TestReport) error {
	out := junitTestSuites{
		Name: "jsm validate",
		Time: fmt.Sprintf("%.3f", r.EndTime.Sub(r.StartTime).Seconds()),
	}

	// A schema can have only warnings, such as an unexercised change, so list it too
	keys := reportKeys(r)
	for k := range r.UnexercisedChanges {
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)

	for _, k := range keys {
		suite := jr.suite(r, k)
		out.Tests += suite.Tests
		out.Failures += suite.Failures
		out.Suites = append(out.Suites, suite)
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(out); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

// suite returns the test suite for the schema with the given key.
func (jr *JUnitReporter) suite(r *schema.TestReport, k schema.Key) junitTestSuite {
	suite := junitTestSuite{Name: string(k) + schema.SchemaSuffix}
	for _, spec := range r.PassedTests[k] {
		suite.Cases = append(suite.Cases, junitTestCase{Name: spec.TestInfo.Path, ClassName: suite.Name})
	}
	for _, spec := range r.FailedTests[k] {
		suite.Cases = append(suite.Cases, junitTestCase{
			Name:      spec.TestInfo.Path,
			ClassName: suite.Name,
			Failure:   &junitFailure{Message: spec.ResultLabel(), Text: fmt.Sprint(spec.Err)},
		})
	}
	slices.SortStableFunc(suite.Cases, func(a, b junitTestCase) int { return cmp.Compare(a.Name, b.Name) })

	warnings := schemaWarnings(r, k)
	if jr.Strict {
		for _, warning := range warnings {
			suite.Cases = append(suite.Cases, junitTestCase{
				Name:      "warning: " + warning,
				ClassName: suite.Name,
				Failure:   &junitFailure{Message: "warning, and --strict is set", Text: warning},
			})
		}
	} else if len(warnings) > 0 {
		suite.SystemOut = "WARN " + strings.Join(warnings, "\nWARN ") + "\n"
	}

	suite.Tests = len(suite.Cases)
	for _, c := range suite.Cases {
		if c.Failure != nil {
			suite.Failures++
		}
	}
	return suite
}

// schemaWarnings returns the warnings for the schema with the given key: that it has no pass or no
// fail document, and each change since the version before it which no pass document uses.
func schemaWarnings(r *schema.TestReport, k schema.Key) []string {
	var warnings []string
	if s := r.Summaries[k]; s != nil {
		if s.PassDocs == 0 {
			warnings = append(warnings, "no pass documents")
		}
		if s.FailDocs == 0 {
			warnings = append(warnings, "no fail documents")
		}
	}
	for _, uc := range r.UnexercisedChanges[k] {
		warnings = append(warnings, fmt.Sprintf("unexercised change since %s: %s (%s)",
			uc.Earlier, uc.Change.String(), uc.Change.Effect))
	}
	return warnings
}
