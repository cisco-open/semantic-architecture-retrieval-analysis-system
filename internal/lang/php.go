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

func init() { Register(&PHPParser{}) }

// PHPParser extracts symbols from PHP source files.
type PHPParser struct{}

func (p *PHPParser) Name() string { return "php" }
func (p *PHPParser) Extensions() []string {
	return []string{".php", ".phtml", ".php3", ".php4", ".php5"}
}

func (p *PHPParser) FlowHints() FlowHints {
	return FlowHints{
		EntryFunctions: []string{"main"},
		Keywords: []string{
			"if", "for", "while", "else", "elseif", "foreach", "switch", "case",
			"return", "break", "continue", "throw", "try", "catch", "finally",
			"new", "echo", "print", "isset", "unset", "empty",
			"array", "list", "class", "function", "namespace", "use",
			"require", "include", "require_once", "include_once",
			"static", "abstract", "final", "public", "private", "protected",
			"self", "parent", "trait", "interface",
			"var_dump", "print_r", "die", "exit",
			"count", "strlen", "array_push", "array_pop", "array_map",
			"array_filter", "array_merge", "in_array", "implode", "explode",
		},
		CommentPrefixes: []string{"//", "#"},
	}
}

func (p *PHPParser) IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, "test.php") ||
		strings.Contains(lower, "/tests/") ||
		strings.Contains(lower, "/test/")
}

var (
	phpNamespacePattern   = regexp.MustCompile(`^namespace\s+([\w\\]+)\s*;`)
	phpClassPattern       = regexp.MustCompile(`^(?:(?:abstract|final)\s+)*class\s+(\w+)`)
	phpInterfacePattern   = regexp.MustCompile(`^interface\s+(\w+)`)
	phpTraitPattern       = regexp.MustCompile(`^trait\s+(\w+)`)
	phpEnumPattern        = regexp.MustCompile(`^enum\s+(\w+)`)
	phpFunctionPattern    = regexp.MustCompile(`^(?:(?:public|protected|private|static|abstract|final)\s+)*function\s+(\w+)\s*\(`)
	phpConstPattern       = regexp.MustCompile(`^(?:(?:public|protected|private)\s+)?const\s+(\w+)\s*=`)
	phpDefinePattern      = regexp.MustCompile(`^define\s*\(\s*['"](\w+)['"]`)
	phpTopFunctionPattern = regexp.MustCompile(`^function\s+(\w+)\s*\(`)
)

func (p *PHPParser) ExtractSymbols(content string) []Symbol {
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
			strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") ||
			strings.HasPrefix(trimmed, "<?") || strings.HasPrefix(trimmed, "?>") {
			// Still count braces in comment/tag lines for depth tracking
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
		if m := phpNamespacePattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindPackage, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			continue
		}

		// define()
		if m := phpDefinePattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			continue
		}

		// Enum (PHP 8.1+)
		if m := phpEnumPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindEnum, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			inClass = true
			classDepth = prevDepth
			className = m[1]
			continue
		}

		// Interface
		if m := phpInterfacePattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindInterface, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			inClass = true
			classDepth = prevDepth
			className = m[1]
			continue
		}

		// Trait
		if m := phpTraitPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindTrait, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			inClass = true
			classDepth = prevDepth
			className = m[1]
			continue
		}

		// Class
		if m := phpClassPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindClass, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			inClass = true
			classDepth = prevDepth
			className = m[1]
			continue
		}

		// Class constant
		if inClass {
			if m := phpConstPattern.FindStringSubmatch(trimmed); m != nil {
				symbols = append(symbols, Symbol{Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum, Signature: trimmed, Parent: className})
				continue
			}
		}

		// Function / method
		if m := phpFunctionPattern.FindStringSubmatch(trimmed); m != nil {
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

		// Top-level function (without visibility modifier)
		if !inClass {
			if m := phpTopFunctionPattern.FindStringSubmatch(trimmed); m != nil {
				endLine := findBraceEnd(lines, i, prevDepth)
				symbols = append(symbols, Symbol{Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
				continue
			}
		}
	}

	return symbols
}
