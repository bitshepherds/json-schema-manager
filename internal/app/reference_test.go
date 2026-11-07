package app

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/v2/internal/schema"
)

// TestReferenceIsUpToDate fails when the committed command reference no longer matches the CLI.
func TestReferenceIsUpToDate(t *testing.T) {
	t.Parallel()

	generated := t.TempDir()
	require.NoError(t, WriteReference(generated))

	want := markdownFiles(t, generated)
	got := markdownFiles(t, filepath.Join("..", "..", ReferenceDir))
	for _, name := range slices.Sorted(maps.Keys(want)) {
		if got[name] != want[name] {
			t.Errorf("%s/%s is out of date: run 'make docs' and commit the result", ReferenceDir, name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(got)) {
		if _, ok := want[name]; !ok {
			t.Errorf("%s/%s is for a command which no longer exists: run 'make docs'", ReferenceDir, name)
		}
	}
}

// The command reference is generated from the help, so the help must say what the command does now.
func TestHelpSaysWhatTheCommandDoes(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cmd  *cobra.Command
		says []string
	}{
		"diff follows the scenario": {NewDiffCmd(&MockManager{}), []string{
			"alters JSM cannot tell", "used by pass/x.json", "shown by fail/y.json",
			"no pass document uses it", "no fail document shows it", "neither side ahead flags nothing",
			"Every change is listed", "doesn't read compat-exceptions.yml",
			"the older version's own documents shows, by the newer version's verdict",
		}},
		"check-party warns of a version below oldestLive": {
			NewCheckPartyCmd(&MockManager{}), []string{"below its family's oldestLive"},
		},
		"validate fails a major with no migration statement": {NewValidateCmd(&MockManager{}), []string{
			"migration statement", "writer-ahead family an inherited pass document doesn't run on the later version",
			"reader-ahead family nor does an inherited fail document",
		}},
		"create-schema gives the three declaration changes": {NewCreateSchemaCmd(&MockManager{}), []string{
			"moving oldestLive forward", "adding or changing a migration statement",
			"adding a declaration to a family with none", "migrations:", "refused otherwise",
			"must name a version the family has",
		}},
		"check-changes lets an entry go once it clears nothing": {
			NewCheckChangesCmd(&MockManager{}), []string{"while it clears something"},
		},
		"a major bump copies only the target's own documents": {
			NewCreateSchemaVersionCmd(&MockManager{}), []string{"not the rest of its major"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			long := strings.Join(strings.Fields(tt.cmd.Long), " ")
			for _, says := range tt.says {
				assert.Contains(t, long, says)
			}
		})
	}
}

// Readers copy the sample in the help into a new family, so it must hold in a family with only 1.0.0.
func TestFamilyFileHelpSampleIsValid(t *testing.T) {
	t.Parallel()

	_, rest, found := strings.Cut(strings.TrimLeft(familyFileHelp, "\n"), "\n\n")
	require.True(t, found)
	sample, _, found := strings.Cut(rest, "\n\n")
	require.True(t, found)
	require.Contains(t, sample, "scenario:")

	r := setupTestRegistry(t)
	s := schema.New("domain_family_1_0_0", r)
	require.NoError(t, os.MkdirAll(s.Path(schema.HomeDir), 0o750))
	require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte(`{}`), 0o600))
	require.NoError(t, os.WriteFile(r.FamilyDeclarationPath(s.Key()), []byte(sample), 0o600))

	_, err := r.FamilyDeclaration(s.Key())
	require.NoError(t, err, sample)
}

func TestWriteReferenceMissingDir(t *testing.T) {
	t.Parallel()

	require.Error(t, WriteReference(filepath.Join(t.TempDir(), "missing")))
}

// markdownFiles returns the contents of each Markdown file in dir, by name.
func markdownFiles(t *testing.T, dir string) map[string]string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(dir, "*.md"))
	require.NoError(t, err)

	files := make(map[string]string, len(paths))
	for _, p := range paths {
		data, rErr := os.ReadFile(p)
		require.NoError(t, rErr)
		files[filepath.Base(p)] = string(data)
	}
	return files
}
