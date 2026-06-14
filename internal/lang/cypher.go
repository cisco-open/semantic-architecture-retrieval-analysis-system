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

func init() { Register(&CypherParser{}) }

// CypherParser extracts symbols from Cypher query files (Neo4j).
// Cypher is a declarative graph query language — it has no procedural control
// flow, so CFG generation is marked as unsupported. Semantic search, symbol
// extraction, and tracing still work.
type CypherParser struct{}

func (p *CypherParser) Name() string         { return "cypher" }
func (p *CypherParser) Extensions() []string { return []string{".cypher", ".cql"} }

func (p *CypherParser) FlowHints() FlowHints {
	return FlowHints{
		Keywords: []string{
			"MATCH", "OPTIONAL", "WHERE", "RETURN",
			"CREATE", "MERGE", "DELETE", "DETACH", "REMOVE", "SET",
			"WITH", "UNWIND", "FOREACH",
			"CALL", "YIELD",
			"ORDER", "SKIP", "LIMIT",
			"UNION", "UNION ALL",
			"CASE", "WHEN", "THEN", "ELSE", "END",
			"EXISTS", "NOT", "AND", "OR", "XOR", "IN",
			"AS", "DISTINCT", "COUNT",
			"INDEX", "CONSTRAINT", "UNIQUE", "ASSERT",
			"LOAD", "CSV", "HEADERS",
			"EXPLAIN", "PROFILE",
		},
		CommentPrefixes: []string{"//"},
	}
}

func (p *CypherParser) IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	parts := strings.Split(lower, "/")
	for _, part := range parts {
		switch part {
		case "test", "tests", "testdata", "fixtures":
			return true
		}
	}
	return strings.Contains(lower, "_test.cypher") ||
		strings.Contains(lower, "_test.cql")
}

var (
	// CREATE INDEX [name] [IF NOT EXISTS] FOR (n:Label) ON (n.prop)
	// CREATE INDEX name FOR ...
	cypherIndexPattern = regexp.MustCompile(`(?i)^\s*CREATE\s+(?:(?:RANGE|TEXT|POINT|FULLTEXT|LOOKUP|VECTOR)\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)`)
	// CREATE CONSTRAINT [name] [IF NOT EXISTS] FOR/ON ...
	cypherConstraintPattern = regexp.MustCompile(`(?i)^\s*CREATE\s+CONSTRAINT\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)`)
	// Node label pattern: (:Label) or (n:Label) in CREATE/MERGE
	cypherNodePattern = regexp.MustCompile(`(?i)^\s*(?:CREATE|MERGE)\s+\(\s*\w*\s*:\s*(\w+)`)
	// Relationship type in CREATE/MERGE: -[:TYPE]-> or -[:TYPE {props}]->
	cypherRelPattern = regexp.MustCompile(`\[(?:\w*\s*)?:\s*(\w+)[^\]]*\]`)
	// CALL db.procedure() or CALL custom.proc()
	cypherCallPattern = regexp.MustCompile(`(?i)^\s*CALL\s+([\w.]+)\s*\(`)
	// :param name => value (cypher-shell parameters)
	cypherParamPattern = regexp.MustCompile(`(?i)^\s*:param\s+(\w+)`)
)

func (p *CypherParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	seenLabels := make(map[string]bool)
	seenRels := make(map[string]bool)

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}

		// CREATE INDEX
		if m := cypherIndexPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum,
				Signature: normalizeSQL(trimmed),
			})
			continue
		}

		// CREATE CONSTRAINT
		if m := cypherConstraintPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum,
				Signature: normalizeSQL(trimmed),
			})
			continue
		}

		// Node labels from CREATE/MERGE
		if m := cypherNodePattern.FindStringSubmatch(line); m != nil {
			label := m[1]
			if !seenLabels[label] {
				seenLabels[label] = true
				symbols = append(symbols, Symbol{
					Name: label, Kind: KindType, StartLine: lineNum, EndLine: lineNum,
					Signature: normalizeSQL(trimmed),
				})
			}
		}

		// Relationship types (on any line that contains [-[:TYPE]->] patterns)
		if strings.Contains(line, "[") && strings.Contains(line, "]") {
			if matches := cypherRelPattern.FindAllStringSubmatch(line, -1); len(matches) > 0 {
				for _, m := range matches {
					rel := m[1]
					if !seenRels[rel] {
						seenRels[rel] = true
						symbols = append(symbols, Symbol{
							Name: rel, Kind: KindProperty, StartLine: lineNum, EndLine: lineNum,
							Signature: normalizeSQL(trimmed),
						})
					}
				}
			}
		}

		// CALL procedure
		if m := cypherCallPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: lineNum,
				Signature: normalizeSQL(trimmed),
			})
			continue
		}

		// :param declarations
		if m := cypherParamPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindVariable, StartLine: lineNum, EndLine: lineNum,
				Signature: normalizeSQL(trimmed),
			})
			continue
		}
	}

	return symbols
}
