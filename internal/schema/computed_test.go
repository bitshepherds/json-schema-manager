package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/v2/internal/config"
)

func TestComputed_StoreRenderInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     config.Env
		info    RenderInfo
		setup   func() *Computed
		wantLen int
	}{
		{
			name: "Store render to nil RenderCache",
			env:  config.Env("prod"),
			info: RenderInfo{Rendered: []byte(`{"$id": "https://example.com/schema.json"}`)},
			setup: func() *Computed {
				return &Computed{}
			},
			wantLen: 1,
		},
		{
			name: "Store render to existing RenderCache",
			env:  config.Env("dev"),
			info: RenderInfo{Rendered: []byte(`{"$id": "https://dev.example.com/schema.json"}`)},
			setup: func() *Computed {
				c := &Computed{}
				c.StoreRenderInfo(
					config.Env("prod"),
					RenderInfo{Rendered: []byte(`{"$id": "https://example.com/prod.json"}`)},
				)
				return c
			},
			wantLen: 2,
		},
		{
			name: "Overwrite existing render for same env",
			env:  config.Env("prod"),
			info: RenderInfo{Rendered: []byte(`{"$id": "https://example.com/updated.json"}`)},
			setup: func() *Computed {
				c := &Computed{}
				c.StoreRenderInfo(
					config.Env("prod"),
					RenderInfo{Rendered: []byte(`{"$id": "https://example.com/original.json"}`)},
				)
				return c
			},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := tt.setup()
			c.StoreRenderInfo(tt.env, tt.info)

			assert.NotNil(t, c.renders)
			assert.Len(t, c.renders, tt.wantLen)
			assert.Equal(t, tt.info, c.renders[tt.env])
		})
	}
}

func TestComputed_StoreID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     config.Env
		id      ID
		setup   func() *Computed
		wantLen int
	}{
		{
			name: "Store ID to nil IDCache",
			env:  config.Env("prod"),
			id:   ID("https://example.com/schema.json"),
			setup: func() *Computed {
				return &Computed{}
			},
			wantLen: 1,
		},
		{
			name: "Store ID to existing IDCache",
			env:  config.Env("dev"),
			id:   ID("https://dev.example.com/schema.json"),
			setup: func() *Computed {
				c := &Computed{}
				c.StoreID(config.Env("prod"), ID("https://example.com/prod.json"))
				return c
			},
			wantLen: 2,
		},
		{
			name: "Overwrite existing ID for same env",
			env:  config.Env("prod"),
			id:   ID("https://example.com/updated.json"),
			setup: func() *Computed {
				c := &Computed{}
				c.StoreID(config.Env("prod"), ID("https://example.com/original.json"))
				return c
			},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := tt.setup()
			c.StoreID(tt.env, tt.id)

			assert.NotNil(t, c.ids)
			assert.Len(t, c.ids, tt.wantLen)
			assert.Equal(t, tt.id, c.ids[tt.env])
		})
	}
}

func TestComputed_StoreTests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		testType     TestDocType
		infos        []TestInfo
		setup        func() *Computed
		wantLen      int
		wantCacheLen int
	}{
		{
			name:     "Store pass tests to nil TestCache",
			testType: TestDocTypePass,
			infos: []TestInfo{
				{Path: "/path/to/pass/test1.json"},
				{Path: "/path/to/pass/test2.json"},
			},
			setup: func() *Computed {
				return &Computed{}
			},
			wantLen:      2,
			wantCacheLen: 1,
		},
		{
			name:     "Store fail tests to nil TestCache",
			testType: TestDocTypeFail,
			infos: []TestInfo{
				{Path: "/path/to/fail/invalid-doc.json"},
			},
			setup: func() *Computed {
				return &Computed{}
			},
			wantLen:      1,
			wantCacheLen: 1,
		},
		{
			name:     "Store tests to existing TestCache with different type",
			testType: TestDocTypeFail,
			infos: []TestInfo{
				{Path: "/path/to/fail/fail-test.json"},
			},
			setup: func() *Computed {
				c := &Computed{}
				c.StoreTests(TestDocTypePass, []TestInfo{{Path: "/path/to/pass/pass-test.json"}})
				return c
			},
			wantLen:      1,
			wantCacheLen: 2,
		},
		{
			name:     "Overwrite existing tests for same type",
			testType: TestDocTypePass,
			infos: []TestInfo{
				{Path: "/path/to/pass/new-test.json"},
			},
			setup: func() *Computed {
				c := &Computed{}
				c.StoreTests(TestDocTypePass, []TestInfo{{Path: "/path/to/pass/old-test.json"}})
				return c
			},
			wantLen:      1,
			wantCacheLen: 1,
		},
		{
			name:     "Store empty test list",
			testType: TestDocTypePass,
			infos:    []TestInfo{},
			setup: func() *Computed {
				return &Computed{}
			},
			wantLen:      0,
			wantCacheLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := tt.setup()
			c.StoreTests(tt.testType, tt.infos)

			assert.NotNil(t, c.tests)
			assert.Len(t, c.tests, tt.wantCacheLen)
			assert.Len(t, c.tests[tt.testType], tt.wantLen)
			assert.Equal(t, tt.infos, c.tests[tt.testType])
		})
	}
}

func TestComputed_Tests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		testType  TestDocType
		setup     func() *Computed
		wantInfos []TestInfo
	}{
		{
			name:     "Get pass tests",
			testType: TestDocTypePass,
			setup: func() *Computed {
				c := &Computed{}
				c.StoreTests(TestDocTypePass, []TestInfo{
					{Path: "/original/pass/test1.json"},
					{Path: "/original/pass/test2.json"},
				})
				return c
			},
			wantInfos: []TestInfo{
				{Path: "/original/pass/test1.json"},
				{Path: "/original/pass/test2.json"},
			},
		},
		{
			name:     "Get fail tests",
			testType: TestDocTypeFail,
			setup: func() *Computed {
				c := &Computed{}
				c.StoreTests(TestDocTypeFail, []TestInfo{
					{Path: "/original/fail/invalid.json"},
				})
				return c
			},
			wantInfos: []TestInfo{
				{Path: "/original/fail/invalid.json"},
			},
		},
		{
			name:     "Get tests from nil TestCache",
			testType: TestDocTypePass,
			setup: func() *Computed {
				return &Computed{}
			},
			wantInfos: nil,
		},
		{
			name:     "Get tests for type not in cache",
			testType: TestDocTypeFail,
			setup: func() *Computed {
				c := &Computed{}
				c.StoreTests(TestDocTypePass, []TestInfo{{Path: "/path/to/pass/test.json"}})
				return c
			},
			wantInfos: nil,
		},
		{
			name:     "Get empty test list",
			testType: TestDocTypePass,
			setup: func() *Computed {
				c := &Computed{}
				c.StoreTests(TestDocTypePass, []TestInfo{})
				return c
			},
			wantInfos: []TestInfo{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := tt.setup()
			infos := c.Tests(tt.testType)

			assert.Equal(t, tt.wantInfos, infos)
		})
	}
}

func TestNewTestInfo(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	validPath := filepath.Join(tmpDir, "valid.json")
	invalidJSONPath := filepath.Join(tmpDir, "invalid.json")
	nonExistentPath := filepath.Join(tmpDir, "non-existent.json")

	validData := []byte(`{"key": "value"}`)
	if err := os.WriteFile(validPath, validData, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(invalidJSONPath, []byte(`{invalid}`), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name               string
		path               string
		wantErrMsgContains string
	}{
		{
			name: "Success",
			path: validPath,
		},
		{
			name:               "Non-existent file",
			path:               nonExistentPath,
			wantErrMsgContains: "could not be read",
		},
		{
			name:               "Invalid JSON",
			path:               invalidJSONPath,
			wantErrMsgContains: "is not valid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			info, err := NewTestInfo(tt.path)

			if tt.wantErrMsgContains != "" {
				require.ErrorContains(t, err, tt.wantErrMsgContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.path, info.Path)
			assert.Equal(t, validData, info.SrcDoc)
			m, ok := info.Unmarshalled.(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, "value", m["key"])
		})
	}
}

func TestComputed_RenderInfo(t *testing.T) {
	t.Parallel()

	c := &Computed{}
	env := config.Env("prod")
	info := RenderInfo{ID: ID("https://example.com")}

	c.StoreRenderInfo(env, info)
	assert.Equal(t, info, c.RenderInfo(env))

	// Missing env should return zero value
	assert.Equal(t, RenderInfo{}, c.RenderInfo(config.Env("missing")))
}

func TestComputed_ID(t *testing.T) {
	t.Parallel()

	c := &Computed{}
	env := config.Env("prod")
	id := ID("https://example.com")

	c.StoreID(env, id)
	assert.Equal(t, id, c.ID(env))

	// Missing env should return zero value
	assert.Equal(t, ID(""), c.ID(config.Env("missing")))
}
