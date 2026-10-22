package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReferenceIsUpToDate fails when the committed command reference no longer matches the CLI.
func TestReferenceIsUpToDate(t *testing.T) {
	t.Parallel()

	generated := t.TempDir()
	require.NoError(t, WriteReference(generated))

	committed := filepath.Join("..", "..", ReferenceDir)
	assert.Equal(t, markdownFiles(t, generated), markdownFiles(t, committed),
		"%s is out of date: run 'make docs' and commit the result", ReferenceDir)
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
