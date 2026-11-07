// Package testenv hides the machine a test runs on, so a test passes or fails alike on a developer's
// laptop and on a CI runner. Only tests import it.
package testenv

import (
	"os"
	"strings"
)

// machineEnv is the environment as Hide found it, less what steers git and jsm.
var machineEnv []string

// steersGitOrJSM says whether an environment variable changes what git or jsm do.
func steersGitOrJSM(name string) bool {
	return strings.HasPrefix(name, "GIT_") || strings.HasPrefix(name, "JSM_")
}

// Vars is what a test's git and jsm see in place of the machine. Git reads no configuration but the
// repository's own, so settings such as tag.gpgsign or init.defaultBranch can't reach it, and it has an
// identity of its own, since git refuses to commit or tag without one wherever it can't invent one.
// HOME names a directory that doesn't exist, so git's ignore and attributes files in it can't reach it
// either, and a test can't write there.
func Vars() map[string]string {
	return map[string]string{
		"HOME":                "/no-home",
		"USERPROFILE":         "/no-home",
		"LC_ALL":              "C",
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME":     "test",
		"GIT_AUTHOR_EMAIL":    "test@example.com",
		"GIT_COMMITTER_NAME":  "test",
		"GIT_COMMITTER_EMAIL": "test@example.com",
	}
}

// Hide takes the machine's state out of this process's environment, and so out of every process it
// starts. Each package's TestMain calls it before any test runs, as tests running in parallel can't
// use t.Setenv. A test that needs a JSM_ variable sets it for the one process it starts.
func Hide() {
	environ := os.Environ()
	machineEnv = make([]string, 0, len(environ))
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if steersGitOrJSM(name) {
			// Unsetenv and Setenv refuse only a name that's empty or holds an "=", which these can't be.
			_ = os.Unsetenv(name)
		} else {
			machineEnv = append(machineEnv, kv)
		}
	}
	// Git looks for its configuration, ignore and attributes files here before HOME.
	_ = os.Unsetenv("XDG_CONFIG_HOME")
	for name, value := range Vars() {
		_ = os.Setenv(name, value)
	}
}

// GoToolEnv is the environment to run the go tool in. The go tool is the one thing a test starts that
// needs the machine, as its build cache and modules are found from HOME.
func GoToolEnv() []string {
	return machineEnv
}
