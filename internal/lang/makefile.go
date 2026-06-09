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

func init() { Register(&MakefileParser{}) }

// MakefileParser extracts symbols from Makefile source files.
type MakefileParser struct{}

func (p *MakefileParser) Name() string         { return "makefile" }
func (p *MakefileParser) Extensions() []string { return []string{".mk", ".make"} }

// Filenames returns exact filenames this parser handles (no extension).
func (p *MakefileParser) Filenames() []string {
	return []string{"Makefile", "makefile", "GNUmakefile"}
}

func (p *MakefileParser) FlowHints() FlowHints {
	return FlowHints{
		CommentPrefixes: []string{"#"},
	}
}

func (p *MakefileParser) IsTestFile(path string) bool {
	return false
}

var (
	// target: [dependencies] — matched but VAR:= filtered in code
	mkTargetPattern = regexp.MustCompile(`^([a-zA-Z_][\w.-]*)\s*:`)
	// VAR = value, VAR := value, VAR ?= value, VAR += value
	mkVarPattern = regexp.MustCompile(`^\s*([A-Za-z_][\w]*)\s*[:?+]?=`)
	// .PHONY: target1 target2
	mkPhonyPattern = regexp.MustCompile(`^\.PHONY\s*:\s*(.+)`)
	// include file.mk
	mkIncludePattern = regexp.MustCompile(`^-?include\s+(.+)`)
	// define VAR
	mkDefinePattern = regexp.MustCompile(`^define\s+(\w+)`)
)

func (p *MakefileParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	inDefine := false

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// End of define block
		if inDefine && trimmed == "endef" {
			inDefine = false
			continue
		}
		if inDefine {
			continue
		}

		// define VAR
		if m := mkDefinePattern.FindStringSubmatch(line); m != nil {
			endLine := lineNum
			for j := i + 1; j < len(lines); j++ {
				if strings.TrimSpace(lines[j]) == "endef" {
					endLine = j + 1
					break
				}
			}
			inDefine = true
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindVariable, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			continue
		}

		// .PHONY declaration
		if m := mkPhonyPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: ".PHONY", Kind: KindProperty, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed,
			})
			continue
		}

		// include
		if m := mkIncludePattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: "include " + strings.TrimSpace(m[1]), Kind: KindImport, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed,
			})
			continue
		}

		// Variable assignment (at column 0) — check before targets to avoid
		// matching "VAR :=" as a target.
		if !strings.HasPrefix(line, "\t") {
			if m := mkVarPattern.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, Symbol{
					Name: m[1], Kind: KindVariable, StartLine: lineNum, EndLine: lineNum,
					Signature: trimmed,
				})
				continue
			}
		}

		// Target (must start at column 0, not indented)
		if !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, " ") {
			if m := mkTargetPattern.FindStringSubmatch(line); m != nil {
				name := m[1]
				// Skip built-in special targets except .PHONY (already handled)
				if strings.HasPrefix(name, ".") {
					continue
				}
				endLine := findMakeTargetEnd(lines, i)
				symbols = append(symbols, Symbol{
					Name: name, Kind: KindFunction, StartLine: lineNum, EndLine: endLine,
					Signature: trimmed,
				})
				continue
			}
		}
	}

	return symbols
}

// findMakeTargetEnd finds the end of a Makefile target's recipe block.
// Recipe lines are indented with a tab.
func findMakeTargetEnd(lines []string, startIdx int) int {
	for i := startIdx + 1; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			continue
		}
		// Recipe lines start with a tab
		if !strings.HasPrefix(line, "\t") {
			return i
		}
	}
	return len(lines)
}
