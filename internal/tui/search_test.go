/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/search"
)

func sampleResults() []search.Result {
	return []search.Result{
		{FilePath: "src/auth.go", StartLine: 1, EndLine: 10, Content: "func Login(user, pass string) error {\n\treturn validate(user, pass)\n}", Score: 0.95},
		{FilePath: "src/handler.go", StartLine: 5, EndLine: 15, Content: "func HandleLogin(w http.ResponseWriter, r *http.Request) {\n\terr := Login(r.Form)\n}", Score: 0.82},
		{FilePath: "src/db.go", StartLine: 1, EndLine: 8, Content: "func Connect(dsn string) (*DB, error) {\n\treturn sql.Open(\"postgres\", dsn)\n}", Score: 0.65},
		{FilePath: "test/auth_test.go", StartLine: 1, EndLine: 5, Content: "func TestLogin(t *testing.T) {\n\terr := Login(\"user\", \"pass\")\n}", Score: 0.40},
	}
}

func TestNewSearchModel(t *testing.T) {
	m := NewSearchModel("login")
	if m.GetQuery() != "login" {
		t.Errorf("expected query 'login', got %s", m.GetQuery())
	}
	if !m.IsLoading() {
		t.Error("expected loading to be true")
	}
	if len(m.GetResults()) != 0 {
		t.Error("expected no results initially")
	}
}

func TestNewSearchModelWithResults(t *testing.T) {
	results := sampleResults()
	m := NewSearchModelWithResults("login", results)
	if m.IsLoading() {
		t.Error("expected loading to be false")
	}
	if len(m.GetResults()) != 4 {
		t.Errorf("expected 4 results, got %d", len(m.GetResults()))
	}
}

func TestSearchModelSearchDoneMsg(t *testing.T) {
	m := NewSearchModel("login")
	results := sampleResults()

	updated, _ := m.Update(SearchDoneMsg{Results: results, Query: "login"})
	m = updated.(SearchModel)

	if m.IsLoading() {
		t.Error("expected loading to be false after SearchDoneMsg")
	}
	if len(m.GetResults()) != 4 {
		t.Errorf("expected 4 results, got %d", len(m.GetResults()))
	}
}

func TestSearchModelSearchDoneMsgWithError(t *testing.T) {
	m := NewSearchModel("test")

	updated, _ := m.Update(SearchDoneMsg{Err: errTest})
	m = updated.(SearchModel)

	if !m.HasError() {
		t.Error("expected error")
	}
}

var errTest = &testError{}

type testError struct{}

func (e *testError) Error() string { return "test error" }

func TestSearchModelNavigation(t *testing.T) {
	m := NewSearchModelWithResults("login", sampleResults())

	// Down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(SearchModel)
	if m.GetSelected() != 1 {
		t.Errorf("expected selected 1, got %d", m.GetSelected())
	}

	// Down again
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(SearchModel)
	if m.GetSelected() != 2 {
		t.Errorf("expected selected 2, got %d", m.GetSelected())
	}

	// Up
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(SearchModel)
	if m.GetSelected() != 1 {
		t.Errorf("expected selected 1, got %d", m.GetSelected())
	}
}

func TestSearchModelNavigationBounds(t *testing.T) {
	m := NewSearchModelWithResults("login", sampleResults())

	// Up at top stays at 0
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(SearchModel)
	if m.GetSelected() != 0 {
		t.Errorf("expected selected 0 at top, got %d", m.GetSelected())
	}

	// Navigate to last
	for i := 0; i < 10; i++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(SearchModel)
	}
	if m.GetSelected() != 3 {
		t.Errorf("expected selected 3 at bottom, got %d", m.GetSelected())
	}

	// Down at bottom stays
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(SearchModel)
	if m.GetSelected() != 3 {
		t.Errorf("expected selected 3 at bottom, got %d", m.GetSelected())
	}
}

func TestSearchModelVimNavigation(t *testing.T) {
	m := NewSearchModelWithResults("login", sampleResults())

	// j = down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(SearchModel)
	if m.GetSelected() != 1 {
		t.Errorf("expected selected 1 after 'j', got %d", m.GetSelected())
	}

	// k = up
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(SearchModel)
	if m.GetSelected() != 0 {
		t.Errorf("expected selected 0 after 'k', got %d", m.GetSelected())
	}
}

func TestSearchModelHomeEnd(t *testing.T) {
	m := NewSearchModelWithResults("login", sampleResults())

	// G = end
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = updated.(SearchModel)
	if m.GetSelected() != 3 {
		t.Errorf("expected selected 3 after 'G', got %d", m.GetSelected())
	}

	// g = home
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = updated.(SearchModel)
	if m.GetSelected() != 0 {
		t.Errorf("expected selected 0 after 'g', got %d", m.GetSelected())
	}
}

func TestSearchModelPreviewToggle(t *testing.T) {
	m := NewSearchModelWithResults("login", sampleResults())

	if m.preview {
		t.Error("preview should start disabled")
	}

	// Enter toggles preview
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(SearchModel)
	if !m.preview {
		t.Error("preview should be enabled after enter")
	}

	// Enter again toggles off
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(SearchModel)
	if m.preview {
		t.Error("preview should be disabled after second enter")
	}
}

func TestSearchModelQuit(t *testing.T) {
	m := NewSearchModelWithResults("login", sampleResults())

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(SearchModel)

	if !m.quitting {
		t.Error("expected quitting after 'q'")
	}
	if cmd == nil {
		t.Error("expected quit command")
	}
}

func TestSearchModelEscQuit(t *testing.T) {
	m := NewSearchModelWithResults("login", sampleResults())

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(SearchModel)

	if !m.quitting {
		t.Error("expected quitting after Esc")
	}
}

func TestSearchModelViewLoading(t *testing.T) {
	m := NewSearchModel("login")
	view := m.View()

	if !strings.Contains(view, "Searching") {
		t.Error("expected loading indicator")
	}
}

func TestSearchModelViewNoResults(t *testing.T) {
	m := NewSearchModelWithResults("xyz", nil)
	view := m.View()

	if !strings.Contains(view, "No results") {
		t.Error("expected 'No results' message")
	}
}

func TestSearchModelViewWithResults(t *testing.T) {
	m := NewSearchModelWithResults("login", sampleResults())
	view := m.View()

	if !strings.Contains(view, "src/auth.go") {
		t.Error("expected file path in results view")
	}
	if !strings.Contains(view, "0.95") {
		t.Error("expected score in results view")
	}
	if !strings.Contains(view, "login") {
		t.Error("expected query in results view")
	}
}

func TestSearchModelViewWithPreview(t *testing.T) {
	m := NewSearchModelWithResults("login", sampleResults())
	m.preview = true

	view := m.View()

	if !strings.Contains(view, "Preview") {
		t.Error("expected preview section")
	}
	if !strings.Contains(view, "func Login") {
		t.Error("expected code content in preview")
	}
}

func TestSearchModelViewError(t *testing.T) {
	m := NewSearchModel("test")
	m.loading = false
	m.err = errTest

	view := m.View()
	if !strings.Contains(view, "Error") {
		t.Error("expected error message in view")
	}
}

func TestSearchModelViewQuit(t *testing.T) {
	m := NewSearchModelWithResults("login", sampleResults())
	m.quitting = true

	view := m.View()
	if view != "" {
		t.Errorf("expected empty view on quit, got: %s", view)
	}
}

func TestSearchModelWindowSize(t *testing.T) {
	m := NewSearchModelWithResults("login", sampleResults())

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(SearchModel)

	if m.width != 120 || m.height != 40 {
		t.Errorf("expected 120x40, got %dx%d", m.width, m.height)
	}
}

func TestSearchModelViewHelp(t *testing.T) {
	m := NewSearchModelWithResults("login", sampleResults())
	view := m.View()

	if !strings.Contains(view, "navigate") {
		t.Error("expected navigation help text")
	}
	if !strings.Contains(view, "preview") {
		t.Error("expected preview help text")
	}
	if !strings.Contains(view, "quit") {
		t.Error("expected quit help text")
	}
}

func TestSearchModelInit(t *testing.T) {
	m := NewSearchModel("test")
	cmd := m.Init()
	if cmd != nil {
		t.Error("expected nil Init command")
	}
}

func TestSearchModelResultCount(t *testing.T) {
	m := NewSearchModelWithResults("login", sampleResults())
	view := m.View()

	if !strings.Contains(view, "4 results") {
		t.Error("expected result count in header")
	}
}
