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

func init() { Register(&CParser{}) }

// CParser extracts symbols from C source files.
type CParser struct{}

func (p *CParser) Name() string         { return "c" }
func (p *CParser) Extensions() []string { return []string{".c", ".h"} }

func (p *CParser) FlowHints() FlowHints {
	return FlowHints{
		EntryFunctions: []string{"main"},
		Keywords: []string{
			"if", "for", "while", "else", "switch", "case", "return",
			"break", "continue", "do", "goto", "sizeof", "typeof",
			"struct", "enum", "union", "typedef",
			"printf", "fprintf", "sprintf", "snprintf", "scanf",
			"malloc", "calloc", "realloc", "free",
			"memcpy", "memset", "memmove", "strlen", "strcmp", "strcpy",
			"fopen", "fclose", "fread", "fwrite", "fgets", "fputs",
			"exit", "abort", "assert",
		},
		CommentPrefixes: []string{"//"},
	}
}

func (p *CParser) IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, "test") && strings.HasSuffix(lower, ".c")
}

var (
	cFuncPattern    = regexp.MustCompile(`^(?:static\s+|inline\s+|extern\s+)*(?:const\s+)?(?:unsigned\s+|signed\s+|long\s+|short\s+)*\w+[\s*]+(\w+)\s*\([^)]*\)\s*\{?`)
	cStructPattern  = regexp.MustCompile(`^(?:typedef\s+)?struct\s+(\w+)`)
	cEnumPattern    = regexp.MustCompile(`^(?:typedef\s+)?enum\s+(\w+)`)
	cTypedefPattern = regexp.MustCompile(`^typedef\s+.+\s+(\w+)\s*;`)
	cDefinePattern  = regexp.MustCompile(`^#define\s+(\w+)`)
)

func (p *CParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	braceDepth := 0

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		// Skip comments
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
			braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
			continue
		}

		prevDepth := braceDepth
		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")

		// #define
		if m := cDefinePattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			continue
		}

		// Struct
		if m := cStructPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindStruct, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Enum
		if m := cEnumPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindEnum, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Typedef (simple, not struct/enum)
		if m := cTypedefPattern.FindStringSubmatch(trimmed); m != nil {
			if !strings.Contains(trimmed, "struct") && !strings.Contains(trimmed, "enum") {
				symbols = append(symbols, Symbol{Name: m[1], Kind: KindType, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
				continue
			}
		}

		// Function (only at top level, brace depth 0)
		if prevDepth == 0 && strings.Contains(line, "(") && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "typedef") {
			if m := cFuncPattern.FindStringSubmatch(trimmed); m != nil {
				name := m[1]
				if name != "if" && name != "for" && name != "while" && name != "switch" && name != "return" && name != "sizeof" {
					endLine := findBraceEnd(lines, i, prevDepth)
					symbols = append(symbols, Symbol{Name: name, Kind: KindFunction, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
				}
			}
		}
	}

	return symbols
}
