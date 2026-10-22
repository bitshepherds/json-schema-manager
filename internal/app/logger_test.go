package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/fsh"
)

func TestSetupLogger(t *testing.T) {
	t.Parallel()

	t.Run("success with rd", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		logLevel := &slog.LevelVar{}
		stderr := &bytes.Buffer{}
		envProvider := fsh.NewEnvProvider()
		logger, closer, err := setupLogger(stderr, logLevel, tempDir, "", envProvider)
		require.NoError(t, err)
		defer func() { _ = closer.Close() }()
		assert.NotNil(t, logger)
		assert.FileExists(t, filepath.Join(tempDir, LogFile))
	})

	t.Run("success with no rd", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		// Use a temp file in a temp dir to avoid polluting current dir
		logFile := filepath.Join(tempDir, LogFile)
		logLevel := &slog.LevelVar{}
		stderr := &bytes.Buffer{}
		envProvider := &mockEnvProvider{values: map[string]string{LogEnvVar: logFile}}
		logger, closer, err := setupLogger(stderr, logLevel, "", "", envProvider)
		require.NoError(t, err)
		defer func() { _ = closer.Close() }()
		assert.NotNil(t, logger)
		assert.FileExists(t, logFile)
	})

	t.Run("success with file", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		logLevel := &slog.LevelVar{}
		logLevel.Set(slog.LevelInfo)
		stderr := &bytes.Buffer{}
		envProvider := fsh.NewEnvProvider()

		logger, closer, err := setupLogger(stderr, logLevel, tempDir, "", envProvider)
		require.NoError(t, err)
		require.NotNil(t, logger)
		require.NotNil(t, closer)
		defer func() { _ = closer.Close() }()

		logger.Info("test message", "key", "value")

		// Check console output
		assert.Contains(t, stderr.String(), "test message")
		assert.Contains(t, stderr.String(), "key=value") // Info shows attrs

		// Check file output
		logFile := filepath.Join(tempDir, ".jsm.log")
		data, err := os.ReadFile(logFile)
		require.NoError(t, err)
		assert.Contains(t, string(data), `"msg":"test message"`)
		assert.Contains(t, string(data), `"key":"value"`)
	})

	t.Run("success with env var override", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		logFile := filepath.Join(tempDir, "custom.log")
		envProvider := &mockEnvProvider{values: map[string]string{LogEnvVar: logFile}}

		logLevel := &slog.LevelVar{}
		stderr := &bytes.Buffer{}

		logger, closer, err := setupLogger(stderr, logLevel, "", "", envProvider)
		require.NoError(t, err)
		defer func() { _ = closer.Close() }()

		logger.Info("custom log")
		data, _ := os.ReadFile(logFile)
		assert.Contains(t, string(data), "custom log")
	})

	t.Run("fallback on file error", func(t *testing.T) {
		t.Parallel()
		logLevel := &slog.LevelVar{}
		stderr := &bytes.Buffer{}
		envProvider := fsh.NewEnvProvider()

		// Point to a non-existent directory that cannot be created
		logger, closer, err := setupLogger(stderr, logLevel, "/non/existent/path/unwritable", "", envProvider)
		require.Error(t, err)
		assert.Nil(t, closer)
		assert.NotNil(t, logger)

		logger.Info("fallback message")
		assert.Contains(t, stderr.String(), "fallback message")
	})

	t.Run("default log path when rd and env are empty", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()

		logLevel := &slog.LevelVar{}
		stderr := &bytes.Buffer{}
		envProvider := &mockEnvProvider{values: map[string]string{}}

		// rd is empty, should use LogFile in wd (which we set to tmpDir)
		logger, closer, err := setupLogger(stderr, logLevel, "", tmpDir, envProvider)
		require.NoError(t, err)
		defer func() { _ = closer.Close() }()

		logger.Info("default log")
		assert.FileExists(t, filepath.Join(tmpDir, LogFile))
	})

	t.Run("full fallback when rd, wd, and env are empty", func(t *testing.T) {
		t.Parallel()
		// This will use current directory's .jsm.log
		// We should clean it up if it's created, but better to check the path logic.
		logLevel := &slog.LevelVar{}
		stderr := &bytes.Buffer{}
		envProvider := &mockEnvProvider{values: map[string]string{}}

		// Note: This might fail if the current directory is unwritable, but in tests it usually is.
		// To be safe, we don't necessarily need to RUN it, just check the code path if we could.
		// But let's try to run it and cleanup.
		logger, closer, err := setupLogger(stderr, logLevel, "", "", envProvider)
		if err == nil {
			defer func() { _ = closer.Close() }()
			defer func() { _ = os.Remove(LogFile) }()
		}
		assert.NotNil(t, logger)
	})

	t.Run("error opening log file", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		logFile := filepath.Join(tempDir, "read-only.log")

		require.NoError(t, os.WriteFile(logFile, []byte(""), 0o444))

		logLevel := &slog.LevelVar{}
		stderr := &bytes.Buffer{}
		envProvider := &mockEnvProvider{values: map[string]string{LogEnvVar: logFile}}

		logger, closer, err := setupLogger(stderr, logLevel, "", "", envProvider)
		require.Error(t, err)
		assert.Nil(t, closer)
		assert.NotNil(t, logger)

		logger.Info("read-only fallback")
		assert.Contains(t, stderr.String(), "read-only fallback")
	})
}

func TestConsoleHandler_Thorough(t *testing.T) {
	t.Parallel()

	t.Run("levels", func(t *testing.T) {
		t.Parallel()
		logLevel := &slog.LevelVar{}
		buf := &bytes.Buffer{}
		handler := &consoleHandler{w: buf, level: logLevel}

		tests := []struct {
			level slog.Level
			msg   string
			want  string
		}{
			{slog.LevelDebug, "d", "d\n"},
			{slog.LevelInfo, "i", "i\n"},
			{slog.LevelWarn, "w", "Warning: w\n"},
			{slog.LevelError, "e", "Error: e\n"},
		}
		for _, tt := range tests {
			buf.Reset()
			logLevel.Set(slog.LevelDebug) // Enable all
			err := handler.Handle(context.Background(), slog.Record{Level: tt.level, Message: tt.msg})
			require.NoError(t, err)
			assert.Equal(t, tt.want, buf.String())
		}
	})

	t.Run("attributes and formatting", func(t *testing.T) {
		t.Parallel()
		logLevel := &slog.LevelVar{}
		buf := &bytes.Buffer{}
		handler := &consoleHandler{w: buf, level: logLevel}

		buf.Reset()
		logLevel.Set(slog.LevelDebug)

		// Test formatAttr with error
		rec := slog.NewRecord(time.Now(), slog.LevelError, "msg", 0)
		rec.AddAttrs(slog.Attr{Key: "err", Value: slog.StringValue("boom")})
		err := handler.Handle(context.Background(), rec)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Error: msg: boom")

		// Test attributes at Info level (should be shown)
		buf.Reset()
		logLevel.Set(slog.LevelInfo)
		rec2 := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
		rec2.AddAttrs(slog.Attr{Key: "foo", Value: slog.StringValue("bar")})
		err2 := handler.Handle(context.Background(), rec2)
		require.NoError(t, err2)
		assert.Equal(t, "msg foo=bar\n", buf.String())

		// Test WithAttrs
		buf.Reset()
		logLevel.Set(slog.LevelDebug)
		h2 := handler.WithAttrs([]slog.Attr{slog.Int("pid", 123)})
		err3 := h2.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0))
		require.NoError(t, err3)
		assert.Contains(t, buf.String(), "msg pid=123")

		// The component attribute is for the log file, not the console
		buf.Reset()
		h4 := handler.WithAttrs([]slog.Attr{slog.String("component", "watcher")})
		require.NoError(t, h4.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)))
		assert.Equal(t, "msg\n", buf.String())

		// Test WithGroup
		h3 := h2.WithGroup("somegroup")
		assert.Equal(t, h2, h3) // Currently returns self
	})
}

type errHandler struct{}

func (e *errHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (e *errHandler) Handle(context.Context, slog.Record) error { return errors.New("handler error") }
func (e *errHandler) WithAttrs(_ []slog.Attr) slog.Handler      { return e }
func (e *errHandler) WithGroup(_ string) slog.Handler           { return e }

func TestMultiHandler_Thorough(t *testing.T) {
	t.Parallel()

	t.Run("Enabled", func(t *testing.T) {
		t.Parallel()
		h1 := &consoleHandler{w: &bytes.Buffer{}, level: &slog.LevelVar{}}
		h2 := &consoleHandler{w: &bytes.Buffer{}, level: &slog.LevelVar{}}
		multi := &multiHandler{handlers: []slog.Handler{h1, h2}}

		assert.True(t, multi.Enabled(context.Background(), slog.LevelInfo))

		// False case: none enabled
		h1.level.Set(slog.LevelError)
		h2.level.Set(slog.LevelError)
		assert.False(t, multi.Enabled(context.Background(), slog.LevelInfo))
	})

	t.Run("Handle error propagation", func(t *testing.T) {
		t.Parallel()
		eh := &errHandler{}
		m2 := &multiHandler{handlers: []slog.Handler{eh}}
		err := m2.Handle(context.Background(), slog.Record{Level: slog.LevelInfo})
		require.Error(t, err)
	})

	t.Run("WithAttrs and WithGroup", func(t *testing.T) {
		t.Parallel()
		h1 := &consoleHandler{w: &bytes.Buffer{}, level: &slog.LevelVar{}}
		h2 := &consoleHandler{w: &bytes.Buffer{}, level: &slog.LevelVar{}}
		multi := &multiHandler{handlers: []slog.Handler{h1, h2}}

		m3 := multi.WithAttrs([]slog.Attr{slog.String("v", "1")})
		assert.IsType(t, &multiHandler{}, m3)

		m4 := m3.WithGroup("g")
		assert.IsType(t, &multiHandler{}, m4)
	})
}

// mockEnvProvider is a test implementation of fsh.EnvProvider.
type mockEnvProvider struct {
	values map[string]string
}

func (m *mockEnvProvider) Get(key string) string {
	if m.values == nil {
		return ""
	}
	return m.values[key]
}
