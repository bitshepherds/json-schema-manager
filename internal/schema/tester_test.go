package schema

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/validator"
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

	tr.SetScope(TestScopePass)
	assert.Equal(t, TestScopePass, tr.scope)

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

func TestTester_testSchema_Errs(t *testing.T) {
	t.Parallel()

	t.Run("schema not found", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		tr := NewTester(r)
		_, err := tr.testSchema(context.Background(), Key("missing_family_1_0_0"))
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
		_, err := tr.testSchema(context.Background(), k)
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
		_, err := tr.testSchema(context.Background(), k)
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

func TestTester_ScopedTests(t *testing.T) {
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
	require.NoError(t, os.WriteFile(filepath.Join(passDir, "pass.json"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(failDir, "fail.json"), []byte("[]"), 0o600))

	// Create a future version in the same family with a pass test
	kFuture := Key("domain_family_1_1_0")
	createSchemaFiles(t, r, schemaMap{
		kFuture: `{"type": "object"}`,
	})
	sFuture, _ := r.GetSchemaByKey(kFuture)
	passDirFuture := filepath.Join(sFuture.Path(HomeDir), string(TestDocTypePass))
	failDirFuture := filepath.Join(sFuture.Path(HomeDir), string(TestDocTypeFail))
	require.NoError(t, os.MkdirAll(passDirFuture, 0o755))
	require.NoError(t, os.MkdirAll(failDirFuture, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(passDirFuture, "future_pass.json"), []byte("{}"), 0o600))

	t.Run("Pass only", func(t *testing.T) {
		t.Parallel()
		tr := NewTester(r)
		tr.SetScope(TestScopePass)
		specs, err := tr.getSpecsForSchema(s)
		require.NoError(t, err)
		assert.Len(t, specs, 1)
		assert.Equal(t, TestDocTypePass, specs[0].TestDocType)
	})

	t.Run("Fail only", func(t *testing.T) {
		t.Parallel()
		tr := NewTester(r)
		tr.SetScope(TestScopeFail)
		specs, err := tr.getSpecsForSchema(s)
		require.NoError(t, err)
		assert.Len(t, specs, 1)
		assert.Equal(t, TestDocTypeFail, specs[0].TestDocType)
	})

	t.Run("Breaking", func(t *testing.T) {
		t.Parallel()
		tr := NewTester(r)
		tr.SetScope(TestScopeConsumerBreaking)
		specs, err := tr.getSpecsForSchema(s)
		require.NoError(t, err)
		assert.Len(t, specs, 1)
		assert.Equal(t, TestDocTypePass, specs[0].TestDocType)
		assert.Equal(t, "future_pass.json", filepath.Base(specs[0].TestInfo.Path))
		// Check that ForwardVersion is set for breaking tests
		require.NotNil(t, specs[0].ForwardVersion)
		assert.Equal(t, sFuture.core.version, *specs[0].ForwardVersion)
	})

	t.Run("All", func(t *testing.T) {
		t.Parallel()
		tr := NewTester(r)
		tr.SetScope(TestScopeAll)
		specs, err := tr.getSpecsForSchema(s)
		require.NoError(t, err)
		// Should have: 1 local pass, 1 local fail, 1 breaking pass
		assert.Len(t, specs, 3)

		var breakingCount, localPassCount, localFailCount int
		for _, spec := range specs {
			switch {
			case spec.ForwardVersion != nil:
				breakingCount++
				assert.Equal(t, TestDocTypePass, spec.TestDocType)
			case spec.TestDocType == TestDocTypePass:
				localPassCount++
			case spec.TestDocType == TestDocTypeFail:
				localFailCount++
			}
		}
		assert.Equal(t, 1, breakingCount)
		assert.Equal(t, 1, localPassCount)
		assert.Equal(t, 1, localFailCount)
	})
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

func TestTester_testSchema_ContextCancelled(t *testing.T) {
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

	_, err := tr.testSchema(ctx, k)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestTester_getSpecsForSchema_ScopedErrs(t *testing.T) {
	t.Parallel()

	t.Run("Fail directory error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		k := Key("domain_family_1_0_0")
		createSchemaFiles(t, r, schemaMap{
			k: `{"type": "object"}`,
		})
		s, _ := r.GetSchemaByKey(k)
		tr := NewTester(r)
		tr.SetScope(TestScopeFail)
		createTestDocs(t, r, k, TestDocTypeFail, map[string]string{"bad.json": `{`})
		_, err := tr.getSpecsForSchema(s)
		require.Error(t, err)
	})

	t.Run("Breaking schema missing", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		k := Key("domain_family_1_0_0")
		createSchemaFiles(t, r, schemaMap{
			k: `{"type": "object"}`,
		})
		s, _ := r.GetSchemaByKey(k)
		tr := NewTester(r)
		tr.SetScope(TestScopeConsumerBreaking)

		// Create directory structure for future version but NO schema file
		famDir := s.Path(FamilyDir)
		futureDir := filepath.Join(famDir, "1", "1", "0")
		require.NoError(t, os.MkdirAll(futureDir, 0o755))

		_, err := tr.getSpecsForSchema(s)
		require.Error(t, err)
	})

	t.Run("Breaking specs error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		k := Key("domain_family_1_0_0")
		createSchemaFiles(t, r, schemaMap{
			k: `{"type": "object"}`,
		})
		s, _ := r.GetSchemaByKey(k)
		tr := NewTester(r)
		tr.SetScope(TestScopeConsumerBreaking)

		// Create a future version with a pass document which is not JSON
		kFuture := Key("domain_family_1_2_0")
		createSchemaFiles(t, r, schemaMap{
			kFuture: `{"type": "object"}`,
		})
		createTestDocs(t, r, kFuture, TestDocTypePass, map[string]string{"bad.json": `{`})

		_, err := tr.getSpecsForSchema(s)
		require.Error(t, err)
	})

	t.Run("Breaking family error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		k2 := Key("d2_f2_1_0_0")
		createSchemaFiles(t, r, schemaMap{
			k2: `{"type": "object"}`,
		})
		s2, _ := r.GetSchemaByKey(k2)

		// Make family directory a file to cause ReadDir error
		famDir := s2.Path(FamilyDir)
		require.NoError(t, os.RemoveAll(famDir))
		require.NoError(t, os.WriteFile(famDir, []byte(""), 0o600))

		tr := NewTester(r)
		tr.SetScope(TestScopeConsumerBreaking)
		_, err := tr.getSpecsForSchema(s2)
		require.Error(t, err)
	})

	t.Run("Breaking multiple future", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		kBase := Key("d2_f2_1_0_0")
		kNext := Key("d2_f2_1_1_0")
		kLast := Key("d2_f2_1_2_0")

		createSchemaFiles(t, r, schemaMap{
			kBase: `{"type": "object"}`,
			kNext: `{"type": "object"}`,
			kLast: `{"type": "object"}`,
		})

		// Setup pass tests for all
		for _, k := range []Key{kNext, kLast} {
			sn, _ := r.GetSchemaByKey(k)
			passDir := filepath.Join(sn.Path(HomeDir), string(TestDocTypePass))
			require.NoError(t, os.MkdirAll(passDir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(passDir, "pass.json"), []byte("{}"), 0o600))
		}

		sBase, _ := r.GetSchemaByKey(kBase)
		tr := NewTester(r)
		tr.SetScope(TestScopeConsumerBreaking)

		specs, err := tr.getSpecsForSchema(sBase)
		require.NoError(t, err)
		assert.Len(t, specs, 2)
	})
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

func TestNewTestScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    TestScope
		wantErr bool
	}{
		{"local", "local", TestScopeLocal, false},
		{"pass-only", "pass-only", TestScopePass, false},
		{"fail-only", "fail-only", TestScopeFail, false},
		{"consumer-breaking", "consumer-breaking", TestScopeConsumerBreaking, false},
		{"all", "all", TestScopeAll, false},
		{"invalid", "invalid", "", true},
		{"empty", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewTestScope(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorAs(t, err, new(*InvalidTestScopeError))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestErrs_Strings(t *testing.T) {
	t.Parallel()
	assert.Contains(t, (&NoSchemaTargetsError{}).Error(), "must specify a schema")
	assert.Contains(t, (&InvalidTestScopeError{Scope: "foo"}).Error(), "Invalid test scope: foo")
}

func TestTester_SetSkipCompatible(t *testing.T) {
	t.Parallel()
	tr := NewTester(&Registry{})
	assert.False(t, tr.skipCompatible)
	tr.SetSkipCompatible(true)
	assert.True(t, tr.skipCompatible)
}

func TestTester_ProviderCompatibility(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T) (*Registry, Key, Key) {
		t.Helper()
		r := setupTestRegistry(t)
		earlierKey := Key("domain_family_1_0_0")
		targetKey := Key("domain_family_1_0_1")
		createSchemaFiles(t, r, schemaMap{
			earlierKey: `{"type": "object", "required": ["name"]}`,
			targetKey:  `{"type": "object"}`,
		})
		targetSchema, _ := r.GetSchemaByKey(targetKey)
		require.NoError(t, os.MkdirAll(filepath.Join(targetSchema.Path(HomeDir), "pass"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(targetSchema.Path(HomeDir), "fail"), 0o755))
		require.NoError(
			t,
			os.WriteFile(filepath.Join(targetSchema.Path(HomeDir), "pass", "valid.json"), []byte(`{}`), 0o600),
		)

		earlierSchema, _ := r.GetSchemaByKey(earlierKey)
		require.NoError(t, os.MkdirAll(filepath.Join(earlierSchema.Path(HomeDir), "pass"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(earlierSchema.Path(HomeDir), "fail"), 0o755))

		return r, earlierKey, targetKey
	}

	t.Run("compatibility check detects breaking change", func(t *testing.T) {
		t.Parallel()
		r, earlierKey, targetKey := setup(t)
		tr := NewTester(r)
		tr.SetSkipCompatible(false)

		mc, ok := r.compiler.(*mockCompiler)
		require.True(t, ok)
		mc.CompileFunc = func(id string) (validator.Validator, error) {
			if strings.Contains(id, "1_0_0") {
				return &mockValidator{Err: errors.New("required property 'name' missing")}, nil
			}
			return &mockValidator{Err: nil}, nil
		}

		report, err := tr.TestSingleSchema(context.Background(), targetKey)
		require.NoError(t, err)
		assert.NotEmpty(t, report.FailedTests[earlierKey])
	})

	t.Run("skip compatibility check", func(t *testing.T) {
		t.Parallel()
		r, earlierKey, targetKey := setup(t)
		tr := NewTester(r)
		tr.SetSkipCompatible(true)

		mc, ok := r.compiler.(*mockCompiler)
		require.True(t, ok)
		mc.CompileFunc = func(id string) (validator.Validator, error) {
			if strings.Contains(id, "1_0_0") {
				return &mockValidator{Err: errors.New("fail")}, nil
			}
			return &mockValidator{Err: nil}, nil
		}

		report, err := tr.TestSingleSchema(context.Background(), targetKey)
		require.NoError(t, err)
		assert.Empty(t, report.FailedTests[earlierKey])
	})

	t.Run("context cancellation stops compatibility check", func(t *testing.T) {
		t.Parallel()
		r, _, targetKey := setup(t)
		tr := NewTester(r)
		tr.SetSkipCompatible(false)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := tr.TestSingleSchema(ctx, targetKey)
		assert.True(t, err == nil || errors.Is(err, context.Canceled))
	})

	t.Run("stop on first error in compatibility check", func(t *testing.T) {
		t.Parallel()
		r, earlierKey, targetKey := setup(t)
		tr := NewTester(r)
		tr.SetStopOnFirstError(true)
		tr.SetSkipCompatible(false)

		mc, ok := r.compiler.(*mockCompiler)
		require.True(t, ok)
		mc.CompileFunc = func(id string) (validator.Validator, error) {
			if strings.Contains(id, "1_0_0") {
				return &mockValidator{Err: errors.New("fail")}, nil
			}
			return &mockValidator{}, nil
		}

		report, err := tr.TestSingleSchema(context.Background(), targetKey)
		require.NoError(t, err)
		assert.NotEmpty(t, report.FailedTests[earlierKey])
	})
}

func TestTester_ProviderCompatibility_NoEarlierSchemas(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	targetKey := Key("domain_family_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		targetKey: `{"type": "object"}`,
	})
	s, _ := r.GetSchemaByKey(targetKey)
	require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), "pass"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), "fail"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(s.Path(HomeDir), "pass", "valid.json"), []byte(`{}`), 0o600))

	tr := NewTester(r)
	tr.SetSkipCompatible(false)
	report, err := tr.TestSingleSchema(context.Background(), targetKey)
	require.NoError(t, err)
	assert.Len(t, report.PassedTests[targetKey], 1)
}

func TestTester_ProviderCompatibility_NoPassTests(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	targetKey := Key("domain_family_1_0_1")
	createSchemaFiles(t, r, schemaMap{
		Key("domain_family_1_0_0"): `{"type": "object"}`,
		targetKey:                  `{"type": "object"}`,
	})
	s, _ := r.GetSchemaByKey(targetKey)
	require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), "pass"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), "fail"), 0o755))

	tr := NewTester(r)
	tr.SetSkipCompatible(false)
	report, err := tr.TestSingleSchema(context.Background(), targetKey)
	require.NoError(t, err)
	assert.Empty(t, report.FailedTests)
}

func TestTester_ProviderCompatibility_LocalTestsFail(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	targetKey := Key("domain_family_1_0_1")
	earlierKey := Key("domain_family_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		earlierKey: `{"type": "object"}`,
		targetKey:  `{"type": "object"}`,
	})
	s, _ := r.GetSchemaByKey(targetKey)
	require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), "pass"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), "fail"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(s.Path(HomeDir), "pass", "invalid.json"), []byte(`{}`), 0o600))

	mc, ok := r.compiler.(*mockCompiler)
	require.True(t, ok)
	mc.CompileFunc = func(_ string) (validator.Validator, error) {
		return &mockValidator{Err: errors.New("fail")}, nil
	}

	tr := NewTester(r)
	tr.SetSkipCompatible(false)
	report, err := tr.TestSingleSchema(context.Background(), targetKey)
	require.NoError(t, err)
	assert.NotEmpty(t, report.FailedTests[targetKey])
	assert.Empty(t, report.FailedTests[earlierKey])
}

func TestTester_ProviderCompatibility_RenderError(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	targetKey := Key("domain_family_1_0_1")
	earlierKey := Key("domain_family_1_0_0")
	createSchemaFiles(t, r, schemaMap{
		earlierKey: `{"type": "object"}`,
		targetKey:  `{"type": "object"}`,
	})
	ts, _ := r.GetSchemaByKey(targetKey)
	require.NoError(t, os.MkdirAll(filepath.Join(ts.Path(HomeDir), "pass"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(ts.Path(HomeDir), "fail"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ts.Path(HomeDir), "pass", "v.json"), []byte(`{}`), 0o600))

	mc, ok := r.compiler.(*mockCompiler)
	require.True(t, ok)
	mc.CompileFunc = func(id string) (validator.Validator, error) {
		if strings.Contains(id, "1_0_0") {
			return nil, errors.New("compile error")
		}
		return &mockValidator{}, nil
	}

	tr := NewTester(r)
	tr.SetSkipCompatible(false)
	_, err := tr.TestSingleSchema(context.Background(), targetKey)
	require.Error(t, err)
}

func TestTester_ProviderCompatibility_PassTestsPass(t *testing.T) {
	t.Parallel()
	r := setupTestRegistry(t)
	targetKey := Key("domain_family_1_0_1")
	createSchemaFiles(t, r, schemaMap{
		Key("domain_family_1_0_0"): `{"type": "object"}`,
		targetKey:                  `{"type": "object"}`,
	})
	ts, _ := r.GetSchemaByKey(targetKey)
	require.NoError(t, os.MkdirAll(filepath.Join(ts.Path(HomeDir), "pass"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(ts.Path(HomeDir), "fail"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ts.Path(HomeDir), "pass", "v.json"), []byte(`{}`), 0o600))

	tr := NewTester(r)
	tr.SetSkipCompatible(false)
	report, err := tr.TestSingleSchema(context.Background(), targetKey)
	require.NoError(t, err)
	assert.Empty(t, report.FailedTests)
}

func TestTester_testSchemaCompatibleWithEarlierVersions_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("GetSchemaByKey error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		tr := NewTester(r)
		err := tr.testSchemaCompatibleWithEarlierVersions(context.Background(), Key("missing_1_0_0"))
		require.Error(t, err)
	})

	t.Run("TestDocuments error - pass directory is file", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		k := Key("d_f_1_0_0")
		createSchemaFiles(t, r, schemaMap{k: `{}`})
		s, _ := r.GetSchemaByKey(k)
		require.NoError(t, os.WriteFile(filepath.Join(s.Path(HomeDir), "pass"), []byte(""), 0o600))

		tr := NewTester(r)
		err := tr.testSchemaCompatibleWithEarlierVersions(context.Background(), k)
		require.Error(t, err)
	})

	t.Run("MajorFamilyEarlierSchemas error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		k := Key("d_f_1_0_1")
		createSchemaFiles(t, r, schemaMap{k: `{}`})
		s, _ := r.GetSchemaByKey(k)
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), "pass"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), "fail"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(s.Path(HomeDir), "pass", "v.json"), []byte(`{}`), 0o600))

		// Make major dir a file
		majorDir := filepath.Join(s.Path(FamilyDir), "1")
		require.NoError(t, os.RemoveAll(majorDir))
		require.NoError(t, os.WriteFile(majorDir, []byte(""), 0o600))

		tr := NewTester(r)
		err := tr.testSchemaCompatibleWithEarlierVersions(context.Background(), k)
		require.Error(t, err)
	})

	t.Run("testEarlierSchemaWithTargetTests - GetSchemaByKey error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		tr := NewTester(r)
		targetKey := Key("t_1_0_0")
		createSchemaFiles(t, r, schemaMap{targetKey: `{}`})
		ts, _ := r.GetSchemaByKey(targetKey)
		passTests := []TestInfo{{Path: "p.json"}}

		err := tr.testEarlierSchemaWithTargetTests(context.Background(), Key("invalid_1_0_0"), ts, passTests)
		require.Error(t, err)
	})

	t.Run("testEarlierSchemaWithTargetTests - context cancelled", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		tr := NewTester(r)
		targetKey := Key("t_1_0_0")
		earlierKey := Key("e_1_0_0")
		createSchemaFiles(t, r, schemaMap{targetKey: `{}`, earlierKey: `{}`})
		_, _ = r.GetSchemaByKey(targetKey)
		es, _ := r.GetSchemaByKey(earlierKey)
		passTests := []TestInfo{{Path: "p.json"}}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := tr.testEarlierSchemaWithTargetTests(ctx, earlierKey, es, passTests)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("MajorFamilyEarlierSchemas error - mocked", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)

		k := Key("d_f_1_0_1")
		createSchemaFiles(t, r, schemaMap{k: `{}`})

		s, err := r.GetSchemaByKey(k)
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), "pass"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(s.Path(HomeDir), "pass", "t.json"), []byte(`{}`), 0o600))

		mockResolver := &mockPathResolver{
			getUintSubdirectories: func(_ string) ([]uint64, error) {
				return nil, errors.New("listing error")
			},
		}

		// Inject mock resolver into registry
		r.pathResolver = mockResolver

		tr := NewTester(r)
		err = tr.testSchemaCompatibleWithEarlierVersions(context.Background(), k)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "listing error")
	})

	t.Run("context cancelled during loop", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		k1 := Key("d_f_1_0_0")
		k1b := Key("d_f_1_0_1")
		k2 := Key("d_f_1_0_2")
		createSchemaFiles(t, r, schemaMap{k1: `{}`, k1b: `{}`, k2: `{}`})
		s, _ := r.GetSchemaByKey(k2)
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), "pass"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), "fail"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(s.Path(HomeDir), "pass", "v.json"), []byte(`{}`), 0o600))

		ctx, cancel := context.WithCancel(context.Background())
		tr := NewTester(r)
		tr.SetNumWorkers(1)

		mc, ok := r.compiler.(*mockCompiler)
		require.True(t, ok)
		mc.CompileFunc = func(id string) (validator.Validator, error) {
			if strings.Contains(id, "1_0_0") {
				cancel()
				time.Sleep(100 * time.Millisecond) // Give loop time to hit select
			}
			return &mockValidator{}, nil
		}

		err := tr.testSchemaCompatibleWithEarlierVersions(ctx, k2)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("render failure during loop", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		tr := NewTester(r)
		k1 := Key("d_f_1_0_0")
		k2 := Key("d_f_1_0_1")
		createSchemaFiles(t, r, schemaMap{k1: `{}`, k2: `{}`})
		s, _ := r.GetSchemaByKey(k2)
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), "pass"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), "fail"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(s.Path(HomeDir), "pass", "v.json"), []byte(`{}`), 0o600))

		mc, ok := r.compiler.(*mockCompiler)
		require.True(t, ok)
		mc.CompileFunc = func(id string) (validator.Validator, error) {
			if strings.Contains(id, "1_0_0") {
				return nil, errors.New("render fail")
			}
			return &mockValidator{}, nil
		}

		err := tr.testSchemaCompatibleWithEarlierVersions(context.Background(), k2)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "render fail")
	})

	t.Run("external context cancelled before wait", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		tr := NewTester(r)
		k1 := Key("d_f_1_0_0")
		k2 := Key("d_f_1_0_1")
		createSchemaFiles(t, r, schemaMap{k1: `{}`, k2: `{}`})
		s, _ := r.GetSchemaByKey(k2)
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), "pass"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), "fail"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(s.Path(HomeDir), "pass", "v.json"), []byte(`{}`), 0o600))

		ctx, cancel := context.WithCancel(context.Background())

		mc, ok := r.compiler.(*mockCompiler)
		require.True(t, ok)
		mc.CompileFunc = func(_ string) (validator.Validator, error) {
			cancel()
			return &mockValidator{}, nil
		}

		err := tr.testSchemaCompatibleWithEarlierVersions(ctx, k2)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("GetSchemaByKey failure with invalid JSON", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		tr := NewTester(r)
		k := Key("d_f_1_0_0")
		createSchemaFiles(t, r, schemaMap{k: `{"type": "object"}`})
		s, _ := r.GetSchemaByKey(k)
		require.NoError(t, os.WriteFile(s.Path(FilePath), []byte("{ invalid json"), 0o600))

		// Clear cache to force reload
		r.mu.Lock()
		delete(r.cache, k)
		r.mu.Unlock()

		err := tr.testSchemaCompatibleWithEarlierVersions(context.Background(), k)
		require.Error(t, err)
		assert.ErrorAs(t, err, new(*InvalidJSONError))
	})
}

func TestTester_ProviderCompatibility_FoundSchemas(t *testing.T) {
	t.Parallel()

	// 1.0.1's pass document breaks 1.0.0, as in TestTester_ProviderCompatibility
	setup := func(t *testing.T, localFails bool) (*Tester, Key, Key) {
		t.Helper()
		r := setupTestRegistry(t)
		earlierKey := Key("domain_family_1_0_0")
		targetKey := Key("domain_family_1_0_1")
		createSchemaFiles(t, r, schemaMap{
			earlierKey: `{"type": "object", "required": ["name"]}`,
			targetKey:  `{"type": "object"}`,
		})
		for _, k := range []Key{earlierKey, targetKey} {
			s, _ := r.GetSchemaByKey(k)
			require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), "pass"), 0o755))
			require.NoError(t, os.MkdirAll(filepath.Join(s.Path(HomeDir), "fail"), 0o755))
		}
		targetSchema, _ := r.GetSchemaByKey(targetKey)
		require.NoError(
			t,
			os.WriteFile(filepath.Join(targetSchema.Path(HomeDir), "pass", "valid.json"), []byte(`{}`), 0o600),
		)

		mc, ok := r.compiler.(*mockCompiler)
		require.True(t, ok)
		mc.CompileFunc = func(id string) (validator.Validator, error) {
			if strings.Contains(id, "1_0_0") || localFails {
				return &mockValidator{Err: errors.New("required property 'name' missing")}, nil
			}
			return &mockValidator{}, nil
		}

		tr := NewTester(r)
		tr.SetStopOnFirstError(false)
		return tr, earlierKey, targetKey
	}

	t.Run("default scope runs the compatibility check", func(t *testing.T) {
		t.Parallel()
		tr, earlierKey, targetKey := setup(t, false)

		report, err := tr.TestFoundSchemas(context.Background(), "domain/family")
		require.NoError(t, err)
		require.Len(t, report.FailedTests[earlierKey], 1)
		spec := report.FailedTests[earlierKey][0]
		require.NotNil(t, spec.ForwardVersion)
		ts, _ := tr.registry.GetSchemaByKey(targetKey)
		assert.Equal(t, filepath.Join(ts.Path(HomeDir), "pass", "valid.json"), spec.TestInfo.Path)
	})

	t.Run("skip compatibility check", func(t *testing.T) {
		t.Parallel()
		tr, _, _ := setup(t, false)
		tr.SetSkipCompatible(true)

		report, err := tr.TestFoundSchemas(context.Background(), "domain/family")
		require.NoError(t, err)
		assert.Empty(t, report.FailedTests)
	})

	t.Run("local failures skip the compatibility check", func(t *testing.T) {
		t.Parallel()
		tr, earlierKey, targetKey := setup(t, true)

		report, err := tr.TestFoundSchemas(context.Background(), "domain/family")
		require.NoError(t, err)
		assert.NotEmpty(t, report.FailedTests[targetKey])
		assert.Empty(t, report.FailedTests[earlierKey])
	})

	for _, scope := range []TestScope{TestScopeConsumerBreaking, TestScopeAll} {
		t.Run(string(scope)+" reports the breaking change once", func(t *testing.T) {
			t.Parallel()
			tr, earlierKey, _ := setup(t, false)
			tr.SetScope(scope)

			report, err := tr.TestFoundSchemas(context.Background(), "domain/family")
			require.NoError(t, err)
			assert.Len(t, report.FailedTests[earlierKey], 1)
		})
	}
}
