package fsh

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPathResolver is a test implementation of PathResolver.
type mockPathResolver struct {
	canonicalPathFn       func(path string) (string, error)
	absFn                 func(path string) (string, error)
	getUintSubdirectories func(dirPath string) ([]uint64, error)
}

func (m *mockPathResolver) CanonicalPath(path string) (string, error) {
	if m.canonicalPathFn != nil {
		return m.canonicalPathFn(path)
	}
	return path, nil
}

func (m *mockPathResolver) Abs(path string) (string, error) {
	if m.absFn != nil {
		return m.absFn(path)
	}
	return filepath.Abs(path)
}

func (m *mockPathResolver) GetUintSubdirectories(dirPath string) ([]uint64, error) {
	if m.getUintSubdirectories != nil {
		return m.getUintSubdirectories(dirPath)
	}
	return GetUintSubdirectories(dirPath)
}

func TestStandardPathResolver(t *testing.T) {
	t.Parallel()

	t.Run("CanonicalPath resolves symlinks", func(t *testing.T) {
		t.Parallel()
		resolver := NewPathResolver()

		dir := t.TempDir()
		target := filepath.Join(dir, "target")
		require.NoError(t, os.Mkdir(target, 0o755))

		link := filepath.Join(dir, "link")
		require.NoError(t, os.Symlink(target, link))

		canonical, err := resolver.CanonicalPath(link)
		require.NoError(t, err)

		expected, _ := filepath.EvalSymlinks(target)
		assert.Equal(t, expected, canonical)
	})

	t.Run("CanonicalPath returns error for non-existent path", func(t *testing.T) {
		t.Parallel()
		resolver := NewPathResolver()

		_, err := resolver.CanonicalPath("/non/existent/path")
		require.Error(t, err)
	})

	t.Run("CanonicalPath returns error for empty path", func(t *testing.T) {
		t.Parallel()
		resolver := NewPathResolver()

		// filepath.EvalSymlinks("") returns ".", nil on some systems, so we test an invalid path instead
		_, err := resolver.CanonicalPath("\000") // Null character is invalid in paths
		require.Error(t, err)
	})

	t.Run("Abs returns absolute path", func(t *testing.T) {
		t.Parallel()
		resolver := NewPathResolver()

		abs, err := resolver.Abs("relative/path")
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(abs))
	})

	t.Run("CanonicalPath fails with null byte", func(t *testing.T) {
		t.Parallel()
		resolver := NewPathResolver()

		_, err := resolver.CanonicalPath("invalid\x00path")
		assert.Error(t, err)
	})

	t.Run("GetUintSubdirectories delegation", func(t *testing.T) {
		t.Parallel()
		resolver := NewPathResolver()

		tmpDir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "1"), 0o755))
		require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "10"), 0o755))
		require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "not-a-number"), 0o755))

		got, err := resolver.GetUintSubdirectories(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, []uint64{1, 10}, got)
	})
}

// TestCanonicalPathFailsWithDeletedWorkingDirectory isn't parallel: it
// changes the process's working directory and then deletes it, which
// would break any test running alongside it that resolves a relative path
// (issue #33). As a top-level sequential test, it runs before Go releases
// the paused parallel tests, and t.Chdir restores the old directory when
// it ends.
func TestCanonicalPathFailsWithDeletedWorkingDirectory(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	require.NoError(t, os.RemoveAll(tmp))

	resolver := NewPathResolver()
	_, err := resolver.CanonicalPath(".")
	if runtime.GOOS == "darwin" {
		// Darwin caches the working directory path, so Getwd may
		// succeed even after the directory is removed.
		t.Logf("darwin: CanonicalPath error (may be nil): %v", err)
	} else {
		require.Error(t, err)
	}
}

func TestMockPathResolver(t *testing.T) {
	t.Parallel()

	t.Run("returns error when Abs fails", func(t *testing.T) {
		t.Parallel()
		mock := &mockPathResolver{
			absFn: func(_ string) (string, error) {
				return "", os.ErrPermission
			},
		}

		_, err := mock.Abs("some/path")
		assert.ErrorIs(t, err, os.ErrPermission)
	})

	t.Run("returns error when CanonicalPath fails", func(t *testing.T) {
		t.Parallel()
		mock := &mockPathResolver{
			canonicalPathFn: func(_ string) (string, error) {
				return "", os.ErrPermission
			},
		}

		_, err := mock.CanonicalPath("some/path")
		assert.ErrorIs(t, err, os.ErrPermission)
	})

	t.Run("CanonicalPath fails with null byte in relative path", func(t *testing.T) {
		t.Parallel()
		resolver := NewPathResolver()
		// On many systems, Abs(".") works but Abs("...\x00") might fail
		_, err := resolver.CanonicalPath("relative\x00path")
		assert.Error(t, err)
	})
}
