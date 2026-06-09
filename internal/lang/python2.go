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

func init() { Register(&Python2Parser{}) }

// Python2Parser extracts symbols from Python 2 source files.
// Python 2 and 3 share .py, so this parser registers for .py2 and .pyw extensions.
// The main PythonParser already handles most Python 2 syntax (def, class).
// This parser additionally recognizes Python 2 idioms like print statements,
// old-style classes, and lacks async/await support.
// It can also be selected explicitly via ParserByName("python2").
type Python2Parser struct{}

func (p *Python2Parser) Name() string         { return "python2" }
func (p *Python2Parser) Extensions() []string { return []string{".py2", ".pyw"} }

func (p *Python2Parser) FlowHints() FlowHints {
	return FlowHints{
		EntryFunctions: []string{"main"},
		IsEntryFile: func(content string) bool {
			return strings.Contains(content, `if __name__`)
		},
		Keywords: []string{
			"if", "for", "while", "else", "elif", "try", "except", "finally",
			"raise", "import", "from", "class", "def", "return", "yield",
			"with", "as", "pass", "break", "continue",
			"lambda", "print", "len", "range", "xrange", "type", "super", "self",
			"isinstance", "issubclass", "hasattr", "getattr", "setattr",
			"dict", "list", "set", "tuple", "str", "int", "float", "bool",
			"raw_input", "input", "map", "filter", "zip", "sorted", "reversed",
			"enumerate", "any", "all", "min", "max", "sum", "abs", "round",
			"unicode", "basestring", "long", "execfile",
		},
		CommentPrefixes: []string{"#"},
	}
}

func (p *Python2Parser) IsTestFile(path string) bool {
	base := strings.ToLower(path)
	return strings.Contains(base, "test_") || strings.HasSuffix(base, "_test.py2") || strings.HasSuffix(base, "_test.pyw")
}

var (
	py2FuncPattern  = regexp.MustCompile(`^(\s*)def\s+(\w+)\s*\(`)
	py2ClassPattern = regexp.MustCompile(`^class\s+(\w+)`)
	py2VarPattern   = regexp.MustCompile(`^([A-Z][A-Z_0-9]+)\s*=`)
)

func (p *Python2Parser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	var classStack []py2ClassCtx

	for i, line := range lines {
		lineNum := i + 1

		// Track class context via indentation
		if len(classStack) > 0 {
			indent := leadingSpaces(line)
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && indent <= classStack[len(classStack)-1].indent {
				for len(classStack) > 0 && indent <= classStack[len(classStack)-1].indent {
					classStack[len(classStack)-1].sym.EndLine = lineNum - 1
					classStack = classStack[:len(classStack)-1]
				}
			}
		}

		trimmed := strings.TrimSpace(line)

		// Class
		if m := py2ClassPattern.FindStringSubmatch(trimmed); m != nil {
			sym := Symbol{Name: m[1], Kind: KindClass, StartLine: lineNum, EndLine: lineNum, Signature: trimmed}
			symbols = append(symbols, sym)
			classStack = append(classStack, py2ClassCtx{
				indent: leadingSpaces(line),
				sym:    &symbols[len(symbols)-1],
			})
			continue
		}

		// Function/method
		if m := py2FuncPattern.FindStringSubmatch(line); m != nil {
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
		if m := py2VarPattern.FindStringSubmatch(trimmed); m != nil {
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

type py2ClassCtx struct {
	indent int
	sym    *Symbol
}
