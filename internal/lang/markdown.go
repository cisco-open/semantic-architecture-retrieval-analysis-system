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

func init() { Register(&MarkdownParser{}) }

// MarkdownParser extracts symbols from Markdown source files.
type MarkdownParser struct{}

func (p *MarkdownParser) Name() string         { return "markdown" }
func (p *MarkdownParser) Extensions() []string { return []string{".md", ".markdown", ".mdx"} }

func (p *MarkdownParser) FlowHints() FlowHints {
	return FlowHints{
		CommentPrefixes: []string{"<!--"},
	}
}

func (p *MarkdownParser) IsTestFile(path string) bool {
	return false
}

var (
	// # Heading, ## Heading, ### Heading, etc.
	mdHeadingPattern = regexp.MustCompile(`^(#{1,6})\s+(.+)`)
	// ```language  (fenced code block)
	mdCodeFenceOpen  = regexp.MustCompile("^```(\\w+)")
	mdCodeFenceClose = regexp.MustCompile("^```\\s*$")
	// [def]: url  (link reference definition)
	mdLinkDefPattern = regexp.MustCompile(`^\[([^\]]+)\]:\s+(.+)`)
	// YAML front matter delimiter
	mdFrontMatterDelim = regexp.MustCompile(`^---\s*$`)
	// YAML key in front matter: key: value
	mdFrontMatterKey = regexp.MustCompile(`^(\w[\w-]*)\s*:`)
)

func (p *MarkdownParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol

	inCodeBlock := false
	inFrontMatter := false
	frontMatterStarted := false

	// Track heading positions for EndLine calculation
	type headingEntry struct {
		idx   int // index in symbols slice
		level int
	}
	var headingStack []headingEntry

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		// YAML front matter (must be at the very start of the file)
		if i == 0 && mdFrontMatterDelim.MatchString(trimmed) {
			inFrontMatter = true
			frontMatterStarted = true
			continue
		}
		if inFrontMatter {
			if mdFrontMatterDelim.MatchString(trimmed) {
				inFrontMatter = false
				continue
			}
			// Top-level front matter keys (not indented)
			if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				if m := mdFrontMatterKey.FindStringSubmatch(trimmed); m != nil {
					symbols = append(symbols, Symbol{
						Name: m[1], Kind: KindProperty, StartLine: lineNum, EndLine: lineNum,
						Signature: trimmed,
					})
				}
			}
			continue
		}

		// Fenced code blocks — toggle and skip contents
		if mdCodeFenceOpen.MatchString(trimmed) && !inCodeBlock {
			inCodeBlock = true
			if m := mdCodeFenceOpen.FindStringSubmatch(trimmed); m != nil {
				symbols = append(symbols, Symbol{
					Name: "code:" + m[1], Kind: KindModule, StartLine: lineNum, EndLine: lineNum,
					Signature: trimmed,
				})
			}
			continue
		}
		if mdCodeFenceClose.MatchString(trimmed) && inCodeBlock {
			// Update end line of the last code block symbol
			for j := len(symbols) - 1; j >= 0; j-- {
				if strings.HasPrefix(symbols[j].Name, "code:") && symbols[j].EndLine == symbols[j].StartLine {
					symbols[j].EndLine = lineNum
					break
				}
			}
			inCodeBlock = false
			continue
		}
		if inCodeBlock {
			continue
		}

		// Headings
		if m := mdHeadingPattern.FindStringSubmatch(line); m != nil {
			level := len(m[1])
			title := strings.TrimSpace(m[2])

			// Close headings of same or deeper level
			for len(headingStack) > 0 && headingStack[len(headingStack)-1].level >= level {
				top := headingStack[len(headingStack)-1]
				symbols[top.idx].EndLine = lineNum - 1
				headingStack = headingStack[:len(headingStack)-1]
			}

			kind := KindModule
			if level >= 3 {
				kind = KindFunction
			}

			sym := Symbol{
				Name: title, Kind: kind, StartLine: lineNum, EndLine: len(lines),
				Signature: trimmed,
			}
			symbols = append(symbols, sym)
			headingStack = append(headingStack, headingEntry{idx: len(symbols) - 1, level: level})
			continue
		}

		// Link reference definitions
		if m := mdLinkDefPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed,
			})
			continue
		}
	}

	// Close any remaining open headings
	_ = frontMatterStarted // used for front matter detection

	return symbols
}
