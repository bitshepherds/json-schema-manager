package fsh_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/v2/internal/fsh"
)

func TestCanonicalPath(t *testing.T) {
	t.Parallel()

	t.Run("resolves absolute path", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "test")
		require.NoError(t, os.Mkdir(path, 0o755))

		canonical, err := fsh.CanonicalPath(path)
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(canonical))
		assert.Contains(t, canonical, "test")
	})

	t.Run("resolves symlinks", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		target := filepath.Join(dir, "target")
		require.NoError(t, os.Mkdir(target, 0o755))

		link := filepath.Join(dir, "link")
		require.NoError(t, os.Symlink(target, link))

		canonical, err := fsh.CanonicalPath(link)
		require.NoError(t, err)

		expected, _ := filepath.EvalSymlinks(target)
		assert.Equal(t, expected, canonical)
	})

	t.Run("returns error for non-existent path", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "non-existent")

		_, err := fsh.CanonicalPath(path)
		require.Error(t, err)
		assert.True(t, os.IsNotExist(err))
	})
}

func TestAbs(t *testing.T) {
	t.Parallel()

	t.Run("returns absolute path", func(t *testing.T) {
		t.Parallel()
		abs, err := fsh.Abs("relative/path")
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(abs))
	})
}
