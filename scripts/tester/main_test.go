package main

import (
	"context"
	"errors"
	"go/version"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLogEnv, when set, turns this test binary into a fake command: it records how it was called and exits.
const fakeLogEnv = "TESTER_TEST_FAKE_LOG"

func TestMain(m *testing.M) {
	if logPath := os.Getenv(fakeLogEnv); logPath != "" {
		os.Exit(recordFakeCall(logPath))
	}
	os.Exit(m.Run())
}

func recordFakeCall(logPath string) int {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	line := strings.Join(append([]string{name}, os.Args[1:]...), "\x00") + "\n"
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 1
	}
	_, writeErr := f.WriteString(line)
	if err = errors.Join(writeErr, f.Close()); err != nil {
		return 1
	}
	return 0
}

// A regression in Go 1.26 makes a test binary built with -race on darwin/arm64 crash or hang in the child it forks,
// before exec, so a test that starts git fails for no reason in the code. See golang/go#79804. Go 1.26.5 has the fix.
func TestGoVersionHasTheDarwinRaceForkFix(t *testing.T) {
	t.Parallel()

	fixed, known := hasDarwinRaceForkFix(runtime.Version())
	if !known {
		t.Skipf("cannot tell whether %s has the fix for golang/go#79804, which came in %s",
			runtime.Version(), firstRaceForkFix)
	}
	assert.True(t, fixed,
		"tests are built with %s, which is below %s, the first release with the fix for golang/go#79804",
		runtime.Version(), firstRaceForkFix)
}

func TestHasDarwinRaceForkFix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		goVersion string
		fixed     bool
		known     bool
	}{
		{goVersion: "go1.26.0", fixed: false, known: true},
		{goVersion: "go1.26.4", fixed: false, known: true},
		{goVersion: "go1.26.5", fixed: true, known: true},
		{goVersion: "go1.27.3", fixed: true, known: true},
		{goVersion: "go1.27.3 X:nocoverageredesign", fixed: true, known: true},
		{goVersion: "devel go1.28-abc123 Thu Nov 5 10:00:00 2026 +0000", known: false},
		{goVersion: "devel go1.27-abc123 Thu Nov 5 10:00:00 2026 +0000", known: false},
		{goVersion: "devel go1.26-abc123 Thu Nov 5 10:00:00 2026 +0000", known: false},
		{goVersion: "devel +abc123 Thu Nov 5 10:00:00 2026 +0000", known: false},
		{goVersion: "", known: false},
	}
	for _, tt := range tests {
		t.Run(tt.goVersion, func(t *testing.T) {
			t.Parallel()

			fixed, known := hasDarwinRaceForkFix(tt.goVersion)

			assert.Equal(t, tt.known, known, "known")
			assert.Equal(t, tt.fixed, fixed, "fixed")
		})
	}
}

// firstRaceForkFix is the first Go release with the fix for golang/go#79804.
const firstRaceForkFix = "go1.26.5"

// hasDarwinRaceForkFix says whether goVersion, as runtime.Version gives it, has the fix for golang/go#79804, and
// whether that can be told at all. A release is judged by its version, ignoring any suffix such as " X:...". A
// development build ("devel ...") can't be told: its language version doesn't say whether it was built before or after
// the fix, so it is never known.
func hasDarwinRaceForkFix(goVersion string) (fixed, known bool) {
	if strings.HasPrefix(goVersion, "devel") {
		return false, false
	}
	v, _, _ := strings.Cut(goVersion, " ")
	if !version.IsValid(v) {
		return false, false
	}
	return version.Compare(v, firstRaceForkFix) >= 0, true
}

// A route is one of the two ways the tester starts the tests.
type route struct {
	name    string
	flags   []string // what selects the route
	command string   // the fake it must call
	prefix  []string // the arguments that come before the test arguments
}

func routes() []route {
	return []route{
		{name: "coverage", flags: []string{"--test-race-coverage"}, command: "go", prefix: []string{"test"}},
		{name: "plain", command: "gotestsum", prefix: []string{"--"}},
	}
}

func TestTesterSetsATimeLimit(t *testing.T) {
	t.Parallel()

	for _, r := range routes() {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()

			args := runTester(t, r, "-count=1", "./...")

			assert.Equal(t, []string{"-timeout=45s", "-count=1", "./..."}, args[:3])
		})
	}
}

func TestTesterKeepsACallersTimeLimit(t *testing.T) {
	t.Parallel()

	callers := map[string][]string{
		"equals form":      {"-timeout=2m", "./..."},
		"separate value":   {"-timeout", "2m", "./..."},
		"after a package":  {"./...", "-timeout=2m"},
		"double dash form": {"--timeout=2m", "./..."},
	}
	for _, r := range routes() {
		for name, caller := range callers {
			t.Run(r.name+"/"+name, func(t *testing.T) {
				t.Parallel()

				args := runTester(t, r, caller...)

				assert.Equal(t, "-timeout=45s", args[0], "the default goes first, so a later one wins")
				assert.Equal(t, caller, args[1:len(caller)+1], "the caller's arguments come after it, as given")
			})
		}
	}
}

func TestTesterLeavesArgumentsForTheTestsAlone(t *testing.T) {
	t.Parallel()

	for _, r := range routes() {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()

			args := runTester(t, r, "./...", "-args", "-timeout=1s")

			assert.Equal(t, []string{"-timeout=45s", "./...", "-args", "-timeout=1s"}, args[:4])
		})
	}
}

// runTester runs the built tester with fake go and gotestsum alone on its path, and returns the test arguments the
// route's command was given.
func runTester(t *testing.T, r route, args ...string) []string {
	t.Helper()

	dir := t.TempDir()
	tester := filepath.Join(dir, executable("tester"))
	build := exec.CommandContext(context.Background(), "go", "build", "-o", tester, ".")
	out, err := build.CombinedOutput()
	require.NoError(t, err, "building the tester: %s", out)

	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.Mkdir(binDir, 0o700))
	self, err := os.Executable()
	require.NoError(t, err)
	selfContent, err := os.ReadFile(self)
	require.NoError(t, err)
	for _, name := range []string{"go", "gotestsum"} {
		require.NoError(t, os.WriteFile(filepath.Join(binDir, executable(name)), selfContent, 0o700))
	}

	logPath := filepath.Join(dir, "calls.log")
	cmd := exec.CommandContext(context.Background(), tester, append(slices.Clone(r.flags), args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+binDir, fakeLogEnv+"="+logPath)
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, "running the tester: %s", out)

	logged, err := os.ReadFile(logPath)
	require.NoError(t, err)
	want := append([]string{r.command}, r.prefix...)
	for line := range strings.Lines(string(logged)) {
		call := strings.Split(strings.TrimSuffix(line, "\n"), "\x00")
		if len(call) >= len(want) && slices.Equal(call[:len(want)], want) {
			return call[len(want):]
		}
	}
	require.Failf(t, "the tester did not start the tests", "want a call of %v, got:\n%q", want, logged)
	return nil
}

func executable(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
