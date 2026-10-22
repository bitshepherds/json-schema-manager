package app

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/schema"
)

func TestNewCheckConsumerCmd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		args         []string
		wantManifest string
		wantFormat   string
		wantStrict   bool
	}{
		{"default manifest", nil, "jsm-consumer.yml", "text", false},
		{"named manifest, JSON and strict", []string{"m.yml", "-o", "json", "--strict"}, "m.yml", "json", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &MockManager{}
			m.On("CheckConsumer", context.Background(), tt.wantManifest, tt.wantFormat, true, tt.wantStrict).
				Return(nil).Once()
			cmd := NewCheckConsumerCmd(m)
			cmd.Flags().Bool("nocolour", false, "")
			cmd.SetArgs(tt.args)
			require.NoError(t, cmd.ExecuteContext(context.Background()))
			m.AssertExpectations(t)
		})
	}
}

func TestCLIManager_CheckConsumer(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	setup := func(t *testing.T, schemaContent string) (*schema.Registry, string) {
		t.Helper()
		registry := setupTestRegistry(t)
		s := schema.New("shop_order_1_0_0", registry)
		for _, sub := range []string{"pass", "fail"} {
			require.NoError(t, os.MkdirAll(filepath.Join(s.Path(schema.HomeDir), sub), 0o755))
		}
		require.NoError(t, os.WriteFile(s.Path(schema.FilePath), []byte(schemaContent), 0o600))
		manifest := filepath.Join(t.TempDir(), "jsm-consumer.yml")
		require.NoError(t, os.WriteFile(manifest, []byte("schemas: [shop_order_1_0_0]\n"), 0o600))
		return registry, manifest
	}

	t.Run("passes, in text and JSON", func(t *testing.T) {
		t.Parallel()
		registry, manifest := setup(t, `{}`)
		var out bytes.Buffer
		mgr := NewCLIManager(logger, registry, schema.NewTester(registry), &MockGitter{}, nil, &out)
		require.NoError(t, mgr.CheckConsumer(context.Background(), manifest, "text", false, true))
		assert.Contains(t, out.String(), "[PASS] shop_order_1_0_0")
		out.Reset()
		require.NoError(t, mgr.CheckConsumer(context.Background(), manifest, "json", false, true))
		assert.Contains(t, out.String(), `"status": "pass"`)
	})

	t.Run("manifest cannot be read", func(t *testing.T) {
		t.Parallel()
		registry, _ := setup(t, `{}`)
		mgr := NewCLIManager(logger, registry, schema.NewTester(registry), &MockGitter{}, nil, io.Discard)
		require.Error(t, mgr.CheckConsumer(context.Background(), "missing.yml", "text", false, false))
	})

	t.Run("pinned version does not render", func(t *testing.T) {
		t.Parallel()
		registry, manifest := setup(t, "{\"$ref\": \"{{ JSM `missing_1_0_0` }}\"}")
		mgr := NewCLIManager(logger, registry, schema.NewTester(registry), &MockGitter{}, nil, io.Discard)
		err := mgr.CheckConsumer(context.Background(), manifest, "text", false, false)
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrTestsFailed)
	})

	t.Run("the report cannot be written", func(t *testing.T) {
		t.Parallel()
		registry, manifest := setup(t, `{}`)
		mgr := NewCLIManager(logger, registry, schema.NewTester(registry), &MockGitter{}, nil, &failingWriter{})
		require.Error(t, mgr.CheckConsumer(context.Background(), manifest, "text", false, false))
	})
}
