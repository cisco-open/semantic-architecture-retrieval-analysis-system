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

func init() {
	Register(&EnvFileParser{})
	Register(&PropertiesParser{})
}

// ---------------------------------------------------------------------------
// .env parser
// ---------------------------------------------------------------------------

// EnvFileParser extracts symbols from .env files.
type EnvFileParser struct{}

func (p *EnvFileParser) Name() string         { return "env" }
func (p *EnvFileParser) Extensions() []string { return []string{".env"} }

func (p *EnvFileParser) FlowHints() FlowHints {
	return FlowHints{
		CommentPrefixes: []string{"#"},
	}
}

func (p *EnvFileParser) IsTestFile(path string) bool {
	return false
}

var (
	// KEY=value  or  export KEY=value
	envVarPattern = regexp.MustCompile(`^\s*(?:export\s+)?([A-Za-z_]\w*)\s*=`)
)

func (p *EnvFileParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if m := envVarPattern.FindStringSubmatch(line); m != nil {
			kind := KindVariable
			if strings.HasPrefix(trimmed, "export ") {
				kind = KindVariable
			}
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: kind, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed,
			})
		}
	}

	return symbols
}

// ---------------------------------------------------------------------------
// .properties parser
// ---------------------------------------------------------------------------

// PropertiesParser extracts symbols from Java .properties files.
type PropertiesParser struct{}

func (p *PropertiesParser) Name() string         { return "properties" }
func (p *PropertiesParser) Extensions() []string { return []string{".properties"} }

func (p *PropertiesParser) FlowHints() FlowHints {
	return FlowHints{
		CommentPrefixes: []string{"#", "!"},
	}
}

func (p *PropertiesParser) IsTestFile(path string) bool {
	return false
}

var (
	// key=value  or  key: value  or  key value
	propKeyPattern = regexp.MustCompile(`^\s*([a-zA-Z_][\w.-]*)\s*[=: ]`)
)

func (p *PropertiesParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "!") {
			continue
		}

		if m := propKeyPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindProperty, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed,
			})
		}
	}

	return symbols
}
