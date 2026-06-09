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

func init() { Register(&CppParser{}) }

// CppParser extracts symbols from C++ source files.
type CppParser struct{}

func (p *CppParser) Name() string { return "cpp" }
func (p *CppParser) Extensions() []string {
	return []string{".cpp", ".cc", ".cxx", ".hpp", ".hxx", ".hh"}
}

func (p *CppParser) FlowHints() FlowHints {
	return FlowHints{
		EntryFunctions: []string{"main"},
		Keywords: []string{
			"if", "for", "while", "else", "switch", "case", "return",
			"break", "continue", "do", "goto", "sizeof", "typeof", "typeid",
			"new", "delete", "throw", "try", "catch",
			"this", "class", "struct", "enum", "union", "namespace", "using",
			"template", "typename", "static_cast", "dynamic_cast",
			"reinterpret_cast", "const_cast", "decltype", "auto",
			"std", "cout", "cerr", "endl", "string", "vector", "map", "set",
			"make_shared", "make_unique", "move", "forward",
			"printf", "fprintf", "sprintf", "scanf",
			"malloc", "calloc", "realloc", "free",
			"memcpy", "memset", "strlen", "strcmp",
		},
		CommentPrefixes: []string{"//"},
	}
}

func (p *CppParser) IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, "test") && (strings.HasSuffix(lower, ".cpp") || strings.HasSuffix(lower, ".cc"))
}

var (
	cppClassPattern     = regexp.MustCompile(`^(?:template\s*<[^>]*>\s*)?class\s+(\w+)`)
	cppStructPattern    = regexp.MustCompile(`^(?:template\s*<[^>]*>\s*)?struct\s+(\w+)`)
	cppNamespacePattern = regexp.MustCompile(`^namespace\s+(\w+)`)
	cppFuncPattern      = regexp.MustCompile(`^(?:static\s+|inline\s+|virtual\s+|explicit\s+|constexpr\s+)*(?:const\s+)?(?:unsigned\s+|signed\s+)*\w+[\s*&]+(\w+)\s*\(`)
	cppMethodQualified  = regexp.MustCompile(`^(?:\w+[\s*&]+)?(\w+)::(\w+)\s*\(`)
	cppEnumPattern      = regexp.MustCompile(`^enum\s+(?:class\s+)?(\w+)`)
	cppTemplateFunc     = regexp.MustCompile(`^template\s*<[^>]*>\s*(?:\w+[\s*&]+)(\w+)\s*\(`)
)

func (p *CppParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	braceDepth := 0
	inClass := false
	className := ""
	classDepth := 0

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
			braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
			continue
		}

		prevDepth := braceDepth
		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")

		if inClass && braceDepth <= classDepth {
			inClass = false
			className = ""
		}

		// Namespace
		if m := cppNamespacePattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindModule, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Enum
		if m := cppEnumPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindEnum, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Class
		if m := cppClassPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindClass, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			inClass = true
			classDepth = prevDepth
			className = m[1]
			continue
		}

		// Struct
		if m := cppStructPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindStruct, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			inClass = true
			classDepth = prevDepth
			className = m[1]
			continue
		}

		// Qualified method (ClassName::method)
		if m := cppMethodQualified.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: m[2], Kind: KindMethod, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed, Parent: m[1],
			})
			continue
		}

		// Template function
		if m := cppTemplateFunc.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Free function or method in class body
		if !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "typedef") && strings.Contains(line, "(") {
			if m := cppFuncPattern.FindStringSubmatch(trimmed); m != nil {
				name := m[1]
				if name != "if" && name != "for" && name != "while" && name != "switch" && name != "return" && name != "sizeof" && name != "delete" && name != "new" {
					endLine := findBraceEnd(lines, i, prevDepth)
					kind := KindFunction
					parent := ""
					if inClass {
						kind = KindMethod
						parent = className
					}
					symbols = append(symbols, Symbol{
						Name: name, Kind: kind, StartLine: lineNum, EndLine: endLine,
						Signature: trimmed, Parent: parent,
					})
				}
			}
		}
	}

	return symbols
}
