/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc.
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package lang

import (
	"regexp"
	"strings"
)

func init() { Register(&MermaidParser{}) }

// MermaidParser extracts symbols from Mermaid diagram files.
type MermaidParser struct{}

func (p *MermaidParser) Name() string         { return "mermaid" }
func (p *MermaidParser) Extensions() []string { return []string{".mermaid", ".mmd"} }

func (p *MermaidParser) FlowHints() FlowHints {
	return FlowHints{
		CommentPrefixes: []string{"%%"},
	}
}

func (p *MermaidParser) IsTestFile(path string) bool {
	return false
}

var (
	// Diagram type declarations: graph TD, flowchart LR, sequenceDiagram, classDiagram, etc.
	mmDiagramPattern = regexp.MustCompile(`^\s*(graph|flowchart|sequenceDiagram|classDiagram|stateDiagram|stateDiagram-v2|erDiagram|gantt|pie|gitGraph|mindmap|timeline|quadrantChart|sankey-beta|xychart-beta|block-beta|kanban|architecture-beta|packet-beta|requirementDiagram|C4Context|C4Container|C4Component|C4Deployment|journey)\b(.*)`)
	// subgraph name
	mmSubgraphPattern = regexp.MustCompile(`^\s*subgraph\s+(.+?)(?:\s*\[.*\])?\s*$`)
	// end (closes subgraph)
	mmEndPattern = regexp.MustCompile(`^\s*end\s*$`)
	// Node definitions: A[Label], A(Label), A{Label}, A((Label)), A>Label], A{{Label}}, or A --> B
	mmNodePattern = regexp.MustCompile(`(\w+)[\[\(\{><]`)
	// Class definition in classDiagram: class ClassName
	mmClassDefPattern = regexp.MustCompile(`^\s*class\s+(\w+)`)
	// State definition in stateDiagram: state "Name" as alias
	mmStatePattern = regexp.MustCompile(`^\s*state\s+"?([^"]+)"?\s+as\s+(\w+)`)
	// Participant in sequenceDiagram: participant Name  or  actor Name
	mmParticipantPattern = regexp.MustCompile(`^\s*(?:participant|actor)\s+(\w+)(?:\s+as\s+(.+))?`)
	// Section in gantt/journey: section Name
	mmSectionPattern = regexp.MustCompile(`^\s*section\s+(.+)`)
	// Title directive: title Name
	mmTitlePattern = regexp.MustCompile(`^\s*title\s+(.+)`)
)

func (p *MermaidParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol

	type subgraphEntry struct {
		idx int // index in symbols slice
	}
	var subgraphStack []subgraphEntry

	currentDiagram := ""

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "%%") {
			continue
		}

		// Diagram type declaration
		if m := mmDiagramPattern.FindStringSubmatch(line); m != nil {
			currentDiagram = m[1]
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindModule, StartLine: lineNum, EndLine: len(lines),
				Signature: trimmed,
			})
			continue
		}

		// Title
		if m := mmTitlePattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: strings.TrimSpace(m[1]), Kind: KindProperty, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed,
			})
			continue
		}

		// Subgraph open
		if m := mmSubgraphPattern.FindStringSubmatch(line); m != nil {
			name := strings.TrimSpace(m[1])
			sym := Symbol{
				Name: name, Kind: KindClass, StartLine: lineNum, EndLine: len(lines),
				Signature: trimmed, Parent: currentDiagram,
			}
			symbols = append(symbols, sym)
			subgraphStack = append(subgraphStack, subgraphEntry{idx: len(symbols) - 1})
			continue
		}

		// Subgraph close
		if mmEndPattern.MatchString(line) {
			if len(subgraphStack) > 0 {
				top := subgraphStack[len(subgraphStack)-1]
				symbols[top.idx].EndLine = lineNum
				subgraphStack = subgraphStack[:len(subgraphStack)-1]
			}
			continue
		}

		// Section (gantt, journey)
		if m := mmSectionPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: strings.TrimSpace(m[1]), Kind: KindFunction, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed, Parent: currentDiagram,
			})
			continue
		}

		// Participant/Actor (sequenceDiagram)
		if m := mmParticipantPattern.FindStringSubmatch(line); m != nil {
			name := m[1]
			if m[2] != "" {
				name = strings.TrimSpace(m[2])
			}
			symbols = append(symbols, Symbol{
				Name: name, Kind: KindType, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed, Parent: currentDiagram,
			})
			continue
		}

		// Class definition (classDiagram)
		if m := mmClassDefPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindClass, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed, Parent: currentDiagram,
			})
			continue
		}

		// State definition (stateDiagram)
		if m := mmStatePattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[2], Kind: KindType, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed, Parent: currentDiagram,
			})
			continue
		}

		// Node definitions (flowchart/graph) — find all nodes on the line
		if currentDiagram == "graph" || currentDiagram == "flowchart" {
			matches := mmNodePattern.FindAllStringSubmatch(line, -1)
			for _, m := range matches {
				name := m[1]
				// Skip keywords
				if name == "subgraph" || name == "end" || name == "style" ||
					name == "click" || name == "linkStyle" || name == "classDef" {
					continue
				}
				// Avoid duplicates
				found := false
				for _, s := range symbols {
					if s.Name == name && s.Kind == KindStruct {
						found = true
						break
					}
				}
				if !found {
					symbols = append(symbols, Symbol{
						Name: name, Kind: KindStruct, StartLine: lineNum, EndLine: lineNum,
						Signature: trimmed, Parent: currentDiagram,
					})
				}
			}
		}
	}

	return symbols
}
