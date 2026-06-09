/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cfg

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToJSON serializes the CFG as indented JSON.
func (c *CFG) ToJSON() (string, error) {
	out := struct {
		*CFG
		Paths []Path `json:"paths,omitempty"`
	}{CFG: c}
	if len(c.Blocks) > 0 {
		out.Paths = c.EnumeratePaths(0)
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ToMermaid renders the CFG as a Mermaid flowchart. The output can be pasted
// into any Mermaid renderer (GitHub markdown, Obsidian, mermaid.live).
func (c *CFG) ToMermaid() string {
	var b strings.Builder
	b.WriteString("flowchart TD\n")

	// Nodes
	for _, blk := range c.Blocks {
		shape := nodeShapeFor(blk.Kind)
		text := mermaidEscape(displayLabel(blk))
		fmt.Fprintf(&b, "    B%d%s%s%s\n", blk.ID, shape.open, text, shape.close)
	}

	// Style entry/exit/return distinctively.
	for _, blk := range c.Blocks {
		switch blk.Kind {
		case KindEntry:
			fmt.Fprintf(&b, "    style B%d fill:#d4edda,stroke:#155724,color:#000\n", blk.ID)
		case KindExit:
			fmt.Fprintf(&b, "    style B%d fill:#f8d7da,stroke:#721c24,color:#000\n", blk.ID)
		case KindReturn:
			fmt.Fprintf(&b, "    style B%d fill:#fff3cd,stroke:#856404,color:#000\n", blk.ID)
		case KindBranch, KindLoopHead, KindSwitch:
			fmt.Fprintf(&b, "    style B%d fill:#cce5ff,stroke:#004085,color:#000\n", blk.ID)
		}
	}

	// Edges
	for _, e := range c.Edges {
		arrow := "-->"
		if e.Back {
			arrow = "-.->"
		}
		if e.Label != "" {
			fmt.Fprintf(&b, "    B%d %s|%s| B%d\n", e.From, arrow, mermaidEscape(e.Label), e.To)
		} else {
			fmt.Fprintf(&b, "    B%d %s B%d\n", e.From, arrow, e.To)
		}
	}

	return b.String()
}

type mermaidShape struct{ open, close string }

func nodeShapeFor(k BlockKind) mermaidShape {
	switch k {
	case KindEntry, KindExit:
		return mermaidShape{"([", "])"} // stadium
	case KindBranch, KindLoopHead, KindSwitch:
		return mermaidShape{"{", "}"} // diamond
	case KindReturn, KindBreak, KindContinue:
		return mermaidShape{"[/", "/]"} // parallelogram
	case KindMerge:
		return mermaidShape{"((", "))"} // circle
	default:
		return mermaidShape{"[", "]"} // rectangle
	}
}

func displayLabel(blk *Block) string {
	if blk == nil {
		return ""
	}
	base := blk.Label
	if base == "" {
		base = string(blk.Kind)
	}
	if len(blk.Lines) > 0 {
		return fmt.Sprintf("L%d: %s", blk.Lines[0], base)
	}
	return base
}

// mermaidEscape escapes characters that break Mermaid label parsing.
func mermaidEscape(s string) string {
	// Mermaid is sensitive to quotes, brackets, and pipes inside labels.
	r := strings.NewReplacer(
		`"`, `&quot;`,
		`(`, `&#40;`,
		`)`, `&#41;`,
		`[`, `&#91;`,
		`]`, `&#93;`,
		`{`, `&#123;`,
		`}`, `&#125;`,
		`|`, `&#124;`,
		"\n", " ",
		"\r", " ",
	)
	return r.Replace(s)
}

// ---------------------------------------------------------------------------
// Plain-text rendering
// ---------------------------------------------------------------------------

// ToText renders the CFG and enumerated paths as a plain-text report
// suitable for human reading and LLM ingestion.
func (c *CFG) ToText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Control Flow Graph for %s\n", c.Function)
	fmt.Fprintf(&b, "File: %s:%d-%d\n", c.File, c.StartLine, c.EndLine)
	if c.Language != "" {
		fmt.Fprintf(&b, "Language: %s\n", c.Language)
	}
	fmt.Fprintf(&b, "\nBlocks (%d):\n", len(c.Blocks))
	for _, blk := range c.Blocks {
		lineInfo := ""
		if len(blk.Lines) > 0 {
			lineInfo = fmt.Sprintf("  [L%d]", blk.Lines[0])
		}
		fmt.Fprintf(&b, "  B%d %-10s %s%s\n", blk.ID, blk.Kind, blk.Label, lineInfo)
	}

	fmt.Fprintf(&b, "\nEdges (%d):\n", len(c.Edges))
	for _, e := range c.Edges {
		lbl := ""
		if e.Label != "" {
			lbl = " [" + e.Label + "]"
		}
		if e.Back {
			fmt.Fprintf(&b, "  B%d ~~> B%d%s  (back-edge)\n", e.From, e.To, lbl)
		} else {
			fmt.Fprintf(&b, "  B%d --> B%d%s\n", e.From, e.To, lbl)
		}
	}

	paths := c.EnumeratePaths(0)
	fmt.Fprintf(&b, "\nExecution paths (%d):\n", len(paths))
	for i, p := range paths {
		fmt.Fprintf(&b, "\n  Path %d:\n", i+1)
		fmt.Fprintf(&b, "    Blocks: %s\n", formatBlockList(p.Blocks))
		if len(p.Decisions) > 0 {
			fmt.Fprintf(&b, "    Decisions:\n")
			for _, d := range p.Decisions {
				fmt.Fprintf(&b, "      - %s\n", d)
			}
		} else {
			fmt.Fprintf(&b, "    Decisions: (linear, no branches)\n")
		}
	}

	if len(c.Notes) > 0 {
		fmt.Fprintf(&b, "\nNotes:\n")
		for _, n := range c.Notes {
			fmt.Fprintf(&b, "  - %s\n", n)
		}
	}

	return b.String()
}

func formatBlockList(blocks []int) string {
	parts := make([]string, len(blocks))
	for i, id := range blocks {
		parts[i] = fmt.Sprintf("B%d", id)
	}
	return strings.Join(parts, " → ")
}

// ToPathsText renders just the enumerated paths in a compact form, including
// the source lines visited by each. Useful as input to an LLM for test
// generation.
func (c *CFG) ToPathsText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Function: %s (%s:%d-%d)\n", c.Function, c.File, c.StartLine, c.EndLine)
	paths := c.EnumeratePaths(0)
	fmt.Fprintf(&b, "Total execution paths: %d\n", len(paths))
	for i, p := range paths {
		fmt.Fprintf(&b, "\n=== Path %d ===\n", i+1)
		if len(p.Decisions) > 0 {
			fmt.Fprintf(&b, "Decisions:\n")
			for _, d := range p.Decisions {
				fmt.Fprintf(&b, "  - %s\n", d)
			}
		} else {
			fmt.Fprintf(&b, "Decisions: linear (no branches)\n")
		}
		// Collect line ranges from blocks visited.
		fmt.Fprintf(&b, "Source lines visited:\n")
		for _, blkID := range p.Blocks {
			blk := c.blockByID(blkID)
			if blk == nil || blk.Kind == KindEntry || blk.Kind == KindExit || blk.Kind == KindMerge {
				continue
			}
			for j, ln := range blk.Lines {
				var code string
				if j < len(blk.Code) {
					code = strings.TrimSpace(blk.Code[j])
				}
				fmt.Fprintf(&b, "  L%d  %s\n", ln, code)
			}
		}
	}
	if len(c.Notes) > 0 {
		fmt.Fprintf(&b, "\nNotes:\n")
		for _, n := range c.Notes {
			fmt.Fprintf(&b, "  - %s\n", n)
		}
	}
	return b.String()
}
