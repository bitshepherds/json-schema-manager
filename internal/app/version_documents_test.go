package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests run the whole CLI to show what a new version carries, and what --strict asks of each
// version's documents.

const (
	versionDocsPass = `{}`
	versionDocsFail = `[]`
)

// familiesThatInherit are declarations of families whose rule runs earlier documents across versions.
var familiesThatInherit = map[string]string{
	"writer ahead":               "scenario: owner-writes\n",
	"reader ahead":               "scenario: owner-reads\n",
	"either":                     "scenario: uncoordinated\ndocumentsLive: indefinitely\n",
	"lock-step, writer override": "scenario: lock-step\nahead: writer\n",
}

// familiesThatDoNot are declarations of families with no cross-version pairs.
var familiesThatDoNot = map[string]string{
	"lock-step":                 "scenario: lock-step\n",
	"stored, naming versions":   "scenario: stored-self-naming\n",
	"reader picks, by override": "scenario: owner-writes\nreaderPicksSchema: true\n",
	"neither, by override":      "scenario: owner-writes\nahead: neither\n",
}

// newVersionDocsRegistry returns a family with the given declaration, whose 1.1.0 has a pass and a fail
// document, an exception file, and other files which aren't test documents.
func newVersionDocsRegistry(t *testing.T, declaration string) *declTestRegistry {
	t.Helper()
	r := newDeclTestRegistry(t)
	r.write("domain/family/family.yml", declaration)
	r.addDocs("1/1/0", "pass", "fail")
	r.write("domain/family/1/1/0/compat-exceptions.yml", "exceptions: []\n")
	r.write("domain/family/1/1/0/NOTES.md", "A note beside the schema.\n")
	r.write("domain/family/1/1/0/pass/NOTES.md", "A note beside the documents.\n")
	r.write("domain/family/1/1/0/pass/fixtures/nested.json", "{}\n")
	return r
}

// addDocs gives the version one document in each of the given directories.
func (r *declTestRegistry) addDocs(version string, dirs ...string) {
	r.t.Helper()
	for _, dir := range dirs {
		content := versionDocsPass
		if dir == "fail" {
			content = versionDocsFail
		}
		r.write(filepath.Join("domain/family", version, dir, "doc.json"), content)
	}
}

// addVersion adds a version with no documents, as a new version has once it carries only the schema.
func (r *declTestRegistry) addVersion(version, key string) {
	r.t.Helper()
	r.write(filepath.Join("domain/family", version, key+".schema.json"), declTestSchema)
}

// filesIn returns every file below the directory, by its path from there.
func filesIn(t *testing.T, dir string) []string {
	t.Helper()
	files := []string{}
	require.NoError(t, filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, rErr := filepath.Rel(dir, p)
		files = append(files, filepath.ToSlash(rel))
		return rErr
	}))
	return files
}

func assertCarriesSchemaOnly(t *testing.T, r *declTestRegistry, version, key string) {
	t.Helper()
	home := filepath.Join(r.reg, "domain/family", version)
	assert.Equal(t, []string{key + ".schema.json"}, filesIn(t, home))
	assert.DirExists(t, filepath.Join(home, "pass"), "there to put documents in")
	assert.DirExists(t, filepath.Join(home, "fail"), "there to put documents in")
}

func assertCarriesDocuments(t *testing.T, r *declTestRegistry, version, key string) {
	t.Helper()
	home := filepath.Join(r.reg, "domain/family", version)
	assert.Equal(t, []string{key + ".schema.json", "fail/doc.json", "pass/doc.json"}, filesIn(t, home))

	pass, err := os.ReadFile(filepath.Join(home, "pass/doc.json"))
	require.NoError(t, err)
	assert.Equal(t, versionDocsPass, string(pass))
	fail, err := os.ReadFile(filepath.Join(home, "fail/doc.json"))
	require.NoError(t, err)
	assert.Equal(t, versionDocsFail, string(fail))
}

func TestCreateSchemaVersionPatchCopiesSchemaOnly(t *testing.T) {
	t.Parallel()
	for name, declaration := range familiesThatInherit {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newVersionDocsRegistry(t, declaration)
			_, err := r.jsm("create-schema-version", declTestLater, "patch")
			require.NoError(t, err)
			assertCarriesSchemaOnly(t, r, "1/1/1", "domain_family_1_1_1")
		})
	}
}

func TestCreateSchemaVersionMinorCopiesSchemaOnly(t *testing.T) {
	t.Parallel()
	for name, declaration := range familiesThatInherit {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newVersionDocsRegistry(t, declaration)
			_, err := r.jsm("create-schema-version", declTestLater, "minor")
			require.NoError(t, err)
			assertCarriesSchemaOnly(t, r, "1/2/0", "domain_family_1_2_0")

			content, err := os.ReadFile(filepath.Join(r.reg, "domain/family/1/2/0/domain_family_1_2_0.schema.json"))
			require.NoError(t, err)
			assert.JSONEq(t, declTestSchema, string(content), "the schema is a copy of the target")
		})
	}
}

func TestCreateSchemaVersionMajorCopiesDocuments(t *testing.T) {
	t.Parallel()
	for name, declaration := range familiesThatInherit {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newVersionDocsRegistry(t, declaration)
			_, err := r.jsm("create-schema-version", declTestLater, "major")
			require.NoError(t, err)
			assertCarriesDocuments(t, r, "2/0/0", "domain_family_2_0_0")
		})
	}
}

func TestCreateSchemaVersionNeitherFamilyCopiesDocuments(t *testing.T) {
	t.Parallel()
	bumps := map[string]struct{ version, key string }{
		"patch": {"1/1/1", "domain_family_1_1_1"},
		"minor": {"1/2/0", "domain_family_1_2_0"},
		"major": {"2/0/0", "domain_family_2_0_0"},
	}
	for name, declaration := range familiesThatDoNot {
		for bump, want := range bumps {
			t.Run(name+" "+bump, func(t *testing.T) {
				t.Parallel()
				r := newVersionDocsRegistry(t, declaration)
				_, err := r.jsm("create-schema-version", declTestLater, bump)
				require.NoError(t, err)
				assertCarriesDocuments(t, r, want.version, want.key)
			})
		}
	}
}

func TestCreateSchemaVersionNeverCopiesExceptionFile(t *testing.T) {
	t.Parallel()
	declarations := map[string]string{
		"family that inherits":   familiesThatInherit["writer ahead"],
		"family that never does": familiesThatDoNot["lock-step"],
	}
	bumps := map[string]string{"patch": "1/1/1", "minor": "1/2/0", "major": "2/0/0"}
	for name, declaration := range declarations {
		for bump, version := range bumps {
			t.Run(name+" "+bump, func(t *testing.T) {
				t.Parallel()
				r := newVersionDocsRegistry(t, declaration)
				_, err := r.jsm("create-schema-version", declTestLater, bump)
				require.NoError(t, err)
				home := filepath.Join(r.reg, "domain/family", version)
				assert.NotContains(t, filesIn(t, home), "compat-exceptions.yml")
			})
		}
	}
}

func TestCreateSchemaVersionHelpSaysWhatAVersionCarries(t *testing.T) {
	t.Parallel()
	r := newVersionDocsRegistry(t, familiesThatInherit["writer ahead"])
	out, err := r.jsm("create-schema-version", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "the schema only")
	assert.Contains(t, out, "A major bump")
	assert.Contains(t, out, "no pairs of versions")
}

func TestStrictCountsInheritedDocuments(t *testing.T) {
	t.Parallel()

	for name, declaration := range familiesThatInherit {
		t.Run("a version made with no documents passes, "+name, func(t *testing.T) {
			t.Parallel()
			r := newVersionDocsRegistry(t, declaration)
			_, err := r.jsm("create-schema-version", declTestLater, "minor")
			require.NoError(t, err)

			out, err := r.jsm("validate", "domain_family_1_2_0", "--strict")
			require.NoError(t, err, out)
		})
	}

	t.Run("each type may come from a different earlier version", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		r.write("domain/family/family.yml", familiesThatInherit["reader ahead"])
		r.addDocs("1/0/0", "pass")
		r.addDocs("1/1/0", "fail")
		r.addVersion("1/2/0", "domain_family_1_2_0")

		out, err := r.jsm("validate", "domain_family_1_2_0", "--strict")
		require.NoError(t, err, out)

		out, err = r.jsm("validate", declTestLater, "--strict")
		require.NoError(t, err, out)

		out, err = r.jsm("validate", declTestKey, "--strict")
		require.ErrorIs(t, err, ErrTestsFailed, "1.0.0 has no fail document, and nothing to inherit")
		assert.Contains(t, out, "no fail documents")
	})

	t.Run("a type no earlier version has is still missing", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		r.write("domain/family/family.yml", familiesThatInherit["writer ahead"])
		r.addDocs("1/0/0", "pass")

		out, err := r.jsm("validate", declTestLater, "--strict", "--nocolour")
		require.ErrorIs(t, err, ErrTestsFailed)
		assert.Contains(t, out, "no fail documents")
		assert.NotContains(t, out, "no pass")
	})

	t.Run("a later version's documents are not inherited", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		r.write("domain/family/family.yml", familiesThatInherit["writer ahead"])
		r.addDocs("1/1/0", "pass", "fail")

		_, err := r.jsm("validate", declTestKey, "--strict")
		require.ErrorIs(t, err, ErrTestsFailed)
	})

	t.Run("reports say what a version inherits", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		r.write("domain/family/family.yml", familiesThatInherit["writer ahead"])
		r.addDocs("1/0/0", "pass", "fail")

		out, err := r.jsm("validate", declTestLater, "--verbose", "--nocolour")
		require.NoError(t, err, out)
		assert.Contains(t, out, "inherited: 1 pass and 1 fail documents from earlier versions in its major")
		assert.NotContains(t, out, "no pass")

		out, err = r.jsm("validate", declTestLater, "--output", "json")
		require.NoError(t, err, out)
		assert.Contains(t, out, `"inheritedPassDocuments": 1`)
		assert.Contains(t, out, `"inheritedFailDocuments": 1`)
		assert.Contains(t, out, `"missingDocuments": false`)

		out, err = r.jsm("validate", declTestLater, "--output", "junit", "--strict")
		require.NoError(t, err, out)
		assert.NotContains(t, out, "no pass documents")
	})

	t.Run("documents already copied into a version count, and still run", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		r.write("domain/family/family.yml", familiesThatInherit["writer ahead"])
		r.addDocs("1/1/0", "pass", "fail")
		r.addDocs("1/0/0", "pass", "fail")

		rep, err := r.runJSON(declTestLater, "--strict")
		require.NoError(t, err)
		assert.Contains(t, rep.labels(), "1.1.0 pass on 1.1.0")
		assert.Contains(t, rep.labels(), "1.1.0 fail on 1.1.0")
	})
}

func TestStrictRequiresOwnDocumentsForFirstOfMajor(t *testing.T) {
	t.Parallel()

	t.Run("1.0.0 needs its own", func(t *testing.T) {
		t.Parallel()
		r := newVersionDocsRegistry(t, familiesThatInherit["writer ahead"])

		out, err := r.jsm("validate", declTestKey, "--strict", "--nocolour")
		require.ErrorIs(t, err, ErrTestsFailed)
		assert.Contains(t, out, "no pass or fail documents")
	})

	t.Run("a later major's first version inherits nothing across the major", func(t *testing.T) {
		t.Parallel()
		r := newVersionDocsRegistry(t, familiesThatInherit["writer ahead"])
		r.addVersion("2/0/0", "domain_family_2_0_0")

		out, err := r.jsm("validate", "domain_family_2_0_0", "--strict", "--nocolour")
		require.ErrorIs(t, err, ErrTestsFailed)
		assert.Contains(t, out, "no pass or fail documents")
	})

	t.Run("the first version on disk is the first, if 1.0.0 has gone", func(t *testing.T) {
		t.Parallel()
		r := newDeclTestRegistry(t)
		r.write("domain/family/family.yml", familiesThatInherit["writer ahead"])
		require.NoError(t, os.RemoveAll(filepath.Join(r.reg, "domain/family/1/0")))

		_, err := r.jsm("validate", declTestLater, "--strict")
		require.ErrorIs(t, err, ErrTestsFailed)
	})

	t.Run("the copies a major bump makes count as its own", func(t *testing.T) {
		t.Parallel()
		r := newVersionDocsRegistry(t, familiesThatInherit["writer ahead"])
		_, err := r.jsm("create-schema-version", declTestLater, "major")
		require.NoError(t, err)

		out, err := r.jsm("validate", "domain_family_2_0_0", "--strict")
		require.NoError(t, err, out)
	})

	t.Run("later versions in a major inherit only from that major", func(t *testing.T) {
		t.Parallel()
		r := newVersionDocsRegistry(t, familiesThatInherit["writer ahead"])
		r.addVersion("2/0/0", "domain_family_2_0_0")
		r.addDocs("2/0/0", "pass")
		r.addVersion("2/1/0", "domain_family_2_1_0")

		out, err := r.jsm("validate", "domain_family_2_1_0", "--strict", "--nocolour")
		require.ErrorIs(t, err, ErrTestsFailed)
		assert.Contains(t, out, "no fail documents")
	})
}

func TestStrictNeitherFamilyRequiresDocumentsInEveryVersion(t *testing.T) {
	t.Parallel()
	for name, declaration := range familiesThatDoNot {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newDeclTestRegistry(t)
			r.write("domain/family/family.yml", declaration)
			r.addDocs("1/0/0", "pass", "fail")

			out, err := r.jsm("validate", declTestLater, "--strict", "--nocolour")
			require.ErrorIs(t, err, ErrTestsFailed)
			assert.Contains(t, out, "no pass or fail documents")

			r.addDocs("1/1/0", "pass", "fail")
			out, err = r.jsm("validate", declTestLater, "--strict")
			require.NoError(t, err, out)
		})
	}

	t.Run("create-schema-version leaves it passing", func(t *testing.T) {
		t.Parallel()
		r := newVersionDocsRegistry(t, familiesThatDoNot["lock-step"])
		_, err := r.jsm("create-schema-version", declTestLater, "minor")
		require.NoError(t, err)

		out, err := r.jsm("validate", "domain_family_1_2_0", "--strict")
		require.NoError(t, err, out)
	})
}
