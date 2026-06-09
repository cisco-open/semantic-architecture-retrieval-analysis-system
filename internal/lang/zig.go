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

func init() { Register(&ZigParser{}) }

// ZigParser extracts symbols from Zig source files.
type ZigParser struct{}

func (p *ZigParser) Name() string         { return "zig" }
func (p *ZigParser) Extensions() []string { return []string{".zig"} }

func (p *ZigParser) FlowHints() FlowHints {
	return FlowHints{
		EntryFunctions: []string{"main"},
		Keywords: []string{
			"if", "for", "while", "else", "switch", "return",
			"break", "continue", "try", "catch", "defer", "errdefer",
			"unreachable", "comptime", "inline", "noalias",
			"fn", "struct", "enum", "union", "const", "var",
			"pub", "extern", "export", "test",
			"undefined", "null", "true", "false",
		},
		CommentPrefixes: []string{"//"},
	}
}

func (p *ZigParser) IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, "test") && strings.HasSuffix(lower, ".zig")
}

var (
	zigFnPattern     = regexp.MustCompile(`^(?:pub\s+)?(?:export\s+)?(?:inline\s+)?fn\s+(\w+)\s*\(`)
	zigStructPattern = regexp.MustCompile(`^(?:pub\s+)?const\s+(\w+)\s*=\s*(?:packed\s+|extern\s+)?struct\b`)
	zigEnumPattern   = regexp.MustCompile(`^(?:pub\s+)?const\s+(\w+)\s*=\s*(?:packed\s+|extern\s+)?enum\b`)
	zigUnionPattern  = regexp.MustCompile(`^(?:pub\s+)?const\s+(\w+)\s*=\s*(?:packed\s+|extern\s+)?(?:tagged\s+)?union\b`)
	zigConstPattern  = regexp.MustCompile(`^(?:pub\s+)?const\s+(\w+)\s*(?::[^=]*)?\s*=\s*[^{}]`)
	zigVarPattern    = regexp.MustCompile(`^(?:pub\s+)?(?:threadlocal\s+)?var\s+(\w+)\s*[=:]`)
	zigTestPattern   = regexp.MustCompile(`^test\s+"([^"]+)"`)
	zigErrorPattern  = regexp.MustCompile(`^(?:pub\s+)?const\s+(\w+)\s*=\s*error\b`)
)

func (p *ZigParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	braceDepth := 0

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "//") {
			continue
		}

		prevDepth := braceDepth
		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")

		// Test block
		if m := zigTestPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Function
		if m := zigFnPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Struct
		if m := zigStructPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindStruct, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Enum
		if m := zigEnumPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindEnum, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Union
		if m := zigUnionPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindType, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Error set
		if m := zigErrorPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindType, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Top-level const (non-struct/enum/union/error)
		if prevDepth == 0 {
			if m := zigConstPattern.FindStringSubmatch(trimmed); m != nil {
				symbols = append(symbols, Symbol{Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
				continue
			}
		}

		// Var
		if prevDepth == 0 {
			if m := zigVarPattern.FindStringSubmatch(trimmed); m != nil {
				symbols = append(symbols, Symbol{Name: m[1], Kind: KindVariable, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			}
		}
	}

	return symbols
}
