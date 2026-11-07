package schema

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scenariosTable reads the table of scenarios and their facts from the docs, by scenario name, so
// the code and what the docs promise can't drift apart.
func scenariosTable(t *testing.T) map[Scenario]FamilyFacts {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ScenariosDoc))
	require.NoError(t, err)

	row := regexp.MustCompile("(?m)^\\| `([a-z-]+)` \\| ([a-z]+) \\| ([a-z]+|states it) \\| (yes|no) \\| ([a-z]+) \\|$")
	table := make(map[Scenario]FamilyFacts)
	for _, m := range row.FindAllStringSubmatch(string(data), -1) {
		live := DocumentsLive(m[3])
		if m[3] == "states it" {
			live = ""
		}
		table[Scenario(m[1])] = FamilyFacts{Ahead(m[2]), live, m[4] == "yes", Role(m[5])}
	}
	require.Len(t, table, 9, "the docs' table lists all nine scenarios")
	return table
}

// declarationFor is the smallest valid declaration naming the scenario.
func declarationFor(info ScenarioInfo) string {
	content := "scenario: " + string(info.Name) + "\n"
	switch info.Facts.DocumentsLive {
	case "":
		content += "documentsLive: indefinitely\n"
	case DocumentsLiveRetention:
		content += "oldestLive: 1.0.0\n"
	case DocumentsLiveMoment, DocumentsLiveIndefinitely:
	}
	return content
}

func TestFamilyDeclarationAcceptsEachScenarioName(t *testing.T) {
	t.Parallel()

	names := []Scenario{
		"provider-first", "consumer-first", "config-file", "stored-one-schema", "stored-self-naming",
		"retained-stream", "uncoordinated", "lock-step", "third-party-format",
	}
	all := Scenarios()
	require.Len(t, all, len(names))
	for i, name := range names {
		assert.Equal(t, name, all[i].Name, "the docs' order")
		d, err := ParseFamilyDeclaration([]byte(declarationFor(all[i])))
		require.NoError(t, err, name)
		assert.Equal(t, name, d.Scenario)
	}
}

func TestFamilyDeclarationExpandsScenarioToFacts(t *testing.T) {
	t.Parallel()

	table := scenariosTable(t)
	for _, info := range Scenarios() {
		want, ok := table[info.Name]
		require.True(t, ok, info.Name)
		d, err := ParseFamilyDeclaration([]byte(declarationFor(info)))
		require.NoError(t, err, info.Name)
		if want.DocumentsLive == "" {
			want.DocumentsLive = DocumentsLiveIndefinitely // the value declarationFor states
		}
		assert.Equal(t, want, d.Facts, info.Name)
	}
}

func TestFamilyDeclarationRequiresDocumentsLiveWhereItVaries(t *testing.T) {
	t.Parallel()

	for _, s := range []Scenario{ScenarioUncoordinated, ScenarioThirdPartyFormat} {
		assert.True(t, s.DocumentsLiveVaries())

		_, err := ParseFamilyDeclaration([]byte("scenario: " + string(s)))
		require.Error(t, err, s)
		assert.Contains(t, err.Error(), "documentsLive")
		assert.Contains(t, err.Error(), "If you don't know how long they live, choose indefinitely")

		d, err := ParseFamilyDeclaration([]byte("scenario: " + string(s) + "\ndocumentsLive: indefinitely"))
		require.NoError(t, err, s)
		assert.Equal(t, DocumentsLiveIndefinitely, d.Facts.DocumentsLive)
	}
	assert.False(t, ScenarioProviderFirst.DocumentsLiveVaries())
	assert.False(t, Scenario("nonsense").DocumentsLiveVaries())
}

func TestFamilyDeclarationOverridesAFact(t *testing.T) {
	t.Parallel()

	d, err := ParseFamilyDeclaration([]byte(`
scenario: provider-first
ahead: either
documentsLive: retention
readerPicksSchema: true
schema: describes
oldestLive: 1.2.0
`))
	require.NoError(t, err)
	assert.Equal(t, ScenarioProviderFirst, d.Scenario)
	assert.Equal(t, FamilyFacts{AheadEither, DocumentsLiveRetention, true, SchemaDescribes}, d.Facts)
	assert.Equal(t, SemVer{1, 2, 0}, d.OldestLive)
}

func TestFamilyDeclarationRefusesUnknownValues(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct{ content, want string }{
		"empty":             {"", "must name a scenario"},
		"only a comment":    {"# Nothing yet\n", "must name a scenario"},
		"no scenario":       {"ahead: writer", "must name a scenario"},
		"scenario":          {"scenario: nonsense", `scenario "nonsense" is not one of`},
		"ahead":             {"scenario: lock-step\nahead: sideways", `ahead "sideways" is not one of`},
		"documentsLive":     {"scenario: lock-step\ndocumentsLive: forever", `documentsLive "forever" is not one of`},
		"schema":            {"scenario: lock-step\nschema: guesses", `schema "guesses" is not one of`},
		"readerPicksSchema": {"scenario: lock-step\nreaderPicksSchema: maybe", "cannot unmarshal"},
		"unknown field":     {"scenario: lock-step\nscenery: nice", "scenery"},
		"not yaml":          {"scenario: [", "yaml"},
	} {
		_, err := ParseFamilyDeclaration([]byte(tt.content))
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), tt.want, name)
	}
}

func TestFamilyDeclarationRequiresOldestLiveForRetention(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct{ content, want string }{
		"scenario implies it": {"scenario: retained-stream", "oldestLive"},
		"override asks for it": {
			"scenario: provider-first\ndocumentsLive: retention", "oldestLive",
		},
		"not a version":     {"scenario: retained-stream\noldestLive: soon", "not a version"},
		"bad version parts": {"scenario: retained-stream\noldestLive: 0.1.0", "not a version"},
		"without retention": {"scenario: provider-first\noldestLive: 1.0.0", "only applies"},
	} {
		_, err := ParseFamilyDeclaration([]byte(tt.content))
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), tt.want, name)
	}
}

func TestFamilyDeclarationChangeFrom(t *testing.T) {
	t.Parallel()

	parse := func(content string) *FamilyDeclaration {
		d, err := ParseFamilyDeclaration([]byte(content))
		require.NoError(t, err, content)
		return d
	}
	retained := parse("scenario: retained-stream\noldestLive: 1.2.0")

	assert.Empty(t, retained.ChangeFrom(retained), "no change")
	assert.Empty(t, parse("scenario: retained-stream\noldestLive: 1.3.0").ChangeFrom(retained), "forward")
	assert.Contains(t, parse("scenario: retained-stream\noldestLive: 1.1.0").ChangeFrom(retained), "cannot move back")
	assert.Contains(t, parse("scenario: lock-step").ChangeFrom(retained), "cannot change")
	assert.Empty(t, parse("scenario: lock-step\nahead: neither").ChangeFrom(parse("scenario: lock-step")),
		"stating what the scenario already implies changes nothing")
	assert.Contains(t, parse("scenario: lock-step\nahead: reader").ChangeFrom(parse("scenario: lock-step")),
		"cannot change")
	assert.Contains(t, parse("scenario: config-file").ChangeFrom(parse("scenario: stored-one-schema")),
		"cannot change", "the same facts under another name are still another scenario")
}

func TestFamilySpecContent(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		spec       FamilySpec
		wantLive   DocumentsLive
		wantOldest SemVer
	}{
		"plain":     {FamilySpec{Scenario: ScenarioLockStep}, DocumentsLiveMoment, SemVer{}},
		"retention": {FamilySpec{Scenario: ScenarioRetainedStream}, DocumentsLiveRetention, SemVer{1, 0, 0}},
		"states documentsLive": {
			FamilySpec{ScenarioThirdPartyFormat, DocumentsLiveIndefinitely}, DocumentsLiveIndefinitely, SemVer{},
		},
		"states retention": {
			FamilySpec{ScenarioUncoordinated, DocumentsLiveRetention}, DocumentsLiveRetention, SemVer{1, 0, 0},
		},
	} {
		data, err := tt.spec.content()
		require.NoError(t, err, name)
		d, err := ParseFamilyDeclaration(data)
		require.NoError(t, err, name)
		assert.Equal(t, tt.spec.Scenario, d.Scenario, name)
		assert.Equal(t, tt.wantLive, d.Facts.DocumentsLive, name)
		assert.Equal(t, tt.wantOldest, d.OldestLive, name)
	}

	_, err := FamilySpec{Scenario: "nonsense"}.content()
	require.Error(t, err)
	_, err = FamilySpec{Scenario: ScenarioUncoordinated}.content()
	require.Error(t, err)
}

func TestRegistryFamilyDeclaration(t *testing.T) {
	t.Parallel()

	key := Key("domain_family_1_0_0")
	setup := func(t *testing.T) *Registry {
		t.Helper()
		r := setupTestRegistry(t)
		s := New(key, r)
		require.NoError(t, os.MkdirAll(s.Path(HomeDir), 0o750))
		require.NoError(t, os.WriteFile(s.Path(FilePath), []byte("{}"), 0o600))
		return r
	}

	t.Run("reads the declaration", func(t *testing.T) {
		t.Parallel()
		r := setup(t)
		require.NoError(t, os.WriteFile(r.FamilyDeclarationPath(key), []byte("scenario: lock-step"), 0o600))
		d, err := r.FamilyDeclaration(key)
		require.NoError(t, err)
		assert.Equal(t, ScenarioLockStep, d.Scenario)
		require.NoError(t, r.RequireFamilyDeclared(key))
		require.NoError(t, r.RequireFamilyDeclarations(context.Background(), ""))
	})

	t.Run("no declaration says how to declare it", func(t *testing.T) {
		t.Parallel()
		r := setup(t)
		_, err := r.FamilyDeclaration(key)
		var missing *MissingFamilyDeclarationError
		require.ErrorAs(t, err, &missing)
		assert.Contains(t, err.Error(), "scenario: provider-first")
		assert.Contains(t, err.Error(), ScenariosDoc)
		require.ErrorAs(t, r.RequireFamilyDeclared(key), &missing)
	})

	t.Run("invalid declaration names the file and the problem", func(t *testing.T) {
		t.Parallel()
		r := setup(t)
		require.NoError(t, os.WriteFile(r.FamilyDeclarationPath(key), []byte("scenario: nonsense"), 0o600))
		_, err := r.FamilyDeclaration(key)
		var invalid *InvalidFamilyDeclarationError
		require.ErrorAs(t, err, &invalid)
		assert.Contains(t, err.Error(), r.FamilyDeclarationPath(key))
		assert.Contains(t, err.Error(), "nonsense")
		require.Error(t, errors.Unwrap(err))
	})

	t.Run("unreadable declaration", func(t *testing.T) {
		t.Parallel()
		r := setup(t)
		require.NoError(t, os.Mkdir(r.FamilyDeclarationPath(key), 0o750))
		_, err := r.FamilyDeclaration(key)
		require.Error(t, err)
		assert.NotErrorAs(t, err, new(*MissingFamilyDeclarationError))
	})

	t.Run("a schema which doesn't exist is left for the command to report", func(t *testing.T) {
		t.Parallel()
		r := setup(t)
		require.NoError(t, r.RequireFamilyDeclared(Key("domain_other_1_0_0")))
	})

	t.Run("scope lists every undeclared family, once each", func(t *testing.T) {
		t.Parallel()
		r := setup(t)
		for _, k := range []Key{"domain_family_1_1_0", "domain_other_1_0_0"} {
			s := New(k, r)
			require.NoError(t, os.MkdirAll(s.Path(HomeDir), 0o750))
			require.NoError(t, os.WriteFile(s.Path(FilePath), []byte("{}"), 0o600))
		}
		err := r.RequireFamilyDeclarations(context.Background(), "")
		require.Error(t, err)
		assert.Equal(t, 1, strings.Count(err.Error(), filepath.Join("domain", "family", FamilyDeclarationFile)))
		assert.Equal(t, 1, strings.Count(err.Error(), filepath.Join("domain", "other", FamilyDeclarationFile)))
	})

	t.Run("scope which isn't there", func(t *testing.T) {
		t.Parallel()
		r := setup(t)
		require.Error(t, r.RequireFamilyDeclarations(context.Background(), "missing"))
		require.Error(t, r.RequireFamilyDeclarations(context.Background(), "NOT VALID"))
	})

	t.Run("a misplaced schema stops the search", func(t *testing.T) {
		t.Parallel()
		r := setup(t)
		misplaced := filepath.Join(r.RootDirectory(), "domain_family_1_0_0"+SchemaSuffix)
		require.NoError(t, os.WriteFile(misplaced, []byte("{}"), 0o600))
		var wrong *MisplacedSchemaError
		require.ErrorAs(t, r.RequireFamilyDeclarations(context.Background(), ""), &wrong)
	})
}

func TestCreateSchemaWritesDeclarationBesideVersions(t *testing.T) {
	t.Parallel()

	t.Run("writes family.yml in the family's directory", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		s, err := r.CreateSchema("domain/family", FamilySpec{Scenario: ScenarioRetainedStream})
		require.NoError(t, err)
		d, err := r.FamilyDeclaration(s.Key())
		require.NoError(t, err)
		assert.Equal(t, ScenarioRetainedStream, d.Scenario)
		assert.Equal(t, SemVer{1, 0, 0}, d.OldestLive)
		assert.Equal(t, filepath.Join(s.Path(FamilyDir), FamilyDeclarationFile), r.FamilyDeclarationPath(s.Key()))
	})

	t.Run("an invalid choice creates nothing", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		_, err := r.CreateSchema("domain/family", FamilySpec{Scenario: "nonsense"})
		require.Error(t, err)
		assert.NoDirExists(t, filepath.Join(r.RootDirectory(), "domain"))
	})

	t.Run("keeps a declaration already there", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		path := r.FamilyDeclarationPath("domain_family_1_0_0")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte("scenario: lock-step"), 0o600))

		_, err := r.CreateSchema("domain/family", FamilySpec{Scenario: ScenarioProviderFirst})

		var declared *FamilyAlreadyDeclaredError
		require.ErrorAs(t, err, &declared)
		assert.Contains(t, err.Error(), path)
		data, rErr := os.ReadFile(path)
		require.NoError(t, rErr)
		assert.Equal(t, "scenario: lock-step", string(data))
	})
}

func TestFamilyDeclarationReadsMigrationStatements(t *testing.T) {
	t.Parallel()

	d, err := ParseFamilyDeclaration([]byte("scenario: stored-one-schema\nmigrations:\n  2: See the runbook.\n"))
	require.NoError(t, err)
	assert.Equal(t, map[uint64]string{2: "See the runbook."}, d.Migrations)

	_, err = ParseFamilyDeclaration([]byte("scenario: stored-one-schema\nmigrations:\n  2: \"  \"\n"))
	require.ErrorContains(t, err, "major 2 is empty")

	_, err = ParseFamilyDeclaration([]byte("scenario: stored-one-schema\nmigrations:\n  two: See the runbook.\n"))
	require.Error(t, err)

	was, err := ParseFamilyDeclaration([]byte("scenario: stored-one-schema\n"))
	require.NoError(t, err)
	assert.Empty(t, d.ChangeFrom(was), "a deployed family gains a statement for each new major")
}

func TestFamilyFactsNeedingMigrationStatement(t *testing.T) {
	t.Parallel()

	want := map[Scenario]bool{
		ScenarioProviderFirst: false, ScenarioConsumerFirst: false, ScenarioConfigFile: true,
		ScenarioStoredOneSchema: true, ScenarioStoredSelfNaming: false, ScenarioRetainedStream: false,
		ScenarioLockStep: false,
	}
	for scenario, needs := range want {
		info, ok := scenarioInfo(scenario)
		require.True(t, ok)
		assert.Equal(t, needs, info.Facts.NeedsMigrationStatement(), scenario)
	}
}

func TestFamilyDeclarationLive(t *testing.T) {
	t.Parallel()

	everything := &FamilyDeclaration{}
	assert.True(t, everything.Live(SemVer{1, 0, 0}), "a family with no oldestLive holds every version")

	retained := &FamilyDeclaration{OldestLive: SemVer{1, 2, 0}}
	assert.False(t, retained.Live(SemVer{1, 1, 9}))
	assert.True(t, retained.Live(SemVer{1, 2, 0}))
	assert.True(t, retained.Live(SemVer{1, 2, 1}))
	assert.True(t, retained.Live(SemVer{2, 0, 0}))
}

// Readers copy the samples in the docs, so each must be a declaration the parser accepts.
func TestFamilyDeclarationSamplesInTheDocsAreValid(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", ScenariosDoc))
	require.NoError(t, err)

	samples := regexp.MustCompile("(?s)```yaml\n(scenario:.*?)```").FindAllSubmatch(data, -1)
	require.NotEmpty(t, samples)
	for _, sample := range samples {
		_, pErr := ParseFamilyDeclaration(sample[1])
		assert.NoError(t, pErr, string(sample[1]))
	}
}
