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

func init() { Register(&TypeScriptParser{}) }

// TypeScriptParser extracts symbols from TypeScript source files.
type TypeScriptParser struct{}

func (p *TypeScriptParser) Name() string         { return "typescript" }
func (p *TypeScriptParser) Extensions() []string { return []string{".ts", ".tsx"} }

func (p *TypeScriptParser) FlowHints() FlowHints {
	return FlowHints{
		EntryFunctions: []string{"main"},
		Keywords: []string{
			"if", "for", "while", "else", "switch", "case", "return",
			"break", "continue", "throw", "try", "catch", "finally",
			"typeof", "instanceof", "new", "delete", "void",
			"require", "import", "from", "var", "let", "const",
			"function", "class", "async", "await", "yield",
			"super", "this", "of", "in", "as", "is", "keyof",
			"console", "setTimeout", "setInterval", "clearTimeout",
			"clearInterval", "Promise", "Array", "Object", "String",
			"Number", "Boolean", "Math", "JSON", "Date", "RegExp",
			"parseInt", "parseFloat", "isNaN", "isFinite",
			"Record", "Partial", "Required", "Readonly", "Pick", "Omit",
		},
		CommentPrefixes: []string{"//"},
	}
}

func (p *TypeScriptParser) IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, ".test.") || strings.Contains(lower, ".spec.") ||
		strings.Contains(lower, "__tests__")
}

var (
	tsFuncDeclPattern   = regexp.MustCompile(`^(?:export\s+)?(?:async\s+)?function\s+(\w+)`)
	tsArrowConstPattern = regexp.MustCompile(`^(?:export\s+)?const\s+(\w+)\s*(?::\s*\S+\s*)?=\s*(?:async\s+)?\(`)
	tsClassPattern      = regexp.MustCompile(`^(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\s+(\w+)`)
	tsInterfacePattern  = regexp.MustCompile(`^(?:export\s+)?interface\s+(\w+)`)
	tsTypePattern       = regexp.MustCompile(`^(?:export\s+)?type\s+(\w+)\s*[=<]`)
	tsEnumPattern       = regexp.MustCompile(`^(?:export\s+)?(?:const\s+)?enum\s+(\w+)`)
	tsMethodPattern     = regexp.MustCompile(`^\s+(?:public\s+|private\s+|protected\s+|static\s+|async\s+|readonly\s+)*(\w+)\s*\(`)
	tsConstPattern      = regexp.MustCompile(`^(?:export\s+)?const\s+(\w+)\s*(?::\s*\S+\s*)?=\s*[^(]`)
)

func (p *TypeScriptParser) ExtractSymbols(content string) []Symbol {
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

		// Interface
		if m := tsInterfacePattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindInterface, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Type alias
		if m := tsTypePattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindType, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			continue
		}

		// Enum
		if m := tsEnumPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindEnum, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Class
		if m := tsClassPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindClass, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			inClass = true
			classIndent = prevDepth
			className = m[1]
			continue
		}

		// Method inside class
		if inClass && !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "/*") {
			if m := tsMethodPattern.FindStringSubmatch(line); m != nil {
				name := m[1]
				if name != "if" && name != "for" && name != "while" && name != "switch" && name != "catch" && name != "constructor" {
					endLine := findBraceEnd(lines, i, prevDepth)
					symbols = append(symbols, Symbol{
						Name: name, Kind: KindMethod, StartLine: lineNum, EndLine: endLine,
						Signature: trimmed, Parent: className,
					})
					continue
				}
				if name == "constructor" {
					endLine := findBraceEnd(lines, i, prevDepth)
					symbols = append(symbols, Symbol{
						Name: "constructor", Kind: KindMethod, StartLine: lineNum, EndLine: endLine,
						Signature: trimmed, Parent: className,
					})
					continue
				}
			}
		}

		// Function declaration
		if m := tsFuncDeclPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Arrow function
		if m := tsArrowConstPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Const (non-function)
		if !inClass {
			if m := tsConstPattern.FindStringSubmatch(trimmed); m != nil {
				symbols = append(symbols, Symbol{Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
				continue
			}
		}
	}

	return symbols
}
