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

func init() { Register(&RubyParser{}) }

// RubyParser extracts symbols from Ruby source files.
type RubyParser struct{}

func (p *RubyParser) Name() string         { return "ruby" }
func (p *RubyParser) Extensions() []string { return []string{".rb", ".rake", ".gemspec"} }

func (p *RubyParser) FlowHints() FlowHints {
	return FlowHints{
		EntryFunctions: []string{"main"},
		Keywords: []string{
			"if", "unless", "while", "until", "for", "case", "when",
			"return", "break", "next", "redo", "retry",
			"begin", "rescue", "ensure", "raise",
			"class", "module", "def", "do", "end",
			"self", "super", "require", "include", "extend",
			"attr_reader", "attr_writer", "attr_accessor",
			"puts", "print", "p", "pp", "warn",
			"lambda", "proc", "block_given", "yield",
			"nil", "true", "false",
		},
		CommentPrefixes: []string{"#"},
	}
}

func (p *RubyParser) IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, "_test.rb") ||
		strings.HasSuffix(lower, "_spec.rb") ||
		strings.Contains(lower, "/test/") ||
		strings.Contains(lower, "/spec/")
}

var (
	rbModulePattern  = regexp.MustCompile(`^\s*module\s+([A-Z]\w*)`)
	rbClassPattern   = regexp.MustCompile(`^\s*class\s+([A-Z]\w*)(?:\s*<\s*\S+)?`)
	rbDefPattern     = regexp.MustCompile(`^\s*def\s+(?:self\.)?(\w+[?!=]?)`)
	rbSelfDefPattern = regexp.MustCompile(`^\s*def\s+self\.(\w+[?!=]?)`)
	rbConstPattern   = regexp.MustCompile(`^\s*([A-Z][A-Z0-9_]+)\s*=\s*`)
	rbAttrPattern    = regexp.MustCompile(`^\s*attr_(?:reader|writer|accessor)\s+(.+)`)
	rbAliasPattern   = regexp.MustCompile(`^\s*alias(?:_method)?\s+:?(\w+)`)
)

func (p *RubyParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol

	type scope struct {
		name  string
		depth int
	}
	var scopeStack []scope
	depth := 0

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Track depth via begin/end keywords (simplified)
		opens := countRubyOpens(trimmed)
		closes := countRubyCloses(trimmed)

		// Module
		if m := rbModulePattern.FindStringSubmatch(line); m != nil {
			endLine := findRubyEnd(lines, i, depth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindModule, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			scopeStack = append(scopeStack, scope{name: m[1], depth: depth})
		}

		// Class
		if m := rbClassPattern.FindStringSubmatch(line); m != nil {
			endLine := findRubyEnd(lines, i, depth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindClass, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			scopeStack = append(scopeStack, scope{name: m[1], depth: depth})
		}

		// Method
		if m := rbDefPattern.FindStringSubmatch(line); m != nil {
			endLine := findRubyEnd(lines, i, depth)
			kind := KindFunction
			parent := ""
			if rbSelfDefPattern.MatchString(line) {
				kind = KindFunction
			} else if len(scopeStack) > 0 {
				kind = KindMethod
				parent = scopeStack[len(scopeStack)-1].name
			}
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: kind, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed, Parent: parent,
			})
		}

		// Constant
		if m := rbConstPattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
		}

		// attr_reader/writer/accessor
		if m := rbAttrPattern.FindStringSubmatch(trimmed); m != nil {
			attrs := strings.Split(m[1], ",")
			for _, a := range attrs {
				a = strings.TrimSpace(a)
				a = strings.TrimPrefix(a, ":")
				if a != "" {
					parent := ""
					if len(scopeStack) > 0 {
						parent = scopeStack[len(scopeStack)-1].name
					}
					symbols = append(symbols, Symbol{Name: a, Kind: KindProperty, StartLine: lineNum, EndLine: lineNum, Signature: trimmed, Parent: parent})
				}
			}
		}

		depth += opens - closes

		// Pop scope stack when returning to or below a scope's depth
		for len(scopeStack) > 0 && depth <= scopeStack[len(scopeStack)-1].depth {
			scopeStack = scopeStack[:len(scopeStack)-1]
		}
	}

	return symbols
}

// countRubyOpens counts block-opening keywords on a line.
func countRubyOpens(line string) int {
	count := 0
	// Match keywords that open blocks: class, module, def, do, if, unless, while, until, for, begin, case
	// Only count if at start of statement (not inline)
	openers := []string{"class ", "module ", "def ", " do", "\tdo"}
	for _, op := range openers {
		if strings.Contains(line, op) {
			count++
		}
	}
	// Standalone block openers at line start
	trimmed := strings.TrimSpace(line)
	standalone := []string{"if ", "unless ", "while ", "until ", "for ", "begin", "case "}
	for _, op := range standalone {
		if strings.HasPrefix(trimmed, op) || trimmed == strings.TrimSpace(op) {
			// Don't count single-line conditionals (has then/;/end on same line)
			if !strings.Contains(line, " then ") && !strings.HasSuffix(trimmed, "end") {
				count++
			}
			break
		}
	}
	return count
}

// countRubyCloses counts `end` keywords on a line.
func countRubyCloses(line string) int {
	trimmed := strings.TrimSpace(line)
	if trimmed == "end" || strings.HasPrefix(trimmed, "end ") || strings.HasPrefix(trimmed, "end;") {
		return 1
	}
	return 0
}

// findRubyEnd finds the matching `end` line for a block starting at startIdx.
func findRubyEnd(lines []string, startIdx, startDepth int) int {
	depth := startDepth
	for i := startIdx; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		depth += countRubyOpens(trimmed)
		depth -= countRubyCloses(trimmed)
		if depth <= startDepth && i > startIdx {
			return i + 1
		}
	}
	return len(lines)
}
