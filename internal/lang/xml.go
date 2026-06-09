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

func init() { Register(&XMLParser{}) }

// XMLParser extracts symbols from XML source files.
type XMLParser struct{}

func (p *XMLParser) Name() string { return "xml" }
func (p *XMLParser) Extensions() []string {
	return []string{".xml", ".xsl", ".xslt", ".xsd", ".plist", ".xaml"}
}

func (p *XMLParser) FlowHints() FlowHints {
	return FlowHints{
		CommentPrefixes: []string{"<!--"},
	}
}

func (p *XMLParser) IsTestFile(path string) bool {
	return false
}

var (
	xmlDeclPattern     = regexp.MustCompile(`<\?xml\s+[^?]*\?>`)
	xmlOpenTagPattern  = regexp.MustCompile(`^<([a-zA-Z][\w:.-]*)\b([^>]*)>`)
	xmlCloseTagPattern = regexp.MustCompile(`^</([a-zA-Z][\w:.-]*)\s*>`)
	xmlSelfClose       = regexp.MustCompile(`^<([a-zA-Z][\w:.-]*)\b([^>]*)/\s*>`)
	xmlIDAttr          = regexp.MustCompile(`\bid="([^"]+)"`)
	xmlNameAttr        = regexp.MustCompile(`\bname="([^"]+)"`)
	xmlNSPattern       = regexp.MustCompile(`xmlns(?::(\w+))?="([^"]+)"`)
)

var xmlTagStartPattern = regexp.MustCompile(`^<([a-zA-Z][\w:.-]*)\b`)

func (p *XMLParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	var tagStack []xmlTagCtx
	depth := 0

	// Track multi-line opening tags
	var pendingTag string
	var pendingAttrs string
	pendingStart := 0

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "<!--") {
			continue
		}

		// If we're accumulating a multi-line opening tag
		if pendingTag != "" {
			pendingAttrs += " " + trimmed
			if strings.Contains(line, ">") {
				selfClosing := strings.Contains(trimmed, "/>")
				name := buildXMLSymbolName(pendingTag, pendingAttrs)

				if selfClosing {
					symbols = append(symbols, Symbol{Name: name, Kind: KindProperty, StartLine: pendingStart, EndLine: lineNum, Signature: "<" + pendingTag + "..."})
				} else {
					kind := KindStruct
					if depth == 0 {
						kind = KindClass
					}
					sym := Symbol{Name: name, Kind: kind, StartLine: pendingStart, EndLine: lineNum, Signature: "<" + pendingTag + "...>"}
					symbols = append(symbols, sym)
					tagStack = append(tagStack, xmlTagCtx{tag: pendingTag, sym: &symbols[len(symbols)-1]})
					depth++
				}

				// Extract namespaces from accumulated attrs
				for _, m := range xmlNSPattern.FindAllStringSubmatch(pendingAttrs, -1) {
					prefix := m[1]
					uri := m[2]
					nsName := "xmlns"
					if prefix != "" {
						nsName = "xmlns:" + prefix
					}
					symbols = append(symbols, Symbol{Name: nsName, Kind: KindImport, StartLine: pendingStart, EndLine: lineNum, Signature: uri})
				}

				pendingTag = ""
				pendingAttrs = ""
			}
			continue
		}

		// XML declaration
		if xmlDeclPattern.MatchString(trimmed) {
			symbols = append(symbols, Symbol{Name: "xml-declaration", Kind: KindModule, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			continue
		}

		// Namespace declarations on single-line tags
		for _, m := range xmlNSPattern.FindAllStringSubmatch(line, -1) {
			prefix := m[1]
			uri := m[2]
			name := "xmlns"
			if prefix != "" {
				name = "xmlns:" + prefix
			}
			symbols = append(symbols, Symbol{Name: name, Kind: KindImport, StartLine: lineNum, EndLine: lineNum, Signature: uri})
		}

		// Closing tags
		if m := xmlCloseTagPattern.FindStringSubmatch(trimmed); m != nil {
			tag := m[1]
			for j := len(tagStack) - 1; j >= 0; j-- {
				if tagStack[j].tag == tag {
					tagStack[j].sym.EndLine = lineNum
					tagStack = tagStack[:j]
					break
				}
			}
			depth--
			if depth < 0 {
				depth = 0
			}
			continue
		}

		// Self-closing tags (single line)
		if m := xmlSelfClose.FindStringSubmatch(trimmed); m != nil {
			name := buildXMLSymbolName(m[1], m[2])
			symbols = append(symbols, Symbol{Name: name, Kind: KindProperty, StartLine: lineNum, EndLine: lineNum, Signature: trimmed})
			continue
		}

		// Opening tags (single line)
		if m := xmlOpenTagPattern.FindStringSubmatch(trimmed); m != nil {
			tag := m[1]
			attrs := m[2]
			name := buildXMLSymbolName(tag, attrs)

			kind := KindStruct
			if depth == 0 {
				kind = KindClass
			}

			sym := Symbol{Name: name, Kind: kind, StartLine: lineNum, EndLine: lineNum, Signature: trimmed}

			closeTag := "</" + tag
			if strings.Contains(line, closeTag) {
				symbols = append(symbols, sym)
				continue
			}

			symbols = append(symbols, sym)
			tagStack = append(tagStack, xmlTagCtx{tag: tag, sym: &symbols[len(symbols)-1]})
			depth++
			continue
		}

		// Multi-line opening tag (starts with <tag but no > on this line)
		if m := xmlTagStartPattern.FindStringSubmatch(trimmed); m != nil {
			if !strings.Contains(line, ">") {
				pendingTag = m[1]
				pendingAttrs = trimmed
				pendingStart = lineNum
			}
		}
	}

	// Close remaining open tags
	for j := len(tagStack) - 1; j >= 0; j-- {
		tagStack[j].sym.EndLine = len(lines)
	}

	return symbols
}

func buildXMLSymbolName(tag, attrs string) string {
	if m := xmlIDAttr.FindStringSubmatch(attrs); m != nil {
		return tag + "#" + m[1]
	}
	if m := xmlNameAttr.FindStringSubmatch(attrs); m != nil {
		return tag + "[" + m[1] + "]"
	}
	return tag
}

type xmlTagCtx struct {
	tag string
	sym *Symbol
}
