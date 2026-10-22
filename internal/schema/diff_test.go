package schema

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/config"
	"github.com/bitshepherds/json-schema-manager/internal/coverage"
	"github.com/bitshepherds/json-schema-manager/internal/fsh"
	"github.com/bitshepherds/json-schema-manager/internal/structdiff"
	"github.com/bitshepherds/json-schema-manager/internal/validator"
)

// writePassDoc writes a pass document for the schema with key k.
func writePassDoc(t *testing.T, r *Registry, k Key, name, content string) {
	t.Helper()
	s, err := r.GetSchemaByKey(k)
	require.NoError(t, err)
	dir := filepath.Join(s.Path(HomeDir), string(TestDocTypePass))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

// makeTestDirs creates the pass and fail directories of each schema.
func makeTestDirs(t *testing.T, r *Registry, keys ...Key) {
	t.Helper()
	for _, k := range keys {
		for _, tt := range []TestDocType{TestDocTypePass, TestDocTypeFail} {
			require.NoError(t, os.MkdirAll(filepath.Join(New(k, r).Path(HomeDir), string(tt)), 0o755))
		}
	}
}

// failToCompile makes the registry's compiler fail to compile the schema with key k.
func failToCompile(t *testing.T, r *Registry, k Key) {
	t.Helper()
	mc, ok := r.compiler.(*mockCompiler)
	require.True(t, ok)
	mc.CompileFunc = func(id string) (validator.Validator, error) {
		if strings.Contains(id, string(k)) {
			return nil, errors.New("compile error")
		}
		return &mockValidator{}, nil
	}
}

const (
	enumV100 = `{"properties": {"status": {"enum": ["placed", "paid"]}}}`
	enumV110 = `{"properties": {"status": {"enum": ["placed", "paid", "refunded"]}}}`
)

func TestDiff(t *testing.T) {
	t.Parallel()

	r := setupTestRegistry(t)
	createSchemaFiles(t, r, schemaMap{"shop_order_1_0_0": enumV100, "shop_order_1_1_0": enumV110})
	writePassDoc(t, r, "shop_order_1_1_0", "paid.json", `{"status": "paid"}`)
	writePassDoc(t, r, "shop_order_1_1_0", "refunded.json", `{"status": "refunded"}`)
	older, err := r.GetSchemaByKey("shop_order_1_0_0")
	require.NoError(t, err)
	newer, err := r.GetSchemaByKey("shop_order_1_1_0")
	require.NoError(t, err)

	changes, err := Diff(older, newer)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, "/status", changes[0].Change.Location)
	assert.Equal(t, structdiff.Widens, changes[0].Change.Effect)
	require.Len(t, changes[0].UsedBy, 1)
	assert.Equal(t, "refunded.json", filepath.Base(changes[0].UsedBy[0]))
}

func TestDiff_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		noCompile Key
		// passIsFile makes the newer schema's pass directory a file
		passIsFile bool
	}{
		{name: "older does not render", noCompile: "d_f_1_0_0"},
		{name: "newer does not render", noCompile: "d_f_1_1_0"},
		{name: "newer's pass documents cannot be read", passIsFile: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := setupTestRegistry(t)
			createSchemaFiles(t, r, schemaMap{"d_f_1_0_0": `{}`, "d_f_1_1_0": `{}`})
			if tt.noCompile != "" {
				failToCompile(t, r, tt.noCompile)
			}
			older, err := r.GetSchemaByKey("d_f_1_0_0")
			require.NoError(t, err)
			newer, err := r.GetSchemaByKey("d_f_1_1_0")
			require.NoError(t, err)
			if tt.passIsFile {
				require.NoError(t, os.WriteFile(filepath.Join(newer.Path(HomeDir), "pass"), nil, 0o600))
			}

			_, err = Diff(older, newer)
			require.Error(t, err)
		})
	}
}

func TestTester_UnexercisedChanges(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T) *Registry {
		t.Helper()
		r := setupTestRegistry(t)
		createSchemaFiles(t, r, schemaMap{"shop_order_1_0_0": enumV100, "shop_order_1_1_0": enumV110})
		makeTestDirs(t, r, "shop_order_1_0_0", "shop_order_1_1_0")
		writePassDoc(t, r, "shop_order_1_0_0", "paid.json", `{"status": "paid"}`)
		writePassDoc(t, r, "shop_order_1_1_0", "paid.json", `{"status": "paid"}`)
		return r
	}

	t.Run("a single schema", func(t *testing.T) {
		t.Parallel()
		tr := NewTester(setup(t))
		report, err := tr.TestSingleSchema(context.Background(), "shop_order_1_1_0")
		require.NoError(t, err)
		require.Len(t, report.UnexercisedChanges["shop_order_1_1_0"], 1)
		uc := report.UnexercisedChanges["shop_order_1_1_0"][0]
		assert.Equal(t, Key("shop_order_1_0_0"), uc.Earlier)
		assert.Equal(t, `adds "refunded"`, uc.Change.Detail)
	})

	t.Run("found schemas, in every test scope", func(t *testing.T) {
		t.Parallel()
		for _, scope := range []TestScope{TestScopeLocal, TestScopeAll} {
			tr := NewTester(setup(t))
			tr.SetScope(scope)
			report, err := tr.TestFoundSchemas(context.Background(), "")
			require.NoError(t, err)
			assert.Len(t, report.UnexercisedChanges["shop_order_1_1_0"], 1, "scope %s", scope)
			assert.Empty(t, report.UnexercisedChanges["shop_order_1_0_0"], "1.0.0 has no earlier version")
		}
	})

	t.Run("skipped with the compatibility check", func(t *testing.T) {
		t.Parallel()
		tr := NewTester(setup(t))
		tr.SetSkipCompatible(true)
		report, err := tr.TestSingleSchema(context.Background(), "shop_order_1_1_0")
		require.NoError(t, err)
		assert.Empty(t, report.UnexercisedChanges)
	})

	t.Run("cleared by a pass document which uses the change", func(t *testing.T) {
		t.Parallel()
		r := setup(t)
		writePassDoc(t, r, "shop_order_1_1_0", "refunded.json", `{"status": "refunded"}`)
		report, err := NewTester(r).TestSingleSchema(context.Background(), "shop_order_1_1_0")
		require.NoError(t, err)
		assert.Empty(t, report.UnexercisedChanges)
	})
}

func TestTester_findUnexercisedChanges_Errors(t *testing.T) {
	t.Parallel()

	t.Run("schema not found", func(t *testing.T) {
		t.Parallel()
		tr := NewTester(setupTestRegistry(t))
		require.Error(t, tr.findUnexercisedChanges("missing_1_0_0"))
	})

	t.Run("earlier versions cannot be listed", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		createSchemaFiles(t, r, schemaMap{"d_f_1_0_1": `{}`})
		s, err := r.GetSchemaByKey("d_f_1_0_1")
		require.NoError(t, err)
		majorDir := filepath.Join(s.Path(FamilyDir), "1")
		require.NoError(t, os.RemoveAll(majorDir))
		require.NoError(t, os.WriteFile(majorDir, nil, 0o600))

		require.Error(t, NewTester(r).findUnexercisedChanges("d_f_1_0_1"))
	})

	t.Run("earlier version has no schema file", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		createSchemaFiles(t, r, schemaMap{"d_f_1_0_1": `{}`})
		require.NoError(t, os.MkdirAll(New("d_f_1_0_0", r).Path(HomeDir), 0o755))

		require.Error(t, NewTester(r).findUnexercisedChanges("d_f_1_0_1"))
	})

	// With no pass documents, the compatibility check has nothing to run, so the comparison is
	// the first to render the earlier version
	t.Run("earlier version does not render", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		createSchemaFiles(t, r, schemaMap{"d_f_1_0_0": `{}`, "d_f_1_0_1": `{}`})
		makeTestDirs(t, r, "d_f_1_0_1")
		failToCompile(t, r, "d_f_1_0_0")

		_, err := NewTester(r).TestSingleSchema(context.Background(), "d_f_1_0_1")
		require.Error(t, err)
		require.Error(t, NewTester(r).testFoundSchema(context.Background(), "d_f_1_0_1"))
	})
}

func TestTester_Summary(t *testing.T) {
	t.Parallel()

	const schemaJSON = `{"properties": {"status": {"enum": ["paid", "refunded"]}}, "anyOf": [{"required": ["status"]}]}`

	t.Run("with the real validator", func(t *testing.T) {
		t.Parallel()
		regDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(regDir, config.JsmRegistryConfigFile),
			[]byte(testConfigData), 0o600))
		r, err := NewRegistry(regDir, validator.NewSanthoshCompiler(), fsh.NewPathResolver(), fsh.NewEnvProvider())
		require.NoError(t, err)
		createSchemaFiles(t, r, schemaMap{"shop_order_1_0_0": schemaJSON})
		makeTestDirs(t, r, "shop_order_1_0_0")
		writePassDoc(t, r, "shop_order_1_0_0", "paid.json", `{"status": "paid"}`)
		s, err := r.GetSchemaByKey("shop_order_1_0_0")
		require.NoError(t, err)
		failPath := filepath.Join(s.Path(HomeDir), "fail", "lost.json")
		require.NoError(t, os.WriteFile(failPath, []byte(`{"status": "lost"}`), 0o600))

		report, err := NewTester(r).TestSingleSchema(context.Background(), "shop_order_1_0_0")
		require.NoError(t, err)
		summary := report.Summaries["shop_order_1_0_0"]
		require.NotNil(t, summary)
		assert.Equal(t, 1, summary.PassDocs)
		assert.Equal(t, 1, summary.FailDocs)
		assert.False(t, summary.MissingDocuments())
		assert.Equal(t, []coverage.Point{
			{Location: "/", Kind: coverage.Property, Detail: "status", Covered: true},
			{Location: "/status", Kind: coverage.EnumValue, Detail: `"paid"`, Covered: true},
			{Location: "/status", Kind: coverage.EnumValue, Detail: `"refunded"`, Covered: false},
			{Location: "/", Kind: coverage.Branch, Detail: "anyOf/0", Covered: true},
		}, summary.Coverage)
		assert.Equal(t, 3, summary.Covered())
		assert.Equal(t, map[string][]string{failPath: {"#/properties/status/enum"}}, summary.FailKeywords)
		assert.Equal(t, 0, report.MissingDocuments())
	})

	t.Run("subschemas which do not compile are left out", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		mc, ok := r.compiler.(*mockCompiler)
		require.True(t, ok)
		mc.CompileFunc = func(id string) (validator.Validator, error) {
			if strings.Contains(id, "#") {
				return nil, errors.New("compile error")
			}
			return &mockValidator{}, nil
		}
		createSchemaFiles(t, r, schemaMap{"shop_order_1_0_0": schemaJSON})
		makeTestDirs(t, r, "shop_order_1_0_0")
		writePassDoc(t, r, "shop_order_1_0_0", "paid.json", `{"status": "paid"}`)

		report, err := NewTester(r).TestSingleSchema(context.Background(), "shop_order_1_0_0")
		require.NoError(t, err)
		summary := report.Summaries["shop_order_1_0_0"]
		require.NotNil(t, summary)
		assert.Len(t, summary.Coverage, 3, "the anyOf subschema is left out")
		assert.True(t, summary.MissingDocuments(), "there is no fail document")
		assert.Equal(t, 1, report.MissingDocuments())
	})
}

func TestSchema_NewestInFamily(t *testing.T) {
	t.Parallel()

	r := setupTestRegistry(t)
	createSchemaFiles(t, r, schemaMap{
		"shop_order_1_0_0": `{}`, "shop_order_1_2_0": `{}`, "shop_order_2_0_0": `{}`, "shop_order_2_0_3": `{}`,
	})
	s, err := r.GetSchemaByKey("shop_order_1_0_0")
	require.NoError(t, err)
	newest, err := s.NewestInFamily()
	require.NoError(t, err)
	assert.Equal(t, Key("shop_order_2_0_3"), newest)

	// A major version directory with no versions in it
	require.NoError(t, os.MkdirAll(filepath.Join(s.Path(FamilyDir), "3"), 0o755))
	_, err = s.NewestInFamily()
	require.ErrorAs(t, err, new(*NotFoundError))

	// The family directory cannot be read
	require.NoError(t, os.RemoveAll(s.Path(FamilyDir)))
	_, err = s.NewestInFamily()
	require.Error(t, err)
}
