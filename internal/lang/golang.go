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

func init() { Register(&GoParser{}) }

// GoParser extracts symbols from Go source files.
type GoParser struct{}

func (p *GoParser) Name() string         { return "go" }
func (p *GoParser) Extensions() []string { return []string{".go"} }

func (p *GoParser) FlowHints() FlowHints {
	return FlowHints{
		EntryFunctions: []string{"main", "init"},
		IsEntryFile: func(content string) bool {
			for _, line := range strings.Split(content, "\n") {
				t := strings.TrimSpace(line)
				if strings.HasPrefix(t, "package ") {
					return strings.TrimSpace(strings.TrimPrefix(t, "package")) == "main"
				}
			}
			return false
		},
		Keywords: []string{
			"if", "for", "switch", "select", "go", "defer", "return", "range",
			"case", "make", "len", "cap", "new", "append", "copy", "delete",
			"close", "panic", "recover", "print", "println", "complex", "real",
			"imag", "func", "type", "map", "chan", "var", "const", "fallthrough",
		},
		CommentPrefixes: []string{"//"},
	}
}
func (p *GoParser) IsTestFile(path string) bool {
	return strings.HasSuffix(path, "_test.go")
}

var (
	goFuncPattern    = regexp.MustCompile(`^func\s+(\w+)\s*\(`)
	goMethodPattern  = regexp.MustCompile(`^func\s+\(\s*\w+\s+\*?\s*(\w+)\s*\)\s+(\w+)\s*\(`)
	goTypePattern    = regexp.MustCompile(`^type\s+(\w+)\s+struct\b`)
	goIfacePattern   = regexp.MustCompile(`^type\s+(\w+)\s+interface\b`)
	goConstPattern   = regexp.MustCompile(`^(?:const\s+)?(\w+)\s*=`)
	goVarPattern     = regexp.MustCompile(`^var\s+(\w+)\s`)
	goPackagePattern = regexp.MustCompile(`^package\s+(\w+)`)
)

func (p *GoParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	var currentFunc *Symbol
	braceDepth := 0

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
		if currentFunc != nil && braceDepth <= 0 {
			currentFunc.EndLine = lineNum
			currentFunc = nil
		}

		if m := goPackagePattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindPackage, StartLine: lineNum, EndLine: lineNum})
			continue
		}

		if m := goMethodPattern.FindStringSubmatch(trimmed); m != nil {
			sym := Symbol{Name: m[2], Kind: KindMethod, StartLine: lineNum, EndLine: lineNum, Signature: trimmed, Parent: m[1]}
			symbols = append(symbols, sym)
			currentFunc = &symbols[len(symbols)-1]
			braceDepth = strings.Count(line, "{") - strings.Count(line, "}")
			continue
		}

		if m := goFuncPattern.FindStringSubmatch(trimmed); m != nil {
			sym := Symbol{Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: lineNum, Signature: trimmed}
			symbols = append(symbols, sym)
			currentFunc = &symbols[len(symbols)-1]
			braceDepth = strings.Count(line, "{") - strings.Count(line, "}")
			continue
		}

		if m := goIfacePattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindInterface, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			continue
		}

		if m := goTypePattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindStruct, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			continue
		}

		if m := goVarPattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindVariable, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			continue
		}

		if strings.HasPrefix(trimmed, "const ") {
			if m := goConstPattern.FindStringSubmatch(trimmed); m != nil {
				symbols = append(symbols, Symbol{Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			}
		}
	}

	return symbols
}
