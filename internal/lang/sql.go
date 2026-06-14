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

func init() { Register(&SQLParser{}) }

// SQLParser extracts symbols from SQL files (DDL, DML, stored procedures).
// Covers common SQL dialects: ANSI SQL, PostgreSQL PL/pgSQL, MySQL, T-SQL,
// Oracle PL/SQL.
type SQLParser struct{}

func (p *SQLParser) Name() string { return "sql" }
func (p *SQLParser) Extensions() []string {
	return []string{".sql", ".ddl", ".dml", ".pgsql", ".plsql"}
}

func (p *SQLParser) FlowHints() FlowHints {
	return FlowHints{
		Keywords: []string{
			"SELECT", "INSERT", "UPDATE", "DELETE", "MERGE",
			"CREATE", "ALTER", "DROP", "TRUNCATE",
			"BEGIN", "END", "COMMIT", "ROLLBACK", "SAVEPOINT",
			"IF", "ELSE", "ELSEIF", "ELSIF", "THEN", "CASE", "WHEN",
			"WHILE", "LOOP", "FOR", "REPEAT", "LEAVE", "ITERATE",
			"DECLARE", "SET", "INTO", "FETCH", "OPEN", "CLOSE",
			"CURSOR", "EXCEPTION", "RAISE", "SIGNAL",
			"RETURN", "RETURNS", "CALL", "EXECUTE", "EXEC",
			"GRANT", "REVOKE", "DENY",
			"JOIN", "LEFT", "RIGHT", "INNER", "OUTER", "CROSS",
			"WHERE", "GROUP", "HAVING", "ORDER", "LIMIT", "OFFSET",
			"UNION", "INTERSECT", "EXCEPT",
			"INDEX", "CONSTRAINT", "PRIMARY", "FOREIGN", "UNIQUE",
			"TRIGGER", "EVENT", "SEQUENCE",
			"SCHEMA", "DATABASE", "TABLE", "VIEW", "FUNCTION", "PROCEDURE",
		},
		CommentPrefixes: []string{"--"},
	}
}

func (p *SQLParser) IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	parts := strings.Split(lower, "/")
	for _, part := range parts {
		switch part {
		case "test", "tests", "testdata", "fixtures", "seed":
			return true
		}
	}
	return strings.Contains(lower, "_test.sql") ||
		strings.Contains(lower, "_test.ddl") ||
		strings.Contains(lower, "_test.dml")
}

var (
	// CREATE [OR REPLACE] FUNCTION [schema.]name
	sqlFuncPattern = regexp.MustCompile(`(?i)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+(?:(?:\w+)\.)?(\w+)`)
	// CREATE [OR REPLACE] PROCEDURE [schema.]name
	sqlProcPattern = regexp.MustCompile(`(?i)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?PROCEDURE\s+(?:(?:\w+)\.)?(\w+)`)
	// CREATE [OR REPLACE] [TEMP|TEMPORARY] TABLE [IF NOT EXISTS] [schema.]name
	sqlTablePattern = regexp.MustCompile(`(?i)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:(?:TEMP|TEMPORARY|GLOBAL\s+TEMPORARY|LOCAL\s+TEMPORARY)\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:(?:\w+)\.)?(\w+)`)
	// CREATE [OR REPLACE] [TEMP|TEMPORARY] VIEW [schema.]name
	sqlViewPattern = regexp.MustCompile(`(?i)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:(?:TEMP|TEMPORARY|MATERIALIZED)\s+)?VIEW\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:(?:\w+)\.)?(\w+)`)
	// CREATE [OR REPLACE] TRIGGER [schema.]name
	sqlTriggerPattern = regexp.MustCompile(`(?i)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:CONSTRAINT\s+)?TRIGGER\s+(?:(?:\w+)\.)?(\w+)`)
	// CREATE [UNIQUE] INDEX [CONCURRENTLY] [IF NOT EXISTS] name
	sqlIndexPattern = regexp.MustCompile(`(?i)^\s*CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:CONCURRENTLY\s+)?(?:IF\s+NOT\s+EXISTS\s+)?(\w+)`)
	// CREATE SCHEMA [IF NOT EXISTS] name
	sqlSchemaPattern = regexp.MustCompile(`(?i)^\s*CREATE\s+SCHEMA\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)`)
	// CREATE TYPE [schema.]name
	sqlTypePattern = regexp.MustCompile(`(?i)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?TYPE\s+(?:(?:\w+)\.)?(\w+)`)
	// CREATE SEQUENCE [schema.]name
	sqlSequencePattern = regexp.MustCompile(`(?i)^\s*CREATE\s+SEQUENCE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:(?:\w+)\.)?(\w+)`)
	// ALTER TABLE [schema.]name
	sqlAlterTablePattern = regexp.MustCompile(`(?i)^\s*ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:(?:\w+)\.)?(\w+)`)
	// DECLARE @var or DECLARE var (T-SQL / PL/pgSQL)
	sqlDeclarePattern = regexp.MustCompile(`(?i)^\s*DECLARE\s+@?(\w+)`)
)

func (p *SQLParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	inBlock := false // track multi-line block (for finding END)

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}

		// Skip block comments
		if strings.HasPrefix(trimmed, "/*") {
			continue
		}

		// CREATE FUNCTION
		if m := sqlFuncPattern.FindStringSubmatch(line); m != nil {
			endLine := findSQLBlockEnd(lines, i)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: endLine,
				Signature: normalizeSQL(trimmed),
			})
			inBlock = true
			continue
		}

		// CREATE PROCEDURE
		if m := sqlProcPattern.FindStringSubmatch(line); m != nil {
			endLine := findSQLBlockEnd(lines, i)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: endLine,
				Signature: normalizeSQL(trimmed),
			})
			inBlock = true
			continue
		}

		// CREATE TRIGGER
		if m := sqlTriggerPattern.FindStringSubmatch(line); m != nil {
			endLine := findSQLBlockEnd(lines, i)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: endLine,
				Signature: normalizeSQL(trimmed),
			})
			inBlock = true
			continue
		}

		// CREATE TABLE
		if m := sqlTablePattern.FindStringSubmatch(line); m != nil {
			endLine := findSQLStatementEnd(lines, i)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindType, StartLine: lineNum, EndLine: endLine,
				Signature: normalizeSQL(trimmed),
			})
			continue
		}

		// CREATE VIEW
		if m := sqlViewPattern.FindStringSubmatch(line); m != nil {
			endLine := findSQLStatementEnd(lines, i)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindType, StartLine: lineNum, EndLine: endLine,
				Signature: normalizeSQL(trimmed),
			})
			continue
		}

		// CREATE TYPE
		if m := sqlTypePattern.FindStringSubmatch(line); m != nil {
			endLine := findSQLStatementEnd(lines, i)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindType, StartLine: lineNum, EndLine: endLine,
				Signature: normalizeSQL(trimmed),
			})
			continue
		}

		// CREATE SCHEMA
		if m := sqlSchemaPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindModule, StartLine: lineNum, EndLine: lineNum,
				Signature: normalizeSQL(trimmed),
			})
			continue
		}

		// CREATE SEQUENCE
		if m := sqlSequencePattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum,
				Signature: normalizeSQL(trimmed),
			})
			continue
		}

		// CREATE INDEX
		if m := sqlIndexPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum,
				Signature: normalizeSQL(trimmed),
			})
			continue
		}

		// ALTER TABLE (only at top level, not inside a procedure)
		if !inBlock {
			if m := sqlAlterTablePattern.FindStringSubmatch(line); m != nil {
				endLine := findSQLStatementEnd(lines, i)
				symbols = append(symbols, Symbol{
					Name: m[1], Kind: KindType, StartLine: lineNum, EndLine: endLine,
					Signature: normalizeSQL(trimmed),
					Parent:    "ALTER",
				})
				continue
			}
		}

		// DECLARE variable
		if m := sqlDeclarePattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindVariable, StartLine: lineNum, EndLine: lineNum,
				Signature: normalizeSQL(trimmed),
			})
			continue
		}

		// Reset block tracking on END
		upper := strings.ToUpper(trimmed)
		if inBlock && (upper == "END;" || upper == "END" || strings.HasPrefix(upper, "END;") ||
			strings.HasPrefix(upper, "END ") || upper == "$$;" || upper == "$$") {
			inBlock = false
		}
	}

	return symbols
}

// findSQLBlockEnd finds the END of a BEGIN..END block (functions, procedures, triggers).
// It handles both BEGIN/END nesting and PostgreSQL $$ delimiters.
func findSQLBlockEnd(lines []string, startIdx int) int {
	depth := 0
	dollarCount := 0 // track $$ pairs

	for i := startIdx; i < len(lines); i++ {
		upper := strings.ToUpper(strings.TrimSpace(lines[i]))

		// Strip comments
		if idx := strings.Index(upper, "--"); idx >= 0 {
			upper = strings.TrimSpace(upper[:idx])
		}
		if upper == "" {
			continue
		}

		// $$ delimiter (PostgreSQL) — count pairs
		if strings.Contains(upper, "$$") {
			dollarCount += strings.Count(upper, "$$")
			if dollarCount >= 2 {
				return i + 1
			}
			continue
		}

		// Count BEGIN/END nesting (for non-$$ blocks like T-SQL)
		if containsSQLKeyword(upper, "BEGIN") {
			depth++
		}
		if containsSQLKeyword(upper, "END") {
			depth--
			if depth <= 0 && dollarCount == 0 {
				return i + 1
			}
		}
	}
	return len(lines)
}

// findSQLStatementEnd finds the end of a SQL statement (terminated by ;).
func findSQLStatementEnd(lines []string, startIdx int) int {
	depth := 0
	for i := startIdx; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		for _, ch := range line {
			if ch == '(' {
				depth++
			} else if ch == ')' {
				depth--
			}
		}
		if depth <= 0 && strings.HasSuffix(line, ";") {
			return i + 1
		}
	}
	return len(lines)
}

// containsSQLKeyword checks if a line contains a SQL keyword as a whole word.
func containsSQLKeyword(upper, keyword string) bool {
	idx := strings.Index(upper, keyword)
	if idx < 0 {
		return false
	}
	// Check word boundary before
	if idx > 0 {
		ch := upper[idx-1]
		if ch != ' ' && ch != '\t' && ch != ';' && ch != '\n' {
			return false
		}
	}
	// Check word boundary after
	end := idx + len(keyword)
	if end < len(upper) {
		ch := upper[end]
		if ch != ' ' && ch != '\t' && ch != ';' && ch != '\n' {
			return false
		}
	}
	return true
}

// normalizeSQL collapses multi-space runs for cleaner signatures.
func normalizeSQL(s string) string {
	parts := strings.Fields(s)
	return strings.Join(parts, " ")
}
