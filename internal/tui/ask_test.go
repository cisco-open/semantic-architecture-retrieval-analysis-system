/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewAskModel(t *testing.T) {
	m := NewAskModel("how does auth work?")
	if m.GetQuery() != "how does auth work?" {
		t.Errorf("expected query, got %s", m.GetQuery())
	}
	if !m.IsLoading() {
		t.Error("expected loading initially")
	}
	if m.IsDone() {
		t.Error("expected not done initially")
	}
	if m.GetResponse() != "" {
		t.Error("expected empty response initially")
	}
}

func TestAskModelInit(t *testing.T) {
	m := NewAskModel("test")
	cmd := m.Init()
	if cmd != nil {
		t.Error("expected nil Init command")
	}
}

func TestAskModelStreamChunks(t *testing.T) {
	m := NewAskModel("test")

	// First chunk
	updated, _ := m.Update(AskStreamChunkMsg{Content: "Hello "})
	m = updated.(AskModel)

	if m.IsLoading() {
		t.Error("expected loading false after first chunk")
	}
	if m.GetResponse() != "Hello " {
		t.Errorf("expected 'Hello ', got '%s'", m.GetResponse())
	}

	// Second chunk
	updated, _ = m.Update(AskStreamChunkMsg{Content: "World"})
	m = updated.(AskModel)

	if m.GetResponse() != "Hello World" {
		t.Errorf("expected 'Hello World', got '%s'", m.GetResponse())
	}

	// Done chunk
	updated, _ = m.Update(AskStreamChunkMsg{Done: true})
	m = updated.(AskModel)

	if !m.IsDone() {
		t.Error("expected done after Done chunk")
	}
}

func TestAskModelStreamError(t *testing.T) {
	m := NewAskModel("test")

	updated, _ := m.Update(AskStreamChunkMsg{Err: errors.New("connection failed")})
	m = updated.(AskModel)

	if !m.HasError() {
		t.Error("expected error")
	}
	if !m.IsDone() {
		t.Error("expected done after error")
	}
}

func TestAskModelQuit(t *testing.T) {
	m := NewAskModel("test")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(AskModel)

	if !m.quitting {
		t.Error("expected quitting")
	}
	if cmd == nil {
		t.Error("expected quit command")
	}
}

func TestAskModelEscQuit(t *testing.T) {
	m := NewAskModel("test")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(AskModel)

	if !m.quitting {
		t.Error("expected quitting after Esc")
	}
}

func TestAskModelScroll(t *testing.T) {
	m := NewAskModel("test")

	// Down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(AskModel)
	if m.scroll != 1 {
		t.Errorf("expected scroll 1, got %d", m.scroll)
	}

	// Up
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(AskModel)
	if m.scroll != 0 {
		t.Errorf("expected scroll 0, got %d", m.scroll)
	}

	// Up at 0 stays
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(AskModel)
	if m.scroll != 0 {
		t.Errorf("expected scroll stays at 0, got %d", m.scroll)
	}
}

func TestAskModelVimScroll(t *testing.T) {
	m := NewAskModel("test")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(AskModel)
	if m.scroll != 1 {
		t.Errorf("expected scroll 1 after 'j', got %d", m.scroll)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(AskModel)
	if m.scroll != 0 {
		t.Errorf("expected scroll 0 after 'k', got %d", m.scroll)
	}
}

func TestAskModelHomeScroll(t *testing.T) {
	m := NewAskModel("test")
	m.scroll = 10

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = updated.(AskModel)
	if m.scroll != 0 {
		t.Errorf("expected scroll 0 after 'g', got %d", m.scroll)
	}
}

func TestAskModelViewLoading(t *testing.T) {
	m := NewAskModel("test")
	view := m.View()

	if !strings.Contains(view, "Thinking") {
		t.Error("expected 'Thinking' in loading view")
	}
	if !strings.Contains(view, "test") {
		t.Error("expected query in view")
	}
}

func TestAskModelViewWithResponse(t *testing.T) {
	m := NewAskModel("test")

	updated, _ := m.Update(AskStreamChunkMsg{Content: "The Login function handles auth."})
	m = updated.(AskModel)

	view := m.View()
	if !strings.Contains(view, "Login") {
		t.Error("expected response content in view")
	}
}

func TestAskModelViewDone(t *testing.T) {
	m := NewAskModel("test")

	updated, _ := m.Update(AskStreamChunkMsg{Content: "answer"})
	m = updated.(AskModel)
	updated, _ = m.Update(AskStreamChunkMsg{Done: true})
	m = updated.(AskModel)

	view := m.View()
	if !strings.Contains(view, "end") {
		t.Error("expected 'end' marker in done view")
	}
}

func TestAskModelViewError(t *testing.T) {
	m := NewAskModel("test")

	updated, _ := m.Update(AskStreamChunkMsg{Err: errors.New("boom")})
	m = updated.(AskModel)

	view := m.View()
	if !strings.Contains(view, "Error") {
		t.Error("expected 'Error' in error view")
	}
	if !strings.Contains(view, "boom") {
		t.Error("expected error message in view")
	}
}

func TestAskModelViewQuit(t *testing.T) {
	m := NewAskModel("test")
	m.quitting = true

	view := m.View()
	if view != "" {
		t.Errorf("expected empty view on quit, got: %s", view)
	}
}

func TestAskModelViewHelp(t *testing.T) {
	m := NewAskModel("test")

	updated, _ := m.Update(AskStreamChunkMsg{Content: "text"})
	m = updated.(AskModel)

	view := m.View()
	if !strings.Contains(view, "scroll") {
		t.Error("expected 'scroll' in help")
	}
	if !strings.Contains(view, "quit") {
		t.Error("expected 'quit' in help")
	}
}

func TestAskModelWindowSize(t *testing.T) {
	m := NewAskModel("test")

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(AskModel)

	if m.width != 100 || m.height != 40 {
		t.Errorf("expected 100x40, got %dx%d", m.width, m.height)
	}
}
