/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc.
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package lang

import (
	"regexp"
	"strings"
)

func init() { Register(&PerlParser{}) }

// PerlParser extracts symbols from Perl source files.
type PerlParser struct{}

func (p *PerlParser) Name() string         { return "perl" }
func (p *PerlParser) Extensions() []string { return []string{".pl", ".pm", ".t", ".psgi"} }

func (p *PerlParser) FlowHints() FlowHints {
	return FlowHints{
		EntryFunctions: []string{"main"},
		Keywords: []string{
			"if", "elsif", "else", "unless", "while", "until", "for", "foreach",
			"do", "return", "last", "next", "redo",
			"my", "our", "local", "use", "require", "no",
			"eval", "die", "warn", "print", "say",
			"chomp", "chop", "push", "pop", "shift", "unshift",
			"defined", "exists", "delete", "ref",
			"bless", "new", "SUPER",
			"open", "close", "read", "write",
			"grep", "map", "sort", "join", "split",
			"BEGIN", "END", "AUTOLOAD", "DESTROY",
		},
		CommentPrefixes: []string{"#"},
	}
}

func (p *PerlParser) IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".t") ||
		strings.Contains(lower, "/t/") ||
		strings.Contains(lower, "/test/") ||
		strings.Contains(lower, "/tests/")
}

var (
	// package Foo::Bar;
	plPackagePattern = regexp.MustCompile(`^\s*package\s+([\w:]+)`)
	// sub foo { ... }  or  sub foo ($self, ...) { ... }
	plSubPattern = regexp.MustCompile(`^\s*sub\s+(\w+)`)
	// use constant { NAME => ... }  or  use constant NAME => ...
	plConstantPattern = regexp.MustCompile(`^\s*use\s+constant\s+(\w+)\s*=>`)
	// our/my $VAR = ...  (package-level scalars, typically constants or config)
	plOurPattern = regexp.MustCompile(`^\s*(?:our|my)\s+([\$@%]\w+)\s*=`)
	// has 'attr' => (...)   Moose/Moo attribute
	plHasPattern = regexp.MustCompile(`^\s*has\s+['"]?(\w+)['"]?\s*=>`)
	// BEGIN { ... }  END { ... }
	plSpecialBlockPattern = regexp.MustCompile(`^\s*(BEGIN|END|INIT|CHECK|UNITCHECK|AUTOLOAD|DESTROY)\s*\{`)
)

func (p *PerlParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol

	currentPackage := ""

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Skip POD documentation blocks
		if strings.HasPrefix(trimmed, "=") {
			// POD starts with =head, =over, =pod, etc. and ends with =cut
			if strings.HasPrefix(trimmed, "=cut") {
				continue
			}
			if strings.HasPrefix(trimmed, "=head") ||
				strings.HasPrefix(trimmed, "=over") ||
				strings.HasPrefix(trimmed, "=pod") ||
				strings.HasPrefix(trimmed, "=begin") ||
				strings.HasPrefix(trimmed, "=item") ||
				strings.HasPrefix(trimmed, "=back") ||
				strings.HasPrefix(trimmed, "=for") ||
				strings.HasPrefix(trimmed, "=encoding") {
				// Skip until =cut
				for j := i + 1; j < len(lines); j++ {
					if strings.TrimSpace(lines[j]) == "=cut" {
						break
					}
				}
				continue
			}
		}

		// Package declaration
		if m := plPackagePattern.FindStringSubmatch(line); m != nil {
			currentPackage = m[1]
			endLine := findPerlPackageEnd(lines, i)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindPackage, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			continue
		}

		// Subroutine
		if m := plSubPattern.FindStringSubmatch(line); m != nil {
			endLine := findPerlBlockEnd(lines, i)
			kind := KindFunction
			parent := ""
			if currentPackage != "" {
				kind = KindMethod
				parent = currentPackage
			}
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: kind, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed, Parent: parent,
			})
			continue
		}

		// use constant NAME => value
		if m := plConstantPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed, Parent: currentPackage,
			})
			continue
		}

		// our/my variable declarations at package scope
		if m := plOurPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindVariable, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed, Parent: currentPackage,
			})
			continue
		}

		// Moose/Moo attributes: has 'name' => (...)
		if m := plHasPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindProperty, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed, Parent: currentPackage,
			})
			continue
		}

		// Special blocks: BEGIN, END, etc.
		if m := plSpecialBlockPattern.FindStringSubmatch(line); m != nil {
			endLine := findPerlBlockEnd(lines, i)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			continue
		}
	}

	return symbols
}

// findPerlBlockEnd finds the closing brace for a block starting at startIdx.
func findPerlBlockEnd(lines []string, startIdx int) int {
	depth := 0
	for i := startIdx; i < len(lines); i++ {
		line := lines[i]
		// Skip comments and strings (simplified)
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

// findPerlPackageEnd finds the end of a package block.
// A package ends at the next package declaration or end of file.
func findPerlPackageEnd(lines []string, startIdx int) int {
	for i := startIdx + 1; i < len(lines); i++ {
		if plPackagePattern.MatchString(lines[i]) {
			return i // next package starts here
		}
	}
	return len(lines)
}
