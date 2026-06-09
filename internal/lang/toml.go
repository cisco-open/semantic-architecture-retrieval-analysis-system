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

func init() { Register(&TOMLParser{}) }

// TOMLParser extracts symbols from TOML configuration files.
type TOMLParser struct{}

func (p *TOMLParser) Name() string         { return "toml" }
func (p *TOMLParser) Extensions() []string { return []string{".toml"} }

func (p *TOMLParser) FlowHints() FlowHints {
	return FlowHints{
		CommentPrefixes: []string{"#"},
	}
}

func (p *TOMLParser) IsTestFile(path string) bool {
	return false
}

var (
	// [section] or [section.subsection]
	tomlTablePattern = regexp.MustCompile(`^\s*\[([^\[\]]+)\]\s*$`)
	// [[array.of.tables]]
	tomlArrayTablePattern = regexp.MustCompile(`^\s*\[\[([^\[\]]+)\]\]\s*$`)
	// key = value (top-level or within a table)
	tomlKeyPattern = regexp.MustCompile(`^\s*([\w][\w.-]*)\s*=`)
)

func (p *TOMLParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol

	currentTable := ""

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Array of tables: [[name]]
		if m := tomlArrayTablePattern.FindStringSubmatch(line); m != nil {
			name := strings.TrimSpace(m[1])
			currentTable = name
			endLine := findTOMLSectionEnd(lines, i)
			symbols = append(symbols, Symbol{
				Name: "[[" + name + "]]", Kind: KindClass, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			continue
		}

		// Table: [name]
		if m := tomlTablePattern.FindStringSubmatch(line); m != nil {
			name := strings.TrimSpace(m[1])
			currentTable = name
			endLine := findTOMLSectionEnd(lines, i)
			symbols = append(symbols, Symbol{
				Name: "[" + name + "]", Kind: KindModule, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			continue
		}

		// Key = value
		if m := tomlKeyPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindProperty, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed, Parent: currentTable,
			})
			continue
		}
	}

	return symbols
}

// findTOMLSectionEnd returns the line before the next section header or EOF.
func findTOMLSectionEnd(lines []string, startIdx int) int {
	for i := startIdx + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if tomlTablePattern.MatchString(trimmed) || tomlArrayTablePattern.MatchString(trimmed) {
			return i
		}
	}
	return len(lines)
}
