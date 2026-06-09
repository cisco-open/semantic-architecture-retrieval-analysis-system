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

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// ProgressUpdateMsg reports indexing progress to the TUI.
type ProgressUpdateMsg struct {
	Current     int
	Total       int
	CurrentFile string
}

// ProgressDoneMsg signals that indexing has finished.
type ProgressDoneMsg struct {
	FilesIndexed  int
	ChunksCreated int
	Duration      time.Duration
	Err           error
}

// ProgressModel is a Bubble Tea model that displays indexing progress.
type ProgressModel struct {
	progress    progress.Model
	spinner     spinner.Model
	current     int
	total       int
	currentFile string
	done        bool
	err         error
	result      ProgressDoneMsg
	width       int
}

// NewProgressModel creates a new progress bar model.
func NewProgressModel() ProgressModel {
	p := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(50),
	)

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = SpinnerStyle

	return ProgressModel{
		progress: p,
		spinner:  s,
	}
}

// GetResult returns the final indexing result.
func (m ProgressModel) GetResult() ProgressDoneMsg {
	return m.result
}

func (m ProgressModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m ProgressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		pw := msg.Width - 10
		if pw > 80 {
			pw = 80
		}
		if pw < 20 {
			pw = 20
		}
		m.progress.Width = pw
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		}

	case ProgressUpdateMsg:
		m.current = msg.Current
		m.total = msg.Total
		m.currentFile = msg.CurrentFile
		var pct float64
		if msg.Total > 0 {
			pct = float64(msg.Current) / float64(msg.Total)
		}
		return m, m.progress.SetPercent(pct)

	case ProgressDoneMsg:
		m.done = true
		m.result = msg
		m.err = msg.Err
		return m, tea.Quit

	case progress.FrameMsg:
		pm, cmd := m.progress.Update(msg)
		m.progress = pm.(progress.Model)
		return m, cmd

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m ProgressModel) View() string {
	if m.done {
		if m.err != nil {
			return ErrorStyle.Render(fmt.Sprintf("\n  %s Indexing failed: %v\n", SymbolCross, m.err))
		}
		return SuccessStyle.Render(fmt.Sprintf("\n  %s Indexed %d files, %d chunks in %s\n",
			SymbolCheck, m.result.FilesIndexed, m.result.ChunksCreated,
			m.result.Duration.Truncate(time.Millisecond))) + "\n"
	}

	var b strings.Builder

	b.WriteString("\n")

	if m.total == 0 {
		// Scanning phase
		b.WriteString(fmt.Sprintf("  %s Scanning project files...\n", m.spinner.View()))
	} else {
		// Indexing phase
		b.WriteString(fmt.Sprintf("  %s Indexing files (%d/%d)\n\n",
			m.spinner.View(), m.current, m.total))
		b.WriteString("  " + m.progress.View() + "\n\n")

		if m.currentFile != "" {
			file := m.currentFile
			if len(file) > 60 {
				file = "..." + file[len(file)-57:]
			}
			b.WriteString(MutedStyle.Render(fmt.Sprintf("  %s %s", SymbolArrow, file)) + "\n")
		}
	}

	return b.String()
}
