package schema

import "fmt"

// Rule names what a document run proves. Each is a promise in ScenariosDoc.
type Rule string

const (
	// RuleOwnDocuments is a version accepting its own pass documents and rejecting its own fail documents.
	RuleOwnDocuments Rule = "own-documents"

	// RuleNoLoosening is a later version accepting nothing an earlier one rejects.
	RuleNoLoosening Rule = "no-loosening"

	// RuleNoTightening is a later version rejecting nothing an earlier one accepts.
	RuleNoTightening Rule = "no-tightening"
)

// What a version did with a document, for the messages which say so.
const (
	verdictAccepted = "accepted"
	verdictRejected = "rejected"
)

// versionPair is an earlier and a later version in one major of a family.
type versionPair struct {
	earlier, later Key
}

// acrossVersionRun is one row of the rule table in ScenariosDoc: a kind of document run between the
// two versions of a pair.
type acrossVersionRun struct {
	fromLater bool // the document is the later version's, and runs on the earlier one, or the other way round
	docType   TestDocType
	rule      Rule
}

// acrossVersionRuns is the rule table in ScenariosDoc.
var acrossVersionRuns = []acrossVersionRun{
	{fromLater: true, docType: TestDocTypePass, rule: RuleNoLoosening},
	{fromLater: false, docType: TestDocTypeFail, rule: RuleNoLoosening},
	{fromLater: false, docType: TestDocTypePass, rule: RuleNoTightening},
	{fromLater: true, docType: TestDocTypeFail, rule: RuleNoTightening},
}

// appliesTo reports whether the run is one the family's rule asks for: a writer ahead of its reader
// needs no loosening, and a reader ahead of its writer needs no tightening.
func (run acrossVersionRun) appliesTo(ahead Ahead) bool {
	if ahead == AheadEither {
		return true
	}
	if run.rule == RuleNoLoosening {
		return ahead == AheadWriter
	}
	return ahead == AheadReader
}

// source returns the version of the pair whose document the run uses.
func (run acrossVersionRun) source(earlier, later *Schema) *Schema {
	if run.fromLater {
		return later
	}
	return earlier
}

// spec returns the run of the document on the other version of the pair.
func (run acrossVersionRun) spec(earlier, later *Schema, doc TestInfo, decl *FamilyDeclaration) Spec {
	from, on := earlier, later
	if run.fromLater {
		from, on = later, earlier
	}
	return Spec{
		Schema:       on,
		TestInfo:     doc,
		TestDocType:  run.docType,
		Rule:         run.rule,
		DocumentFrom: from.Key(),
		Earlier:      earlier.Key(),
		Later:        later.Key(),
		Declaration:  decl,
	}
}

// RuleBrokenError is returned when a document run across two versions shows a change the family's
// rule doesn't allow. It names the rule, both versions and the document, and says what the break means
// for the family's scenario.
type RuleBrokenError struct {
	Rule         Rule
	Earlier      Key
	Later        Key
	DocumentFrom Key
	ValidatedBy  Key
	DocType      TestDocType
	DocPath      string
	Declaration  *FamilyDeclaration
	Wrapped      error
}

func (e *RuleBrokenError) Error() string {
	verb := verdictAccepted
	if e.DocType == TestDocTypePass {
		verb = verdictRejected
	}
	return fmt.Sprintf("%s broken between %s and %s: %s document %s of %s was %s by %s. %s "+
		"If this is deliberate, or %s is already deployed, list it in %s in %s's directory. Detail: %s",
		e.Rule, e.Earlier, e.Later, e.DocType, e.DocPath, e.DocumentFrom, verb, e.ValidatedBy, e.meaning(),
		e.Later, ExceptionsFile, e.Later, e.Wrapped)
}

// Unwrap returns the error from the document run itself.
func (e *RuleBrokenError) Unwrap() error {
	return e.Wrapped
}

// meaning says what the break means for the family's scenario, and what to do about it.
func (e *RuleBrokenError) meaning() string {
	scenario := e.Declaration.Scenario
	loosened := e.Rule == RuleNoLoosening

	if e.Declaration.Facts.Schema == SchemaDescribes {
		change := fmt.Sprintf("%s rejects documents %s accepts", e.Later, e.Earlier)
		if loosened {
			change = fmt.Sprintf("%s accepts documents %s rejects", e.Later, e.Earlier)
		}
		return fmt.Sprintf("In a %s family, this says the real format has changed: %s. Its owner decides that, "+
			"so check your readers cope with it. If the format hasn't really changed this way, correct %s.",
			scenario, change, e.Later)
	}

	var harm string
	switch {
	case loosened:
		harm = fmt.Sprintf("a writer on %s can send a reader still on %s a document it rejects", e.Later, e.Earlier)
	case e.Declaration.Facts.DocumentsLive == DocumentsLiveMoment:
		harm = fmt.Sprintf("a reader on %s rejects a document a writer still on %s can send", e.Later, e.Earlier)
	default:
		harm = fmt.Sprintf("a reader on %s rejects a document written under %s, which may still be around",
			e.Later, e.Earlier)
	}
	return fmt.Sprintf("In a %s family, %s. Undo the change, or make it in a new major version.", scenario, harm)
}
