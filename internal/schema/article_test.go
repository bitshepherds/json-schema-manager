package schema

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The article before a scenario's name follows the sound it starts with, so messages read "an
// owner-reads family", never "a owner-reads family" (#153).
func TestScenarioArticle(t *testing.T) {
	t.Parallel()

	for s, want := range map[Scenario]string{
		ScenarioOwnerWrites:      "an",
		ScenarioOwnerReads:       "an",
		ScenarioUncoordinated:    "an",
		ScenarioConfigFile:       "a",
		ScenarioStoredOneSchema:  "a",
		ScenarioStoredSelfNaming: "a",
		ScenarioRetainedStream:   "a",
		ScenarioLockStep:         "a",
		ScenarioThirdPartyFormat: "a",
	} {
		assert.Equal(t, want, s.Article(), string(s))
	}
	assert.Len(t, Scenarios(), 9, "a new scenario needs a row here")
}

func TestRuleBrokenErrorMeaning_OwnerReads(t *testing.T) {
	t.Parallel()

	for _, role := range []Role{SchemaPrescribes, SchemaDescribes} {
		err := &RuleBrokenError{
			Rule:         RuleNoTightening,
			Earlier:      ruleEarlier,
			Later:        ruleLater,
			DocumentFrom: ruleLater,
			ValidatedBy:  ruleEarlier,
			DocType:      TestDocTypePass,
			DocPath:      "doc.json",
			Declaration: &FamilyDeclaration{
				Scenario: ScenarioOwnerReads,
				Facts:    FamilyFacts{DocumentsLive: DocumentsLiveMoment, Schema: role},
			},
			Wrapped: errors.New("detail"),
		}
		assert.Contains(t, err.Error(), "In an owner-reads family", string(role))
		assert.NotContains(t, err.Error(), "a owner-reads", string(role))
	}
}

func TestMissingMigration_Article(t *testing.T) {
	t.Parallel()

	r := ruleRegistry(t, "scenario: uncoordinated\ndocumentsLive: indefinitely\n")
	major2 := Key("domain_family_2_0_0")
	createSchemaFiles(t, r, schemaMap{major2: `{}`})
	rep, err := NewTester(r).TestSingleSchema(context.Background(), major2)
	require.NoError(t, err)
	require.Len(t, rep.MissingMigrations, 1)
	for _, m := range rep.MissingMigrations {
		assert.Contains(t, m.Why, "in an uncoordinated family")
	}
}
