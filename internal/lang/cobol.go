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

func init() { Register(&COBOLParser{}) }

// COBOLParser extracts symbols from COBOL source files.
// It handles both fixed-format (columns 1-6 seq, 7 indicator, 8-72 code)
// and free-format COBOL. Copybooks (.cpy) are also supported.
type COBOLParser struct{}

func (p *COBOLParser) Name() string         { return "cobol" }
func (p *COBOLParser) Extensions() []string { return []string{".cob", ".cbl", ".cpy", ".cobol"} }

func (p *COBOLParser) FlowHints() FlowHints {
	return FlowHints{
		Keywords: []string{
			"PERFORM", "CALL", "GO", "TO", "IF", "ELSE", "END-IF",
			"EVALUATE", "WHEN", "END-EVALUATE",
			"MOVE", "COMPUTE", "ADD", "SUBTRACT", "MULTIPLY", "DIVIDE",
			"READ", "WRITE", "REWRITE", "DELETE", "START",
			"OPEN", "CLOSE", "DISPLAY", "ACCEPT", "STOP", "EXIT",
			"STRING", "UNSTRING", "INSPECT", "SEARCH",
			"INITIALIZE", "SET", "SORT", "MERGE", "RETURN",
			"SECTION", "DIVISION", "PARAGRAPH",
			"WORKING-STORAGE", "LINKAGE", "FILE", "LOCAL-STORAGE",
			"PROCEDURE", "DATA", "ENVIRONMENT", "IDENTIFICATION",
			"COPY", "REPLACE", "EXEC", "END-EXEC",
		},
		CommentPrefixes: []string{"*"},
	}
}

func (p *COBOLParser) IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	parts := strings.Split(lower, "/")
	for _, part := range parts {
		switch part {
		case "test", "tests", "testdata", "fixtures":
			return true
		}
	}
	return strings.Contains(lower, "_test.cob") ||
		strings.Contains(lower, "_test.cbl") ||
		strings.Contains(lower, "test-") ||
		strings.Contains(lower, "-test.")
}

var (
	// PROGRAM-ID. program-name.
	cobolProgramIDPattern = regexp.MustCompile(`(?i)^\s*PROGRAM-ID\.\s+(\S+?)[\s.]`)
	// FUNCTION-ID. function-name.
	cobolFunctionIDPattern = regexp.MustCompile(`(?i)^\s*FUNCTION-ID\.\s+(\S+?)[\s.]`)
	// CLASS-ID. class-name.
	cobolClassIDPattern = regexp.MustCompile(`(?i)^\s*CLASS-ID\.\s+(\S+?)[\s.]`)
	// DIVISION headers
	cobolDivisionPattern = regexp.MustCompile(`(?i)^\s*(IDENTIFICATION|ENVIRONMENT|DATA|PROCEDURE)\s+DIVISION`)
	// SECTION in PROCEDURE DIVISION: section-name SECTION.
	cobolSectionPattern = regexp.MustCompile(`(?i)^\s*(\S+)\s+SECTION\s*\.`)
	// Paragraph: a word starting in area A (col 8-11) followed by a period
	// In free format, any line that is a single word followed by a period
	cobolParagraphPattern = regexp.MustCompile(`(?i)^(\s{0,3})([A-Z][A-Z0-9-]+)\s*\.\s*$`)
	// COPY statement: COPY copybook-name.
	cobolCopyPattern = regexp.MustCompile(`(?i)^\s*COPY\s+['"]?(\S+?)['"]?\s*\.`)
	// 01 level data items in WORKING-STORAGE, LINKAGE, etc.
	cobolDataLevel01Pattern = regexp.MustCompile(`(?i)^\s*01\s+(\S+)`)
	// 77 level data items (independent items)
	cobolDataLevel77Pattern = regexp.MustCompile(`(?i)^\s*77\s+(\S+)`)
	// 88 level condition names
	cobolLevel88Pattern = regexp.MustCompile(`(?i)^\s*88\s+(\S+)`)
	// FD file-name or SD sort-file-name
	cobolFDPattern = regexp.MustCompile(`(?i)^\s*(?:FD|SD)\s+(\S+)`)
	// EXEC SQL / EXEC CICS blocks
	cobolExecPattern = regexp.MustCompile(`(?i)^\s*EXEC\s+(SQL|CICS|DLI)`)
)

// extractCodeArea strips fixed-format COBOL columns:
// columns 1-6 are sequence numbers, column 7 is indicator (* = comment, - = continuation).
// Returns the code area (columns 8-72+) and whether the line is a comment.
func extractCodeArea(line string) (string, bool) {
	if len(line) < 7 {
		return strings.TrimSpace(line), false
	}
	indicator := line[6]
	if indicator == '*' || indicator == '/' || indicator == 'd' || indicator == 'D' {
		return "", true
	}
	code := line[7:]
	if len(code) > 65 {
		code = code[:65] // columns 8-72
	}
	return strings.TrimSpace(code), false
}

// isFixedFormat heuristically checks if the content is fixed-format COBOL
// by looking for 6-digit sequence numbers in the first few non-empty lines.
// True fixed-format has columns 1-6 as sequence numbers (typically 6 digits),
// column 7 as indicator area.
func isFixedFormat(lines []string) bool {
	seqCount := 0
	checked := 0
	for _, line := range lines {
		if len(line) < 7 || strings.TrimSpace(line) == "" {
			continue
		}
		checked++
		seq := line[:6]
		// Real sequence numbers are 6 digits (e.g., "000100")
		hasDigit := false
		allDigitsOrSpaces := true
		for _, ch := range seq {
			if ch >= '0' && ch <= '9' {
				hasDigit = true
			} else if ch != ' ' {
				allDigitsOrSpaces = false
				break
			}
		}
		// Require at least 4 digits to distinguish from indented code
		digitCount := 0
		for _, ch := range seq {
			if ch >= '0' && ch <= '9' {
				digitCount++
			}
		}
		if allDigitsOrSpaces && hasDigit && digitCount >= 4 {
			seqCount++
		}
		if checked >= 10 {
			break
		}
	}
	return checked > 0 && seqCount > checked/2
}

func (p *COBOLParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	fixedFormat := isFixedFormat(lines)
	inProcedureDivision := false
	// Default to data context if no DIVISION headers are found (e.g. copybooks)
	hasDivisions := strings.Contains(strings.ToUpper(content), "DIVISION")
	inDataDivision := !hasDivisions
	currentSection := ""

	for i, rawLine := range lines {
		lineNum := i + 1
		var code string
		var isComment bool

		if fixedFormat {
			code, isComment = extractCodeArea(rawLine)
		} else {
			code = strings.TrimSpace(rawLine)
			isComment = strings.HasPrefix(code, "*>") || strings.HasPrefix(code, "*")
		}

		if code == "" || isComment {
			continue
		}

		upper := strings.ToUpper(code)

		// DIVISION tracking
		if m := cobolDivisionPattern.FindStringSubmatch(upper); m != nil {
			div := strings.ToUpper(m[1])
			inProcedureDivision = div == "PROCEDURE"
			inDataDivision = div == "DATA"
			currentSection = ""
			continue
		}

		// PROGRAM-ID
		if m := cobolProgramIDPattern.FindStringSubmatch(code); m != nil {
			name := strings.TrimSuffix(m[1], ".")
			symbols = append(symbols, Symbol{
				Name: name, Kind: KindModule, StartLine: lineNum, EndLine: lineNum,
				Signature: strings.TrimSpace(code),
			})
			continue
		}

		// FUNCTION-ID
		if m := cobolFunctionIDPattern.FindStringSubmatch(code); m != nil {
			name := strings.TrimSuffix(m[1], ".")
			symbols = append(symbols, Symbol{
				Name: name, Kind: KindFunction, StartLine: lineNum, EndLine: lineNum,
				Signature: strings.TrimSpace(code),
			})
			continue
		}

		// CLASS-ID
		if m := cobolClassIDPattern.FindStringSubmatch(code); m != nil {
			name := strings.TrimSuffix(m[1], ".")
			symbols = append(symbols, Symbol{
				Name: name, Kind: KindClass, StartLine: lineNum, EndLine: lineNum,
				Signature: strings.TrimSpace(code),
			})
			continue
		}

		// COPY
		if m := cobolCopyPattern.FindStringSubmatch(code); m != nil {
			name := strings.TrimSuffix(m[1], ".")
			symbols = append(symbols, Symbol{
				Name: name, Kind: KindImport, StartLine: lineNum, EndLine: lineNum,
				Signature: strings.TrimSpace(code),
			})
			continue
		}

		// FD / SD
		if m := cobolFDPattern.FindStringSubmatch(code); m != nil {
			name := strings.TrimSuffix(m[1], ".")
			symbols = append(symbols, Symbol{
				Name: name, Kind: KindType, StartLine: lineNum, EndLine: lineNum,
				Signature: strings.TrimSpace(code),
			})
			continue
		}

		// SECTION in PROCEDURE DIVISION
		if inProcedureDivision {
			if m := cobolSectionPattern.FindStringSubmatch(code); m != nil {
				name := strings.TrimSuffix(m[1], ".")
				// Skip DATA/ENVIRONMENT/IDENTIFICATION division section names
				nameUpper := strings.ToUpper(name)
				if nameUpper != "IDENTIFICATION" && nameUpper != "ENVIRONMENT" &&
					nameUpper != "DATA" && nameUpper != "PROCEDURE" {
					currentSection = name
					symbols = append(symbols, Symbol{
						Name: name, Kind: KindFunction, StartLine: lineNum, EndLine: lineNum,
						Signature: strings.TrimSpace(code),
					})
				}
				continue
			}

			// Paragraph: a line starting with a word (area A) followed by period only
			if m := cobolParagraphPattern.FindStringSubmatch(code); m != nil {
				name := m[2]
				nameUpper := strings.ToUpper(name)
				// Skip known keywords that look like paragraphs
				if nameUpper != "EXIT" && nameUpper != "STOP" && nameUpper != "END" &&
					nameUpper != "ELSE" && nameUpper != "WHEN" {
					parent := ""
					if currentSection != "" {
						parent = currentSection
					}
					symbols = append(symbols, Symbol{
						Name: name, Kind: KindFunction, StartLine: lineNum, EndLine: lineNum,
						Signature: strings.TrimSpace(code), Parent: parent,
					})
				}
				continue
			}
		}

		// Data division items
		if inDataDivision {
			// 01 level
			if m := cobolDataLevel01Pattern.FindStringSubmatch(code); m != nil {
				name := strings.TrimSuffix(m[1], ".")
				if strings.ToUpper(name) != "FILLER" {
					symbols = append(symbols, Symbol{
						Name: name, Kind: KindVariable, StartLine: lineNum, EndLine: lineNum,
						Signature: strings.TrimSpace(code),
					})
				}
				continue
			}

			// 77 level
			if m := cobolDataLevel77Pattern.FindStringSubmatch(code); m != nil {
				name := strings.TrimSuffix(m[1], ".")
				symbols = append(symbols, Symbol{
					Name: name, Kind: KindVariable, StartLine: lineNum, EndLine: lineNum,
					Signature: strings.TrimSpace(code),
				})
				continue
			}

			// 88 level condition
			if m := cobolLevel88Pattern.FindStringSubmatch(code); m != nil {
				name := strings.TrimSuffix(m[1], ".")
				symbols = append(symbols, Symbol{
					Name: name, Kind: KindConstant, StartLine: lineNum, EndLine: lineNum,
					Signature: strings.TrimSpace(code),
				})
				continue
			}
		}
	}

	return symbols
}
