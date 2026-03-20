package schema

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatchEvent describes a file change event in the registry.
// Either the user changed a schema file, in which case we will run the
// tests for that schema matching the test scope, or the user changed a test
// document, in which case we will rerun the single test with its schema.
type WatchEvent struct {
	Key      Key    // The Key of the schema that will be tested
	TestPath string // If set, only this specific test document changed
}

// eventWatcher is an interface that masks fsnotify.Watcher, allowing us to mock it in tests.
type eventWatcher interface {
	Add(name string) error
	Close() error
	Events() chan fsnotify.Event
	Errors() chan error
}

// eventWatcherWrapper wraps fsnotify.Watcher to implement the eventWatcher interface.
type eventWatcherWrapper struct {
	*fsnotify.Watcher
}

func (w *eventWatcherWrapper) Events() chan fsnotify.Event { return w.Watcher.Events }
func (w *eventWatcherWrapper) Errors() chan error          { return w.Watcher.Errors }

// eventWatcherFactory is a function that returns a new eventWatcher.
type eventWatcherFactory func() (eventWatcher, error)

func defaultWatcherFactory() (eventWatcher, error) {
	return createWatcher(fsnotify.NewWatcher)
}

func createWatcher(newFn func() (*fsnotify.Watcher, error)) (eventWatcher, error) {
	w, err := newFn()
	if err != nil {
		return nil, err
	}
	return &eventWatcherWrapper{w}, nil
}

// Watcher monitors the registry for file changes and triggers validation.
type Watcher struct {
	registry *Registry
	logger   *slog.Logger
	Ready    chan struct{}

	newWatcher eventWatcherFactory
}

// NewWatcher creates a new Watcher for the given registry.
func NewWatcher(r *Registry, logger *slog.Logger) *Watcher {
	return &Watcher{
		registry:   r,
		logger:     logger.With("component", "watcher"),
		Ready:      make(chan struct{}),
		newWatcher: defaultWatcherFactory,
	}
}

// Watch starts monitoring the registry for changes. It calls the provided callback
// whenever a relevant change is detected. It blocks until the context is cancelled.
func (w *Watcher) Watch(ctx context.Context, target ResolvedTarget, callback func(WatchEvent)) error {
	watcher, err := w.newWatcher()
	if err != nil {
		return err
	}
	defer func() { _ = watcher.Close() }()

	targetPath := w.registry.RootDirectory()
	if target.Scope != nil && *target.Scope != "" {
		targetPath = filepath.Join(targetPath, filepath.FromSlash(string(*target.Scope)))
	} else if target.Key != nil {
		s, err := w.registry.GetSchemaByKey(*target.Key)
		if err == nil {
			targetPath = filepath.Dir(s.Path(FilePath))
		}
	}

	if err := w.addRecursive(watcher, targetPath); err != nil {
		return err
	}

	w.logger.Info("Watching for changes", "target", targetPath)
	if w.Ready != nil {
		close(w.Ready)
	}

	return w.eventLoop(ctx, watcher, callback)
}

func (w *Watcher) eventLoop(ctx context.Context, watcher eventWatcher, callback func(WatchEvent)) error {
	var timer *time.Timer
	const debounceDuration = 100 * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-watcher.Errors():
			w.logger.Error("Watcher error", "error", err)
		case event, ok := <-watcher.Events():
			if !ok {
				return nil
			}
			if ev := w.handleEvent(watcher, event); ev != nil {
				if timer != nil {
					timer.Stop()
				}
				eventToCallback := *ev
				timer = time.AfterFunc(debounceDuration, func() {
					callback(eventToCallback)
				})
			}
		}
	}
}

// handleEvent processes a single fsnotify event. If it's a new directory, it adds it to the watcher.
// If it's a relevant file change, it returns a pointer to a WatchEvent.
func (w *Watcher) handleEvent(watcher eventWatcher, event fsnotify.Event) *WatchEvent {
	if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
		return nil
	}

	if event.Has(fsnotify.Create) {
		info, err := os.Stat(event.Name)
		if err == nil && info.IsDir() {
			if err := w.addRecursive(watcher, event.Name); err != nil {
				w.logger.Error("Failed to watch new directory", "path", event.Name, "error", err)
			}
			return nil
		}
	}

	return w.mapToWatchEvent(event.Name)
}

// addRecursive adds the given path and all its subdirectories to the watcher.
func (w *Watcher) addRecursive(watcher eventWatcher, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if strings.HasPrefix(filepath.Base(path), ".") && path != root {
				return filepath.SkipDir
			}
			return watcher.Add(path)
		}
		return nil
	})
}

// mapToWatchEvent maps a file path to a WatchEvent. Returns nil if the file is not relevant.
func (w *Watcher) mapToWatchEvent(path string) *WatchEvent {
	if strings.HasSuffix(path, SchemaSuffix) {
		key, err := w.registry.KeyFromSchemaPath(path)
		if err == nil {
			return &WatchEvent{Key: key}
		}
	}

	if filepath.Ext(path) == ".json" && !strings.HasSuffix(path, SchemaSuffix) {
		return w.mapTestDocToWatchEvent(path)
	}

	return nil
}

func (w *Watcher) mapTestDocToWatchEvent(path string) *WatchEvent {
	dir := filepath.Dir(path)
	parentDirName := filepath.Base(dir)
	if parentDirName != string(TestDocTypePass) && parentDirName != string(TestDocTypeFail) {
		return nil
	}

	homeDir := filepath.Dir(dir)
	entries, err := os.ReadDir(homeDir)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), SchemaSuffix) {
			schemaPath := filepath.Join(homeDir, entry.Name())
			key, err := w.registry.KeyFromSchemaPath(schemaPath)
			if err == nil {
				return &WatchEvent{Key: key, TestPath: path}
			}
		}
	}
	return nil
}
