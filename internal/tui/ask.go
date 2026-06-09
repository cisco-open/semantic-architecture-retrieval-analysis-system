/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// AskStreamChunkMsg delivers a streaming LLM response chunk to the TUI.
type AskStreamChunkMsg struct {
	Content string
	Done    bool
	Err     error
}

// AskModel is the Bubble Tea model for the ask command's streaming display.
type AskModel struct {
	query    string
	response *strings.Builder
	err      error
	loading  bool
	done     bool
	quitting bool
	width    int
	height   int
	scroll   int
	stylize  bool
}

// NewAskModel creates a new ask TUI model.
// Markdown rendering in the TUI is enabled by default (human-facing).
func NewAskModel(query string) AskModel {
	return AskModel{
		query:    query,
		response: &strings.Builder{},
		loading:  true,
		stylize:  true,
	}
}

// NewAskModelWithStyle creates a new ask TUI model with explicit stylize control.
func NewAskModelWithStyle(query string, stylize bool) AskModel {
	return AskModel{
		query:    query,
		response: &strings.Builder{},
		loading:  true,
		stylize:  stylize,
	}
}

func (m AskModel) Init() tea.Cmd {
	return nil
}

func (m AskModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			if m.scroll > 0 {
				m.scroll--
			}
		case "down", "j":
			m.scroll++
		case "home", "g":
			m.scroll = 0
		}

	case AskStreamChunkMsg:
		if msg.Err != nil {
			m.err = msg.Err
			m.loading = false
			m.done = true
			return m, nil
		}
		if msg.Done {
			m.loading = false
			m.done = true
			return m, nil
		}
		m.response.WriteString(msg.Content)
		m.loading = false
		return m, nil
	}

	return m, nil
}

func (m AskModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Header
	b.WriteString(TitleStyle.Render("  "+SymbolBrain+" Ask") + " ")
	b.WriteString(FocusedStyle.Render("\""+m.query+"\"") + "\n\n")

	if m.err != nil {
		b.WriteString(ErrorStyle.Render("  "+SymbolCross+" Error: "+m.err.Error()) + "\n")
		b.WriteString("\n" + m.renderHelp())
		return b.String()
	}

	if m.loading && m.response.Len() == 0 {
		b.WriteString(SpinnerStyle.Render("  Thinking...") + "\n")
		return b.String()
	}

	// Response content with scrolling
	content := m.response.String()
	if m.stylize {
		renderWidth := m.width - 4 // account for indentation
		if renderWidth < 40 {
			renderWidth = 80
		}
		content = RenderMarkdown(content, renderWidth)
	}
	lines := strings.Split(content, "\n")

	visibleLines := m.visibleLines()
	if m.scroll > len(lines)-visibleLines {
		m.scroll = len(lines) - visibleLines
		if m.scroll < 0 {
			m.scroll = 0
		}
	}

	end := m.scroll + visibleLines
	if end > len(lines) {
		end = len(lines)
	}

	for i := m.scroll; i < end; i++ {
		b.WriteString("  " + lines[i] + "\n")
	}

	// Status
	if !m.done {
		b.WriteString("\n" + SpinnerStyle.Render("  ▍") + "\n")
	} else {
		b.WriteString("\n" + DimStyle.Render("  ── end ──") + "\n")
	}

	b.WriteString("\n" + m.renderHelp())

	return b.String()
}

func (m AskModel) visibleLines() int {
	if m.height <= 0 {
		return 30
	}
	v := m.height - 8
	if v < 5 {
		v = 5
	}
	return v
}

func (m AskModel) renderHelp() string {
	return HelpKeyStyle.Render("  ↑↓") + HelpDescStyle.Render(" scroll  ") +
		HelpKeyStyle.Render("q") + HelpDescStyle.Render(" quit")
}

// GetResponse returns the full response text so far.
func (m AskModel) GetResponse() string { return m.response.String() }

// IsDone returns whether the response is complete.
func (m AskModel) IsDone() bool { return m.done }

// IsLoading returns whether still waiting for first chunk.
func (m AskModel) IsLoading() bool { return m.loading }

// HasError returns whether an error occurred.
func (m AskModel) HasError() bool { return m.err != nil }

// GetQuery returns the query.
func (m AskModel) GetQuery() string { return m.query }
