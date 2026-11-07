package testenv

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	Hide()
	os.Exit(m.Run())
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(out)
}

// TestHideKeepsTheMachineFromGitAndJSM builds a machine set up to break tests, then hides it. It isn't
// parallel, as t.Setenv sets the environment it's about.
func TestHideKeepsTheMachineFromGitAndJSM(t *testing.T) {
	t.Cleanup(Hide) // Runs last, once t.Setenv's values are gone, so later tests see a hidden machine too.

	home := t.TempDir()
	config := "[user]\n\tname = machine\n[tag]\n\tgpgsign = true\n"
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "git"), 0o750))
	for _, rel := range []string{".gitconfig", filepath.Join(".config", "git", "config")} {
		require.NoError(t, os.WriteFile(filepath.Join(home, rel), []byte(config), 0o600))
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_DIR", filepath.Join(home, "elsewhere"))
	t.Setenv("JSM_REGISTRY_ROOT_DIR", home)
	t.Setenv("JSM_LOG_FILE", filepath.Join(home, "jsm.log"))

	Hide()

	gone := []string{"XDG_CONFIG_HOME", "GIT_CONFIG_SYSTEM", "GIT_DIR", "JSM_REGISTRY_ROOT_DIR", "JSM_LOG_FILE"}
	for _, name := range gone {
		_, set := os.LookupEnv(name)
		assert.False(t, set, "%s is still set", name)
	}
	for name, value := range Vars() {
		assert.Equal(t, value, os.Getenv(name), name)
	}

	dir := t.TempDir()
	assert.Empty(t, gitIn(t, dir, "config", "--list"), "git reads the machine's configuration")
	assert.Contains(t, gitIn(t, dir, "var", "GIT_COMMITTER_IDENT"), "test <test@example.com>")
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "first")
	gitIn(t, dir, "tag", "-a", "annotated", "-m", "needs an identity, and isn't signed")

	goEnv := GoToolEnv()
	assert.Contains(t, goEnv, "HOME="+home, "the go tool finds its caches from the real HOME")
	assert.Contains(t, goEnv, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"))
	assert.False(t, slices.ContainsFunc(goEnv, func(kv string) bool {
		return strings.HasPrefix(kv, "GIT_") || strings.HasPrefix(kv, "JSM_")
	}), "the go tool runs git to stamp its builds, so git's variables are kept from it")
}

// TestEveryTestPackageHidesTheMachine is what stops a test relying on the machine again: a new package
// can't start git or read the environment without having hidden the machine first.
func TestEveryTestPackageHidesTheMachine(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")
	var unhidden, seen []string
	for _, top := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() || d.Name() == "testdata" {
				return nil
			}
			tests, err := filepath.Glob(filepath.Join(path, "*_test.go"))
			if err != nil {
				return err
			}
			if len(tests) == 0 {
				return nil
			}
			seen = append(seen, path)
			if !anyCallsHide(t, tests) {
				unhidden = append(unhidden, path)
			}
			return nil
		})
		require.NoError(t, err)
	}

	assert.Contains(t, seen, filepath.Join(root, "internal", "app"), "the walk found the test packages")
	assert.Empty(t, unhidden, "these packages' tests can see the machine: give each a TestMain that calls testenv.Hide")
}

func anyCallsHide(t *testing.T, files []string) bool {
	t.Helper()
	for _, f := range files {
		content, err := os.ReadFile(f)
		require.NoError(t, err)
		if strings.Contains(string(content), "testenv.Hide()") || strings.Contains(string(content), "\tHide()\n") {
			return true
		}
	}
	return false
}
