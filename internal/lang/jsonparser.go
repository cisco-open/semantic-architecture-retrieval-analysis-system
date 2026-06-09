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

func init() { Register(&JSONParser{}) }

// JSONParser extracts symbols from JSON and JSONC files.
type JSONParser struct{}

func (p *JSONParser) Name() string         { return "json" }
func (p *JSONParser) Extensions() []string { return []string{".json", ".jsonc", ".json5"} }

func (p *JSONParser) FlowHints() FlowHints {
	return FlowHints{
		CommentPrefixes: []string{"//"},
	}
}

func (p *JSONParser) IsTestFile(path string) bool {
	return false
}

var (
	// Top-level key: "key": value (minimal or no indentation)
	jsonKeyPattern = regexp.MustCompile(`^\s*"([^"]+)"\s*:`)
)

func (p *JSONParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol

	braceDepth := 0
	bracketDepth := 0

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}

		prevBraceDepth := braceDepth

		// Track brace/bracket depth (simplified, ignores strings)
		for _, ch := range line {
			switch ch {
			case '{':
				braceDepth++
			case '}':
				braceDepth--
			case '[':
				bracketDepth++
			case ']':
				bracketDepth--
			}
		}

		// Extract keys at depth 1 (top-level object keys)
		if m := jsonKeyPattern.FindStringSubmatch(line); m != nil {
			if prevBraceDepth == 1 {
				kind := KindProperty
				// If the value starts an object or array, treat as a section
				rest := strings.TrimSpace(line[strings.Index(line, ":")+1:])
				if strings.HasPrefix(rest, "{") || strings.HasPrefix(rest, "[") {
					kind = KindModule
				}
				symbols = append(symbols, Symbol{
					Name: m[1], Kind: kind, StartLine: lineNum, EndLine: lineNum,
					Signature: trimmed,
				})
			}
		}
	}

	return symbols
}
