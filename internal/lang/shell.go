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

func init() { Register(&ShellParser{}) }

// ShellParser extracts symbols from shell script files (bash, sh, zsh, ksh).
type ShellParser struct{}

func (p *ShellParser) Name() string         { return "shell" }
func (p *ShellParser) Extensions() []string { return []string{".sh", ".bash", ".zsh", ".ksh", ".bats"} }

func (p *ShellParser) FlowHints() FlowHints {
	return FlowHints{
		EntryFunctions: []string{"main"},
		Keywords: []string{
			"if", "then", "else", "elif", "fi",
			"for", "while", "until", "do", "done",
			"case", "esac", "in",
			"function", "return", "exit",
			"local", "declare", "typeset", "readonly",
			"export", "unset", "shift",
			"source", "eval", "exec",
			"echo", "printf", "read",
			"true", "false", "test",
			"cd", "pwd", "pushd", "popd",
			"set", "trap", "wait",
		},
		CommentPrefixes: []string{"#"},
	}
}

func (p *ShellParser) IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".bats") ||
		strings.HasSuffix(lower, "_test.sh") ||
		strings.Contains(lower, "/test/") ||
		strings.Contains(lower, "/tests/")
}

var (
	// function name() { ... }  or  function name { ... }
	shFuncKeywordPattern = regexp.MustCompile(`^\s*function\s+(\w+)\s*(?:\(\s*\))?\s*\{?`)
	// name() { ... }
	shFuncPattern = regexp.MustCompile(`^\s*(\w+)\s*\(\s*\)\s*\{?`)
	// export VAR=value  or  export VAR
	shExportPattern = regexp.MustCompile(`^\s*export\s+(\w+)`)
	// VAR=value (top-level assignments, uppercase convention)
	shVarPattern = regexp.MustCompile(`^\s*([A-Z][A-Z0-9_]+)=`)
	// alias name='...'  or  alias name="..."
	shAliasPattern = regexp.MustCompile(`^\s*alias\s+(\w+)=`)
	// readonly VAR=value
	shReadonlyPattern = regexp.MustCompile(`^\s*readonly\s+(\w+)`)
)

func (p *ShellParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// function keyword style
		if m := shFuncKeywordPattern.FindStringSubmatch(line); m != nil {
			endLine := findShellFuncEnd(lines, i)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			continue
		}

		// name() style (but not inside other constructs)
		if m := shFuncPattern.FindStringSubmatch(line); m != nil {
			name := m[1]
			// Skip keywords that look like functions
			if name != "if" && name != "for" && name != "while" && name != "until" && name != "case" {
				endLine := findShellFuncEnd(lines, i)
				symbols = append(symbols, Symbol{
					Name: name, Kind: KindFunction, StartLine: lineNum, EndLine: endLine,
					Signature: trimmed,
				})
				continue
			}
		}

		// readonly
		if m := shReadonlyPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed,
			})
			continue
		}

		// export
		if m := shExportPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindVariable, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed,
			})
			continue
		}

		// Uppercase variable assignment
		if m := shVarPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindVariable, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed,
			})
			continue
		}

		// alias
		if m := shAliasPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed,
			})
			continue
		}
	}

	return symbols
}

// findShellFuncEnd finds the closing brace for a shell function.
func findShellFuncEnd(lines []string, startIdx int) int {
	depth := 0
	for i := startIdx; i < len(lines); i++ {
		line := lines[i]
		for _, ch := range line {
			if ch == '{' {
				depth++
			} else if ch == '}' {
				depth--
				if depth == 0 {
					return i + 1
				}
			}
		}
	}
	return len(lines)
}
