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

func init() { Register(&PythonParser{}) }

// PythonParser extracts symbols from Python source files.
type PythonParser struct{}

func (p *PythonParser) Name() string         { return "python" }
func (p *PythonParser) Extensions() []string { return []string{".py", ".pyi"} }
func (p *PythonParser) IsTestFile(path string) bool {
	base := strings.ToLower(path)
	return strings.Contains(base, "test_") || strings.HasSuffix(base, "_test.py")
}

func (p *PythonParser) FlowHints() FlowHints {
	return FlowHints{
		EntryFunctions: []string{"main"},
		IsEntryFile: func(content string) bool {
			return strings.Contains(content, `if __name__`)
		},
		Keywords: []string{
			"if", "for", "while", "else", "elif", "try", "except", "finally",
			"raise", "import", "from", "class", "def", "return", "yield",
			"async", "await", "with", "as", "pass", "break", "continue",
			"lambda", "print", "len", "range", "type", "super", "self",
			"isinstance", "issubclass", "hasattr", "getattr", "setattr",
			"dict", "list", "set", "tuple", "str", "int", "float", "bool",
			"open", "input", "map", "filter", "zip", "sorted", "reversed",
			"enumerate", "any", "all", "min", "max", "sum", "abs", "round",
			"format", "repr", "id", "hash", "vars", "dir", "next", "iter",
		},
		CommentPrefixes: []string{"#"},
	}
}

var (
	pyFuncPattern      = regexp.MustCompile(`^(\s*)def\s+(\w+)\s*\(`)
	pyAsyncFuncPattern = regexp.MustCompile(`^(\s*)async\s+def\s+(\w+)\s*\(`)
	pyClassPattern     = regexp.MustCompile(`^class\s+(\w+)`)
	pyVarPattern       = regexp.MustCompile(`^([A-Z][A-Z_0-9]+)\s*=`)
)

func (p *PythonParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	var classStack []classCtx

	for i, line := range lines {
		lineNum := i + 1

		// Track class context via indentation
		if len(classStack) > 0 {
			indent := leadingSpaces(line)
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && indent <= classStack[len(classStack)-1].indent {
				// Close class(es) that ended
				for len(classStack) > 0 && indent <= classStack[len(classStack)-1].indent {
					classStack[len(classStack)-1].sym.EndLine = lineNum - 1
					classStack = classStack[:len(classStack)-1]
				}
			}
		}

		trimmed := strings.TrimSpace(line)

		// Class
		if m := pyClassPattern.FindStringSubmatch(trimmed); m != nil {
			sym := Symbol{Name: m[1], Kind: KindClass, StartLine: lineNum, EndLine: lineNum, Signature: trimmed}
			symbols = append(symbols, sym)
			classStack = append(classStack, classCtx{
				indent: leadingSpaces(line),
				sym:    &symbols[len(symbols)-1],
			})
			continue
		}

		// Async function/method
		if m := pyAsyncFuncPattern.FindStringSubmatch(line); m != nil {
			indent := len(m[1])
			kind := KindFunction
			parent := ""
			if len(classStack) > 0 && indent > classStack[len(classStack)-1].indent {
				kind = KindMethod
				parent = classStack[len(classStack)-1].sym.Name
			}
			endLine := findPythonBlockEnd(lines, i, indent)
			symbols = append(symbols, Symbol{
				Name: m[2], Kind: kind, StartLine: lineNum, EndLine: endLine,
				Signature: strings.TrimSpace(line), Parent: parent,
			})
			continue
		}

		// Function/method
		if m := pyFuncPattern.FindStringSubmatch(line); m != nil {
			indent := len(m[1])
			kind := KindFunction
			parent := ""
			if len(classStack) > 0 && indent > classStack[len(classStack)-1].indent {
				kind = KindMethod
				parent = classStack[len(classStack)-1].sym.Name
			}
			endLine := findPythonBlockEnd(lines, i, indent)
			symbols = append(symbols, Symbol{
				Name: m[2], Kind: kind, StartLine: lineNum, EndLine: endLine,
				Signature: strings.TrimSpace(line), Parent: parent,
			})
			continue
		}

		// Module-level constants (ALL_CAPS = ...)
		if m := pyVarPattern.FindStringSubmatch(trimmed); m != nil {
			if len(classStack) == 0 {
				symbols = append(symbols, Symbol{Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			}
		}
	}

	// Close any remaining classes
	for i := len(classStack) - 1; i >= 0; i-- {
		classStack[i].sym.EndLine = len(lines)
	}

	return symbols
}

type classCtx struct {
	indent int
	sym    *Symbol
}

func leadingSpaces(line string) int {
	count := 0
	for _, ch := range line {
		if ch == ' ' {
			count++
		} else if ch == '\t' {
			count += 4
		} else {
			break
		}
	}
	return count
}

func findPythonBlockEnd(lines []string, startIdx, defIndent int) int {
	for i := startIdx + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := leadingSpaces(lines[i])
		if indent <= defIndent {
			return i // line i is past the block; return i as 1-indexed endLine = i
		}
	}
	return len(lines)
}
