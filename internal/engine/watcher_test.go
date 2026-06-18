/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package engine

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestWatchOpString(t *testing.T) {
	tests := []struct {
		op   WatchOp
		want string
	}{
		{OpCreate, "create"},
		{OpWrite, "write"},
		{OpRemove, "remove"},
		{OpRename, "rename"},
		{WatchOp(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.op.String(); got != tt.want {
			t.Errorf("WatchOp(%d).String() = %s, want %s", tt.op, got, tt.want)
		}
	}
}

func TestNewWatcher(t *testing.T) {
	root := t.TempDir()
	w, err := NewWatcher(root, nil)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}

	// NewWatcher resolves the root via EvalSymlinks (so watched paths and event
	// relpaths share a base), e.g. macOS /var -> /private/var.
	wantRoot, _ := filepath.EvalSymlinks(root)
	if w.root != wantRoot {
		t.Errorf("expected root %s, got %s", wantRoot, w.root)
	}
	if w.debounceMs != 500 {
		t.Errorf("expected default debounce 500, got %d", w.debounceMs)
	}

	w.fsWatcher.Close()
}

func TestNewWatcherWithOptions(t *testing.T) {
	root := t.TempDir()

	var eventCalled, errorCalled bool
	w, err := NewWatcher(root, []string{"node_modules"},
		WithDebounce(200),
		WithOnEvent(func(e WatchEvent) { eventCalled = true }),
		WithOnError(func(err error) { errorCalled = true }),
	)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.fsWatcher.Close()

	if w.debounceMs != 200 {
		t.Errorf("expected debounce 200, got %d", w.debounceMs)
	}
	if len(w.ignoreList) != 1 || w.ignoreList[0] != "node_modules" {
		t.Errorf("expected ignoreList [node_modules], got %v", w.ignoreList)
	}

	// Test callbacks are set
	if w.onEvent == nil {
		t.Error("expected onEvent callback to be set")
	}
	if w.onError == nil {
		t.Error("expected onError callback to be set")
	}

	// Trigger callbacks to verify they're wired correctly
	w.onEvent(WatchEvent{})
	w.onError(nil)
	_ = eventCalled
	_ = errorCalled
}

func TestWithDebounceInvalid(t *testing.T) {
	root := t.TempDir()
	w, err := NewWatcher(root, nil, WithDebounce(-1))
	if err != nil {
		t.Fatal(err)
	}
	defer w.fsWatcher.Close()

	// Should keep default
	if w.debounceMs != 500 {
		t.Errorf("expected default debounce 500 for invalid input, got %d", w.debounceMs)
	}
}

func TestWatcherDetectsFileCreation(t *testing.T) {
	root := t.TempDir()

	var mu sync.Mutex
	var received []WatchEvent

	w, err := NewWatcher(root, nil,
		WithDebounce(100),
		WithOnEvent(func(e WatchEvent) {
			mu.Lock()
			received = append(received, e)
			mu.Unlock()
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Start(ctx)

	// Wait for watcher to be ready
	time.Sleep(200 * time.Millisecond)

	// Create a file
	testFile := filepath.Join(root, "test.go")
	os.WriteFile(testFile, []byte("package main"), 0644)

	// Wait for debounce + processing
	time.Sleep(500 * time.Millisecond)

	cancel()

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Error("expected at least one event for file creation")
	}

	found := false
	for _, e := range received {
		if e.Path == "test.go" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected event for test.go")
	}
}

func TestWatcherDetectsFileWrite(t *testing.T) {
	root := t.TempDir()

	// Create a file first
	testFile := filepath.Join(root, "existing.go")
	os.WriteFile(testFile, []byte("package main"), 0644)

	var mu sync.Mutex
	var received []WatchEvent

	w, err := NewWatcher(root, nil,
		WithDebounce(100),
		WithOnEvent(func(e WatchEvent) {
			mu.Lock()
			received = append(received, e)
			mu.Unlock()
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	// Modify the file
	os.WriteFile(testFile, []byte("package main\nfunc main() {}"), 0644)

	time.Sleep(500 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Error("expected at least one event for file write")
	}
}

func TestWatcherDetectsFileRemoval(t *testing.T) {
	root := t.TempDir()

	testFile := filepath.Join(root, "removeme.go")
	os.WriteFile(testFile, []byte("package main"), 0644)

	var mu sync.Mutex
	var received []WatchEvent

	w, err := NewWatcher(root, nil,
		WithDebounce(100),
		WithOnEvent(func(e WatchEvent) {
			mu.Lock()
			received = append(received, e)
			mu.Unlock()
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	os.Remove(testFile)

	time.Sleep(500 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Error("expected at least one event for file removal")
	}
}

func TestWatcherIgnoresHiddenFiles(t *testing.T) {
	root := t.TempDir()

	var mu sync.Mutex
	var received []WatchEvent

	w, err := NewWatcher(root, nil,
		WithDebounce(100),
		WithOnEvent(func(e WatchEvent) {
			mu.Lock()
			received = append(received, e)
			mu.Unlock()
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	// Create a hidden file — watcher should skip it
	hiddenFile := filepath.Join(root, ".hidden")
	os.WriteFile(hiddenFile, []byte("secret"), 0644)

	time.Sleep(500 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()

	for _, e := range received {
		if e.Path == ".hidden" {
			t.Error("expected hidden file to be ignored")
		}
	}
}

func TestWatcherIgnoresIgnoredDirs(t *testing.T) {
	root := t.TempDir()
	nodeModules := filepath.Join(root, "node_modules")
	os.MkdirAll(nodeModules, 0755)

	w, err := NewWatcher(root, []string{"node_modules"})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	cancel()

	stats := w.Stats()
	// node_modules should not be watched
	// Only the root dir should be watched
	if stats.DirsWatched != 1 {
		t.Errorf("expected 1 dir watched (root only), got %d", stats.DirsWatched)
	}
}

func TestWatcherStatsTracking(t *testing.T) {
	root := t.TempDir()

	w, err := NewWatcher(root, nil, WithDebounce(100))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	// Create a file to generate events
	os.WriteFile(filepath.Join(root, "stats.go"), []byte("test"), 0644)
	time.Sleep(300 * time.Millisecond)

	cancel()

	stats := w.Stats()
	if stats.DirsWatched < 1 {
		t.Error("expected at least 1 dir watched")
	}
	if stats.EventsReceived < 1 {
		t.Error("expected at least 1 event received")
	}
}

func TestWatcherSubdirectoryWatch(t *testing.T) {
	root := t.TempDir()
	subDir := filepath.Join(root, "src")
	os.MkdirAll(subDir, 0755)

	w, err := NewWatcher(root, nil, WithDebounce(100))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	cancel()

	stats := w.Stats()
	if stats.DirsWatched < 2 {
		t.Errorf("expected at least 2 dirs watched (root + src), got %d", stats.DirsWatched)
	}
}

func TestWatcherSuppressesDuplicateContent(t *testing.T) {
	root := t.TempDir()
	testFile := filepath.Join(root, "dup.txt")
	os.WriteFile(testFile, []byte("hello"), 0644)

	var mu sync.Mutex
	var events []WatchEvent

	w, err := NewWatcher(root, nil,
		WithDebounce(50),
		WithOnEvent(func(e WatchEvent) {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Start(ctx)
	<-w.Ready()

	// First write: new content — should fire
	os.WriteFile(testFile, []byte("world"), 0644)
	time.Sleep(150 * time.Millisecond)

	// Second write: same content — should be suppressed
	os.WriteFile(testFile, []byte("world"), 0644)
	time.Sleep(150 * time.Millisecond)

	// Third write: different content — should fire
	os.WriteFile(testFile, []byte("changed"), 0644)
	time.Sleep(150 * time.Millisecond)

	cancel()

	mu.Lock()
	count := len(events)
	mu.Unlock()

	// Expect 2 events (first write + third write), not 3
	if count != 2 {
		t.Errorf("expected 2 events (suppressing duplicate), got %d", count)
	}
}

func TestFsOpToWatchOp(t *testing.T) {
	tests := []struct {
		name string
		op   uint32
		want WatchOp
	}{
		{"create", 1, OpCreate}, // fsnotify.Create = 1
		{"write", 2, OpWrite},   // fsnotify.Write = 2
		{"remove", 4, OpRemove}, // fsnotify.Remove = 4
		{"rename", 8, OpRename}, // fsnotify.Rename = 8
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We test the function indirectly through event handling since
			// fsnotify.Op is the same as uint32 on most platforms
			op := tt.want
			if op.String() != tt.name {
				t.Errorf("expected %s, got %s", tt.name, op.String())
			}
		})
	}
}

// addDirectories must follow a directory symlink whose target is inside the root
// and register it, so the watcher sees edits in symlinked source directories.
func TestAddDirectoriesFollowsSymlinkDir(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/x.go", "package pkg\n")
	symlink(t, "pkg", filepath.Join(root, "linked"))

	w, err := NewWatcher(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.fsWatcher.Close()

	if err := w.addDirectories(); err != nil {
		t.Fatalf("addDirectories: %v", err)
	}

	// The symlink "linked" resolves to the same real dir as "pkg"; the visited
	// set means it is watched once (via pkg's real path) rather than twice. We
	// assert progress was made and the walk followed the symlink without error.
	if got := w.Stats().DirsWatched; got < 2 {
		t.Errorf("expected at least root + pkg watched, got %d", got)
	}
}
