package schema

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/v2/internal/validator"
)

func TestTestLog_AddTest(t *testing.T) {
	t.Parallel()
	l := NewTestLog()
	k := Key("key")
	s1 := &Spec{}
	s2 := &Spec{}
	l.AddTest(k, s1)
	l.AddTest(k, s2)
	assert.Len(t, l[k], 2)
}

func TestTestReport_AddTests(t *testing.T) {
	t.Parallel()
	r := NewTestReport()
	k := Key("key")
	s1 := &Spec{}
	s2 := &Spec{}
	r.AddPassedTest(k, s1)
	r.AddFailedTest(k, s2)
	assert.Len(t, r.PassedTests[k], 1)
	assert.Len(t, r.FailedTests[k], 1)
}

func TestTester_Configuration(t *testing.T) {
	t.Parallel()
	reg := &Registry{}
	tr := NewTester(reg)

	tr.SetStopOnFirstError(false)
	assert.False(t, tr.stopOnFirstError)

	tr.SetNumWorkers(10)
	assert.Equal(t, 10, tr.numWorkers)
}

func TestTester_TestSingleSchema(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		k := Key("domain_family_1_0_0")
		createSchemaFiles(t, r, schemaMap{
			k: `{"type": "object"}`,
		})

		s, _ := r.GetSchemaByKey(k)
		homeDir := s.Path(HomeDir)
		passDir := filepath.Join(homeDir, string(TestDocTypePass))
		failDir := filepath.Join(homeDir, string(TestDocTypeFail))
		require.NoError(t, os.MkdirAll(passDir, 0o755))
		require.NoError(t, os.MkdirAll(failDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(passDir, "valid.json"), []byte("{}"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(failDir, "invalid.json"), []byte("[]"), 0o600))

		tr := NewTester(r)
		tr.SetStopOnFirstError(false)

		mc, ok := r.compiler.(*mockCompiler)
		require.True(t, ok)
		mc.CompileFunc = func(_ string) (validator.Validator, error) {
			return &mockValidator{
				Err: nil, // Always pass validation
			}, nil
		}

		report, err := tr.TestSingleSchema(context.Background(), k)
		require.NoError(t, err)
		assert.NotNil(t, report)
		// 1 pass test passed, 1 fail test FAILED (unexpectedly passed validation)
		assert.Len(t, report.PassedTests[k], 1)
		assert.Len(t, report.FailedTests[k], 1)
	})

	t.Run("TestSpecificDocument - success", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		k := Key("domain_family_1_0_0")
		createSchemaFiles(t, r, schemaMap{
			k: `{"type": "object"}`,
		})

		s, _ := r.GetSchemaByKey(k)
		passDir := filepath.Join(s.Path(HomeDir), string(TestDocTypePass))
		require.NoError(t, os.MkdirAll(passDir, 0o755))
		testPath := filepath.Join(passDir, "valid.json")
		require.NoError(t, os.WriteFile(testPath, []byte("{}"), 0o600))

		tr := NewTester(r)
		mc, ok := r.compiler.(*mockCompiler)
		require.True(t, ok)
		mc.CompileFunc = func(_ string) (validator.Validator, error) {
			return &mockValidator{}, nil
		}

		report, err := tr.TestSpecificDocument(context.Background(), k, testPath)
		require.NoError(t, err)
		assert.Len(t, report.PassedTests[k], 1)
	})

	t.Run("TestSpecificDocument - invalid directory", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		k := Key("domain_family_1_0_0")
		createSchemaFiles(t, r, schemaMap{
			k: `{"type": "object"}`,
		})

		s, _ := r.GetSchemaByKey(k)
		invalidDir := filepath.Join(s.Path(HomeDir), "invalid-dir")
		require.NoError(t, os.MkdirAll(invalidDir, 0o755))
		testPath := filepath.Join(invalidDir, "test.json")
		require.NoError(t, os.WriteFile(testPath, []byte("{}"), 0o600))

		tr := NewTester(r)
		_, err := tr.TestSpecificDocument(context.Background(), k, testPath)
		require.Error(t, err)
		assert.ErrorAs(t, err, new(*InvalidTestDocumentDirectoryError))
	})

	t.Run("TestSpecificDocument - schema not found", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		tr := NewTester(r)
		_, err := tr.TestSpecificDocument(context.Background(), Key("missing_family_1_0_0"), "any.json")
		require.Error(t, err)
	})

	t.Run("TestSpecificDocument - render error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		k := Key("domain_family_1_0_0")
		createSchemaFiles(t, r, schemaMap{
			k: `{"type": "object"}`,
		})

		tr := NewTester(r)
		mc, ok := r.compiler.(*mockCompiler)
		require.True(t, ok)
		mc.CompileFunc = func(_ string) (validator.Validator, error) {
			return nil, errors.New("render failure")
		}

		_, err := tr.TestSpecificDocument(context.Background(), k, "any.json")
		require.Error(t, err)
		assert.ErrorContains(t, err, "render failure")
	})

	t.Run("TestSpecificDocument - cancelled context", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		tr := NewTester(r)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := tr.TestSpecificDocument(ctx, Key("any_1_0_0"), "any.json")
		assert.ErrorIs(t, err, context.Canceled)
	})

	testCases := []struct {
		name       string
		docType    string
		errorMsg   string
		checkField string
	}{
		{"validation failure", "pass", "validation failed", "FailedTests"},
		{"fail doc success", "fail", "expected fail", "PassedTests"},
	}

	for _, tc := range testCases {
		t.Run("TestSpecificDocument - "+tc.name, func(t *testing.T) {
			t.Parallel()
			r := setupTestRegistry(t)
			k := Key("domain_family_1_0_0")
			createSchemaFiles(t, r, schemaMap{
				k: `{"type": "object"}`,
			})
			docDir := filepath.Join(r.RootDirectory(), "domain", "family", "1", "0", "0", tc.docType)
			require.NoError(t, os.MkdirAll(docDir, 0o755))
			testPath := filepath.Join(docDir, "test.json")
			require.NoError(t, os.WriteFile(testPath, []byte(`[]`), 0o600))

			tr := NewTester(r)
			mc, ok := r.compiler.(*mockCompiler)
			require.True(t, ok)
			mc.CompileFunc = func(_ string) (validator.Validator, error) {
				return &mockValidator{Err: errors.New(tc.errorMsg)}, nil
			}

			report, err := tr.TestSpecificDocument(context.Background(), k, testPath)
			require.NoError(t, err)
			if tc.checkField == "FailedTests" {
				assert.Len(t, report.FailedTests, 1)
			} else {
				assert.Len(t, report.PassedTests, 1)
			}
		})
	}

	t.Run("TestSpecificDocument - NewTestInfo error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		tr := NewTester(r)
		k := Key("domain_family_1_0_0")
		createSchemaFiles(t, r, schemaMap{k: `{}`})

		// This will fail because the file doesn't exist
		_, err := tr.TestSpecificDocument(context.Background(), k, "non-existent.json")
		assert.Error(t, err)
	})

	t.Run("TestSpecificDocument - Invalid directory", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		k := Key("domain_family_1_0_0")
		createSchemaFiles(t, r, schemaMap{k: `{}`})

		// Create a file in a directory that is NOT pass or fail
		s, _ := r.GetSchemaByKey(k)
		otherDir := filepath.Join(s.Path(HomeDir), "other")
		require.NoError(t, os.MkdirAll(otherDir, 0o755))
		testPath := filepath.Join(otherDir, "test.json")
		require.NoError(t, os.WriteFile(testPath, []byte("{}"), 0o600))

		tr := NewTester(r)
		_, err := tr.TestSpecificDocument(context.Background(), k, testPath)
		require.Error(t, err)
		assert.ErrorAs(t, err, new(*InvalidTestDocumentDirectoryError))
	})
}

func TestTester_TestFoundSchemas(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)

	k1 := Key("domain1_family_1_0_0")
	k2 := Key("domain2_family_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		k1: `{"type": "object"}`,
		k2: `{"type": "object"}`,
	})

	tr := NewTester(r)
	tr.SetStopOnFirstError(false)

	// Ensure pass/fail dirs exist for both schemas
	for _, k := range []Key{k1, k2} {
		s, _ := r.GetSchemaByKey(k)
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), string(TestDocTypePass)), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), string(TestDocTypeFail)), 0o755))
	}

	report, err := tr.TestFoundSchemas(context.Background(), "domain1")
	require.NoError(t, err)
	assert.NotNil(t, report)
}

func TestTester_testVersion_Errs(t *testing.T) {
	t.Parallel()

	t.Run("schema not found", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		tr := NewTester(r)
		err := tr.testVersion(context.Background(), Key("missing_family_1_0_0"))
		require.Error(t, err)
	})

	t.Run("render error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		k := Key("domain_family_1_0_0")
		createSchemaFiles(t, r, schemaMap{
			k: `{"type": "object"}`,
		})
		tr := NewTester(r)

		mc, ok := r.compiler.(*mockCompiler)
		require.True(t, ok)
		mc.CompileFunc = func(_ string) (validator.Validator, error) {
			return nil, errors.New("render failure")
		}
		err := tr.testVersion(context.Background(), k)
		require.Error(t, err)
		assert.ErrorContains(t, err, "render failure")
	})

	t.Run("getSpecs error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		k := Key("domain_family_1_0_0")
		createSchemaFiles(t, r, schemaMap{
			k: `{"type": "object"}`,
		})
		tr := NewTester(r)

		// Trigger an error in getSpecsForSchema with a pass document which is not JSON
		createTestDocs(t, r, k, TestDocTypePass, map[string]string{"bad.json": `{`})
		err := tr.testVersion(context.Background(), k)
		require.Error(t, err)
	})
}

func TestTester_TestFoundSchemas_ErrInTraversal(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	tr := NewTester(r)

	// Invalid search scope triggering error in NewSearcher
	_, err := tr.TestFoundSchemas(context.Background(), "!!!")
	require.Error(t, err)
}

func TestTester_StopOnFirstErr(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	k := Key("domain_family_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		k: `{"type": "object"}`,
	})

	s, _ := r.GetSchemaByKey(k)
	passDir := filepath.Join(s.Path(HomeDir), string(TestDocTypePass))
	failDir := filepath.Join(s.Path(HomeDir), string(TestDocTypeFail))
	require.NoError(t, os.MkdirAll(passDir, 0o755))
	require.NoError(t, os.MkdirAll(failDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(passDir, "test1.json"), []byte("{}"), 0o600))

	tr := NewTester(r)
	tr.SetStopOnFirstError(true)

	// mockValidator that fails validation
	mc, ok := r.compiler.(*mockCompiler)
	require.True(t, ok)
	mc.CompileFunc = func(_ string) (validator.Validator, error) {
		return &mockValidator{
			Err: errors.New("validation failed"),
		}, nil
	}

	report, err := tr.TestSingleSchema(context.Background(), k)
	require.NoError(t, err)
	assert.Len(t, report.FailedTests[k], 1)
}

func TestTester_ContextCancellation_FoundSchemas(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	k := Key("domain_family_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		k: `{"type": "object"}`,
	})

	tr := NewTester(r)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := tr.TestFoundSchemas(ctx, "")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestTester_GetSpecsErrs(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	k := Key("domain_family_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		k: `{"type": "object"}`,
	})
	s, _ := r.GetSchemaByKey(k)
	passDir := filepath.Join(s.Path(HomeDir), string(TestDocTypePass))
	failDir := filepath.Join(s.Path(HomeDir), string(TestDocTypeFail))
	require.NoError(t, os.MkdirAll(passDir, 0o755))
	require.NoError(t, os.MkdirAll(failDir, 0o755))
	// Create an invalid JSON test document
	require.NoError(t, os.WriteFile(filepath.Join(passDir, "invalid.json"), []byte("{"), 0o600))

	tr := NewTester(r)
	_, err := tr.TestSingleSchema(context.Background(), k)
	require.Error(t, err)
	assert.ErrorAs(t, err, new(InvalidTestDocumentError))
}

func TestTester_TestFoundSchemas_StopOnErr(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	k1 := Key("domain_family1_1_0_0")
	k2 := Key("domain_family2_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		k1: `{"type": "object"}`,
		k2: `{"type": "object"}`,
	})

	// Make k1 have a pass test that fails
	s1, _ := r.GetSchemaByKey(k1)
	passDir1 := filepath.Join(s1.Path(HomeDir), string(TestDocTypePass))
	failDir1 := filepath.Join(s1.Path(HomeDir), string(TestDocTypeFail))
	require.NoError(t, os.MkdirAll(passDir1, 0o755))
	require.NoError(t, os.MkdirAll(failDir1, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(passDir1, "test.json"), []byte("{}"), 0o600))

	// Also for k2
	s2, _ := r.GetSchemaByKey(k2)
	require.NoError(t, os.MkdirAll(filepath.Join(s2.Path(HomeDir), string(TestDocTypePass)), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(s2.Path(HomeDir), string(TestDocTypeFail)), 0o755))

	tr := NewTester(r)
	tr.SetStopOnFirstError(true)
	tr.SetNumWorkers(1) // Force sequential for predictable stop

	mc, ok := r.compiler.(*mockCompiler)
	require.True(t, ok)
	mc.CompileFunc = func(_ string) (validator.Validator, error) {
		return &mockValidator{
			Err: errors.New("fail"),
		}, nil
	}

	report, err := tr.TestFoundSchemas(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, report.FailedTests, 1) // Should stop after first failure
}

func TestTester_TestFoundSchemas_SearchErr(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	// Create a file with invalid structure to trigger search error in searcher.go
	regDir := r.rootDirectory
	require.NoError(t, os.MkdirAll(filepath.Join(regDir, "domain"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "domain", "invalid-name.schema.json"), []byte("{}"), 0o600))

	tr := NewTester(r)
	_, err := tr.TestFoundSchemas(context.Background(), "domain")
	require.Error(t, err)
	assert.ErrorAs(t, err, new(*InvalidSchemaFilenameError))
}

func TestTester_testVersion_ContextCancelled(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	k := Key("domain_family_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		k: `{"type": "object"}`,
	})
	s, _ := r.GetSchemaByKey(k)
	passDir := filepath.Join(s.Path(HomeDir), string(TestDocTypePass))
	failDir := filepath.Join(s.Path(HomeDir), string(TestDocTypeFail))
	require.NoError(t, os.MkdirAll(passDir, 0o755))
	require.NoError(t, os.MkdirAll(failDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(passDir, "p1.json"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(passDir, "p2.json"), []byte("{}"), 0o600))

	tr := NewTester(r)
	ctx, cancel := context.WithCancel(context.Background())

	// mockValidator that cancels context when called
	mc, ok := r.compiler.(*mockCompiler)
	require.True(t, ok)
	mc.CompileFunc = func(_ string) (validator.Validator, error) {
		return &mockValidator{
			ValidateFunc: func(_ validator.JSONDocument) error {
				cancel()
				return nil
			},
		}, nil
	}

	err := tr.testVersion(ctx, k)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestTester_TestLog_Initialisation(t *testing.T) {
	t.Parallel()
	l := NewTestLog()
	assert.NotNil(t, l)
}

func TestTester_TestFoundSchemas_NoStopOnErr(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	k1 := Key("d1_f1_1_0_0")
	k2 := Key("d1_f2_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		k1: `{"type": "object"}`,
		k2: `{"type": "object"}`,
	})

	// Make both have tests
	for _, k := range []Key{k1, k2} {
		s, _ := r.GetSchemaByKey(k)
		p := filepath.Join(s.Path(HomeDir), string(TestDocTypePass))
		require.NoError(t, os.MkdirAll(p, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(p, "t.json"), []byte("{}"), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), string(TestDocTypeFail)), 0o755))
	}

	tr := NewTester(r)
	tr.SetStopOnFirstError(false)

	// Mock failure for all
	mc, ok := r.compiler.(*mockCompiler)
	require.True(t, ok)
	mc.CompileFunc = func(_ string) (validator.Validator, error) {
		return &mockValidator{Err: errors.New("boom")}, nil
	}

	report, err := tr.TestFoundSchemas(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, report.FailedTests, 2)
}

func TestTester_TestFoundSchemas_ProducerBreakOnCancel(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)

	// Create two schemas
	k1 := Key("d_f1_1_0_0")
	k2 := Key("d_f2_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		k1: `{"type": "object"}`,
		k2: `{"type": "object"}`,
	})

	for _, k := range []Key{k1, k2} {
		s, _ := r.GetSchemaByKey(k)
		p := filepath.Join(s.Path(HomeDir), string(TestDocTypePass))
		require.NoError(t, os.MkdirAll(p, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(p, "t.json"), []byte("{}"), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), string(TestDocTypeFail)), 0o755))
	}

	tr := NewTester(r)
	tr.SetStopOnFirstError(true)
	tr.SetNumWorkers(1)

	proceed := make(chan struct{})

	mc, ok := r.compiler.(*mockCompiler)
	require.True(t, ok)
	mc.CompileFunc = func(_ string) (validator.Validator, error) {
		return &mockValidator{
			ValidateFunc: func(_ validator.JSONDocument) error {
				<-proceed
				return errors.New("fail")
			},
		}, nil
	}

	// Run TestFoundSchemas in a goroutine
	type result struct {
		report *TestReport
		err    error
	}
	resC := make(chan result, 1)
	go func() {
		report, err := tr.TestFoundSchemas(context.Background(), "")
		resC <- result{report, err}
	}()

	time.Sleep(100 * time.Millisecond)
	close(proceed)

	res := <-resC
	require.NoError(t, res.err)
	assert.NotEmpty(t, res.report.FailedTests)
}

func TestTester_TestFoundSchemas_WorkerErr(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	k := Key("d_f_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		k: `{"type": "object"}`,
	})

	tr := NewTester(r)

	// Force a render error
	mc, ok := r.compiler.(*mockCompiler)
	require.True(t, ok)
	mc.CompileFunc = func(_ string) (validator.Validator, error) {
		return nil, errors.New("render failure")
	}

	_, err := tr.TestFoundSchemas(context.Background(), "")
	require.Error(t, err)
	assert.ErrorContains(t, err, "render failure")
}

func TestTester_TestFoundSchemas_ContextCancelledDuringExecution(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	k := Key("d_f_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		k: `{"type": "object"}`,
	})

	tr := NewTester(r)
	tr.SetNumWorkers(1)

	ctx, cancel := context.WithCancel(context.Background())

	mc, ok := r.compiler.(*mockCompiler)
	require.True(t, ok)
	mc.CompileFunc = func(_ string) (validator.Validator, error) {
		return &mockValidator{
			ValidateFunc: func(_ validator.JSONDocument) error {
				cancel()
				return nil
			},
		}, nil
	}

	s, _ := r.GetSchemaByKey(k)
	p := filepath.Join(s.Path(HomeDir), string(TestDocTypePass))
	require.NoError(t, os.MkdirAll(p, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(p, "t.json"), []byte("{}"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), string(TestDocTypeFail)), 0o755))

	_, err := tr.TestFoundSchemas(ctx, "")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestErrs_Strings(t *testing.T) {
	t.Parallel()
	assert.Contains(t, (&NoSchemaTargetsError{}).Error(), "must specify a schema")
}

// TestTestFoundSchemas_StopOnFirstError_OmitsUnrunSchemas checks that a run which stops at the first
// failure reports only the schemas whose runs finished, and the failing one, and counts the others as
// not tested (#155). A schema a worker had started on as the run stopped once showed as passed.
func TestTestFoundSchemas_StopOnFirstError_OmitsUnrunSchemas(t *testing.T) {
	t.Parallel()

	// setup creates a schema for each key, each with two pass documents, and d_b_1_0_0 with a pass
	// document which fails. Validating a document of a schema in wait waits for that failure.
	setup := func(t *testing.T, keys, wait []Key) *Registry {
		t.Helper()
		r := setupTestRegistry(t)
		schemas := schemaMap{}
		for _, k := range keys {
			schemas[k] = `{"type": "object"}`
		}
		createSchemaFiles(t, r, schemas)
		for _, k := range keys {
			createTestDocs(t, r, k, TestDocTypePass, map[string]string{
				"one.json": `{"from": "` + string(k) + `"}`,
				"two.json": `{"from": "` + string(k) + `"}`,
			})
		}
		createTestDocs(t, r, "d_b_1_0_0", TestDocTypePass, map[string]string{"bad.json": `{"bad": true}`})

		failed := make(chan struct{})
		var once sync.Once
		mc, ok := r.compiler.(*mockCompiler)
		require.True(t, ok)
		mc.CompileFunc = func(_ string) (validator.Validator, error) {
			return &mockValidator{ValidateFunc: func(doc validator.JSONDocument) error {
				m, _ := doc.(map[string]any)
				if m["bad"] == true {
					once.Do(func() { close(failed) })
					return errors.New("bad document")
				}
				if from, _ := m["from"].(string); slices.Contains(wait, Key(from)) {
					<-failed
					time.Sleep(100 * time.Millisecond) // for the run to stop
				}
				return nil
			}}, nil
		}
		return r
	}

	// assertOnly checks the report holds the schemas given, each with runs, and no other.
	assertOnly := func(t *testing.T, report *TestReport, want ...Key) {
		t.Helper()
		reported := reportedKeys(report)
		assert.ElementsMatch(t, want, reported)
		for _, k := range reported {
			ran := len(report.PassedTests[k]) + len(report.FailedTests[k]) + len(report.ExceptedTests[k])
			assert.Positive(t, ran, "%s is reported with no runs", k)
		}
		assert.Len(t, report.FailedTests["d_b_1_0_0"], 1)
	}

	t.Run("schemas cut short", func(t *testing.T) {
		t.Parallel()
		cut := []Key{"d_c_1_0_0", "d_d_1_0_0"}
		r := setup(t, append([]Key{"d_b_1_0_0"}, cut...), cut)
		tr := NewTester(r)
		tr.SetStopOnFirstError(true)
		tr.SetNumWorkers(3)

		report, err := tr.TestFoundSchemas(context.Background(), "")
		require.NoError(t, err)
		assertOnly(t, report, "d_b_1_0_0")
		assert.Equal(t, 2, report.NotTested)
		assert.True(t, report.StoppedEarly())
	})

	t.Run("schemas never started", func(t *testing.T) {
		t.Parallel()
		r := setup(t, []Key{"d_a_1_0_0", "d_b_1_0_0", "d_c_1_0_0", "d_d_1_0_0", "d_e_1_0_0"}, nil)
		tr := NewTester(r)
		tr.SetStopOnFirstError(true)
		tr.SetNumWorkers(1)

		report, err := tr.TestFoundSchemas(context.Background(), "")
		require.NoError(t, err)
		assertOnly(t, report, "d_a_1_0_0", "d_b_1_0_0")
		assert.Equal(t, 3, report.NotTested)
	})
}

// TestTestFoundSchemas_ContinueOnError_TestsAll checks that a run which goes on past failures counts no
// schema as not tested.
func TestTestFoundSchemas_ContinueOnError_TestsAll(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	keys := []Key{"d_a_1_0_0", "d_b_1_0_0"}
	createSchemaFiles(t, r, schemaMap{keys[0]: `{"type": "object"}`, keys[1]: `{"type": "object"}`})
	for _, k := range keys {
		createTestDocs(t, r, k, TestDocTypePass, map[string]string{"ok.json": `{}`})
	}
	mc, ok := r.compiler.(*mockCompiler)
	require.True(t, ok)
	mc.CompileFunc = func(_ string) (validator.Validator, error) {
		return &mockValidator{Err: errors.New("fail")}, nil
	}

	tr := NewTester(r)
	tr.SetStopOnFirstError(false)
	report, err := tr.TestFoundSchemas(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, report.FailedTests, 2)
	assert.Zero(t, report.NotTested)
	assert.False(t, report.StoppedEarly())
}

// reportedKeys returns every schema the report holds anything for.
func reportedKeys(r *TestReport) []Key {
	var keys []Key
	add := func(k Key) {
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	for k := range r.PassedTests {
		add(k)
	}
	for k := range r.FailedTests {
		add(k)
	}
	for k := range r.ExceptedTests {
		add(k)
	}
	for k := range r.Summaries {
		add(k)
	}
	for k := range r.StaleExceptions {
		add(k)
	}
	for k := range r.MissingMigrations {
		add(k)
	}
	for k := range r.UnexercisedChanges {
		add(k)
	}
	for k := range r.AcknowledgedChanges {
		add(k)
	}
	return keys
}

// TestTestFoundSchemas_StopOnFirstError_SearchErrWhileCounting checks that a run which stops at the
// first failure returns the error the search finds, past where the run stopped, as it counts the
// schemas it didn't test.
func TestTestFoundSchemas_StopOnFirstError_SearchErrWhileCounting(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	createSchemaFiles(t, r, schemaMap{"d_a_1_0_0": `{"type": "object"}`, "d_b_1_0_0": `{"type": "object"}`})
	createTestDocs(t, r, "d_a_1_0_0", TestDocTypePass, map[string]string{"bad.json": `{}`})
	badDir := filepath.Join(r.rootDirectory, "d", "zz")
	require.NoError(t, os.MkdirAll(badDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(badDir, "invalid-name.schema.json"), []byte("{}"), 0o600))

	mc, ok := r.compiler.(*mockCompiler)
	require.True(t, ok)
	mc.CompileFunc = func(_ string) (validator.Validator, error) {
		return &mockValidator{Err: errors.New("fail")}, nil
	}

	tr := NewTester(r)
	tr.SetStopOnFirstError(true)
	tr.SetNumWorkers(1)
	_, err := tr.TestFoundSchemas(context.Background(), "")
	assert.ErrorAs(t, err, new(*InvalidSchemaFilenameError))
}

func TestTestReport_DropUnless(t *testing.T) {
	t.Parallel()
	r := NewTestReport()
	for _, k := range []Key{"d_keep_1_0_0", "d_drop_1_0_0"} {
		r.AddPassedTest(k, &Spec{})
		r.AddFailedTest(k, &Spec{})
		r.AddExceptedTest(k, &Spec{})
		r.AddStaleException(k, &Exception{}, "stale")
		r.AddMissingMigration(k, MissingMigration{Major: 2})
		r.AddUnexercisedChange(k, &UnexercisedChange{})
		r.AddAcknowledgedChange(k, &AcknowledgedChange{})
		r.AddSummary(k, &Summary{})
	}

	r.dropUnless(func(k Key) bool { return k == "d_keep_1_0_0" })
	assert.Equal(t, []Key{"d_keep_1_0_0"}, reportedKeys(r))
}
