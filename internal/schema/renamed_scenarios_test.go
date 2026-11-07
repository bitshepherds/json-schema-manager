package schema

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The names scenarios 1 and 2 had before they were named for what the owner does. Only the refusal, in
// family.go, and the tests that prove it may still name them.
func TestNoTrackedFileNamesARenamedScenario(t *testing.T) {
	t.Parallel()

	t.Run("no file tracked in git names provider-first, consumer-first, ScenarioProviderFirst or "+
		"ScenarioConsumerFirst, except the refusal and its own tests", func(t *testing.T) {
		t.Parallel()

		old := []string{
			"provider" + "-first", "consumer" + "-first",
			"Scenario" + "ProviderFirst", "Scenario" + "ConsumerFirst",
		}
		allowed := map[string]bool{
			"internal/schema/family.go":                 true, // the refusal
			"internal/schema/family_test.go":            true, // its tests
			"internal/schema/renamed_scenarios_test.go": true, // this test
			"internal/app/create_schema_test.go":        true, // its tests, through create-schema
		}

		// Only files git tracks, so another stream's working copy, or an ignored build output, can't trip it
		out, err := exec.CommandContext(t.Context(), "git", "ls-files", "-z", "--full-name", ":/").Output()
		require.NoError(t, err, "git ls-files")
		top, err := exec.CommandContext(t.Context(), "git", "rev-parse", "--show-toplevel").Output()
		require.NoError(t, err)
		root := strings.TrimSpace(string(top))

		files := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
		require.NotEmpty(t, files)
		for _, f := range files {
			if allowed[f] {
				continue
			}
			data, rErr := os.ReadFile(filepath.Join(root, f))
			if os.IsNotExist(rErr) {
				continue // deleted in the working copy, not yet committed
			}
			require.NoError(t, rErr, f)
			for _, name := range old {
				assert.False(t, bytes.Contains(data, []byte(name)), "%s names %s", f, name)
			}
		}
	})
}
