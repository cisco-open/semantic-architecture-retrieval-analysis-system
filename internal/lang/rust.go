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

func init() { Register(&RustParser{}) }

// RustParser extracts symbols from Rust source files.
type RustParser struct{}

func (p *RustParser) Name() string         { return "rust" }
func (p *RustParser) Extensions() []string { return []string{".rs"} }

func (p *RustParser) FlowHints() FlowHints {
	return FlowHints{
		EntryFunctions: []string{"main"},
		Keywords: []string{
			"if", "for", "while", "else", "loop", "match", "return",
			"break", "continue", "let", "mut", "fn", "struct", "enum",
			"impl", "trait", "use", "mod", "pub", "self", "super", "crate",
			"async", "await", "move", "unsafe", "dyn", "where", "as", "in",
			"ref", "type", "const", "static", "extern",
			"println", "eprintln", "format", "write", "writeln",
			"vec", "Box", "Rc", "Arc", "Some", "None", "Ok", "Err",
			"String", "Vec", "HashMap", "HashSet", "Option", "Result",
			"assert", "assert_eq", "assert_ne", "panic", "todo", "unimplemented",
		},
		CommentPrefixes: []string{"//"},
	}
}

func (p *RustParser) IsTestFile(path string) bool {
	return strings.Contains(path, "/tests/") || strings.Contains(path, "tests/") || strings.HasSuffix(path, "_test.rs")
}

var (
	rustFnPattern     = regexp.MustCompile(`^(?:pub(?:\(crate\))?\s+)?(?:async\s+)?(?:unsafe\s+)?fn\s+(\w+)`)
	rustStructPattern = regexp.MustCompile(`^(?:pub(?:\(crate\))?\s+)?struct\s+(\w+)`)
	rustEnumPattern   = regexp.MustCompile(`^(?:pub(?:\(crate\))?\s+)?enum\s+(\w+)`)
	rustTraitPattern  = regexp.MustCompile(`^(?:pub(?:\(crate\))?\s+)?(?:unsafe\s+)?trait\s+(\w+)`)
	rustImplPattern   = regexp.MustCompile(`^impl(?:<[^>]*>)?\s+(?:(\w+)\s+for\s+)?(\w+)`)
	rustConstPattern  = regexp.MustCompile(`^(?:pub(?:\(crate\))?\s+)?(?:const|static)\s+(\w+)\s*:`)
	rustTypePattern   = regexp.MustCompile(`^(?:pub(?:\(crate\))?\s+)?type\s+(\w+)`)
	rustModPattern    = regexp.MustCompile(`^(?:pub(?:\(crate\))?\s+)?mod\s+(\w+)`)
)

func (p *RustParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	braceDepth := 0
	implType := ""
	implDepth := -1

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "//") {
			continue
		}

		prevDepth := braceDepth
		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")

		if implDepth >= 0 && braceDepth <= implDepth {
			implType = ""
			implDepth = -1
		}

		// Module
		if m := rustModPattern.FindStringSubmatch(trimmed); m != nil {
			if strings.Contains(trimmed, "{") {
				endLine := findBraceEnd(lines, i, prevDepth)
				symbols = append(symbols, Symbol{Name: m[1], Kind: KindModule, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			} else {
				symbols = append(symbols, Symbol{Name: m[1], Kind: KindModule, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			}
			continue
		}

		// Trait
		if m := rustTraitPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindTrait, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Struct
		if m := rustStructPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindStruct, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Enum
		if m := rustEnumPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindEnum, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// Impl block
		if m := rustImplPattern.FindStringSubmatch(trimmed); m != nil {
			implType = m[2]
			implDepth = prevDepth
			continue
		}

		// Type alias
		if m := rustTypePattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindType, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			continue
		}

		// Const/static
		if m := rustConstPattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			continue
		}

		// Function / method
		if m := rustFnPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			kind := KindFunction
			parent := ""
			if implType != "" && braceDepth > implDepth+1 {
				kind = KindMethod
				parent = implType
			}
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: kind, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed, Parent: parent,
			})
			continue
		}
	}

	return symbols
}
