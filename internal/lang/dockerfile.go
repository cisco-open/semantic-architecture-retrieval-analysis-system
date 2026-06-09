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

func init() { Register(&DockerfileParser{}) }

// DockerfileParser extracts symbols from Dockerfile and Containerfile sources.
type DockerfileParser struct{}

func (p *DockerfileParser) Name() string         { return "dockerfile" }
func (p *DockerfileParser) Extensions() []string { return []string{".dockerfile", ".containerfile"} }

// Filenames returns exact filenames this parser handles (no extension).
func (p *DockerfileParser) Filenames() []string {
	return []string{"Dockerfile", "Containerfile"}
}

func (p *DockerfileParser) FlowHints() FlowHints {
	return FlowHints{
		CommentPrefixes: []string{"#"},
	}
}

func (p *DockerfileParser) IsTestFile(path string) bool {
	return false
}

var (
	// FROM image:tag AS name
	dfFromPattern = regexp.MustCompile(`(?i)^\s*FROM\s+(\S+)(?:\s+AS\s+(\w+))?`)
	// ARG NAME=value
	dfArgPattern = regexp.MustCompile(`(?i)^\s*ARG\s+(\w+)`)
	// ENV KEY=value  or  ENV KEY value
	dfEnvPattern = regexp.MustCompile(`(?i)^\s*ENV\s+(\w+)`)
	// LABEL key=value
	dfLabelPattern = regexp.MustCompile(`(?i)^\s*LABEL\s+(\S+)`)
	// EXPOSE port
	dfExposePattern = regexp.MustCompile(`(?i)^\s*EXPOSE\s+(.+)`)
	// ENTRYPOINT [...] or ENTRYPOINT cmd
	dfEntrypointPattern = regexp.MustCompile(`(?i)^\s*ENTRYPOINT\s+(.+)`)
	// CMD [...] or CMD cmd
	dfCmdPattern = regexp.MustCompile(`(?i)^\s*CMD\s+(.+)`)
	// WORKDIR /path
	dfWorkdirPattern = regexp.MustCompile(`(?i)^\s*WORKDIR\s+(\S+)`)
	// VOLUME ["/data"] or VOLUME /data
	dfVolumePattern = regexp.MustCompile(`(?i)^\s*VOLUME\s+(.+)`)
	// HEALTHCHECK
	dfHealthcheckPattern = regexp.MustCompile(`(?i)^\s*HEALTHCHECK\s+(.+)`)
)

func (p *DockerfileParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol

	currentStage := ""

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// FROM — build stage
		if m := dfFromPattern.FindStringSubmatch(line); m != nil {
			image := m[1]
			if m[2] != "" {
				currentStage = m[2]
				symbols = append(symbols, Symbol{
					Name: m[2], Kind: KindModule, StartLine: lineNum, EndLine: len(lines),
					Signature: trimmed,
				})
			} else {
				currentStage = image
				symbols = append(symbols, Symbol{
					Name: image, Kind: KindModule, StartLine: lineNum, EndLine: len(lines),
					Signature: trimmed,
				})
			}
			// Close previous stage's EndLine
			for j := len(symbols) - 2; j >= 0; j-- {
				if symbols[j].Kind == KindModule {
					symbols[j].EndLine = lineNum - 1
					break
				}
			}
			continue
		}

		// ARG
		if m := dfArgPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindVariable, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed, Parent: currentStage,
			})
			continue
		}

		// ENV
		if m := dfEnvPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindVariable, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed, Parent: currentStage,
			})
			continue
		}

		// EXPOSE
		if m := dfExposePattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: "EXPOSE " + strings.TrimSpace(m[1]), Kind: KindProperty, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed, Parent: currentStage,
			})
			continue
		}

		// ENTRYPOINT
		if dfEntrypointPattern.MatchString(line) {
			symbols = append(symbols, Symbol{
				Name: "ENTRYPOINT", Kind: KindFunction, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed, Parent: currentStage,
			})
			continue
		}

		// CMD
		if dfCmdPattern.MatchString(line) {
			symbols = append(symbols, Symbol{
				Name: "CMD", Kind: KindFunction, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed, Parent: currentStage,
			})
			continue
		}

		// WORKDIR
		if m := dfWorkdirPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: "WORKDIR " + m[1], Kind: KindProperty, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed, Parent: currentStage,
			})
			continue
		}

		// LABEL
		if m := dfLabelPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: "LABEL " + m[1], Kind: KindProperty, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed, Parent: currentStage,
			})
			continue
		}

		// VOLUME
		if m := dfVolumePattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: "VOLUME " + strings.TrimSpace(m[1]), Kind: KindProperty, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed, Parent: currentStage,
			})
			continue
		}

		// HEALTHCHECK
		if dfHealthcheckPattern.MatchString(line) {
			symbols = append(symbols, Symbol{
				Name: "HEALTHCHECK", Kind: KindFunction, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed, Parent: currentStage,
			})
			continue
		}
	}

	return symbols
}
