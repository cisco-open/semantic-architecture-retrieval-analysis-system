/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and SARAS Contributors
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package lang

import (
	"regexp"
	"strings"
)

func init() { Register(&YAMLParser{}) }

// YAMLParser extracts symbols from YAML configuration files.
type YAMLParser struct{}

func (p *YAMLParser) Name() string         { return "yaml" }
func (p *YAMLParser) Extensions() []string { return []string{".yaml", ".yml"} }

func (p *YAMLParser) FlowHints() FlowHints {
	return FlowHints{
		CommentPrefixes: []string{"#"},
	}
}

func (p *YAMLParser) IsTestFile(path string) bool {
	return false
}

var (
	// Top-level key (no leading whitespace): key:
	yamlTopKeyPattern = regexp.MustCompile(`^([a-zA-Z_][\w.-]*)\s*:`)
	// Indented key: key:
	yamlKeyPattern = regexp.MustCompile(`^(\s+)([a-zA-Z_][\w.-]*)\s*:`)
	// Document separator: ---
	yamlDocSeparator = regexp.MustCompile(`^---\s*$`)
)

func (p *YAMLParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Document separator
		if yamlDocSeparator.MatchString(line) {
			symbols = append(symbols, Symbol{
				Name: "---", Kind: KindModule, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed,
			})
			continue
		}

		// Top-level key (no indentation)
		if m := yamlTopKeyPattern.FindStringSubmatch(line); m != nil {
			endLine := findYAMLBlockEnd(lines, i, 0)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindModule, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			continue
		}

		// Second-level key (indented, first level of nesting)
		if m := yamlKeyPattern.FindStringSubmatch(line); m != nil {
			indent := len(m[1])
			if indent <= 4 { // Only capture first couple nesting levels
				parent := ""
				// Find parent top-level key
				for j := i - 1; j >= 0; j-- {
					if pm := yamlTopKeyPattern.FindStringSubmatch(lines[j]); pm != nil {
						parent = pm[1]
						break
					}
				}
				symbols = append(symbols, Symbol{
					Name: m[2], Kind: KindProperty, StartLine: lineNum, EndLine: lineNum,
					Signature: trimmed, Parent: parent,
				})
			}
			continue
		}
	}

	return symbols
}

// findYAMLBlockEnd finds where a top-level YAML block ends (next unindented key or EOF).
func findYAMLBlockEnd(lines []string, startIdx int, startIndent int) int {
	for i := startIdx + 1; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if yamlDocSeparator.MatchString(line) {
			return i
		}
		// If line starts at same or lesser indent, block ended
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			return i
		}
	}
	return len(lines)
}
