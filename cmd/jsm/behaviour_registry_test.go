package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// The behaviour registry, testdata/behaviour-registry, proves each promise JSM makes, with
// purpose-made families rather than real-world schemas. promises.yml lists the promises by ID,
// and each ID is an anchor in user-docs/scenarios.md. Each family holds an expect.yml, beside its
// version directories, naming its one promise and the verdict the promise wants from each check:
//
//	promise: no-loosening    # the promise this family proves
//	gap: 119                 # optional: the issue that closes a known gap, see below
//	validate:                # the verdict of `jsm validate <key>`, for every version
//	  1.0.0: pass
//	  1.1.0: fail
//	validateStrict:          # optional: the same, with --strict, which fails on a warning
//	  1.1.0: fail
//	checkChanges:            # optional: the verdict of `jsm check-changes` for each overlay
//	  deployed: [1.0.0]      # the versions committed and tagged as deployed
//	  overlays:
//	    edit-schema:         # overlays/edit-schema holds files to add or replace
//	      verdict: fail
//	      delete: [1/0/0/pass/a.json]
//
// Paths in an overlay are relative to the family. expect.yml and overlays never reach the copy of
// the family that jsm runs on.
//
// A gap is a promise JSM doesn't keep yet. The verdicts still say what the promise wants, so the
// family's run must differ from them: once it stops differing the gap is stale, and the test fails
// until the entry is removed.

const (
	behaviourRegistryDir = "testdata/behaviour-registry"
	behaviourConfigFile  = "json-schema-manager-config.yml"
	promisesFile         = "promises.yml"
	expectFile           = "expect.yml"
	overlaysDir          = "overlays"
	scenariosPage        = "../../user-docs/scenarios.md"
	schemaFileSuffix     = ".schema.json"
	deploymentTag        = "jsm-deploy/production/v1"

	verdictPass = "pass"
	verdictFail = "fail"
)

type overlayExpectation struct {
	Verdict string   `yaml:"verdict"`
	Delete  []string `yaml:"delete"`
}

type checkChangesExpectation struct {
	Deployed []string                      `yaml:"deployed"`
	Overlays map[string]overlayExpectation `yaml:"overlays"`
}

type familyExpectation struct {
	Promise        string                   `yaml:"promise"`
	Gap            int                      `yaml:"gap"`
	Validate       map[string]string        `yaml:"validate"`
	ValidateStrict map[string]string        `yaml:"validateStrict"`
	CheckChanges   *checkChangesExpectation `yaml:"checkChanges"`
}

// behaviourFamily is one family of the behaviour registry. Its zero expectation stands for a family
// with no expect.yml.
type behaviourFamily struct {
	// path is the family's slash-separated path in the registry, such as loosen/anyof-arm.
	path   string
	dir    string
	expect familyExpectation
}

// verdictRun is one run of the binary, with the verdict the family expects and the one it got.
type verdictRun struct {
	name string
	want string
	got  string
}

func loadPromises(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(behaviourRegistryDir, promisesFile))
	require.NoError(t, err)

	var list struct {
		Promises []string `yaml:"promises"`
	}
	require.NoError(t, yaml.Unmarshal(data, &list))
	require.NotEmpty(t, list.Promises)
	return list.Promises
}

// loadFamilies finds every family in the registry, whether it holds an expect.yml or only versions.
func loadFamilies(t *testing.T) []behaviourFamily {
	t.Helper()

	seen := map[string]bool{}
	err := filepath.WalkDir(behaviourRegistryDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		switch {
		case d.IsDir() && d.Name() == overlaysDir:
			return filepath.SkipDir
		case d.Name() == expectFile:
			seen[filepath.Dir(p)] = true
		case strings.HasSuffix(d.Name(), schemaFileSuffix):
			// <family>/<major>/<minor>/<patch>/<file>
			seen[filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(p))))] = true
		}
		return nil
	})
	require.NoError(t, err)

	families := make([]behaviourFamily, 0, len(seen))
	for dir := range seen {
		families = append(families, loadFamily(t, dir))
	}
	slices.SortFunc(families, func(a, b behaviourFamily) int { return strings.Compare(a.path, b.path) })
	require.NotEmpty(t, families)
	return families
}

func loadFamily(t *testing.T, dir string) behaviourFamily {
	t.Helper()
	rel, err := filepath.Rel(behaviourRegistryDir, dir)
	require.NoError(t, err)
	family := behaviourFamily{path: filepath.ToSlash(rel), dir: dir}

	data, err := os.ReadFile(filepath.Join(dir, expectFile))
	if errors.Is(err, fs.ErrNotExist) {
		return family
	}
	require.NoError(t, err)

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	require.NoError(t, decoder.Decode(&family.expect), "%s", dir)
	return family
}

func findFamily(t *testing.T, name string) behaviourFamily {
	t.Helper()
	for _, family := range loadFamilies(t) {
		if family.path == name {
			return family
		}
	}
	require.Failf(t, "no such family", "%s", name)
	return behaviourFamily{}
}

// versions returns the family's version directories, as version strings such as 1.2.3.
func (f behaviourFamily) versions(t *testing.T) []string {
	t.Helper()
	var versions []string
	err := filepath.WalkDir(f.dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() && d.Name() == overlaysDir {
			return filepath.SkipDir
		}
		if strings.HasSuffix(d.Name(), schemaFileSuffix) {
			rel, relErr := filepath.Rel(f.dir, filepath.Dir(p))
			if relErr != nil {
				return relErr
			}
			versions = append(versions, strings.ReplaceAll(filepath.ToSlash(rel), "/", "."))
		}
		return nil
	})
	require.NoError(t, err)
	slices.Sort(versions)
	return versions
}

// key returns the schema key of the family's given version.
func (f behaviourFamily) key(version string) string {
	return strings.ReplaceAll(f.path, "/", "_") + "_" + strings.ReplaceAll(version, ".", "_")
}

// expectationProblems says what's wrong with the family's expect.yml itself: a verdict that isn't
// one, a version with no verdict, or a verdict for a version that isn't there.
func expectationProblems(t *testing.T, f behaviourFamily) []string {
	t.Helper()
	versions := f.versions(t)

	var problems []string
	for _, version := range versions {
		if _, ok := f.expect.Validate[version]; !ok {
			problems = append(problems, "version "+version+" has no validate verdict")
		}
	}
	for check, verdicts := range map[string]map[string]string{
		"validate":       f.expect.Validate,
		"validateStrict": f.expect.ValidateStrict,
	} {
		for version, verdict := range verdicts {
			if !slices.Contains(versions, version) {
				problems = append(problems, check+" names a version that isn't there: "+version)
			}
			if verdict != verdictPass && verdict != verdictFail {
				problems = append(problems, check+" "+version+" has no valid verdict: "+verdict)
			}
		}
	}

	if f.expect.CheckChanges != nil {
		problems = append(problems, checkChangesProblems(t, f, versions)...)
	}
	return problems
}

// checkChangesProblems says what's wrong with the family's check-changes expectations. An overlay
// must change something, and each overlay directory must be named, so a misspelt name can't quietly
// turn an overlay into no change at all.
func checkChangesProblems(t *testing.T, f behaviourFamily, versions []string) []string {
	t.Helper()
	cc := f.expect.CheckChanges

	var problems []string
	if len(cc.Deployed) == 0 || len(cc.Overlays) == 0 {
		problems = append(problems, "checkChanges needs deployed versions and at least one overlay")
	}
	for _, version := range cc.Deployed {
		if !slices.Contains(versions, version) {
			problems = append(problems, "deployed names a version that isn't there: "+version)
		}
	}

	entries, err := os.ReadDir(filepath.Join(f.dir, overlaysDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		require.NoError(t, err)
	}
	dirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		dirs = append(dirs, entry.Name())
		if _, ok := cc.Overlays[entry.Name()]; !ok {
			problems = append(problems, "no overlay is named for the directory "+entry.Name())
		}
	}

	for name, overlay := range cc.Overlays {
		if overlay.Verdict != verdictPass && overlay.Verdict != verdictFail {
			problems = append(problems, "overlay "+name+" has no valid verdict: "+overlay.Verdict)
		}
		if len(overlay.Delete) == 0 && !slices.Contains(dirs, name) {
			problems = append(problems, "overlay "+name+" changes nothing")
		}
		for _, p := range overlay.Delete {
			if !filepath.IsLocal(p) {
				problems = append(problems, "overlay "+name+" deletes outside the family: "+p)
			}
		}
	}
	return problems
}

// isolatedEnv is the environment for every run. TestMain has already hidden the machine, but
// TestBehaviourRegistryIgnoresAmbientRegistry sets JSM_ variables to prove they can't reach a run.
func isolatedEnv(t *testing.T, registryRoot string) []string {
	t.Helper()
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "JSM_") {
			env = append(env, kv)
		}
	}
	return append(env,
		"JSM_REGISTRY_ROOT_DIR="+registryRoot,
		"JSM_LOG_FILE="+filepath.Join(t.TempDir(), "jsm.log"),
	)
}

// verdictOf runs the binary on the registry at root. It returns the check's verdict, from the exit
// code alone, or says why the check could not run, which is never a verdict.
func verdictOf(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), binaryPath, args...)
	cmd.Dir = root
	cmd.Env = isolatedEnv(t, root)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return verdictPass
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 1:
		return verdictFail
	default:
		return fmt.Sprintf("could not run (%v): %s", err, stderr.String())
	}
}

// tempRegistry creates an empty registry in a temp directory, and returns its root.
func tempRegistry(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	copyFile(t, filepath.Join(behaviourRegistryDir, behaviourConfigFile), filepath.Join(root, behaviourConfigFile))
	return root
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(to), 0o750))
	require.NoError(t, os.WriteFile(to, data, 0o600))
}

// copyTree copies every file under from to the same place under to, skipping those keep refuses,
// which it gives each file's slash-separated path relative to from.
func copyTree(t *testing.T, from, to string, keep func(rel string) bool) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(from, p)
		if relErr != nil {
			return relErr
		}
		if keep(filepath.ToSlash(rel)) {
			copyFile(t, p, filepath.Join(to, rel))
		}
		return nil
	})
	require.NoError(t, err)
}

// isFamilyContent reports whether a file of the family reaches jsm. The expectations and overlays are
// the test's own, and belong to no registry.
func isFamilyContent(rel string) bool {
	return rel != expectFile && !strings.HasPrefix(rel, overlaysDir+"/")
}

// copyFamily copies the family's versions to its place in a registry at root.
func copyFamily(t *testing.T, f behaviourFamily, root string, keep func(rel string) bool) {
	t.Helper()
	copyTree(t, f.dir, filepath.Join(root, filepath.FromSlash(f.path)), func(rel string) bool {
		return isFamilyContent(rel) && keep(rel)
	})
}

// runValidate runs validate on a copy of the family, for each version with a verdict, with and
// without --strict.
func runValidate(t *testing.T, f behaviourFamily) []verdictRun {
	t.Helper()
	root := tempRegistry(t)
	copyFamily(t, f, root, func(string) bool { return true })

	runs := make([]verdictRun, 0, len(f.expect.Validate)+len(f.expect.ValidateStrict))
	for _, version := range sortedKeys(f.expect.Validate) {
		runs = append(runs, verdictRun{
			name: "validate " + version,
			want: f.expect.Validate[version],
			got:  verdictOf(t, root, "validate", f.key(version)),
		})
	}
	for _, version := range sortedKeys(f.expect.ValidateStrict) {
		runs = append(runs, verdictRun{
			name: "validate --strict " + version,
			want: f.expect.ValidateStrict[version],
			got:  verdictOf(t, root, "validate", f.key(version), "--strict"),
		})
	}
	return runs
}

// runGit runs git in a registry the test has made, which is never this repository.
func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = root
	cmd.Env = isolatedEnv(t, root)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, output)
}

// deployWithOverlay builds a git repository holding a copy of the family: its deployed versions are
// committed and tagged as deployed, then its other versions and the named overlay are committed on
// top. It returns the repository's root, which is also the registry's.
func deployWithOverlay(t *testing.T, f behaviourFamily, overlayName string) string {
	t.Helper()
	cc := f.expect.CheckChanges
	overlay := cc.Overlays[overlayName]
	root := tempRegistry(t)

	deployedDirs := make([]string, 0, len(cc.Deployed))
	for _, version := range cc.Deployed {
		deployedDirs = append(deployedDirs, strings.ReplaceAll(version, ".", "/")+"/")
	}
	copyFamily(t, f, root, func(rel string) bool {
		return slices.ContainsFunc(deployedDirs, func(dir string) bool { return strings.HasPrefix(rel, dir) })
	})
	runGit(t, root, "init", "-q")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "deployed")
	runGit(t, root, "tag", deploymentTag)

	familyRoot := filepath.Join(root, filepath.FromSlash(f.path))
	copyFamily(t, f, root, func(string) bool { return true })
	overlayDir := filepath.Join(f.dir, overlaysDir, overlayName)
	if _, err := os.Stat(overlayDir); err == nil {
		copyTree(t, overlayDir, familyRoot, func(string) bool { return true })
	}
	for _, p := range overlay.Delete {
		require.NoError(t, os.RemoveAll(filepath.Join(familyRoot, filepath.FromSlash(p))))
	}
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "changes")
	return root
}

// runCheckChanges runs check-changes once for each of the family's overlays.
func runCheckChanges(t *testing.T, f behaviourFamily) []verdictRun {
	t.Helper()
	if f.expect.CheckChanges == nil {
		return nil
	}
	var runs []verdictRun
	for _, name := range sortedKeys(f.expect.CheckChanges.Overlays) {
		root := deployWithOverlay(t, f, name)
		runs = append(runs, verdictRun{
			name: "check-changes with overlay " + name,
			want: f.expect.CheckChanges.Overlays[name].Verdict,
			got:  verdictOf(t, root, "check-changes", "production"),
		})
	}
	return runs
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// verdictProblems compares a family's runs with its expectations. A gap expects at least one run to
// differ, since it records what the promise wants rather than what happens today. A run that could
// not run is a problem whatever the gap says, so a gap can't hide a broken family.
func verdictProblems(f behaviourFamily, runs []verdictRun) []string {
	var problems, differences []string
	for _, run := range runs {
		switch {
		case run.got != verdictPass && run.got != verdictFail:
			problems = append(problems, run.name+": "+run.got)
		case run.got != run.want:
			differences = append(differences, fmt.Sprintf("%s: expected %s, got %s", run.name, run.want, run.got))
		}
	}
	if f.expect.Gap == 0 {
		return append(problems, differences...)
	}
	if len(differences) == 0 && len(problems) == 0 {
		problems = append(problems, fmt.Sprintf(
			"stale gap: the family behaves as promised, so remove gap: %d from its %s", f.expect.Gap, expectFile))
	}
	return problems
}

func promisesWithoutFamily(promises []string, families []behaviourFamily) []string {
	var missing []string
	for _, promise := range promises {
		if !slices.ContainsFunc(families, func(f behaviourFamily) bool { return f.expect.Promise == promise }) {
			missing = append(missing, promise)
		}
	}
	return missing
}

func familiesWithoutPromise(promises []string, families []behaviourFamily) []string {
	var missing []string
	for _, family := range families {
		if !slices.Contains(promises, family.expect.Promise) {
			missing = append(missing, family.path)
		}
	}
	return missing
}

func promisesWithoutAnchor(promises []string, page string) []string {
	var missing []string
	for _, promise := range promises {
		if !strings.Contains(page, `<a id="`+promise+`"></a>`) {
			missing = append(missing, promise)
		}
	}
	return missing
}

func TestBehaviourRegistry(t *testing.T) {
	t.Parallel()
	require.NoError(t, ensureBinary())

	for _, family := range loadFamilies(t) {
		t.Run(family.path, func(t *testing.T) {
			t.Parallel()
			require.Empty(t, expectationProblems(t, family))

			runs := append(runValidate(t, family), runCheckChanges(t, family)...)
			if family.expect.Gap != 0 {
				t.Logf("known gap, closed by #%d: %+v", family.expect.Gap, runs)
			}
			assert.Empty(t, verdictProblems(family, runs))
		})
	}
}

func TestEveryPromiseHasAFamily(t *testing.T) {
	t.Parallel()

	t.Run("in the registry", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, promisesWithoutFamily(loadPromises(t), loadFamilies(t)))
	})

	t.Run("a promise with no family is reported", func(t *testing.T) {
		t.Parallel()
		families := []behaviourFamily{{path: "a/b", expect: familyExpectation{Promise: "kept"}}}
		assert.Equal(t, []string{"lonely"}, promisesWithoutFamily([]string{"kept", "lonely"}, families))
	})
}

func TestEveryFamilyHasAPromise(t *testing.T) {
	t.Parallel()

	t.Run("in the registry", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, familiesWithoutPromise(loadPromises(t), loadFamilies(t)))
	})

	t.Run("a family naming no promise, or an unlisted one, is reported", func(t *testing.T) {
		t.Parallel()
		families := []behaviourFamily{
			{path: "a/named", expect: familyExpectation{Promise: "kept"}},
			{path: "a/unnamed"},
			{path: "a/unlisted", expect: familyExpectation{Promise: "other"}},
		}
		assert.Equal(t, []string{"a/unnamed", "a/unlisted"}, familiesWithoutPromise([]string{"kept"}, families))
	})
}

func TestEveryPromiseAppearsInScenariosPage(t *testing.T) {
	t.Parallel()

	t.Run("on the page", func(t *testing.T) {
		t.Parallel()
		page, err := os.ReadFile(scenariosPage)
		require.NoError(t, err)
		assert.Empty(t, promisesWithoutAnchor(loadPromises(t), string(page)))
	})

	t.Run("an ID with no anchor is reported", func(t *testing.T) {
		t.Parallel()
		page := `<a id="kept"></a>The promise. The word lonely alone is no anchor.`
		assert.Equal(t, []string{"lonely"}, promisesWithoutAnchor([]string{"kept", "lonely"}, page))
	})
}

func TestStaleGapFails(t *testing.T) {
	t.Parallel()
	require.NoError(t, ensureBinary())

	t.Run("a gap on a family already behaving as promised is reported", func(t *testing.T) {
		t.Parallel()
		family := findFamily(t, "own/documents-behave")
		require.Zero(t, family.expect.Gap)
		family.expect.Gap = 119

		problems := verdictProblems(family, runValidate(t, family))
		require.Len(t, problems, 1)
		assert.Contains(t, problems[0], "stale gap")
	})

	t.Run("a standing gap is not reported, and without its entry it fails", func(t *testing.T) {
		t.Parallel()
		family := behaviourFamily{expect: familyExpectation{Gap: 119}}
		runs := []verdictRun{{name: "differs", want: verdictFail, got: verdictPass}}
		assert.Empty(t, verdictProblems(family, runs))

		family.expect.Gap = 0
		assert.NotEmpty(t, verdictProblems(family, runs))
	})

	t.Run("a gap doesn't hide a run that could not run", func(t *testing.T) {
		t.Parallel()
		family := behaviourFamily{expect: familyExpectation{Gap: 119}}
		runs := []verdictRun{
			{name: "differs", want: verdictFail, got: verdictPass},
			{name: "broken", want: verdictFail, got: "could not run"},
		}
		assert.Equal(t, []string{"broken: could not run"}, verdictProblems(family, runs))
	})
}

func TestBehaviourRegistryCheckChanges(t *testing.T) {
	t.Parallel()
	require.NoError(t, ensureBinary())

	// Parallel subtests only start when this function returns, so this runs after they all finish.
	t.Cleanup(func() {
		err := filepath.WalkDir(behaviourRegistryDir, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			assert.NotEqual(t, ".git", d.Name(), "a nested git directory landed in the repo: %s", p)
			return nil
		})
		assert.NoError(t, err)
	})

	checked := 0
	for _, family := range loadFamilies(t) {
		if family.expect.CheckChanges == nil {
			continue
		}
		checked++
		for _, name := range sortedKeys(family.expect.CheckChanges.Overlays) {
			t.Run(family.path+"/"+name, func(t *testing.T) {
				t.Parallel()
				root := deployWithOverlay(t, family, name)
				assert.DirExists(t, filepath.Join(root, ".git"))

				if family.expect.Gap == 0 {
					want := family.expect.CheckChanges.Overlays[name].Verdict
					assert.Equal(t, want, verdictOf(t, root, "check-changes", "production"))
				}
			})
		}
	}
	require.Positive(t, checked, "no family has check-changes verdicts")
}

// snapshotDir records each entry under dir, with its content, mode and modification time.
func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		content := ""
		if !d.IsDir() {
			data, readErr := os.ReadFile(p)
			if readErr != nil {
				return readErr
			}
			content = string(data)
		}
		snapshot[p] = fmt.Sprintf("%v %v %s", info.Mode(), info.ModTime(), content)
		return nil
	})
	require.NoError(t, err)
	return snapshot
}

// TestBehaviourRegistryIgnoresAmbientRegistry isn't parallel, as t.Setenv sets the environment it's about.
func TestBehaviourRegistryIgnoresAmbientRegistry(t *testing.T) {
	require.NoError(t, ensureBinary())

	ambient := tempRegistry(t)
	copyFamily(t, findFamily(t, "own/pass-document-rejected"), ambient, func(string) bool { return true })
	t.Setenv("JSM_REGISTRY_ROOT_DIR", ambient)
	t.Setenv("JSM_LOG_FILE", filepath.Join(ambient, "ambient.log"))
	before := snapshotDir(t, ambient)

	for _, name := range []string{"own/documents-behave", "deployed/fixed"} {
		family := findFamily(t, name)
		runs := append(runValidate(t, family), runCheckChanges(t, family)...)
		require.NotEmpty(t, runs)
		assert.Empty(t, verdictProblems(family, runs), name)
	}

	assert.Equal(t, before, snapshotDir(t, ambient), "the ambient registry changed")
}
