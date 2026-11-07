package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests tie what a release names to go.mod's module path, so a version 2 reaches people who
// install with go install, and released binaries report their version rather than dev.

const repoRoot = "../.."

// goInstallPrefix starts every go install line for jsm, whatever major version it names.
const goInstallPrefix = "go install github.com/bitshepherds/json-schema-manager"

var (
	moduleLine   = regexp.MustCompile(`(?m)^module\s+(\S+)\s*$`)
	versionFlag  = regexp.MustCompile(`-X\s+(\S+)\.Version=`)
	goInstallArg = regexp.MustCompile(`go install (\S+)`)
)

// modulePath returns the module path go.mod declares.
func modulePath(t *testing.T) string {
	t.Helper()
	m := moduleLine.FindStringSubmatch(readRepoFile(t, "go.mod"))
	require.NotNil(t, m, "go.mod declares no module")
	return m[1]
}

func readRepoFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, name))
	require.NoError(t, err)
	return string(data)
}

// assertVersionLdflagNamesModule checks that every -X ...Version= flag in the file sets the Version
// variable of go.mod's internal/app package, and that there is at least one.
func assertVersionLdflagNamesModule(t *testing.T, name string) {
	t.Helper()
	want := modulePath(t) + "/internal/app"
	matches := versionFlag.FindAllStringSubmatch(readRepoFile(t, name), -1)
	require.NotEmpty(t, matches, "%s sets no -X ...Version= ldflag", name)
	for _, m := range matches {
		assert.Equal(t, want, m[1], "%s sets Version in the wrong package", name)
	}
}

func TestModulePath_IsMajorVersionTwo(t *testing.T) {
	t.Parallel()
	assert.True(t, strings.HasSuffix(modulePath(t), "/v2"),
		"go.mod's module path %q must end in /v2, or go install never picks up v2 releases", modulePath(t))
}

func TestGoreleaser_VersionLdflagNamesModule(t *testing.T) {
	t.Parallel()
	assertVersionLdflagNamesModule(t, ".goreleaser.yml")
}

func TestBuildScript_VersionLdflagNamesModule(t *testing.T) {
	t.Parallel()
	assertVersionLdflagNamesModule(t, "scripts/build/main.go")
}

func TestDocs_GoInstallUsesModulePath(t *testing.T) {
	t.Parallel()
	want := modulePath(t) + "/cmd/jsm@"

	docs := []string{"README.md"}
	err := filepath.WalkDir(filepath.Join(repoRoot, "user-docs"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, relErr := filepath.Rel(repoRoot, path)
			if relErr != nil {
				return relErr
			}
			docs = append(docs, rel)
		}
		return nil
	})
	require.NoError(t, err)

	found := 0
	for _, doc := range docs {
		for i, line := range strings.Split(readRepoFile(t, doc), "\n") {
			if !strings.Contains(line, goInstallPrefix) {
				continue
			}
			found++
			m := goInstallArg.FindStringSubmatch(line)
			require.NotNil(t, m)
			assert.True(t, strings.HasPrefix(m[1], want),
				"%s:%d installs %q, want %s<version>", doc, i+1, m[1], want)
		}
	}
	assert.Positive(t, found, "no go install line for jsm in README.md or user-docs")
}
