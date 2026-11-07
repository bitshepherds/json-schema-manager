package schema

import (
	"context"
	"errors"
	"os"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/v2/internal/validator"
)

// stoppedRun arranges a run which stops at the first failure, a pass document of d_b_1_0_0 which fails,
// and decides which schemas it cuts short and which finish before it, so the case it sets up happens
// every time.
type stoppedRun struct {
	// cut are the schemas whose checks the failure cuts short: checking a document on one waits for it.
	cut []Key
	// finish are the schemas whose checks finish before the failure: it waits for each to start
	// checking its documents, then for it to finish.
	finish []Key
	// after holds back rendering a schema until another has started checking documents, by when it
	// has taken on the pairs the two share.
	after map[Key]Key
}

// signal is a channel closed once, however many times it is closed.
type signal struct {
	once sync.Once
	c    chan struct{}
}

func newSignal() *signal { return &signal{c: make(chan struct{})} }

func (s *signal) close() { s.once.Do(func() { close(s.c) }) }

// wait waits for the signal, giving up after a while so a run set up wrong fails rather than hangs.
func (s *signal) wait() {
	select {
	case <-s.c:
	case <-time.After(5 * time.Second):
	}
}

// setup gives d_b_1_0_0 its failing document, each schema cut short a second document, so it has one
// left to run when the run stops, and each schema the validator the run asks for. Every schema in the
// run must be in cut or finish, or how far it gets is left to chance.
func (sr stoppedRun) setup(t *testing.T, r *Registry) {
	t.Helper()
	for _, k := range sr.cut {
		createTestDocs(t, r, k, TestDocTypePass, map[string]string{"two.json": `{}`})
	}
	createSchemaFiles(t, r, schemaMap{"d_b_1_0_0": `{"type": "object"}`})
	createTestDocs(t, r, "d_b_1_0_0", TestDocTypePass, map[string]string{"bad.json": `{"bad": true}`})

	started := map[Key]*signal{}
	for _, k := range append(append([]Key{"d_b_1_0_0"}, sr.cut...), sr.finish...) {
		started[k] = newSignal()
	}
	failed := newSignal()

	mc, ok := r.compiler.(*mockCompiler)
	require.True(t, ok)
	mc.CompileFunc = func(id string) (validator.Validator, error) {
		if strings.Contains(id, "#") {
			return &mockValidator{}, nil // a subschema, which coverage and diff ask about
		}
		k := Key(strings.TrimSuffix(path.Base(id), SchemaSuffix))
		if first, ok := sr.after[k]; ok {
			started[first].wait()
		}
		return &mockValidator{ValidateFunc: func(doc validator.JSONDocument) error {
			if m, _ := doc.(map[string]any); m["bad"] == true {
				for _, f := range sr.finish {
					started[f].wait()
				}
				time.Sleep(100 * time.Millisecond) // for them to finish
				failed.close()
				return errors.New("bad document")
			}
			started[k].close()
			for _, c := range sr.cut {
				if c == k {
					failed.wait()
					time.Sleep(100 * time.Millisecond) // for the run to stop
				}
			}
			return nil
		}}, nil
	}
}

// run runs every schema at once, stopping at the first failure.
func (sr stoppedRun) run(t *testing.T, r *Registry) *TestReport {
	t.Helper()
	tr := NewTester(r)
	tr.SetStopOnFirstError(true)
	tr.SetNumWorkers(len(sr.cut) + len(sr.finish) + 1)
	report, err := tr.TestFoundSchemas(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, report.FailedTests["d_b_1_0_0"], 1)
	return report
}

// declare gives the family of k the declaration.
func declare(t *testing.T, r *Registry, k Key, declaration string) {
	t.Helper()
	require.NoError(t, os.WriteFile(r.FamilyDeclarationPath(k), []byte(declaration), 0o600))
}

// TestTestFoundSchemas_StopOnFirstError_DropsVersionWhosePairRunsWereCutShort checks that a stopped run
// reports a version as tested only when every run counted under it finished. The runs of a pair count
// under its later version, but the first version to reach the pair runs them, after its own documents.
// Here 1.0.0 takes on the pair and is cut short, so the runs of 1.0.1's documents on 1.0.0 never
// happen, though 1.0.1's own checks finish. It once showed as passed.
func TestTestFoundSchemas_StopOnFirstError_DropsVersionWhosePairRunsWereCutShort(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	createSchemaFiles(t, r, schemaMap{"d_a_1_0_0": `{"type": "object"}`, "d_a_1_0_1": `{"type": "object"}`})
	for _, k := range []Key{"d_a_1_0_0", "d_a_1_0_1"} {
		createTestDocs(t, r, k, TestDocTypePass, map[string]string{"one.json": `{}`})
	}
	sr := stoppedRun{
		cut:    []Key{"d_a_1_0_0"},
		finish: []Key{"d_a_1_0_1"},
		after:  map[Key]Key{"d_a_1_0_1": "d_a_1_0_0"},
	}
	sr.setup(t, r)

	report := sr.run(t, r)
	assert.ElementsMatch(t, []Key{"d_b_1_0_0"}, reportedKeys(report))
	assert.Equal(t, 2, report.NotTested)
}

// TestTestFoundSchemas_StopOnFirstError_KeepsWhatFinishedChecksFound checks that a stopped run keeps
// what a version whose checks all finished found, though it goes under a version which is not tested,
// and that it drops what a version cut short found (#155: warnings found for the schemas that did run
// are still shown).
func TestTestFoundSchemas_StopOnFirstError_KeepsWhatFinishedChecksFound(t *testing.T) {
	t.Parallel()

	const (
		bounded = `{"properties": {"s": {"enum": ["a", "b"]}}}`
		widened = `{"properties": {"s": {"enum": ["a", "b", "c"]}}}`
	)

	// notTested checks the report holds no runs, and no summary, for k.
	notTested := func(t *testing.T, report *TestReport, k Key) {
		t.Helper()
		assert.Empty(t, report.PassedTests[k])
		assert.Empty(t, report.FailedTests[k])
		assert.Empty(t, report.ExceptedTests[k])
		assert.NotContains(t, report.Summaries, k)
		assert.False(t, report.Tested(k), "%s is not tested", k)
	}

	t.Run("a missing migration statement, under its major's first version", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		keys := []Key{"d_m_1_0_0", "d_m_2_0_0", "d_m_2_1_0"}
		for _, k := range keys {
			createSchemaFiles(t, r, schemaMap{k: `{"type": "object"}`})
			createTestDocs(t, r, k, TestDocTypePass, map[string]string{"one.json": `{}`})
		}
		declare(t, r, "d_m_1_0_0", "scenario: lock-step\ndocumentsLive: indefinitely\n")
		sr := stoppedRun{cut: []Key{"d_m_2_0_0"}, finish: []Key{"d_m_1_0_0", "d_m_2_1_0"}}
		sr.setup(t, r)

		report := sr.run(t, r)
		require.Contains(t, report.MissingMigrations, Key("d_m_2_0_0"), "2.1.0 found it, and finished")
		assert.Equal(t, uint64(2), report.MissingMigrations["d_m_2_0_0"].Major)
		notTested(t, report, "d_m_2_0_0")
		assert.Equal(t, 1, report.NotTested)
	})

	t.Run("an unexercised change, under the pair's later version", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		createSchemaFiles(t, r, schemaMap{"d_f_1_0_0": bounded, "d_f_1_1_0": widened})
		for _, k := range []Key{"d_f_1_0_0", "d_f_1_1_0"} {
			createTestDocs(t, r, k, TestDocTypePass, map[string]string{"one.json": `{}`})
		}
		sr := stoppedRun{cut: []Key{"d_f_1_1_0"}, finish: []Key{"d_f_1_0_0"}}
		sr.setup(t, r)

		report := sr.run(t, r)
		assert.NotEmpty(t, report.UnexercisedChanges["d_f_1_1_0"], "1.0.0 compared the pair, and finished")
		notTested(t, report, "d_f_1_1_0")
		assert.Equal(t, 1, report.NotTested)
	})

	t.Run("a stale exception, under the pair's later version", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		createSchemaFiles(t, r, schemaMap{"d_f_1_0_0": bounded, "d_f_1_1_0": bounded})
		for _, k := range []Key{"d_f_1_0_0", "d_f_1_1_0"} {
			createTestDocs(t, r, k, TestDocTypePass, map[string]string{"one.json": `{}`})
		}
		// 1.0.0 finds the first stale as it compares the pair. 1.1.0 finds the second itself, and
		// is cut short.
		writeExceptionFile(t, r, "d_f_1_1_0", `exceptions:
  - version: 1.0.0
    change: "/: maxLength nothing like this"
    reason: never there
  - version: 1.5.0
    document: pass/one.json
    rule: no-loosening
    reason: no such version
`)
		sr := stoppedRun{cut: []Key{"d_f_1_1_0"}, finish: []Key{"d_f_1_0_0"}}
		sr.setup(t, r)

		report := sr.run(t, r)
		require.Len(t, report.StaleExceptions["d_f_1_1_0"], 1)
		assert.Equal(t, "/: maxLength nothing like this", report.StaleExceptions["d_f_1_1_0"][0].Exception.Change)
		notTested(t, report, "d_f_1_1_0")
		assert.Equal(t, 1, report.NotTested)
	})
}
