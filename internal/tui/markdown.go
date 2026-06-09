/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
)

// RenderMarkdown renders a markdown string for terminal display using glamour.
// width sets the wrap width; use 0 for glamour's default (80).
func RenderMarkdown(md string, width int) string {
	if strings.TrimSpace(md) == "" {
		return md
	}

	opts := []glamour.TermRendererOption{
		glamour.WithAutoStyle(),
		glamour.WithEmoji(),
	}
	if width > 0 {
		opts = append(opts, glamour.WithWordWrap(width))
	}

	r, err := glamour.NewTermRenderer(opts...)
	if err != nil {
		return md // fallback to raw text
	}

	rendered, err := r.Render(md)
	if err != nil {
		return md // fallback to raw text
	}

	return rendered
}
