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

func init() { Register(&KotlinParser{}) }

// KotlinParser extracts symbols from Kotlin source files.
type KotlinParser struct{}

func (p *KotlinParser) Name() string         { return "kotlin" }
func (p *KotlinParser) Extensions() []string { return []string{".kt", ".kts"} }

func (p *KotlinParser) FlowHints() FlowHints {
	return FlowHints{
		EntryFunctions: []string{"main"},
		Keywords: []string{
			"if", "for", "while", "else", "when", "return",
			"break", "continue", "throw", "try", "catch", "finally",
			"this", "super", "class", "object", "fun", "val", "var",
			"import", "package", "is", "as", "in", "typealias",
			"abstract", "open", "sealed", "data", "inner", "companion",
			"override", "lateinit", "by", "constructor", "init",
			"println", "print", "require", "check", "error",
			"listOf", "mapOf", "setOf", "mutableListOf", "mutableMapOf",
			"arrayOf", "emptyList", "emptyMap",
		},
		CommentPrefixes: []string{"//"},
	}
}

func (p *KotlinParser) IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, "test.kt") || strings.Contains(lower, "/test/")
}

var (
	ktFunPattern       = regexp.MustCompile(`^(?:(?:public|private|protected|internal|override|open|abstract|suspend|inline|infix|operator|tailrec)\s+)*fun\s+(?:<[^>]*>\s+)?(\w+)\s*\(`)
	ktClassPattern     = regexp.MustCompile(`^(?:(?:public|private|protected|internal|open|abstract|sealed|data|inner|annotation|enum)\s+)*class\s+(\w+)`)
	ktObjectPattern    = regexp.MustCompile(`^(?:(?:public|private|protected|internal)\s+)?(?:companion\s+)?object\s+(\w+)`)
	ktInterfacePattern = regexp.MustCompile(`^(?:(?:public|private|protected|internal|sealed)\s+)*interface\s+(\w+)`)
	ktEnumPattern      = regexp.MustCompile(`^(?:(?:public|private|protected|internal)\s+)?enum\s+class\s+(\w+)`)
	ktValPattern       = regexp.MustCompile(`^(?:(?:public|private|protected|internal|const|override)\s+)*val\s+(\w+)\s*[=:]`)
	ktVarPattern       = regexp.MustCompile(`^(?:(?:public|private|protected|internal|override|lateinit)\s+)*var\s+(\w+)\s*[=:]`)
	ktPackagePattern   = regexp.MustCompile(`^package\s+([\w.]+)`)
	ktTypeAliasPattern = regexp.MustCompile(`^(?:(?:public|private|protected|internal)\s+)?typealias\s+(\w+)`)
)

func (p *KotlinParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	braceDepth := 0
	inClass := false
	className := ""
	classDepth := 0

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "//") {
			continue
		}

		prevDepth := braceDepth
		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")

		if inClass && braceDepth <= classDepth {
			inClass = false
			className = ""
		}

		// Package
		if m := ktPackagePattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindPackage, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			continue
		}

		// Type alias
		if m := ktTypeAliasPattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindType, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			continue
		}

		// Enum class
		if m := ktEnumPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindEnum, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			inClass = true
			classDepth = prevDepth
			className = m[1]
			continue
		}

		// Interface
		if m := ktInterfacePattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindInterface, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			inClass = true
			classDepth = prevDepth
			className = m[1]
			continue
		}

		// Object
		if m := ktObjectPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindClass, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			inClass = true
			classDepth = prevDepth
			className = m[1]
			continue
		}

		// Class
		if m := ktClassPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindClass, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			inClass = true
			classDepth = prevDepth
			className = m[1]
			continue
		}

		// Function / method
		if m := ktFunPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			kind := KindFunction
			parent := ""
			if inClass {
				kind = KindMethod
				parent = className
			}
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: kind, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed, Parent: parent,
			})
			continue
		}

		// Const val (top-level or in companion)
		if !inClass || strings.Contains(trimmed, "const") {
			if m := ktValPattern.FindStringSubmatch(trimmed); m != nil {
				if strings.Contains(trimmed, "const") {
					symbols = append(symbols, Symbol{Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
				} else if !inClass {
					symbols = append(symbols, Symbol{Name: m[1], Kind: KindVariable, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
				}
				continue
			}
		}
	}

	return symbols
}
