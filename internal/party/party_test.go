package party

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/internal/schema"
	"github.com/bitshepherds/json-schema-manager/internal/validator"
)

const testConfig = `
environments:
  prod:
    privateUrlRoot: "https://json-schemas.internal.myorg.io/"
    publicUrlRoot: "https://json-schemas.myorg.io/"
    isProduction: true
`

const (
	writerAhead = "scenario: provider-first\n"
	readerAhead = "scenario: consumer-first\n"
	eitherAhead = "scenario: uncoordinated\ndocumentsLive: moment\n"
	lockStep    = "scenario: lock-step\n"
	selfNaming  = "scenario: stored-self-naming\n"

	v100 = schema.Key("shop_order_1_0_0")
	v110 = schema.Key("shop_order_1_1_0")
	v120 = schema.Key("shop_order_1_2_0")
	v200 = schema.Key("shop_order_2_0_0")

	statusPaid     = `{"properties": {"status": {"enum": ["paid"]}}}`
	statusRefunded = `{"properties": {"status": {"enum": ["paid", "refunded"]}}}`
	refundedDoc    = `{"status": "refunded"}`
	paidDoc        = `{"status": "paid"}`
)

// newRegistry returns a registry holding the given schemas, by key, each with empty pass and
// fail directories, in a family with the given declaration.
func newRegistry(t *testing.T, declaration string, schemas map[schema.Key]string) *schema.Registry {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "json-schema-manager-config.yml"), []byte(testConfig), 0o600))
	r, err := schema.NewRegistry(dir, validator.NewSanthoshCompiler(), fsh.NewPathResolver(), fsh.NewEnvProvider())
	require.NoError(t, err)
	for k, content := range schemas {
		s := schema.New(k, r)
		for _, sub := range []string{"pass", "fail"} {
			require.NoError(t, os.MkdirAll(filepath.Join(s.Path(schema.HomeDir), sub), 0o755))
		}
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte(content), 0o600))
		require.NoError(t, os.WriteFile(r.FamilyDeclarationPath(k), []byte(declaration), 0o600))
	}
	return r
}

// writeDoc writes a file in the version's directory, such as pass/a.json, and returns its path.
func writeDoc(t *testing.T, r *schema.Registry, k schema.Key, name, content string) string {
	t.Helper()
	path := filepath.Join(schema.New(k, r).Path(schema.HomeDir), filepath.FromSlash(name))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func writeManifest(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ManifestFile)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// checkOne checks one listed version for a party with the given role.
func checkOne(t *testing.T, r *schema.Registry, role Role, listed schema.Key) *SchemaResult {
	t.Helper()
	res, err := Check(context.Background(), r, &Manifest{Path: "m.yml", Role: role, Schemas: []string{string(listed)}})
	require.NoError(t, err)
	require.Len(t, res.Schemas, 1)
	assert.Equal(t, role, res.Role)
	return &res.Schemas[0]
}

// requireOneFailure checks the result is a failure of one run, and returns that run.
func requireOneFailure(t *testing.T, sr *SchemaResult) *schema.Spec {
	t.Helper()
	assert.Equal(t, Fail, sr.Status())
	failures := sr.Failures()
	require.Len(t, failures, 1)
	return &failures[0]
}

func TestCheckPartyReaderInWriterAheadRunsLaterPassOnListedVersion(t *testing.T) {
	t.Parallel()

	r := newRegistry(t, writerAhead, map[schema.Key]string{v100: statusPaid, v110: statusRefunded})
	path := writeDoc(t, r, v110, "pass/refunded.json", refundedDoc)

	failure := requireOneFailure(t, checkOne(t, r, RoleReader, v100))

	assert.Equal(t, schema.RuleNoLoosening, failure.Rule)
	assert.Equal(t, schema.TestDocTypePass, failure.TestDocType)
	assert.Equal(t, v110, failure.DocumentFrom)
	assert.Equal(t, v100, failure.Schema.Key())
	assert.Equal(t, v110, failure.ReportKey(), "a failure appears under the later version")
	assert.Equal(t, path, failure.TestInfo.Path)
	assert.ErrorAs(t, failure.Err, new(*schema.RuleBrokenError))
}

func TestCheckPartyReaderInWriterAheadRunsListedFailOnLaterVersions(t *testing.T) {
	t.Parallel()

	r := newRegistry(t, writerAhead, map[schema.Key]string{v100: statusPaid, v110: statusRefunded})
	writeDoc(t, r, v100, "fail/refunded.json", refundedDoc)

	failure := requireOneFailure(t, checkOne(t, r, RoleReader, v100))

	assert.Equal(t, schema.RuleNoLoosening, failure.Rule)
	assert.Equal(t, schema.TestDocTypeFail, failure.TestDocType)
	assert.Equal(t, v100, failure.DocumentFrom)
	assert.Equal(t, v110, failure.Schema.Key())
}

func TestCheckPartyWriterInReaderAheadRunsListedPassOnLaterVersions(t *testing.T) {
	t.Parallel()

	r := newRegistry(t, readerAhead, map[schema.Key]string{v100: statusRefunded, v110: statusPaid})
	writeDoc(t, r, v100, "pass/refunded.json", refundedDoc)

	failure := requireOneFailure(t, checkOne(t, r, RoleWriter, v100))

	assert.Equal(t, schema.RuleNoTightening, failure.Rule)
	assert.Equal(t, schema.TestDocTypePass, failure.TestDocType)
	assert.Equal(t, v100, failure.DocumentFrom)
	assert.Equal(t, v110, failure.Schema.Key())
}

func TestCheckPartyWriterInReaderAheadRunsLaterFailOnListedVersion(t *testing.T) {
	t.Parallel()

	r := newRegistry(t, readerAhead, map[schema.Key]string{v100: statusRefunded, v110: statusPaid})
	writeDoc(t, r, v110, "fail/refunded.json", refundedDoc)

	failure := requireOneFailure(t, checkOne(t, r, RoleWriter, v100))

	assert.Equal(t, schema.RuleNoTightening, failure.Rule)
	assert.Equal(t, schema.TestDocTypeFail, failure.TestDocType)
	assert.Equal(t, v110, failure.DocumentFrom)
	assert.Equal(t, v100, failure.Schema.Key())
}

// bothWays is an either-ahead family whose later version drops one value and adds another, with a
// document which shows each.
func bothWays(t *testing.T) *schema.Registry {
	t.Helper()
	r := newRegistry(t, eitherAhead, map[schema.Key]string{
		v100: `{"properties": {"status": {"enum": ["placed", "paid"]}}}`,
		v110: `{"properties": {"status": {"enum": ["paid", "refunded"]}}}`,
	})
	writeDoc(t, r, v100, "pass/placed.json", `{"status": "placed"}`)
	writeDoc(t, r, v110, "pass/refunded.json", refundedDoc)
	return r
}

func TestCheckPartyEitherRunsTheRoleSide(t *testing.T) {
	t.Parallel()

	r := bothWays(t)

	reader := requireOneFailure(t, checkOne(t, r, RoleReader, v100))
	assert.Equal(t, schema.RuleNoLoosening, reader.Rule)
	assert.Equal(t, "refunded.json", filepath.Base(reader.TestInfo.Path))

	writer := requireOneFailure(t, checkOne(t, r, RoleWriter, v100))
	assert.Equal(t, schema.RuleNoTightening, writer.Rule)
	assert.Equal(t, "placed.json", filepath.Base(writer.TestInfo.Path))
}

func TestCheckPartyChecksEveryLaterVersionInMajor(t *testing.T) {
	t.Parallel()

	r := newRegistry(t, writerAhead, map[schema.Key]string{
		v100: statusPaid,
		v110: statusRefunded,
		v120: statusRefunded,
		v200: statusRefunded,
	})
	for _, k := range []schema.Key{v110, v120, v200} {
		writeDoc(t, r, k, "pass/refunded.json", refundedDoc)
	}

	sr := checkOne(t, r, RoleReader, v100)
	failures := sr.Failures()
	require.Len(t, failures, 2, "every later version in the major, and none in another major")
	assert.Equal(t, v110, failures[0].Later)
	assert.Equal(t, v120, failures[1].Later)

	// 1.1.0 accepts what 1.2.0 sends, and the check doesn't look back at 1.0.0
	sr = checkOne(t, r, RoleReader, v110)
	assert.Empty(t, sr.Failures())
	require.Len(t, sr.Runs, 1)
	assert.Equal(t, v120, sr.Runs[0].Later)
}

func TestCheckPartyGivesNoticeWhenNoLaterVersionCanHarm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		declaration string
		role        Role
		why         string
	}{
		{"reader in reader-ahead", readerAhead, RoleReader, "readers move to a new version before writers"},
		{"reader in a config file family", "scenario: config-file\n", RoleReader, "readers move"},
		{"writer in writer-ahead", writerAhead, RoleWriter, "writers move to a new version before readers"},
		{"reader in neither", lockStep, RoleReader, "both sides move together"},
		{"writer in neither", lockStep, RoleWriter, "both sides move together"},
		{"reader when a document names its version", selfNaming, RoleReader, "the document names"},
		{"writer when a document names its version", selfNaming, RoleWriter, "the document names"},
		{
			"the effective fact decides, not the scenario",
			writerAhead + "readerPicksSchema: true\n", RoleReader, "the document names",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRegistry(t, tt.declaration, map[schema.Key]string{v100: statusPaid, v110: statusPaid})
			// Each of these would fail, in one direction or the other, if it ran
			writeDoc(t, r, v110, "pass/refunded.json", refundedDoc)
			writeDoc(t, r, v110, "fail/paid.json", paidDoc)
			writeDoc(t, r, v100, "pass/refunded.json", refundedDoc)
			writeDoc(t, r, v100, "fail/paid.json", paidDoc)

			sr := checkOne(t, r, tt.role, v100)

			assert.Equal(t, Notice, sr.Status())
			assert.Contains(t, sr.Notice, tt.why)
			assert.Contains(t, sr.Notice, "no later version can harm a "+string(tt.role)+" on "+string(v100))
			assert.Empty(t, sr.Runs)
		})
	}

	t.Run("a newer major still warns", func(t *testing.T) {
		t.Parallel()
		r := newRegistry(t, lockStep, map[schema.Key]string{v100: statusPaid, v200: statusPaid})
		sr := checkOne(t, r, RoleReader, v100)
		assert.Equal(t, Warn, sr.Status())
		assert.NotEmpty(t, sr.Notice)
		assert.Equal(t, v200, sr.NewerMajor)
	})
}

func TestCheckPartyRequiresRole(t *testing.T) {
	t.Parallel()

	m, err := LoadManifest(writeManifest(t, "role: writer\nschemas:\n  - shop_order_1_0_0\n"))
	require.NoError(t, err)
	assert.Equal(t, RoleWriter, m.Role)
	assert.Equal(t, []string{"shop_order_1_0_0"}, m.Schemas)

	tests := []struct {
		name    string
		content string
		reason  string
	}{
		{"no role", "schemas:\n  - shop_order_1_0_0\n", "must state its role, reader or writer"},
		{"another role", "role: both\nschemas:\n  - shop_order_1_0_0\n", `role "both" must be reader or writer`},
	}
	for _, tt := range tests {
		_, err = LoadManifest(writeManifest(t, tt.content))
		var me *ManifestError
		require.ErrorAs(t, err, &me, tt.name)
		assert.Contains(t, me.Error(), tt.reason, tt.name)
	}
}

func TestCheckPartyUsesSamePairsAsRuleEngine(t *testing.T) {
	t.Parallel()

	r := bothWays(t)
	writeDoc(t, r, v100, "fail/lost.json", `{"status": "lost"}`)
	writeDoc(t, r, v110, "fail/lost.json", `{"status": "lost"}`)

	// One line for each run and its outcome, in the words of the rule
	line := func(spec *schema.Spec) string {
		return spec.ResultLabel() + " " + spec.TestInfo.Path
	}

	tester := schema.NewTester(r)
	tester.SetStopOnFirstError(false)
	tr, err := tester.TestSingleSchema(context.Background(), v100)
	require.NoError(t, err)
	var validate []string
	for _, log := range []schema.TestLog{tr.PassedTests, tr.FailedTests} {
		for i := range log[v110] {
			validate = append(validate, line(&log[v110][i]))
		}
	}
	require.Len(t, validate, 4, "the four runs of the rule table")

	var party []string
	for _, role := range []Role{RoleReader, RoleWriter} {
		sr := checkOne(t, r, role, v100)
		for i := range sr.Runs {
			party = append(party, line(&sr.Runs[i]))
		}
	}

	slices.Sort(validate)
	slices.Sort(party)
	assert.Equal(t, validate, party)
}

func TestCheckPartyHonoursDeclaredException(t *testing.T) {
	t.Parallel()

	const entry = `exceptions:
  - version: 1.0.0
    document: pass/refunded.json
    rule: no-loosening
    reason: every reader was upgraded first
`

	declared := func(t *testing.T) *schema.Registry {
		t.Helper()
		r := newRegistry(t, writerAhead, map[schema.Key]string{v100: statusPaid, v110: statusRefunded})
		writeDoc(t, r, v110, "pass/refunded.json", refundedDoc)
		return r
	}

	t.Run("an entry clears its run, and the result keeps the reason", func(t *testing.T) {
		t.Parallel()
		r := declared(t)
		writeDoc(t, r, v110, schema.ExceptionsFile, entry)

		sr := checkOne(t, r, RoleReader, v100)

		assert.Equal(t, Pass, sr.Status())
		assert.Empty(t, sr.Failures())
		excepted := sr.Excepted()
		require.Len(t, excepted, 1)
		assert.Equal(t, "every reader was upgraded first", excepted[0].Exception.Reason)
	})

	t.Run("it clears nothing else", func(t *testing.T) {
		t.Parallel()
		r := declared(t)
		writeDoc(t, r, v110, "pass/also-refunded.json", refundedDoc)
		writeDoc(t, r, v110, schema.ExceptionsFile, entry)

		sr := checkOne(t, r, RoleReader, v100)

		assert.Equal(t, "also-refunded.json", filepath.Base(requireOneFailure(t, sr).TestInfo.Path))
		assert.Len(t, sr.Excepted(), 1)
	})

	t.Run("an entry which clears nothing is validate's to report, not the party's problem", func(t *testing.T) {
		t.Parallel()
		r := newRegistry(t, writerAhead, map[schema.Key]string{v100: statusRefunded, v110: statusRefunded})
		writeDoc(t, r, v110, "pass/refunded.json", refundedDoc)
		writeDoc(t, r, v110, schema.ExceptionsFile, entry)

		sr := checkOne(t, r, RoleReader, v100)

		assert.Equal(t, Pass, sr.Status())
		assert.Empty(t, sr.Excepted())
	})

	t.Run("an exception file which can't be read stops the check", func(t *testing.T) {
		t.Parallel()
		r := declared(t)
		writeDoc(t, r, v110, schema.ExceptionsFile, "exceptions: [")
		_, err := Check(context.Background(), r, &Manifest{Role: RoleReader, Schemas: []string{string(v100)}})
		require.ErrorAs(t, err, new(*schema.InvalidExceptionsError))
	})
}

func TestLoadManifest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		reason  string
	}{
		{"invalid YAML", "schemas: [", "is not valid YAML"},
		{"no schemas", "role: reader\nschemas: []\n", "lists no schemas"},
	}
	for _, tt := range tests {
		_, err := LoadManifest(writeManifest(t, tt.content))
		var me *ManifestError
		require.ErrorAs(t, err, &me, tt.name)
		assert.Contains(t, me.Error(), tt.reason, tt.name)
	}

	_, err := LoadManifest(filepath.Join(t.TempDir(), "missing.yml"))
	require.ErrorContains(t, err, "party manifest")
	require.ErrorContains(t, err, "cannot be read")
}

func TestCheck(t *testing.T) {
	t.Parallel()

	r := newRegistry(t, writerAhead, map[schema.Key]string{v100: statusPaid, v110: statusRefunded, v200: `{}`})
	writeDoc(t, r, v110, "pass/refunded.json", refundedDoc)

	res, err := Check(context.Background(), r, &Manifest{Path: "m.yml", Role: RoleReader, Schemas: []string{
		string(v100), string(v110), "missing_thing_1_0_0", "shop",
	}})
	require.NoError(t, err)
	require.Len(t, res.Schemas, 4)

	assert.Equal(t, Fail, res.Schemas[0].Status())
	assert.Equal(t, v200, res.Schemas[0].NewerMajor)

	assert.Equal(t, Warn, res.Schemas[1].Status())
	assert.Equal(t, "not found in the registry", res.Schemas[2].Problem)
	assert.Equal(t, "not a schema key or canonical URL", res.Schemas[3].Problem)

	assert.Equal(t, 0, res.Count(Pass))
	assert.Equal(t, 3, res.Count(Fail))
	assert.Equal(t, 1, res.Count(Warn))
}

func TestCheck_Errors(t *testing.T) {
	t.Parallel()

	check := func(r *schema.Registry) error {
		_, err := Check(context.Background(), r, &Manifest{Role: RoleReader, Schemas: []string{string(v100)}})
		return err
	}

	t.Run("listed version does not render", func(t *testing.T) {
		t.Parallel()
		broken := "{\"$ref\": \"{{ JSM `missing_1_0_0` }}\"}"
		require.Error(t, check(newRegistry(t, writerAhead, map[schema.Key]string{v100: broken})))
	})

	t.Run("family has a major version with no versions", func(t *testing.T) {
		t.Parallel()
		r := newRegistry(t, writerAhead, map[schema.Key]string{v100: `{}`})
		s, err := r.GetSchemaByKey(v100)
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(schema.FamilyDir), "2"), 0o755))
		require.ErrorAs(t, check(r), new(*schema.NotFoundError))
	})

	t.Run("a later version does not render", func(t *testing.T) {
		t.Parallel()
		broken := "{\"$ref\": \"{{ JSM `missing_1_0_0` }}\"}"
		r := newRegistry(t, writerAhead, map[schema.Key]string{v100: statusPaid, v110: broken})
		writeDoc(t, r, v100, "fail/refunded.json", refundedDoc)
		require.Error(t, check(r))
	})
}

func TestCheckFailsOnFamilyWithoutDeclaration(t *testing.T) {
	t.Parallel()

	r := newRegistry(t, writerAhead, map[schema.Key]string{v100: "{}"})
	require.NoError(t, os.Remove(r.FamilyDeclarationPath(v100)))

	_, err := Check(context.Background(), r, &Manifest{Role: RoleReader, Schemas: []string{string(v100)}})

	var missing *schema.MissingFamilyDeclarationError
	require.ErrorAs(t, err, &missing)
}

func TestStatus(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Pass, (&SchemaResult{}).Status())
	assert.Equal(t, Notice, (&SchemaResult{Notice: "n"}).Status())
	assert.Equal(t, Warn, (&SchemaResult{Notice: "n", NewerMajor: "a_2_0_0"}).Status())
	assert.Equal(t, Fail, (&SchemaResult{Problem: "p", NewerMajor: "a_2_0_0"}).Status())
}
