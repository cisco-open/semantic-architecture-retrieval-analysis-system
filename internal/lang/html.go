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

func init() { Register(&HTMLParser{}) }

// HTMLParser extracts symbols from HTML source files.
type HTMLParser struct{}

func (p *HTMLParser) Name() string         { return "html" }
func (p *HTMLParser) Extensions() []string { return []string{".html", ".htm"} }

func (p *HTMLParser) FlowHints() FlowHints {
	return FlowHints{
		CommentPrefixes: []string{"<!--"},
	}
}

func (p *HTMLParser) IsTestFile(path string) bool {
	return false
}

var (
	htmlIDPattern       = regexp.MustCompile(`\bid="([^"]+)"`)
	htmlClassPattern    = regexp.MustCompile(`\bclass="([^"]+)"`)
	htmlTagOpenPattern  = regexp.MustCompile(`^<(script|style|template|head|body|main|header|footer|nav|section|article|aside|form|table|div)\b[^>]*>`)
	htmlTagClosePattern = regexp.MustCompile(`^</(script|style|template|head|body|main|header|footer|nav|section|article|aside|form|table|div)\s*>`)
	htmlCustomElem      = regexp.MustCompile(`^<([a-z]+-[a-z][\w-]*)\b`)
	htmlDoctype         = regexp.MustCompile(`(?i)^<!DOCTYPE\s+(\w+)`)
	htmlMetaName        = regexp.MustCompile(`<meta\s+[^>]*name="([^"]+)"`)
)

func (p *HTMLParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	var tagStack []tagCtx

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		// DOCTYPE
		if m := htmlDoctype.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{Name: "DOCTYPE " + m[1], Kind: KindModule, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			continue
		}

		// Closing tags — pop stack
		if m := htmlTagClosePattern.FindStringSubmatch(trimmed); m != nil {
			tag := strings.ToLower(m[1])
			for j := len(tagStack) - 1; j >= 0; j-- {
				if tagStack[j].tag == tag {
					tagStack[j].sym.EndLine = lineNum
					tagStack = tagStack[:j]
					break
				}
			}
			continue
		}

		// Structural opening tags
		if m := htmlTagOpenPattern.FindStringSubmatch(trimmed); m != nil {
			tag := strings.ToLower(m[1])
			name := tag

			// Use id if present
			if idm := htmlIDPattern.FindStringSubmatch(line); idm != nil {
				name = tag + "#" + idm[1]
			}

			sym := Symbol{Name: name, Kind: KindStruct, StartLine: lineNum, EndLine: lineNum, Signature: trimmed}

			// Self-closing check
			if strings.HasSuffix(strings.TrimSpace(trimmed), "/>") {
				symbols = append(symbols, sym)
				continue
			}

			// Also check if closing tag is on same line
			closeTag := "</" + tag
			if strings.Contains(line, closeTag) {
				symbols = append(symbols, sym)
				continue
			}

			symbols = append(symbols, sym)
			tagStack = append(tagStack, tagCtx{tag: tag, sym: &symbols[len(symbols)-1]})
			continue
		}

		// Custom elements (web components)
		if m := htmlCustomElem.FindStringSubmatch(trimmed); m != nil {
			name := m[1]
			if idm := htmlIDPattern.FindStringSubmatch(line); idm != nil {
				name = m[1] + "#" + idm[1]
			}
			symbols = append(symbols, Symbol{Name: name, Kind: KindClass, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			continue
		}

		// Elements with id= attributes (catch-all for non-structural tags)
		if idm := htmlIDPattern.FindStringSubmatch(line); idm != nil {
			// Only if not already captured by structural tag patterns
			if !htmlTagOpenPattern.MatchString(trimmed) {
				symbols = append(symbols, Symbol{Name: "#" + idm[1], Kind: KindProperty, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			}
		}

		// <meta name="...">
		if m := htmlMetaName.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{Name: "meta:" + m[1], Kind: KindProperty, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
		}
	}

	// Close remaining open tags
	for j := len(tagStack) - 1; j >= 0; j-- {
		tagStack[j].sym.EndLine = len(lines)
	}

	return symbols
}

type tagCtx struct {
	tag string
	sym *Symbol
}
