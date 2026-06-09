/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewWatchModel(t *testing.T) {
	m := NewWatchModel()
	if len(m.GetEvents()) != 0 {
		t.Error("expected no events initially")
	}
	if m.IsQuitting() {
		t.Error("expected not quitting initially")
	}
	if m.maxEvents != 50 {
		t.Errorf("expected maxEvents 50, got %d", m.maxEvents)
	}
}

func TestWatchModelInit(t *testing.T) {
	m := NewWatchModel()
	cmd := m.Init()
	if cmd == nil {
		t.Error("expected tick command from Init")
	}
}

func TestWatchModelQuit(t *testing.T) {
	m := NewWatchModel()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(WatchModel)

	if !m.IsQuitting() {
		t.Error("expected quitting after 'q'")
	}
	if cmd == nil {
		t.Error("expected quit command")
	}
}

func TestWatchModelEscQuit(t *testing.T) {
	m := NewWatchModel()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(WatchModel)

	if !m.IsQuitting() {
		t.Error("expected quitting after Esc")
	}
}

func TestWatchModelCtrlCQuit(t *testing.T) {
	m := NewWatchModel()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(WatchModel)

	if !m.IsQuitting() {
		t.Error("expected quitting after Ctrl+C")
	}
}

func TestWatchModelReceiveEvent(t *testing.T) {
	m := NewWatchModel()

	evt := WatchEventMsg(WatchEvent{
		Path:      "src/main.go",
		Op:        "write",
		Timestamp: time.Now(),
	})

	updated, _ := m.Update(evt)
	m = updated.(WatchModel)

	events := m.GetEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Path != "src/main.go" {
		t.Errorf("expected path src/main.go, got %s", events[0].Path)
	}
	if events[0].Op != "write" {
		t.Errorf("expected op write, got %s", events[0].Op)
	}
}

func TestWatchModelEventsNewFirst(t *testing.T) {
	m := NewWatchModel()

	// Add events in order
	for i := 0; i < 3; i++ {
		evt := WatchEventMsg(WatchEvent{
			Path:      strings.Repeat("x", i+1) + ".go",
			Op:        "write",
			Timestamp: time.Now(),
		})
		updated, _ := m.Update(evt)
		m = updated.(WatchModel)
	}

	events := m.GetEvents()
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	// Newest should be first
	if events[0].Path != "xxx.go" {
		t.Errorf("expected newest event first, got %s", events[0].Path)
	}
}

func TestWatchModelEventsMaxCap(t *testing.T) {
	m := NewWatchModel()
	m.maxEvents = 5

	for i := 0; i < 10; i++ {
		evt := WatchEventMsg(WatchEvent{
			Path:      "file.go",
			Op:        "write",
			Timestamp: time.Now(),
		})
		updated, _ := m.Update(evt)
		m = updated.(WatchModel)
	}

	if len(m.GetEvents()) != 5 {
		t.Errorf("expected events capped at 5, got %d", len(m.GetEvents()))
	}
}

func TestWatchModelClearEvents(t *testing.T) {
	m := NewWatchModel()

	// Add an event
	evt := WatchEventMsg(WatchEvent{Path: "a.go", Op: "write", Timestamp: time.Now()})
	updated, _ := m.Update(evt)
	m = updated.(WatchModel)

	if len(m.GetEvents()) != 1 {
		t.Fatal("expected 1 event before clear")
	}

	// Press 'c' to clear
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated.(WatchModel)

	if len(m.GetEvents()) != 0 {
		t.Error("expected events cleared")
	}
}

func TestWatchModelReceiveStats(t *testing.T) {
	m := NewWatchModel()

	stats := WatchStatsMsg(WatchStatsInfo{
		DirsWatched:    5,
		EventsReceived: 10,
		FilesIndexed:   100,
		ChunksTotal:    500,
		Errors:         2,
	})

	updated, _ := m.Update(stats)
	m = updated.(WatchModel)

	s := m.GetStats()
	if s.DirsWatched != 5 {
		t.Errorf("expected 5 dirs, got %d", s.DirsWatched)
	}
	if s.FilesIndexed != 100 {
		t.Errorf("expected 100 files, got %d", s.FilesIndexed)
	}
	if s.ChunksTotal != 500 {
		t.Errorf("expected 500 chunks, got %d", s.ChunksTotal)
	}
	if s.Errors != 2 {
		t.Errorf("expected 2 errors, got %d", s.Errors)
	}
}

func TestWatchModelWindowSize(t *testing.T) {
	m := NewWatchModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	m = updated.(WatchModel)

	if m.width != 100 || m.height != 50 {
		t.Errorf("expected 100x50, got %dx%d", m.width, m.height)
	}
}

func TestWatchModelTickReturnsTickCmd(t *testing.T) {
	m := NewWatchModel()

	_, cmd := m.Update(WatchTickMsg(time.Now()))

	if cmd == nil {
		t.Error("expected tick command after WatchTickMsg")
	}
}

func TestWatchModelViewNoEvents(t *testing.T) {
	m := NewWatchModel()
	view := m.View()

	if !strings.Contains(view, "Waiting for changes") {
		t.Error("expected 'Waiting for changes' message")
	}
	if !strings.Contains(view, "Watcher") {
		t.Error("expected Watcher header")
	}
}

func TestWatchModelViewWithEvents(t *testing.T) {
	m := NewWatchModel()

	evt := WatchEventMsg(WatchEvent{
		Path:      "src/auth.go",
		Op:        "write",
		Timestamp: time.Now(),
	})
	updated, _ := m.Update(evt)
	m = updated.(WatchModel)

	view := m.View()
	if !strings.Contains(view, "src/auth.go") {
		t.Error("expected file path in event log")
	}
	if !strings.Contains(view, "write") {
		t.Error("expected op in event log")
	}
}

func TestWatchModelViewWithErrors(t *testing.T) {
	m := NewWatchModel()

	stats := WatchStatsMsg(WatchStatsInfo{Errors: 3})
	updated, _ := m.Update(stats)
	m = updated.(WatchModel)

	view := m.View()
	if !strings.Contains(view, "Errors") {
		t.Error("expected Errors label when errors > 0")
	}
}

func TestWatchModelViewNoErrors(t *testing.T) {
	m := NewWatchModel()

	stats := WatchStatsMsg(WatchStatsInfo{Errors: 0})
	updated, _ := m.Update(stats)
	m = updated.(WatchModel)

	view := m.View()
	if strings.Contains(view, "Errors") {
		t.Error("expected no Errors label when errors == 0")
	}
}

func TestWatchModelViewHelp(t *testing.T) {
	m := NewWatchModel()
	view := m.View()

	if !strings.Contains(view, "clear") {
		t.Error("expected 'clear' in help")
	}
	if !strings.Contains(view, "quit") {
		t.Error("expected 'quit' in help")
	}
}

func TestWatchModelViewQuit(t *testing.T) {
	m := NewWatchModel()
	m.quitting = true

	view := m.View()
	if !strings.Contains(view, "stopped") {
		t.Error("expected 'stopped' message on quit")
	}
}

func TestWatchModelStatsRendering(t *testing.T) {
	m := NewWatchModel()

	stats := WatchStatsMsg(WatchStatsInfo{
		DirsWatched:    3,
		EventsReceived: 7,
		FilesIndexed:   42,
		ChunksTotal:    200,
	})
	updated, _ := m.Update(stats)
	m = updated.(WatchModel)

	view := m.View()
	if !strings.Contains(view, "42") {
		t.Error("expected files count in stats")
	}
	if !strings.Contains(view, "200") {
		t.Error("expected chunks count in stats")
	}
}
