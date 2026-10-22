package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/bitshepherds/json-schema-manager/internal/fsh"
)

const (
	// LogFile is the default name of the log file.
	LogFile = ".jsm.log"
	// LogEnvVar is the name of the environment variable used to override the log file path.
	LogEnvVar = "JSM_LOG_FILE"
)

// setupLogger configures a logger that writes structured logs to a file
// and clean, human-readable logs to the console.
func setupLogger(
	stderr io.Writer,
	logLevel *slog.LevelVar,
	rd string,
	wd string,
	envProvider fsh.EnvProvider,
) (*slog.Logger, io.Closer, error) {
	// 1. Determine log file path
	logPath := envProvider.Get(LogEnvVar)
	if logPath == "" {
		if rd != "" {
			logPath = filepath.Join(rd, LogFile)
		} else {
			if wd != "" {
				logPath = filepath.Join(wd, LogFile)
			} else {
				logPath = LogFile
			}
		}
	}

	// 2. Open log file
	//nolint:gosec // Path is constructed from internal registry logic
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	var logCloser io.Closer
	var fileHandler slog.Handler

	if err == nil {
		logCloser = f
		fileHandler = slog.NewJSONHandler(f, &slog.HandlerOptions{
			Level: slog.LevelDebug, // File always gets full debug info
		})
	}

	// 3. Create console handler
	consoleHandler := &consoleHandler{
		w:     stderr,
		level: logLevel,
	}

	// 4. Combine handlers
	var handlers []slog.Handler
	if fileHandler != nil {
		handlers = append(handlers, fileHandler)
	}
	handlers = append(handlers, consoleHandler)

	multi := &multiHandler{
		handlers: handlers,
	}

	return slog.New(multi), logCloser, err
}

type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle implements the slog.Handler interface.
//
//nolint:gocritic // record is heavy but interface requires value
func (m *multiHandler) Handle(ctx context.Context, record slog.Record) error {
	for _, h := range m.handlers {
		if h.Enabled(ctx, record.Level) {
			if err := h.Handle(ctx, record); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		newHandlers[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: newHandlers}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	newHandlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		newHandlers[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: newHandlers}
}

type consoleHandler struct {
	w     io.Writer
	level *slog.LevelVar
	attrs []slog.Attr
}

func (c *consoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= c.level.Level()
}

// Handle implements the slog.Handler interface.
//
//nolint:gocritic // record is heavy but interface requires value
func (c *consoleHandler) Handle(_ context.Context, record slog.Record) error {
	// Clean output for the console
	switch {
	case record.Level >= slog.LevelError:
		_, _ = fmt.Fprintf(c.w, "Error: %s", record.Message)
	case record.Level >= slog.LevelWarn:
		_, _ = fmt.Fprintf(c.w, "Warning: %s", record.Message)
	default:
		_, _ = fmt.Fprint(c.w, record.Message)
	}

	// Show attributes added via WithAttrs
	for _, a := range c.attrs {
		c.formatAttr(a)
	}

	// Show attributes from the record
	record.Attrs(func(a slog.Attr) bool {
		c.formatAttr(a)
		return true
	})

	_, _ = fmt.Fprintln(c.w)
	return nil
}

func (c *consoleHandler) formatAttr(a slog.Attr) {
	switch a.Key {
	case "component":
		// Which part of jsm logged the message is for the log file, not the console
		return
	case "error", "err":
		_, _ = fmt.Fprintf(c.w, ": %v", a.Value)
	default:
		_, _ = fmt.Fprintf(c.w, " %s=%v", a.Key, a.Value)
	}
}

func (c *consoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &consoleHandler{
		w:     c.w,
		level: c.level,
		attrs: append(c.attrs, attrs...),
	}
}

func (c *consoleHandler) WithGroup(_ string) slog.Handler {
	// Grouping not deeply supported in this simple console output for now
	return c
}
