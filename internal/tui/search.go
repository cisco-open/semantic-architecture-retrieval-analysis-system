/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/search"
)

// SearchDoneMsg signals that search results are ready.
type SearchDoneMsg struct {
	Results []search.Result
	Query   string
	Err     error
}

// SearchModel is the Bubble Tea model for displaying search results.
type SearchModel struct {
	query    string
	results  []search.Result
	selected int
	scroll   int
	err      error
	loading  bool
	width    int
	height   int
	quitting bool
	preview  bool // show content preview for selected result
}

// NewSearchModel creates a new search results model.
func NewSearchModel(query string) SearchModel {
	return SearchModel{
		query:   query,
		loading: true,
	}
}

// NewSearchModelWithResults creates a search model pre-loaded with results.
func NewSearchModelWithResults(query string, results []search.Result) SearchModel {
	return SearchModel{
		query:   query,
		results: results,
		loading: false,
	}
}

func (m SearchModel) Init() tea.Cmd {
	return nil
}

func (m SearchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case SearchDoneMsg:
		m.loading = false
		m.results = msg.Results
		m.query = msg.Query
		m.err = msg.Err
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.quitting = true
			return m, tea.Quit

		case "up", "k":
			if m.selected > 0 {
				m.selected--
				m.adjustScroll()
			}

		case "down", "j":
			if m.selected < len(m.results)-1 {
				m.selected++
				m.adjustScroll()
			}

		case "enter", "p":
			m.preview = !m.preview

		case "home", "g":
			m.selected = 0
			m.scroll = 0

		case "end", "G":
			if len(m.results) > 0 {
				m.selected = len(m.results) - 1
				m.adjustScroll()
			}
		}
	}
	return m, nil
}

func (m *SearchModel) adjustScroll() {
	visibleItems := m.visibleItems()
	if visibleItems <= 0 {
		visibleItems = 10
	}
	if m.selected < m.scroll {
		m.scroll = m.selected
	}
	if m.selected >= m.scroll+visibleItems {
		m.scroll = m.selected - visibleItems + 1
	}
}

func (m SearchModel) visibleItems() int {
	if m.height <= 0 {
		return 10
	}
	// Reserve lines for header, footer, and padding
	available := m.height - 8
	if m.preview {
		available = available / 2
	}
	if available < 3 {
		available = 3
	}
	return available
}

func (m SearchModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	if m.loading {
		b.WriteString(SpinnerStyle.Render("  Searching...") + "\n")
		return b.String()
	}

	if m.err != nil {
		b.WriteString(ErrorStyle.Render(fmt.Sprintf("  %s Error: %v", SymbolCross, m.err)) + "\n")
		return b.String()
	}

	if len(m.results) == 0 {
		b.WriteString(MutedStyle.Render("  No results found.") + "\n")
		b.WriteString("\n" + m.renderHelp())
		return b.String()
	}

	// Results list
	b.WriteString(m.renderResults())

	// Preview pane
	if m.preview && m.selected < len(m.results) {
		b.WriteString("\n")
		b.WriteString(m.renderPreview())
	}

	// Footer
	b.WriteString("\n" + m.renderHelp())

	return b.String()
}

func (m SearchModel) renderHeader() string {
	queryDisplay := FocusedStyle.Render(fmt.Sprintf("\"%s\"", m.query))
	count := MutedStyle.Render(fmt.Sprintf("(%d results)", len(m.results)))
	return fmt.Sprintf("  %s Search: %s %s", SymbolSearch, queryDisplay, count)
}

func (m SearchModel) renderResults() string {
	var b strings.Builder

	visible := m.visibleItems()
	end := m.scroll + visible
	if end > len(m.results) {
		end = len(m.results)
	}

	for i := m.scroll; i < end; i++ {
		r := m.results[i]

		cursor := "  "
		if i == m.selected {
			cursor = FocusedStyle.Render(SymbolArrow + " ")
		}

		// Score badge
		scoreStr := fmt.Sprintf("%.2f", r.Score)
		var scoreBadge string
		if r.Score >= 0.8 {
			scoreBadge = SuccessStyle.Render(scoreStr)
		} else if r.Score >= 0.5 {
			scoreBadge = WarningStyle.Render(scoreStr)
		} else {
			scoreBadge = MutedStyle.Render(scoreStr)
		}

		// File path and line range
		filePath := FilePathStyle.Render(r.FilePath)
		lineRange := DimStyle.Render(fmt.Sprintf(":%d-%d", r.StartLine, r.EndLine))

		line := fmt.Sprintf("%s%s %s%s %s", cursor, scoreBadge, filePath, lineRange, m.contentSnippet(r))
		b.WriteString(line + "\n")
	}

	// Scroll indicator
	if len(m.results) > visible {
		pos := float64(m.scroll) / float64(len(m.results)-visible)
		indicator := DimStyle.Render(fmt.Sprintf("  ── %d/%d (%.0f%%) ──", m.selected+1, len(m.results), pos*100))
		b.WriteString(indicator + "\n")
	}

	return b.String()
}

func (m SearchModel) contentSnippet(r search.Result) string {
	content := strings.TrimSpace(r.Content)
	// Take first line, truncate if needed
	lines := strings.SplitN(content, "\n", 2)
	snippet := lines[0]

	maxLen := 60
	if m.width > 80 {
		maxLen = m.width - 40
	}
	if len(snippet) > maxLen {
		snippet = snippet[:maxLen-3] + "..."
	}

	return DimStyle.Render(snippet)
}

func (m SearchModel) renderPreview() string {
	if m.selected >= len(m.results) {
		return ""
	}

	r := m.results[m.selected]

	header := lipgloss.NewStyle().
		Foreground(ColorSecondary).
		Bold(true).
		Render(fmt.Sprintf("  Preview: %s:%d-%d", r.FilePath, r.StartLine, r.EndLine))

	content := r.Content
	lines := strings.Split(content, "\n")

	// Limit preview lines
	maxLines := 15
	if m.height > 0 {
		maxLines = (m.height - 10) / 2
		if maxLines < 5 {
			maxLines = 5
		}
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines = append(lines, DimStyle.Render("  ... (truncated)"))
	}

	// Add line numbers
	var numbered []string
	for i, line := range lines {
		lineNum := r.StartLine + i
		numStr := DimStyle.Render(fmt.Sprintf("  %4d │ ", lineNum))
		numbered = append(numbered, numStr+line)
	}

	return header + "\n" + strings.Join(numbered, "\n")
}

func (m SearchModel) renderHelp() string {
	return HelpKeyStyle.Render("  ↑↓") + HelpDescStyle.Render(" navigate  ") +
		HelpKeyStyle.Render("enter") + HelpDescStyle.Render(" preview  ") +
		HelpKeyStyle.Render("q") + HelpDescStyle.Render(" quit")
}

// GetSelected returns the currently selected result index.
func (m SearchModel) GetSelected() int { return m.selected }

// GetResults returns the search results.
func (m SearchModel) GetResults() []search.Result { return m.results }

// IsLoading returns whether the model is still loading.
func (m SearchModel) IsLoading() bool { return m.loading }

// GetQuery returns the search query.
func (m SearchModel) GetQuery() string { return m.query }

// HasError returns whether an error occurred.
func (m SearchModel) HasError() bool { return m.err != nil }
