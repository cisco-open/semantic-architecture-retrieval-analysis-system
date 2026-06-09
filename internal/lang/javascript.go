/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package lang

import (
	"regexp"
	"strings"
)

func init() { Register(&JavaScriptParser{}) }

// JavaScriptParser extracts symbols from JavaScript source files.
type JavaScriptParser struct{}

func (p *JavaScriptParser) Name() string         { return "javascript" }
func (p *JavaScriptParser) Extensions() []string { return []string{".js", ".jsx", ".mjs", ".cjs"} }

func (p *JavaScriptParser) FlowHints() FlowHints {
	return FlowHints{
		EntryFunctions: []string{"main"},
		Keywords: []string{
			"if", "for", "while", "else", "switch", "case", "return",
			"break", "continue", "throw", "try", "catch", "finally",
			"typeof", "instanceof", "new", "delete", "void",
			"require", "import", "from", "var", "let", "const",
			"function", "class", "async", "await", "yield",
			"super", "this", "of", "in",
			"console", "setTimeout", "setInterval", "clearTimeout",
			"clearInterval", "Promise", "Array", "Object", "String",
			"Number", "Boolean", "Math", "JSON", "Date", "RegExp",
			"parseInt", "parseFloat", "isNaN", "isFinite",
			"encodeURIComponent", "decodeURIComponent",
		},
		CommentPrefixes: []string{"//"},
	}
}
func (p *JavaScriptParser) IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, ".test.") || strings.Contains(lower, ".spec.") ||
		strings.Contains(lower, "__tests__")
}

var (
	jsFuncDeclPattern     = regexp.MustCompile(`^(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*\(`)
	jsArrowConstPattern   = regexp.MustCompile(`^(?:export\s+)?const\s+(\w+)\s*=\s*(?:async\s+)?\(`)
	jsArrowNoParenPattern = regexp.MustCompile(`^(?:export\s+)?const\s+(\w+)\s*=\s*(?:async\s+)?\w+\s*=>`)
	jsClassPattern        = regexp.MustCompile(`^(?:export\s+)?(?:default\s+)?class\s+(\w+)`)
	jsMethodPattern       = regexp.MustCompile(`^\s+(?:async\s+)?(\w+)\s*\(`)
	jsConstPattern        = regexp.MustCompile(`^(?:export\s+)?const\s+(\w+)\s*=\s*[^(]`)
	jsLetVarPattern       = regexp.MustCompile(`^(?:export\s+)?(?:let|var)\s+(\w+)\s*=`)
)

func (p *JavaScriptParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	inClass := false
	classIndent := 0
	className := ""
	braceDepth := 0

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		prevDepth := braceDepth
		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")

		if inClass && braceDepth <= classIndent {
			inClass = false
			className = ""
		}

		// Class
		if m := jsClassPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, braceDepth-strings.Count(line, "{")+strings.Count(line, "}"))
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindClass, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			inClass = true
			classIndent = prevDepth
			className = m[1]
			continue
		}

		// Method inside class
		if inClass && !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "/*") {
			if m := jsMethodPattern.FindStringSubmatch(line); m != nil {
				name := m[1]
				if name != "if" && name != "for" && name != "while" && name != "switch" && name != "catch" {
					endLine := findBraceEnd(lines, i, prevDepth)
					symbols = append(symbols, Symbol{
						Name: name, Kind: KindMethod, StartLine: lineNum, EndLine: endLine,
						Signature: trimmed, Parent: className,
					})
					continue
				}
			}
		}

		// Function declaration
		if m := jsFuncDeclPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Arrow function (const name = (...) => or const name = async (...) =>)
		if m := jsArrowConstPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}
		if m := jsArrowNoParenPattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			continue
		}

		// Const (non-function)
		if !inClass {
			if m := jsConstPattern.FindStringSubmatch(trimmed); m != nil {
				symbols = append(symbols, Symbol{Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
				continue
			}
		}
	}

	return symbols
}

func findBraceEnd(lines []string, startIdx, startDepth int) int {
	depth := startDepth
	for i := startIdx; i < len(lines); i++ {
		depth += strings.Count(lines[i], "{") - strings.Count(lines[i], "}")
		if depth <= startDepth && i > startIdx {
			return i + 1
		}
	}
	return len(lines)
}
