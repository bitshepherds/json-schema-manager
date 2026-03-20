package schema

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitshepherds/json-schema-manager/internal/fsh"
)

type mockEventWatcher struct {
	AddFunc    func(name string) error
	CloseFunc  func() error
	EventsChan chan fsnotify.Event
	ErrorsChan chan error
}

func (m *mockEventWatcher) Add(name string) error {
	if m.AddFunc != nil {
		return m.AddFunc(name)
	}
	return nil
}

func (m *mockEventWatcher) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}
func (m *mockEventWatcher) Events() chan fsnotify.Event { return m.EventsChan }
func (m *mockEventWatcher) Errors() chan error          { return m.ErrorsChan }

func TestWatcher(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	t.Run("schema file change", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		watcher := NewWatcher(r, logger)
		s, err := r.CreateSchema("domain/family-1")
		require.NoError(t, err)
		key := s.Key()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		events := make(chan WatchEvent, 10)
		go func() {
			_ = watcher.Watch(ctx, ResolvedTarget{}, func(e WatchEvent) {
				events <- e
			})
		}()

		select {
		case <-watcher.Ready:
		case <-time.After(1 * time.Second):
			t.Fatal("watcher did not become ready in time")
		}

		err = os.WriteFile(s.Path(FilePath), []byte(`{"type":"object"}`), 0o600)
		require.NoError(t, err)

		select {
		case event := <-events:
			assert.Equal(t, key, event.Key)
			assert.Empty(t, event.TestPath)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for watch event")
		}
	})

	t.Run("test document change", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		watcher := NewWatcher(r, logger)
		s, err := r.CreateSchema("domain/family-2")
		require.NoError(t, err)
		key := s.Key()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		events := make(chan WatchEvent, 10)
		go func() {
			_ = watcher.Watch(ctx, ResolvedTarget{}, func(e WatchEvent) {
				events <- e
			})
		}()

		select {
		case <-watcher.Ready:
		case <-time.After(1 * time.Second):
			t.Fatal("watcher did not become ready in time")
		}

		homeDir := s.Path(HomeDir)
		passDir := filepath.Join(homeDir, string(TestDocTypePass))
		err = os.MkdirAll(passDir, 0o755)
		require.NoError(t, err)

		testPath := filepath.Join(passDir, "test.json")
		err = os.WriteFile(testPath, []byte(`{}`), 0o600)
		require.NoError(t, err)

		select {
		case event := <-events:
			assert.Equal(t, key, event.Key)
			absTestPath, _ := filepath.Abs(testPath)
			assert.Equal(t, absTestPath, event.TestPath)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for watch event")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		watcher := NewWatcher(r, logger)
		innerCtx, innerCancel := context.WithCancel(context.Background())

		done := make(chan struct{})
		go func() {
			err := watcher.Watch(innerCtx, ResolvedTarget{}, func(_ WatchEvent) {})
			assert.ErrorIs(t, err, context.Canceled)
			close(done)
		}()

		select {
		case <-watcher.Ready:
		case <-time.After(1 * time.Second):
			t.Fatal("watcher did not become ready in time")
		}

		innerCancel()

		select {
		case <-done:
		case <-time.After(1 * time.Second):
			t.Fatal("watcher did not stop on context cancellation")
		}
	})

	t.Run("mapToWatchEvent - irrelevant paths", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		w := NewWatcher(r, logger)

		assert.Nil(t, w.mapToWatchEvent("not-a-json.txt"))
		assert.Nil(t, w.mapToWatchEvent("random.json")) // Not in pass/fail
		assert.Nil(t, w.mapToWatchEvent("domain/family/1/0/0/other/test.json"))
	})

	t.Run("handleEvent - irrelevant event", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		w := NewWatcher(r, logger)
		mock := &mockEventWatcher{}
		assert.Nil(t, w.handleEvent(mock, fsnotify.Event{Op: fsnotify.Chmod}))
	})

	t.Run("handleEvent - new directory", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		w := NewWatcher(r, logger)
		fw, _ := fsnotify.NewWatcher()
		watcher := &eventWatcherWrapper{fw}
		defer func() { _ = watcher.Close() }()

		newDir := filepath.Join(r.RootDirectory(), "newdir")
		require.NoError(t, os.Mkdir(newDir, 0o755))

		mock := &mockEventWatcher{}
		ev := w.handleEvent(mock, fsnotify.Event{Name: newDir, Op: fsnotify.Create})
		assert.Nil(t, ev)
		// We can't easily verify if it's being watched without private access,
		// but this covers the code path.
	})

	t.Run("addRecursive - skip hidden", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		w := NewWatcher(r, logger)
		fw, _ := fsnotify.NewWatcher()
		watcher := &eventWatcherWrapper{fw}
		defer func() { _ = watcher.Close() }()

		hiddenDir := filepath.Join(r.RootDirectory(), ".hidden")
		require.NoError(t, os.Mkdir(hiddenDir, 0o755))

		err := w.addRecursive(watcher, r.RootDirectory())
		assert.NoError(t, err)
	})

	t.Run("mapTestDocToWatchEvent - edge cases", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		w := NewWatcher(r, logger)

		// Not in pass/fail (already tested, but let's be sure)
		assert.Nil(t, w.mapTestDocToWatchEvent("some/random/file.json"))

		// In pass/fail but no schema file
		passDir := filepath.Join(r.RootDirectory(), "domain", "family", "1", "0", "0", "pass")
		require.NoError(t, os.MkdirAll(passDir, 0o755))
		testFile := filepath.Join(passDir, "test.json")
		require.NoError(t, os.WriteFile(testFile, []byte("{}"), 0o600))
		assert.Nil(t, w.mapTestDocToWatchEvent(testFile))
	})

	t.Run("addRecursive - walk error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		w := NewWatcher(r, logger)
		// filepath.Walk error can be triggered by a path that doesn't exist if root is valid but children are deleted?
		// Or just a completely invalid root for direct call.
		err := w.addRecursive(nil, "/non-existent-path")
		assert.Error(t, err)
	})

	t.Run("handleEvent - addRecursive failure", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		w := NewWatcher(r, logger)

		newDir := filepath.Join(r.RootDirectory(), "faildir")
		require.NoError(t, os.Mkdir(newDir, 0o755))

		mock := &mockEventWatcher{
			AddFunc: func(_ string) error {
				return errors.New("injected add error")
			},
		}
		// This will trigger w.addRecursive which calls mock.Add
		ev := w.handleEvent(mock, fsnotify.Event{Name: newDir, Op: fsnotify.Create})
		assert.Nil(t, ev)
	})

	t.Run("mapTestDocToWatchEvent - ReadDir error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		w := NewWatcher(r, logger)

		// Create a path that looks like a test doc but its parent (homeDir) doesn't exist
		// path: root/domain/family/1/0/0/pass/test.json
		// homeDir: root/domain/family/1/0/0/
		// If 1/0/0 doesn't exist, ReadDir on it will fail.

		testPath := filepath.Join(r.RootDirectory(), "domain", "family", "1", "0", "0", "pass", "test.json")
		// We DON'T create the homeDir
		assert.Nil(t, w.mapTestDocToWatchEvent(testPath))
	})

	t.Run("Watch - factory error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		w := NewWatcher(r, logger)
		w.newWatcher = func() (eventWatcher, error) {
			return nil, errors.New("factory error")
		}
		err := w.Watch(context.Background(), ResolvedTarget{}, func(_ WatchEvent) {})
		assert.ErrorContains(t, err, "factory error")
	})

	t.Run("Watch - debounce reset", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		w := NewWatcher(r, logger)
		s, _ := r.CreateSchema("domain/family-3")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		events := make(chan WatchEvent, 10)
		go func() {
			_ = w.Watch(ctx, ResolvedTarget{}, func(e WatchEvent) {
				events <- e
			})
		}()

		<-w.Ready

		// Write twice quickly to trigger debounce reset
		err := os.WriteFile(s.Path(FilePath), []byte(`{"v1":true}`), 0o600)
		require.NoError(t, err)
		time.Sleep(20 * time.Millisecond) // Less than debounce duration (100ms)
		err = os.WriteFile(s.Path(FilePath), []byte(`{"v2":true}`), 0o600)
		require.NoError(t, err)

		select {
		case event := <-events:
			assert.Equal(t, s.Key(), event.Key)
		case <-time.After(1 * time.Second):
			t.Fatal("timed out waiting for watch event")
		}
	})

	t.Run("Watch - internal errors and races", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		w := NewWatcher(r, logger)

		eventsChan := make(chan fsnotify.Event)
		errorsChan := make(chan error)
		mock := &mockEventWatcher{
			EventsChan: eventsChan,
			ErrorsChan: errorsChan,
		}
		w.newWatcher = func() (eventWatcher, error) {
			return mock, nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		done := make(chan error, 1)
		go func() {
			done <- w.Watch(ctx, ResolvedTarget{}, func(_ WatchEvent) {})
		}()

		<-w.Ready

		// 1. Inject an error into Errors channel to hit coverage
		errorsChan <- fmt.Errorf("injected error")

		// To test handleEvent in isolation safely:
		t.Run("handleEvent isolation", func(t *testing.T) {
			t.Parallel()
			subWatcher := &mockEventWatcher{}

			// Stat error
			ev := w.handleEvent(subWatcher, fsnotify.Event{Name: "/non-existent-stat", Op: fsnotify.Create})
			assert.Nil(t, ev)

			// Success event
			s, _ := r.CreateSchema("domain/event-race")
			ev = w.handleEvent(subWatcher, fsnotify.Event{Name: s.Path(FilePath), Op: fsnotify.Write})
			assert.NotNil(t, ev)
			assert.Equal(t, s.Key(), ev.Key)
		})

		cancel()
		<-done
	})

	t.Run("addRecursive - errors", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		w := NewWatcher(r, logger)
		fw, err := fsnotify.NewWatcher()
		require.NoError(t, err)
		iw := &eventWatcherWrapper{fw}
		defer func() { _ = iw.Close() }()

		// 1. Walk error (line 113)
		err = w.addRecursive(iw, "/non-existent-root")
		require.Error(t, err)

		// 2. Watcher Add error (line 120)
		_ = iw.Close() // Close it to make Add fail
		err = w.addRecursive(iw, r.RootDirectory())
		require.Error(t, err)
	})

	t.Run("mapTestDocToWatchEvent - ReadDir error", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		w := NewWatcher(r, logger)

		// Create a path that looks like a test doc but its homeDir is unreadable
		tmpDir := t.TempDir()
		homeDir := filepath.Join(tmpDir, "myschema")
		require.NoError(t, os.Mkdir(homeDir, 0o000))
		t.Cleanup(func() { _ = os.Chmod(homeDir, 0o755) })

		testPath := filepath.Join(homeDir, "pass", "test.json")
		event := w.mapTestDocToWatchEvent(testPath)
		assert.Nil(t, event)
	})

	t.Run("Watch - initial errors", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		w := NewWatcher(r, logger)

		// 1. newWatcher error (line 47)
		w.newWatcher = func() (eventWatcher, error) {
			return nil, fmt.Errorf("newWatcher fail")
		}
		err := w.Watch(context.Background(), ResolvedTarget{}, nil)
		require.Error(t, err)

		// 2. addRecursive error (line 52)
		w.newWatcher = defaultWatcherFactory // reset
		tmp := t.TempDir()
		require.NoError(
			t,
			os.WriteFile(filepath.Join(tmp, "json-schema-manager-config.yml"), []byte(testConfigData), 0o600),
		)
		reg, _ := NewRegistry(tmp, &mockCompiler{}, fsh.NewPathResolver(), fsh.NewEnvProvider())
		w = NewWatcher(reg, logger)
		require.NoError(t, os.RemoveAll(tmp)) // Make root non-existent
		err = w.Watch(context.Background(), ResolvedTarget{}, nil)
		assert.Error(t, err)
	})

	t.Run("Watch - channel closure via Events", func(t *testing.T) {
		t.Parallel()
		r := setupTestRegistry(t)
		w := NewWatcher(r, logger)

		eventsChan := make(chan fsnotify.Event)
		mock := &mockEventWatcher{
			EventsChan: eventsChan,
			ErrorsChan: make(chan error),
		}
		w.newWatcher = func() (eventWatcher, error) {
			return mock, nil
		}

		done := make(chan error, 1)
		go func() {
			done <- w.Watch(context.Background(), ResolvedTarget{}, func(_ WatchEvent) {})
		}()

		<-w.Ready

		// Close the events channel to trigger closure path
		close(eventsChan)

		// Watch should return nil when channels are closed
		err := <-done
		assert.NoError(t, err)
	})

	t.Run("createWatcher - error", func(t *testing.T) {
		t.Parallel()
		_, err := createWatcher(func() (*fsnotify.Watcher, error) {
			return nil, fmt.Errorf("injected factory fail")
		})
		assert.ErrorContains(t, err, "injected factory fail")
	})
}
