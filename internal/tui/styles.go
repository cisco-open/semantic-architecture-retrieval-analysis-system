/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Color palette
	ColorPrimary   = lipgloss.Color("#7C3AED") // violet
	ColorSecondary = lipgloss.Color("#06B6D4") // cyan
	ColorSuccess   = lipgloss.Color("#10B981") // emerald
	ColorWarning   = lipgloss.Color("#F59E0B") // amber
	ColorError     = lipgloss.Color("#EF4444") // red
	ColorMuted     = lipgloss.Color("#6B7280") // gray
	ColorText      = lipgloss.Color("#E5E7EB") // light gray
	ColorDim       = lipgloss.Color("#4B5563") // dim gray
	ColorHighlight = lipgloss.Color("#A78BFA") // light violet
	ColorBg        = lipgloss.Color("#1F2937") // dark bg

	// Title styles
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			MarginBottom(1)

	SubtitleStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Italic(true)

	// Status styles
	SuccessStyle = lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorError).
			Bold(true)

	WarningStyle = lipgloss.NewStyle().
			Foreground(ColorWarning)

	MutedStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	DimStyle = lipgloss.NewStyle().
			Foreground(ColorDim)

	// Input styles
	FocusedStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)

	BlurredStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	CursorStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary)

	// Box styles
	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorDim).
			Padding(1, 2)

	HighlightBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorPrimary).
				Padding(1, 2)

	// List item styles
	ActiveItemStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)

	ItemStyle = lipgloss.NewStyle().
			Foreground(ColorText)

	// Progress styles
	ProgressStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary)

	// Code/file path styles
	FilePathStyle = lipgloss.NewStyle().
			Foreground(ColorHighlight)

	CodeStyle = lipgloss.NewStyle().
			Foreground(ColorText).
			Background(ColorBg).
			Padding(0, 1)

	// Spinner
	SpinnerStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary)

	// Badge / tag
	BadgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(ColorPrimary).
			Padding(0, 1).
			Bold(true)

	// Help / key binding
	HelpKeyStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true)

	HelpDescStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// Branding
	LogoText = `
 ╔═══╗╔═══╗╔═══╗╔═══╗╔═══╗
 ║ S ║║ A ║║ R ║║ A ║║ S ║
 ╚═══╝╚═══╝╚═══╝╚═══╝╚═══╝`

	LogoStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)

	TaglineStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Italic(true).
			MarginBottom(1)
)

// Symbols used across TUI views.
const (
	SymbolCheck    = "✓"
	SymbolCross    = "✗"
	SymbolArrow    = "→"
	SymbolBullet   = "•"
	SymbolSpinner  = "⠋"
	SymbolStar     = "★"
	SymbolFolder   = "📁"
	SymbolFile     = "📄"
	SymbolSearch   = "🔍"
	SymbolBrain    = "🧠"
	SymbolGear     = "⚙"
	SymbolWarning  = "⚠"
	SymbolInfo     = "ℹ"
	SymbolQuestion = "?"
	SymbolDot      = "·"
)
