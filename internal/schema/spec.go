package schema

import (
	"github.com/bitshepherds/json-schema-manager/internal/validator"
)

// Spec defines a single schema test involving the rendered schema and a test JSON document which
// the schema should either validate (if a pass test doc) or not validate (if a fail test doc).
// Note that this would be called 'Test' except the file would be called 'test.go' and its test 'test_test.go'.
type Spec struct {
	Schema       *Schema            // The schema that is being tested with this spec
	TestInfo     TestInfo           // information about the test document itself
	TestDocType  TestDocType        // the type of test document
	Rule         Rule               // what the run proves
	DocumentFrom Key                // the version the document belongs to
	Earlier      Key                // For a run across versions, the earlier version of the pair
	Later        Key                // For a run across versions, the later version of the pair
	Declaration  *FamilyDeclaration // For a run across versions, the family's declaration
	Exception    *Exception         // For a run across versions, the entry in the later version's file which names it
	Err          error              // Once the spec has run, if it didn't give the expected outcome, the related error
}

// NewSpec sets up a run of one of a schema's own documents on it.
func NewSpec(s *Schema, testInfo TestInfo, testDocType TestDocType) Spec {
	return Spec{
		Schema:       s,
		TestInfo:     testInfo,
		TestDocType:  testDocType,
		Rule:         RuleOwnDocuments,
		DocumentFrom: s.Key(),
	}
}

// AcrossVersions reports whether the spec runs a document on a version other than its own.
func (s *Spec) AcrossVersions() bool {
	return s.Later != ""
}

// ReportKey returns the version the report shows the run under: the later version of a run across
// versions, since that's what changed, and otherwise the schema itself.
func (s *Spec) ReportKey() Key {
	if s.AcrossVersions() {
		return s.Later
	}
	return s.Schema.Key()
}

// Run executes the spec using the given validator.
func (s *Spec) Run(v validator.Validator) error {
	u := s.TestInfo.Unmarshalled

	if s.TestDocType == TestDocTypePass {
		err := v.Validate(u)
		if err != nil {
			s.setFailure(&PassTestFailedError{
				SchemaPath:  s.Schema.Path(FilePath),
				TestDocPath: s.TestInfo.Path,
				Wrapped:     err,
			})
			return s.Err
		}
		return nil
	}

	// We expected it to fail and it passed:
	if err := v.Validate(u); err == nil {
		s.setFailure(&FailTestPassedError{SchemaPath: s.Schema.Path(FilePath), TestDocPath: s.TestInfo.Path})
		return s.Err
	}

	// We expect it to fail, and it did:
	return nil
}

// setFailure records why the spec failed, as a broken rule if it ran across versions.
func (s *Spec) setFailure(err error) {
	if s.AcrossVersions() {
		err = &RuleBrokenError{
			Rule:         s.Rule,
			Earlier:      s.Earlier,
			Later:        s.Later,
			DocumentFrom: s.DocumentFrom,
			ValidatedBy:  s.Schema.Key(),
			DocType:      s.TestDocType,
			DocPath:      s.TestInfo.Path,
			Declaration:  s.Declaration,
			Wrapped:      err,
		}
	}
	s.Err = err
}

// ResultLabel returns a human-readable label for the result of the spec.
func (s *Spec) ResultLabel() string {
	if s.AcrossVersions() {
		return s.acrossVersionsLabel()
	}
	if s.TestDocType == TestDocTypePass {
		if s.Err != nil {
			return "failed"
		}
		return "passed"
	}
	if s.Err != nil {
		return "passed, when expected fail"
	}
	return "failed, as expected"
}

func (s *Spec) acrossVersionsLabel() string {
	outcome := "holds"
	if s.Err != nil {
		outcome = "broken"
		if s.Exception != nil {
			outcome = "excepted"
		}
	}
	verb := verdictAccepted
	if (s.TestDocType == TestDocTypePass) == (s.Err != nil) {
		verb = verdictRejected
	}
	from, by := s.DocumentFrom.Version(), s.Schema.Key().Version()
	return string(s.Rule) + " " + outcome + ": " + string(s.TestDocType) + " document of " + from.String('.') +
		" " + verb + " by " + by.String('.')
}
