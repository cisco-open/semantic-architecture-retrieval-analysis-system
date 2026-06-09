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

func init() { Register(&CSSParser{}) }

// CSSParser extracts symbols from CSS/SCSS/LESS source files.
type CSSParser struct{}

func (p *CSSParser) Name() string         { return "css" }
func (p *CSSParser) Extensions() []string { return []string{".css", ".scss", ".less", ".sass"} }

func (p *CSSParser) FlowHints() FlowHints {
	return FlowHints{
		CommentPrefixes: []string{"//"},
	}
}

func (p *CSSParser) IsTestFile(path string) bool {
	return false // CSS doesn't have test files
}

var (
	cssSelectorPattern  = regexp.MustCompile(`^([.#][\w-]+(?:\s*[>,+~]\s*[.#]?[\w-]+)*)\s*\{`)
	cssMediaPattern     = regexp.MustCompile(`^@media\s+(.+?)\s*\{`)
	cssKeyframesPattern = regexp.MustCompile(`^@keyframes\s+([\w-]+)\s*\{`)
	cssVarPattern       = regexp.MustCompile(`^\s*(--[\w-]+)\s*:`)
	cssMixinPattern     = regexp.MustCompile(`^@mixin\s+([\w-]+)`)
	cssFontFacePattern  = regexp.MustCompile(`^@font-face\s*\{`)
	cssLayerPattern     = regexp.MustCompile(`^@layer\s+([\w-]+)`)
	cssCustomSelPattern = regexp.MustCompile(`^([\w-]+(?:\s+[\w-]+)*)\s*\{`)
)

func (p *CSSParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	braceDepth := 0

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
			braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
			continue
		}

		prevDepth := braceDepth
		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")

		// @keyframes
		if m := cssKeyframesPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// @media
		if m := cssMediaPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: "@media " + m[1], Kind: KindModule, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// @mixin (SCSS)
		if m := cssMixinPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// @layer
		if m := cssLayerPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: m[1], Kind: KindModule, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// @font-face
		if cssFontFacePattern.MatchString(trimmed) {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{Name: "@font-face", Kind: KindType, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
			continue
		}

		// CSS custom properties (--var-name)
		if prevDepth > 0 {
			if m := cssVarPattern.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, Symbol{Name: m[1], Kind: KindVariable, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
				continue
			}
		}

		// Class/ID selectors at top level
		if prevDepth == 0 {
			if m := cssSelectorPattern.FindStringSubmatch(trimmed); m != nil {
				endLine := findBraceEnd(lines, i, prevDepth)
				symbols = append(symbols, Symbol{Name: m[1], Kind: KindClass, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
				continue
			}
			// Element selectors (body, main, header, etc.)
			if m := cssCustomSelPattern.FindStringSubmatch(trimmed); m != nil {
				name := m[1]
				if !strings.HasPrefix(name, "@") {
					endLine := findBraceEnd(lines, i, prevDepth)
					symbols = append(symbols, Symbol{Name: name, Kind: KindClass, StartLine: lineNum, EndLine: endLine, Signature: trimmed})
				}
			}
		}
	}

	return symbols
}
