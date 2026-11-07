package schema

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/v2/internal/config"
	"github.com/bitshepherds/json-schema-manager/v2/internal/fsh"
)

// makeRegistryDir makes a folder holding a registry's config file, and returns its canonical path.
func makeRegistryDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, config.JsmRegistryConfigFile)
	require.NoError(t, os.WriteFile(configPath, []byte(config.DefaultConfigContent), 0o600))
	canonical, err := fsh.NewPathResolver().CanonicalPath(dir)
	require.NoError(t, err)
	return canonical
}

func envWith(value string) *mockEnvProvider {
	return &mockEnvProvider{values: map[string]string{RootDirEnvVar: value}}
}

func TestInitRootDirectory_CurrentDirectoryBeatsEnv(t *testing.T) {
	t.Parallel()
	a, b := makeRegistryDir(t), makeRegistryDir(t)

	choice, err := initRootDirectory("", b, fsh.NewPathResolver(), envWith(a))
	require.NoError(t, err)
	assert.Equal(t, b, choice.Dir)
	assert.Equal(t, SourceCurrentDir, choice.Source)
	assert.Equal(t, a, choice.IgnoredEnv, "the variable was ignored, so jsm warns of it")
}

func TestInitRootDirectory_EnvWhenCurrentDirectoryHasNoRegistry(t *testing.T) {
	t.Parallel()
	a := makeRegistryDir(t)

	choice, err := initRootDirectory("", t.TempDir(), fsh.NewPathResolver(), envWith(a))
	require.NoError(t, err)
	assert.Equal(t, a, choice.Dir)
	assert.Equal(t, SourceEnv, choice.Source)
	assert.Empty(t, choice.IgnoredEnv)
}

func TestInitRootDirectory_FlagBeatsBoth(t *testing.T) {
	t.Parallel()
	a, b, flag := makeRegistryDir(t), makeRegistryDir(t), makeRegistryDir(t)

	choice, err := initRootDirectory(flag, b, fsh.NewPathResolver(), envWith(a))
	require.NoError(t, err)
	assert.Equal(t, flag, choice.Dir)
	assert.Equal(t, SourceFlag, choice.Source)
}

func TestInitRootDirectory_FlagGivesNoWarning(t *testing.T) {
	t.Parallel()
	a, b, flag := makeRegistryDir(t), makeRegistryDir(t), makeRegistryDir(t)

	choice, err := initRootDirectory(flag, b, fsh.NewPathResolver(), envWith(a))
	require.NoError(t, err)
	assert.Empty(t, choice.IgnoredEnv, "--registry is explicit, so nothing was ignored")
}

func TestInitRootDirectory_EnvNotARegistry_CurrentDirectoryWins(t *testing.T) {
	t.Parallel()
	b := makeRegistryDir(t)

	for name, env := range map[string]string{
		"a folder with no registry": t.TempDir(),
		"a folder that is missing":  filepath.Join(t.TempDir(), "missing"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			choice, err := initRootDirectory("", b, fsh.NewPathResolver(), envWith(env))
			require.NoError(t, err)
			assert.Equal(t, b, choice.Dir)
			assert.Equal(t, SourceCurrentDir, choice.Source)
			assert.Equal(t, env, choice.IgnoredEnv)
		})
	}
}

func TestInitRootDirectory_SameRegistryByAnotherPath_NoWarning(t *testing.T) {
	t.Parallel()
	b := makeRegistryDir(t)
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(b, link))

	choice, err := initRootDirectory("", b, fsh.NewPathResolver(), envWith(link))
	require.NoError(t, err)
	assert.Equal(t, b, choice.Dir)
	assert.Empty(t, choice.IgnoredEnv, "the variable names the same registry")
}

func TestInitRootDirectory_CurrentDirectoryUnresolvable(t *testing.T) {
	t.Parallel()
	b := makeRegistryDir(t)
	resolver := &mockPathResolver{canonicalPathFn: func(string) (string, error) {
		return "", errors.New("cannot resolve")
	}}

	_, err := initRootDirectory("", b, resolver, &mockEnvProvider{})
	var target *RegistryInitError
	require.ErrorAs(t, err, &target)
	assert.Equal(t, "the current directory", target.Source)
}

func TestNewRegistry_NoRegistryErrorUnchanged(t *testing.T) {
	t.Parallel()
	// The tests run in this package's directory, which holds no registry.
	_, err := NewRegistry("", &mockCompiler{}, fsh.NewPathResolver(), &mockEnvProvider{})
	var target *NoRegistryError
	require.ErrorAs(t, err, &target)
	wd, wdErr := os.Getwd()
	require.NoError(t, wdErr)
	wd, wdErr = fsh.NewPathResolver().CanonicalPath(wd)
	require.NoError(t, wdErr)
	assert.Equal(t, "no registry found: "+wd+" has no json-schema-manager-config.yml. Run jsm in a registry's root "+
		"directory, give its path with --registry (-r) or $JSM_REGISTRY_ROOT_DIR, or create a registry with "+
		"'jsm create-registry <dir>'", err.Error())
}

func TestRegistry_NamesItsSource(t *testing.T) {
	t.Parallel()
	a, b := makeRegistryDir(t), makeRegistryDir(t)
	r, err := NewRegistry(b, &mockCompiler{}, fsh.NewPathResolver(), envWith(a))
	require.NoError(t, err)
	assert.Equal(t, SourceFlag, r.Source())
	assert.Empty(t, r.IgnoredEnv())
	assert.Equal(t, "--registry", SourceFlag.String())
	assert.Equal(t, "the current directory", SourceCurrentDir.String())
	assert.Equal(t, "$JSM_REGISTRY_ROOT_DIR", SourceEnv.String())
}
