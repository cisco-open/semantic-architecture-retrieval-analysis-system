/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// WatchEvent is a display-friendly file change event.
type WatchEvent struct {
	Path      string
	Op        string
	Timestamp time.Time
}

// WatchStatsInfo holds stats for the watch dashboard.
type WatchStatsInfo struct {
	DirsWatched    int
	EventsReceived int
	FilesIndexed   int
	ChunksTotal    int
	Errors         int
	LastEvent      time.Time
}

// WatchTickMsg triggers periodic UI refresh.
type WatchTickMsg time.Time

// WatchEventMsg delivers a new file event to the TUI.
type WatchEventMsg WatchEvent

// WatchStatsMsg delivers updated stats to the TUI.
type WatchStatsMsg WatchStatsInfo

// WatchModel is the Bubble Tea model for the watch dashboard.
type WatchModel struct {
	events    []WatchEvent
	stats     WatchStatsInfo
	width     int
	height    int
	quitting  bool
	maxEvents int
	startTime time.Time
}

// NewWatchModel creates a new watch dashboard model.
func NewWatchModel() WatchModel {
	return WatchModel{
		maxEvents: 50,
		startTime: time.Now(),
	}
}

func (m WatchModel) Init() tea.Cmd {
	return tickCmd()
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return WatchTickMsg(t)
	})
}

func (m WatchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		case "c":
			m.events = nil
			return m, nil
		}

	case WatchTickMsg:
		return m, tickCmd()

	case WatchEventMsg:
		e := WatchEvent(msg)
		m.events = append([]WatchEvent{e}, m.events...)
		if len(m.events) > m.maxEvents {
			m.events = m.events[:m.maxEvents]
		}
		return m, nil

	case WatchStatsMsg:
		m.stats = WatchStatsInfo(msg)
		return m, nil
	}

	return m, nil
}

func (m WatchModel) View() string {
	if m.quitting {
		return SuccessStyle.Render(SymbolCheck+" Watcher stopped.") + "\n"
	}

	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Stats bar
	b.WriteString(m.renderStats())
	b.WriteString("\n\n")

	// Event log
	b.WriteString(m.renderEventLog())

	// Footer
	b.WriteString("\n" + m.renderHelp())

	return b.String()
}

func (m WatchModel) renderHeader() string {
	uptime := time.Since(m.startTime).Truncate(time.Second)
	title := TitleStyle.Render(fmt.Sprintf("  %s SARAS Watcher", SymbolGear))
	uptimeStr := MutedStyle.Render(fmt.Sprintf("  uptime: %s", uptime))
	return title + uptimeStr
}

func (m WatchModel) renderStats() string {
	var parts []string

	parts = append(parts, statsItem("Dirs", fmt.Sprintf("%d", m.stats.DirsWatched), ColorSecondary))
	parts = append(parts, statsItem("Events", fmt.Sprintf("%d", m.stats.EventsReceived), ColorSecondary))
	parts = append(parts, statsItem("Files", fmt.Sprintf("%d", m.stats.FilesIndexed), ColorSuccess))
	parts = append(parts, statsItem("Chunks", fmt.Sprintf("%d", m.stats.ChunksTotal), ColorSuccess))

	if m.stats.Errors > 0 {
		parts = append(parts, statsItem("Errors", fmt.Sprintf("%d", m.stats.Errors), ColorError))
	}

	return "  " + strings.Join(parts, "  "+DimStyle.Render("|")+"  ")
}

func statsItem(label, value string, color lipgloss.Color) string {
	return MutedStyle.Render(label+": ") + lipgloss.NewStyle().Foreground(color).Bold(true).Render(value)
}

func (m WatchModel) renderEventLog() string {
	var b strings.Builder

	header := FocusedStyle.Render("  Recent Events")
	b.WriteString(header + "\n")

	if len(m.events) == 0 {
		b.WriteString(MutedStyle.Render("  Waiting for changes...") + "\n")
		return b.String()
	}

	maxVisible := 20
	if m.height > 0 {
		maxVisible = m.height - 10
		if maxVisible < 5 {
			maxVisible = 5
		}
	}

	count := len(m.events)
	if count > maxVisible {
		count = maxVisible
	}

	for i := 0; i < count; i++ {
		e := m.events[i]
		b.WriteString(m.renderEvent(e) + "\n")
	}

	if len(m.events) > maxVisible {
		b.WriteString(DimStyle.Render(fmt.Sprintf("  ... and %d more", len(m.events)-maxVisible)) + "\n")
	}

	return b.String()
}

func (m WatchModel) renderEvent(e WatchEvent) string {
	ts := DimStyle.Render(e.Timestamp.Format("15:04:05"))

	var opStyle lipgloss.Style
	switch e.Op {
	case "create":
		opStyle = SuccessStyle
	case "write":
		opStyle = lipgloss.NewStyle().Foreground(ColorSecondary)
	case "remove":
		opStyle = ErrorStyle
	case "rename":
		opStyle = WarningStyle
	default:
		opStyle = MutedStyle
	}

	op := opStyle.Render(fmt.Sprintf("%-7s", e.Op))
	path := FilePathStyle.Render(e.Path)

	return fmt.Sprintf("  %s %s %s", ts, op, path)
}

func (m WatchModel) renderHelp() string {
	return HelpKeyStyle.Render("  c") + HelpDescStyle.Render(" clear  ") +
		HelpKeyStyle.Render("q") + HelpDescStyle.Render(" quit")
}

// GetEvents returns the event log.
func (m WatchModel) GetEvents() []WatchEvent { return m.events }

// GetStats returns the current stats.
func (m WatchModel) GetStats() WatchStatsInfo { return m.stats }

// IsQuitting returns whether the model is quitting.
func (m WatchModel) IsQuitting() bool { return m.quitting }
