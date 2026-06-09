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

func init() { Register(&CSharpParser{}) }

// CSharpParser extracts symbols from C# source files.
type CSharpParser struct{}

func (p *CSharpParser) Name() string         { return "csharp" }
func (p *CSharpParser) Extensions() []string { return []string{".cs", ".csx"} }

func (p *CSharpParser) FlowHints() FlowHints {
	return FlowHints{
		EntryFunctions: []string{"Main"},
		IsEntryFile: func(content string) bool {
			return strings.Contains(content, "static void Main") ||
				strings.Contains(content, "static async Task Main") ||
				strings.Contains(content, "static int Main")
		},
		Keywords: []string{
			"if", "for", "while", "else", "foreach", "switch", "case", "return",
			"break", "continue", "throw", "try", "catch", "finally",
			"new", "typeof", "sizeof", "nameof", "is", "as",
			"this", "base", "class", "struct", "enum", "interface",
			"namespace", "using", "async", "await",
			"abstract", "virtual", "override", "sealed", "static",
			"var", "dynamic", "object", "string", "int", "bool",
			"Console", "String", "Math", "Convert", "Task",
			"List", "Dictionary", "HashSet", "Array",
		},
		CommentPrefixes: []string{"//"},
	}
}

func (p *CSharpParser) IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, "test.cs") ||
		strings.HasSuffix(lower, "tests.cs") ||
		strings.Contains(lower, "/tests/") ||
		strings.Contains(lower, "/test/")
}

var (
	csNamespacePattern = regexp.MustCompile(`^(?:file\s+)?namespace\s+([\w.]+)`)
	csClassPattern     = regexp.MustCompile(`^(?:(?:public|private|protected|internal|static|abstract|sealed|partial)\s+)*class\s+(\w+)`)
	csStructPattern    = regexp.MustCompile(`^(?:(?:public|private|protected|internal|readonly|ref|partial)\s+)*struct\s+(\w+)`)
	csInterfacePattern = regexp.MustCompile(`^(?:(?:public|private|protected|internal|partial)\s+)*interface\s+(\w+)`)
	csEnumPattern      = regexp.MustCompile(`^(?:(?:public|private|protected|internal)\s+)*enum\s+(\w+)`)
	csRecordPattern    = regexp.MustCompile(`^(?:(?:public|private|protected|internal|sealed|abstract|partial)\s+)*record\s+(?:struct\s+|class\s+)?(\w+)`)
	csMethodPattern    = regexp.MustCompile(`^(?:(?:public|private|protected|internal|static|virtual|override|abstract|async|sealed|new|extern)\s+)*(?:[\w<>\[\]?,\s]+)\s+(\w+)\s*\(`)
	csPropertyPattern  = regexp.MustCompile(`^(?:(?:public|private|protected|internal|static|virtual|override|abstract|new)\s+)*(?:[\w<>\[\]?,]+)\s+(\w+)\s*\{\s*(?:get|set)`)
	csConstPattern     = regexp.MustCompile(`^(?:(?:public|private|protected|internal)\s+)*const\s+\S+\s+(\w+)\s*=`)
	csDelegatePattern  = regexp.MustCompile(`^(?:(?:public|private|protected|internal)\s+)*delegate\s+\S+\s+(\w+)\s*[(<]`)
	csEventPattern     = regexp.MustCompile(`^(?:(?:public|private|protected|internal|static)\s+)*event\s+\S+\s+(\w+)\s*[;{]`)
	csUsingPattern     = regexp.MustCompile(`^using\s+(?:static\s+)?([\w.]+)\s*;`)
)

func (p *CSharpParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	braceDepth := 0
	inClass := false
	className := ""
	classDepth := 0

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "//") ||
			strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
			braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
			continue
		}

		// Skip attribute lines
		if strings.HasPrefix(trimmed, "[") && !strings.Contains(trimmed, "=") {
			continue
		}

		prevDepth := braceDepth
		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")

		if inClass && braceDepth <= classDepth {
			inClass = false
			className = ""
		}

		// Using
		if m := csUsingPattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindImport, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			continue
		}

		// Namespace
		if m := csNamespacePattern.FindStringSubmatch(trimmed); m != nil {
			// File-scoped namespace (no braces)
			if strings.HasSuffix(trimmed, ";") {
				symbols = append(symbols, Symbol{Name: m[1], Kind: KindPackage, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			} else {
				endLine := findBraceEnd(lines, i, prevDepth)
				symbols = append(symbols, Symbol{Name: m[1], Kind: KindPackage, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			}
			continue
		}

		// Enum
		if m := csEnumPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindEnum, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Interface
		if m := csInterfacePattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindInterface, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			inClass = true
			classDepth = prevDepth
			className = m[1]
			continue
		}

		// Record
		if m := csRecordPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindClass, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			inClass = true
			classDepth = prevDepth
			className = m[1]
			continue
		}

		// Struct
		if m := csStructPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindStruct, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			inClass = true
			classDepth = prevDepth
			className = m[1]
			continue
		}

		// Class
		if m := csClassPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindClass, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			inClass = true
			classDepth = prevDepth
			className = m[1]
			continue
		}

		// Delegate
		if m := csDelegatePattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindType, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			continue
		}

		// Const
		if m := csConstPattern.FindStringSubmatch(trimmed); m != nil {
			parent := ""
			if inClass {
				parent = className
			}
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum, Signature: trimmed, Parent: parent})
			continue
		}

		// Event
		if inClass {
			if m := csEventPattern.FindStringSubmatch(trimmed); m != nil {
				symbols = append(symbols, Symbol{Name: m[1], Kind: KindProperty, StartLine: lineNum, EndLine: lineNum, Signature: trimmed, Parent: className})
				continue
			}
		}

		// Property
		if inClass {
			if m := csPropertyPattern.FindStringSubmatch(trimmed); m != nil {
				symbols = append(symbols, Symbol{Name: m[1], Kind: KindProperty, StartLine: lineNum, EndLine: lineNum, Signature: trimmed, Parent: className})
				continue
			}
		}

		// Method (must come after class/struct/interface checks)
		if m := csMethodPattern.FindStringSubmatch(trimmed); m != nil {
			name := m[1]
			// Skip keywords that look like methods
			if name == "if" || name == "for" || name == "while" || name == "switch" ||
				name == "catch" || name == "using" || name == "lock" || name == "return" ||
				name == "class" || name == "struct" || name == "interface" || name == "enum" ||
				name == "namespace" || name == "new" || name == "typeof" || name == "sizeof" {
				continue
			}
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
			continue
		}
	}

	return symbols
}
