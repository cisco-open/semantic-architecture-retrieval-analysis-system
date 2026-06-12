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

func init() { Register(&SwiftParser{}) }

// SwiftParser extracts symbols from Swift source files.
type SwiftParser struct{}

func (p *SwiftParser) Name() string         { return "swift" }
func (p *SwiftParser) Extensions() []string { return []string{".swift"} }

func (p *SwiftParser) FlowHints() FlowHints {
	return FlowHints{
		EntryFunctions: []string{"main"},
		Keywords: []string{
			"if", "else", "for", "while", "repeat", "switch", "case", "default",
			"break", "continue", "return", "throw", "throws", "rethrows",
			"try", "catch", "defer", "guard", "where",
			"func", "class", "struct", "enum", "protocol", "extension",
			"actor", "import", "typealias", "associatedtype",
			"let", "var", "static", "lazy", "weak", "unowned",
			"public", "private", "internal", "fileprivate", "open",
			"override", "mutating", "nonmutating", "final", "required",
			"convenience", "init", "deinit", "subscript",
			"async", "await", "Task", "MainActor",
			"some", "any", "Self", "self", "super",
			"print", "fatalError", "precondition", "assert",
		},
		CommentPrefixes: []string{"//"},
	}
}

func (p *SwiftParser) IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	parts := strings.Split(lower, "/")
	for _, part := range parts {
		switch part {
		case "test", "tests", "testcase":
			return true
		}
	}
	return strings.HasSuffix(lower, "tests.swift") ||
		strings.HasSuffix(lower, "test.swift") ||
		strings.HasSuffix(lower, "spec.swift")
}

var (
	swiftFuncPattern     = regexp.MustCompile(`^\s*(?:(?:public|private|internal|fileprivate|open|override|static|class|final|@\w+\s+)*\s*)?func\s+(\w+)`)
	swiftClassPattern    = regexp.MustCompile(`^\s*(?:(?:public|private|internal|fileprivate|open|final)\s+)*class\s+(\w+)`)
	swiftStructPattern   = regexp.MustCompile(`^\s*(?:(?:public|private|internal|fileprivate)\s+)*struct\s+(\w+)`)
	swiftEnumPattern     = regexp.MustCompile(`^\s*(?:(?:public|private|internal|fileprivate)\s+)*enum\s+(\w+)`)
	swiftProtocolPattern = regexp.MustCompile(`^\s*(?:(?:public|private|internal|fileprivate)\s+)*protocol\s+(\w+)`)
	swiftExtPattern      = regexp.MustCompile(`^\s*(?:(?:public|private|internal|fileprivate)\s+)*extension\s+(\w+)`)
	swiftActorPattern    = regexp.MustCompile(`^\s*(?:(?:public|private|internal|fileprivate)\s+)*actor\s+(\w+)`)
	swiftTypeAlias       = regexp.MustCompile(`^\s*(?:(?:public|private|internal|fileprivate)\s+)*typealias\s+(\w+)`)
	swiftLetPattern      = regexp.MustCompile(`^\s*(?:(?:public|private|internal|fileprivate|static)\s+)*let\s+(\w+)\s*[=:]`)
	swiftVarPattern      = regexp.MustCompile(`^\s*(?:(?:public|private|internal|fileprivate|static|lazy|weak|unowned)\s+)*var\s+(\w+)\s*[=:]`)
	swiftImportPattern   = regexp.MustCompile(`^\s*import\s+(\w+)`)
	swiftInitPattern     = regexp.MustCompile(`^\s*(?:(?:public|private|internal|fileprivate|required|convenience|override)\s+)*init\s*[\(<]`)
)

func (p *SwiftParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	braceDepth := 0
	inType := false
	typeName := ""
	typeDepth := 0

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}

		prevDepth := braceDepth
		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")

		if inType && braceDepth <= typeDepth {
			inType = false
			typeName = ""
		}

		// Import
		if m := swiftImportPattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindImport, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed,
			})
			continue
		}

		// Protocol
		if m := swiftProtocolPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindInterface, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			inType = true
			typeDepth = prevDepth
			typeName = m[1]
			continue
		}

		// Actor
		if m := swiftActorPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindClass, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			inType = true
			typeDepth = prevDepth
			typeName = m[1]
			continue
		}

		// Enum
		if m := swiftEnumPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindEnum, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			inType = true
			typeDepth = prevDepth
			typeName = m[1]
			continue
		}

		// Struct
		if m := swiftStructPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindStruct, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			inType = true
			typeDepth = prevDepth
			typeName = m[1]
			continue
		}

		// Class
		if m := swiftClassPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindClass, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			inType = true
			typeDepth = prevDepth
			typeName = m[1]
			continue
		}

		// Extension
		if m := swiftExtPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindType, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed, Parent: "extension",
			})
			inType = true
			typeDepth = prevDepth
			typeName = m[1]
			continue
		}

		// Typealias
		if m := swiftTypeAlias.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindType, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed,
			})
			continue
		}

		// Init
		if swiftInitPattern.MatchString(trimmed) {
			endLine := findBraceEnd(lines, i, prevDepth)
			parent := ""
			if inType {
				parent = typeName
			}
			symbols = append(symbols, Symbol{
				Name: "init", Kind: KindMethod, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed, Parent: parent,
			})
			continue
		}

		// Function / method
		if m := swiftFuncPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			kind := KindFunction
			parent := ""
			if inType {
				kind = KindMethod
				parent = typeName
			}
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: kind, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed, Parent: parent,
			})
			continue
		}

		// Top-level let (constants)
		if !inType {
			if m := swiftLetPattern.FindStringSubmatch(trimmed); m != nil {
				symbols = append(symbols, Symbol{
					Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum,
					Signature: trimmed,
				})
				continue
			}
			if m := swiftVarPattern.FindStringSubmatch(trimmed); m != nil {
				symbols = append(symbols, Symbol{
					Name: m[1], Kind: KindVariable, StartLine: lineNum, EndLine: lineNum,
					Signature: trimmed,
				})
				continue
			}
		}
	}

	return symbols
}
