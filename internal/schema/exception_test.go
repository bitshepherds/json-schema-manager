package schema

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const earlierFailDocException = `exceptions:
  - version: 1.0.0
    document: fail/f.json
    rule: no-loosening
    reason: Deliberate.
`

// writeExceptions writes the exception file of the later version.
func writeExceptions(t *testing.T, r *Registry, content string) {
	t.Helper()
	path := filepath.Join(New(ruleLater, r).Path(HomeDir), ExceptionsFile)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestExceptionsApplyToOneDocument(t *testing.T) {
	t.Parallel()
	earlierFailDoc := func(r *Registry) string {
		return filepath.Join(New(ruleEarlier, r).Path(HomeDir), string(TestDocTypeFail), "f.json")
	}

	t.Run("the run it names is excepted", func(t *testing.T) {
		t.Parallel()
		r := ruleRegistry(t, "scenario: provider-first\n")
		behaves(t, r, ruleEarlier)
		writeExceptions(t, r, earlierFailDocException)

		report, err := NewTester(r).TestSpecificDocument(context.Background(), ruleEarlier, earlierFailDoc(r))
		require.NoError(t, err)
		assert.Empty(t, report.FailedTests)
		assert.Len(t, report.ExceptedTests[ruleLater], 1)
		assert.False(t, report.HasFailures())
	})

	t.Run("a run it names which doesn't fail is stale", func(t *testing.T) {
		t.Parallel()
		r := ruleRegistry(t, "scenario: provider-first\n")
		behaves(t, r, ruleEarlier, ruleLater)
		writeExceptions(t, r, earlierFailDocException)

		report, err := NewTester(r).TestSpecificDocument(context.Background(), ruleEarlier, earlierFailDoc(r))
		require.NoError(t, err)
		assert.Len(t, report.StaleExceptions[ruleLater], 1)
		assert.True(t, report.HasFailures())
	})

	t.Run("a file which can't be used stops the run", func(t *testing.T) {
		t.Parallel()
		r := ruleRegistry(t, "scenario: provider-first\n")
		writeExceptions(t, r, "exceptions: [")

		_, err := NewTester(r).TestSpecificDocument(context.Background(), ruleEarlier, earlierFailDoc(r))
		require.ErrorAs(t, err, new(*InvalidExceptionsError))
	})
}

func TestInvalidExceptionsErrorUnwraps(t *testing.T) {
	t.Parallel()
	wrapped := errors.New("bad")
	assert.ErrorIs(t, &InvalidExceptionsError{Path: "p", Wrapped: wrapped}, wrapped)
}

func TestWatchMapsExceptionFileToItsVersion(t *testing.T) {
	t.Parallel()
	r := ruleRegistry(t, "scenario: provider-first\n")
	w := NewWatcher(r, slog.New(slog.NewTextHandler(io.Discard, nil)))

	home := New(ruleLater, r).Path(HomeDir)
	assert.Equal(t, &WatchEvent{Key: ruleLater}, w.mapToWatchEvent(filepath.Join(home, ExceptionsFile)))
	assert.Nil(t, w.mapToWatchEvent(filepath.Join(filepath.Dir(home), ExceptionsFile)), "not a version's directory")
}

func TestParseExceptionsRefuses(t *testing.T) {
	t.Parallel()
	entry := func(version, document, rule, reason string) string {
		return "  - version: " + version + "\n    document: " + document + "\n    rule: " + rule +
			"\n    reason: " + reason + "\n"
	}

	refused := map[string]struct{ file, want string }{
		"no reason": {
			"exceptions:\n  - version: 1.0.0\n    document: fail/a.json\n    rule: no-loosening\n",
			"reason",
		},
		"an empty reason": {"exceptions:\n" + entry("1.0.0", "fail/a.json", "no-loosening", `"  "`), "reason"},
		"no version": {
			"exceptions:\n  - document: fail/a.json\n    rule: no-loosening\n    reason: x\n",
			"version",
		},
		"not a version": {"exceptions:\n" + entry("one", "fail/a.json", "no-loosening", "x"), "version"},
		"a version part which isn't a number": {
			"exceptions:\n" + entry("1.0.x", "fail/a.json", "no-loosening", "x"),
			"version",
		},
		"no document": {"exceptions:\n  - version: 1.0.0\n    rule: no-loosening\n    reason: x\n", "document"},
		"a document outside pass and fail": {
			"exceptions:\n" + entry("1.0.0", "../fail/a.json", "no-loosening", "x"),
			"document",
		},
		"a nested document": {"exceptions:\n" + entry("1.0.0", "fail/more/a.json", "no-loosening", "x"), "document"},
		"a document that isn't JSON": {
			"exceptions:\n" + entry("1.0.0", "fail/a.txt", "no-loosening", "x"),
			"document",
		},
		"a rule which can't be excepted": {
			"exceptions:\n" + entry("1.0.0", "fail/a.json", "own-documents", "x"),
			"rule",
		},
		"no rule": {"exceptions:\n  - version: 1.0.0\n    document: fail/a.json\n    reason: x\n", "rule"},
		"an unknown key": {
			"exceptions:\n  - version: 1.0.0\n    document: fail/a.json\n" +
				"    rule: no-loosening\n    reason: x\n    until: never\n",
			"until",
		},
		"the same entry twice": {
			"exceptions:\n" + entry("1.0.0", "fail/a.json", "no-loosening", "x") +
				entry("1.0.0", "fail/a.json", "no-loosening", "y"),
			"twice",
		},
	}
	for name, tt := range refused {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseExceptions([]byte(tt.file))
			require.ErrorContains(t, err, tt.want)
		})
	}
}
