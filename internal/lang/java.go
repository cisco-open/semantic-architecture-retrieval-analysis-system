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

func init() { Register(&JavaParser{}) }

// JavaParser extracts symbols from Java source files.
type JavaParser struct{}

func (p *JavaParser) Name() string         { return "java" }
func (p *JavaParser) Extensions() []string { return []string{".java"} }

func (p *JavaParser) FlowHints() FlowHints {
	return FlowHints{
		EntryFunctions: []string{"main"},
		IsEntryFile: func(content string) bool {
			return strings.Contains(content, "static void main") ||
				strings.Contains(content, "static void Main")
		},
		Keywords: []string{
			"if", "for", "while", "else", "switch", "case", "return",
			"break", "continue", "throw", "try", "catch", "finally",
			"new", "instanceof", "this", "super",
			"class", "interface", "enum", "extends", "implements",
			"import", "package", "abstract", "final", "static",
			"synchronized", "volatile", "transient", "native",
			"assert", "default", "void",
			"System", "String", "Integer", "Long", "Double", "Float",
			"Boolean", "Object", "Arrays", "Collections", "Optional",
			"Math", "List", "Map", "Set",
		},
		CommentPrefixes: []string{"//"},
	}
}

func (p *JavaParser) IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, "test.java") || strings.Contains(lower, "/test/")
}

var (
	javaClassPattern     = regexp.MustCompile(`^(?:public\s+|private\s+|protected\s+)?(?:abstract\s+|final\s+|static\s+)*class\s+(\w+)`)
	javaInterfacePattern = regexp.MustCompile(`^(?:public\s+|private\s+|protected\s+)?interface\s+(\w+)`)
	javaEnumPattern      = regexp.MustCompile(`^(?:public\s+|private\s+|protected\s+)?enum\s+(\w+)`)
	javaMethodPattern    = regexp.MustCompile(`^\s+(?:public\s+|private\s+|protected\s+)?(?:static\s+|final\s+|abstract\s+|synchronized\s+|native\s+)*(?:\w+(?:<[^>]*>)?(?:\[\])*)\s+(\w+)\s*\(`)
	javaConstructorPat   = regexp.MustCompile(`^\s+(?:public\s+|private\s+|protected\s+)?(\w+)\s*\([^)]*\)\s*(?:throws\s+\w+)?\s*\{?`)
	javaFieldPattern     = regexp.MustCompile(`^\s+(?:public\s+|private\s+|protected\s+)?(?:static\s+)?(?:final\s+)?(?:\w+(?:<[^>]*>)?(?:\[\])*)\s+([A-Z_][A-Z_0-9]*)\s*[=;]`)
	javaPackagePattern   = regexp.MustCompile(`^package\s+([\w.]+)\s*;`)
)

func (p *JavaParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	var classStack []string
	braceDepth := 0

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		prevDepth := braceDepth
		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")

		// Pop class stack when exiting scope
		for len(classStack) > 0 && braceDepth < prevDepth && braceDepth <= len(classStack)-1 {
			classStack = classStack[:len(classStack)-1]
		}

		// Package
		if m := javaPackagePattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindPackage, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			continue
		}

		// Interface
		if m := javaInterfacePattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindInterface, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			classStack = append(classStack, m[1])
			continue
		}

		// Enum
		if m := javaEnumPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindEnum, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			classStack = append(classStack, m[1])
			continue
		}

		// Class
		if m := javaClassPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindClass, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			classStack = append(classStack, m[1])
			continue
		}

		// Method
		if len(classStack) > 0 {
			if m := javaMethodPattern.FindStringSubmatch(line); m != nil {
				name := m[1]
				if name != "if" && name != "for" && name != "while" && name != "switch" && name != "catch" && name != "return" {
					endLine := findBraceEnd(lines, i, prevDepth)
					parent := classStack[len(classStack)-1]
					symbols = append(symbols, Symbol{
						Name: name, Kind: KindMethod, StartLine: lineNum, EndLine: endLine,
						Signature: trimmed, Parent: parent,
					})
					continue
				}
			}
		}

		// Static final constants (ALL_CAPS)
		if len(classStack) > 0 {
			if m := javaFieldPattern.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, Symbol{Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			}
		}
	}

	return symbols
}
