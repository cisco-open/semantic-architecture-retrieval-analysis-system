/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cfg

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// SQL procedural parser (PL/pgSQL, T-SQL, Oracle PL/SQL, MySQL stored programs)
//
// Procedural SQL is keyword-delimited rather than brace-delimited. Blocks open
// with `BEGIN`, `IF ... THEN`, `CASE`, `LOOP` / `WHILE ... LOOP` / `FOR ... LOOP`
// and `REPEAT`, and close with two-word terminators: `END`, `END IF`,
// `END LOOP`, `END CASE`, `END REPEAT`. Keywords are case-insensitive, so we
// match against an upper-cased, comment/string-stripped copy of each line.
//
// We model:
//   - IF / ELSIF / ELSEIF / ELSE / END IF          → skIf chain
//   - CASE / WHEN / ELSE / END CASE                 → skSwitch
//   - LOOP, WHILE..LOOP, WHILE..DO, FOR..LOOP       → skLoop (pre-test)
//   - REPEAT .. UNTIL cond END REPEAT               → skLoop (do-loop)
//   - BEGIN .. [EXCEPTION ..] END                   → nested block / branch
//   - RETURN / RAISE EXCEPTION / SIGNAL             → terminator (skReturn)
//   - EXIT [WHEN cond] / LEAVE                       → break (conditional → if)
//   - CONTINUE [WHEN cond] / ITERATE                 → continue (conditional → if)
//
// Multi-line linear statements (terminated by `;`) are grouped into a single
// linear block, mirroring the Ruby end-keyword parser. Dialect variance means
// some exotic forms degrade to linear; that is reported via Notes.
// ---------------------------------------------------------------------------

type sqlLine struct {
	lineNo int
	raw    string
	upper  string // upper-cased, comment/string-stripped, trimmed
}

// preprocessSQL strips `--` line comments, `/* */` block comments (including
// multi-line), single-quoted string literals and `$$` dollar-quote delimiters,
// then upper-cases the remainder for case-insensitive keyword matching.
func preprocessSQL(body string, startLine int) []sqlLine {
	rawLines := strings.Split(body, "\n")
	out := make([]sqlLine, 0, len(rawLines))
	inBlockComment := false

	for i, raw := range rawLines {
		stripped, stillIn := stripSQL(raw, inBlockComment)
		inBlockComment = stillIn
		out = append(out, sqlLine{
			lineNo: startLine + i,
			raw:    raw,
			upper:  strings.ToUpper(strings.TrimSpace(stripped)),
		})
	}
	return out
}

// stripSQL removes comments and string literals from a single line. It returns
// the stripped text plus whether a `/* */` block comment remains open.
func stripSQL(raw string, inBlockComment bool) (string, bool) {
	var b strings.Builder
	b.Grow(len(raw))
	i, n := 0, len(raw)
	for i < n {
		if inBlockComment {
			if i+1 < n && raw[i] == '*' && raw[i+1] == '/' {
				inBlockComment = false
				i += 2
				continue
			}
			i++
			continue
		}

		c := raw[i]
		switch {
		case c == '-' && i+1 < n && raw[i+1] == '-':
			// Line comment to end of line.
			return b.String(), false
		case c == '/' && i+1 < n && raw[i+1] == '*':
			inBlockComment = true
			i += 2
		case c == '$' && sqlDollarTag(raw, i) > i:
			// Dollar-quote delimiter ($$ or $tag$) — drop the marker so it
			// does not interfere with keyword detection.
			b.WriteByte(' ')
			i = sqlDollarTag(raw, i)
		case c == '\'':
			b.WriteByte(' ')
			i = consumeSQLString(raw, i+1, &b)
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String(), inBlockComment
}

// consumeSQLString blanks out a single-quoted literal (handling the `''`
// escape) starting at index i, and returns the index just past the closing
// quote. The opening quote is assumed already consumed by the caller.
func consumeSQLString(raw string, i int, b *strings.Builder) int {
	n := len(raw)
	for i < n {
		if raw[i] == '\'' {
			if i+1 < n && raw[i+1] == '\'' { // escaped quote
				b.WriteString("  ")
				i += 2
				continue
			}
			b.WriteByte(' ')
			return i + 1
		}
		b.WriteByte(' ')
		i++
	}
	return i
}

// sqlDollarTag returns the index just past a `$$` or `$tag$` delimiter that
// starts at index i, or i if the text at i is not a dollar-quote delimiter.
func sqlDollarTag(s string, i int) int {
	if i >= len(s) || s[i] != '$' {
		return i
	}
	j := i + 1
	for j < len(s) && (isWordByte(s[j])) {
		j++
	}
	if j < len(s) && s[j] == '$' {
		return j + 1
	}
	return i
}

func isWordByte(c byte) bool {
	return c == '_' ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
}

// ---------------------------------------------------------------------------
// Keyword detection (matched against the upper-cased text)
// ---------------------------------------------------------------------------

var (
	reSQLBegin    = regexp.MustCompile(`^BEGIN\b(?:\s+ATOMIC\b)?\s*;?\s*$`)
	reSQLIf       = regexp.MustCompile(`^IF\b`)
	reSQLElsif    = regexp.MustCompile(`^ELS(?:E)?IF\b`)
	reSQLElse     = regexp.MustCompile(`^ELSE\b`)
	reSQLCase     = regexp.MustCompile(`^CASE\b`)
	reSQLWhen     = regexp.MustCompile(`^WHEN\b`)
	reSQLWhile    = regexp.MustCompile(`^WHILE\b`)
	reSQLFor      = regexp.MustCompile(`^FOR\b`)
	reSQLLoop     = regexp.MustCompile(`^LOOP\b`)
	reSQLRepeat   = regexp.MustCompile(`^REPEAT\b`)
	reSQLUntil    = regexp.MustCompile(`^UNTIL\b`)
	reSQLExcept   = regexp.MustCompile(`^EXCEPTION\b`)
	reSQLEndIf    = regexp.MustCompile(`^END\s+IF\b`)
	reSQLEndLoop  = regexp.MustCompile(`^END\s+LOOP\b`)
	reSQLEndCase  = regexp.MustCompile(`^END\s+CASE\b`)
	reSQLEndWhile = regexp.MustCompile(`^END\s+WHILE\b`)
	reSQLEndRep   = regexp.MustCompile(`^END\s+REPEAT\b`)
	reSQLEnd      = regexp.MustCompile(`^END\b`)
	reSQLReturn   = regexp.MustCompile(`^RETURN\b`)
	reSQLRaise    = regexp.MustCompile(`^RAISE\s+EXCEPTION\b`)
	reSQLSignal   = regexp.MustCompile(`^SIGNAL\b`)
	reSQLExit     = regexp.MustCompile(`^EXIT\b`)
	reSQLLeave    = regexp.MustCompile(`^LEAVE\b`)
	reSQLContinue = regexp.MustCompile(`^CONTINUE\b`)
	reSQLIterate  = regexp.MustCompile(`^ITERATE\b`)
)

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

// parseSQLFunctionBody finds the procedure's outermost BEGIN block and parses
// its body. When no BEGIN block is present (e.g. a single-expression SQL
// function) the whole body is treated as linear with an explanatory note.
func parseSQLFunctionBody(body string, startLine int) ([]*stmt, []string) {
	lines := preprocessSQL(body, startLine)

	beginIdx := -1
	for i, l := range lines {
		if reSQLBegin.MatchString(l.upper) {
			beginIdx = i
			break
		}
	}
	if beginIdx < 0 {
		stmts, _ := parseSQLStmts(lines, 0)
		return stmts, []string{"no BEGIN block found; treated body as linear SQL"}
	}

	block, _ := parseSQLBeginBlock(lines, beginIdx)
	if block == nil {
		return nil, nil
	}
	return []*stmt{block}, nil
}

// parseSQLStmts parses statements starting at lines[idx] until it reaches a
// boundary token (END / END IF / ELSIF / ELSE / WHEN / EXCEPTION / UNTIL) or
// end-of-input. It returns the statements and the index of the boundary line.
func parseSQLStmts(lines []sqlLine, idx int) ([]*stmt, int) {
	var out []*stmt
	for idx < len(lines) {
		t := lines[idx].upper
		if t == "" {
			idx++
			continue
		}
		if isSQLBoundary(t) {
			return out, idx
		}
		s, next := parseSQLOne(lines, idx)
		if s != nil {
			out = append(out, s)
		}
		if next <= idx { // safety: always make progress
			next = idx + 1
		}
		idx = next
	}
	return out, idx
}

// parseSQLOne parses a single statement (possibly compound) starting at
// lines[idx] and returns it plus the index of the next unparsed line. The
// caller guarantees lines[idx] is non-empty and not a boundary token.
func parseSQLOne(lines []sqlLine, idx int) (*stmt, int) {
	l := lines[idx]
	t := l.upper
	switch {
	case reSQLBegin.MatchString(t):
		return parseSQLBeginBlock(lines, idx)
	case reSQLIf.MatchString(t):
		return parseSQLIf(lines, idx)
	case reSQLCase.MatchString(t):
		return parseSQLCase(lines, idx)
	case reSQLRepeat.MatchString(t):
		return parseSQLRepeat(lines, idx)
	case reSQLLoop.MatchString(t), reSQLWhile.MatchString(t), reSQLFor.MatchString(t):
		return parseSQLLoop(lines, idx)
	case reSQLReturn.MatchString(t), reSQLRaise.MatchString(t), reSQLSignal.MatchString(t):
		return sqlTermStmt(l, skReturn), idx + 1
	case reSQLExit.MatchString(t), reSQLLeave.MatchString(t):
		return parseSQLJump(lines, idx, skBreak)
	case reSQLContinue.MatchString(t), reSQLIterate.MatchString(t):
		return parseSQLJump(lines, idx, skContinue)
	default:
		return parseSQLLinear(lines, idx)
	}
}

// isSQLBoundary reports whether a line closes or subdivides the enclosing
// block. These stop the statement loop so the enclosing construct's parser can
// consume the matching terminator.
func isSQLBoundary(upper string) bool {
	return reSQLEnd.MatchString(upper) || // covers END, END IF, END LOOP, ...
		reSQLElsif.MatchString(upper) ||
		reSQLElse.MatchString(upper) ||
		reSQLWhen.MatchString(upper) ||
		reSQLExcept.MatchString(upper) ||
		reSQLUntil.MatchString(upper)
}

func sqlTermStmt(l sqlLine, kind stmtKind) *stmt {
	return &stmt{
		kind: kind, line: l.lineNo, endLine: l.lineNo,
		header: strings.TrimSpace(l.raw),
		code:   []string{l.raw}, lines: []int{l.lineNo},
	}
}

// parseSQLLinear accumulates consecutive non-control lines into one linear
// block. Statements terminated by `;` mid-line still group together, which is
// sufficient for control-flow granularity.
func parseSQLLinear(lines []sqlLine, idx int) (*stmt, int) {
	start := lines[idx].lineNo
	end := start
	var code []string
	var ln []int
	for idx < len(lines) {
		l := lines[idx]
		if l.upper == "" {
			idx++
			continue
		}
		if isSQLControlStart(l.upper) || isSQLBoundary(l.upper) {
			break
		}
		code = append(code, l.raw)
		ln = append(ln, l.lineNo)
		end = l.lineNo
		idx++
	}
	if len(code) == 0 {
		return nil, idx
	}
	return &stmt{
		kind: skLinear, line: start, endLine: end,
		code: code, lines: ln,
	}, idx
}

func isSQLControlStart(upper string) bool {
	return reSQLBegin.MatchString(upper) ||
		reSQLIf.MatchString(upper) ||
		reSQLCase.MatchString(upper) ||
		reSQLRepeat.MatchString(upper) ||
		reSQLLoop.MatchString(upper) ||
		reSQLWhile.MatchString(upper) ||
		reSQLFor.MatchString(upper) ||
		reSQLReturn.MatchString(upper) ||
		reSQLRaise.MatchString(upper) ||
		reSQLSignal.MatchString(upper) ||
		reSQLExit.MatchString(upper) ||
		reSQLLeave.MatchString(upper) ||
		reSQLContinue.MatchString(upper) ||
		reSQLIterate.MatchString(upper)
}

// ---------------------------------------------------------------------------
// BEGIN .. [EXCEPTION ..] END
// ---------------------------------------------------------------------------

func parseSQLBeginBlock(lines []sqlLine, idx int) (*stmt, int) {
	open := lines[idx]
	body, next := parseSQLStmts(lines, idx+1)

	// Exception handlers, when present, are modelled as the `else` arm of a
	// branch (the protected body may or may not raise).
	if next < len(lines) && reSQLExcept.MatchString(lines[next].upper) {
		handlers, after := parseSQLExceptionHandlers(lines, next+1)
		endLine := open.lineNo
		if after < len(lines) {
			endLine = lines[after].lineNo
		}
		s := &stmt{
			kind: skIf, line: open.lineNo, endLine: endLine,
			header: "BEGIN ... EXCEPTION", cond: "raises exception?",
			then: body, els: handlers,
		}
		return s, consumeSQLEnd(lines, after)
	}

	// Plain block — transparent sequence of statements.
	endLine := open.lineNo
	if next < len(lines) {
		endLine = lines[next].lineNo
	}
	s := &stmt{
		kind: skBlock, line: open.lineNo, endLine: endLine,
		header: "BEGIN", body: body,
	}
	return s, consumeSQLEnd(lines, next)
}

// parseSQLExceptionHandlers parses one or more `WHEN cond THEN ...` arms that
// follow an EXCEPTION keyword, until the block's `END`.
func parseSQLExceptionHandlers(lines []sqlLine, idx int) ([]*stmt, int) {
	var out []*stmt
	for idx < len(lines) {
		l := lines[idx]
		if l.upper == "" {
			idx++
			continue
		}
		if reSQLEnd.MatchString(l.upper) && !reSQLEndIf.MatchString(l.upper) &&
			!reSQLEndLoop.MatchString(l.upper) && !reSQLEndCase.MatchString(l.upper) &&
			!reSQLEndRep.MatchString(l.upper) && !reSQLEndWhile.MatchString(l.upper) {
			return out, idx
		}
		if reSQLWhen.MatchString(l.upper) {
			body, next := parseSQLStmts(lines, idx+1)
			out = append(out, &stmt{
				kind: skBlock, line: l.lineNo,
				endLine: lastSQLLine(lines, next-1, l.lineNo),
				header:  strings.TrimSpace(l.raw), body: body,
			})
			idx = next
			continue
		}
		idx++
	}
	return out, idx
}

// consumeSQLEnd advances past a bare `END` (and an optional trailing `;`).
func consumeSQLEnd(lines []sqlLine, idx int) int {
	if idx < len(lines) && reSQLEnd.MatchString(lines[idx].upper) {
		return idx + 1
	}
	return idx
}

// ---------------------------------------------------------------------------
// IF .. THEN .. [ELSIF ..] [ELSE ..] END IF
// ---------------------------------------------------------------------------

func parseSQLIf(lines []sqlLine, idx int) (*stmt, int) {
	open := lines[idx]
	cond, bodyStart := sqlScanCond(lines, idx, "THEN")
	cond = strings.TrimPrefix(cond, "IF")
	cond = strings.TrimSpace(cond)

	s := &stmt{
		kind: skIf, line: open.lineNo, endLine: open.lineNo,
		header: strings.TrimSpace(open.raw), cond: cond,
	}
	thenBody, next := parseSQLStmts(lines, bodyStart)
	s.then = thenBody

	if next < len(lines) {
		t := lines[next].upper
		switch {
		case reSQLElsif.MatchString(t):
			inner, after := parseSQLElsifChain(lines, next)
			s.els = []*stmt{inner}
			next = after
		case reSQLElse.MatchString(t):
			elseBody, after := parseSQLStmts(lines, next+1)
			s.els = elseBody
			next = after
		}
	}

	// Consume END IF.
	if next < len(lines) && reSQLEndIf.MatchString(lines[next].upper) {
		s.endLine = lines[next].lineNo
		return s, next + 1
	}
	s.endLine = lastSQLLine(lines, next, open.lineNo)
	return s, next
}

// parseSQLElsifChain parses an ELSIF arm and recursively chains further ELSIF /
// ELSE arms, returning an skIf representing the nested branch. It does NOT
// consume the terminating END IF (the top-level parseSQLIf does that).
func parseSQLElsifChain(lines []sqlLine, idx int) (*stmt, int) {
	open := lines[idx]
	cond, bodyStart := sqlScanCond(lines, idx, "THEN")
	cond = reSQLElsif.ReplaceAllString(cond, "")
	cond = strings.TrimSpace(cond)

	s := &stmt{
		kind: skIf, line: open.lineNo, endLine: open.lineNo,
		header: strings.TrimSpace(open.raw), cond: cond,
	}
	thenBody, next := parseSQLStmts(lines, bodyStart)
	s.then = thenBody

	if next < len(lines) {
		t := lines[next].upper
		switch {
		case reSQLElsif.MatchString(t):
			inner, after := parseSQLElsifChain(lines, next)
			s.els = []*stmt{inner}
			next = after
		case reSQLElse.MatchString(t):
			elseBody, after := parseSQLStmts(lines, next+1)
			s.els = elseBody
			next = after
		}
	}
	s.endLine = lastSQLLine(lines, next-1, open.lineNo)
	return s, next
}

// ---------------------------------------------------------------------------
// CASE .. WHEN .. THEN .. [ELSE ..] END CASE
// ---------------------------------------------------------------------------

func parseSQLCase(lines []sqlLine, idx int) (*stmt, int) {
	open := lines[idx]
	cond := strings.TrimSpace(strings.TrimPrefix(open.upper, "CASE"))
	s := &stmt{
		kind: skSwitch, line: open.lineNo, endLine: open.lineNo,
		header: strings.TrimSpace(open.raw), cond: cond,
	}
	i := idx + 1
	for i < len(lines) {
		l := lines[i]
		if l.upper == "" {
			i++
			continue
		}
		if reSQLEndCase.MatchString(l.upper) || (reSQLEnd.MatchString(l.upper) &&
			!reSQLEndIf.MatchString(l.upper) && !reSQLEndLoop.MatchString(l.upper) &&
			!reSQLEndRep.MatchString(l.upper) && !reSQLEndWhile.MatchString(l.upper)) {
			s.endLine = l.lineNo
			return s, i + 1
		}
		if reSQLElse.MatchString(l.upper) {
			body, after := parseSQLStmts(lines, i+1)
			s.cases = append(s.cases, &caseClause{
				labels: []string{"else"}, isDefault: true,
				line: l.lineNo, endLine: lastSQLLine(lines, after-1, l.lineNo),
				body: body,
			})
			i = after
			continue
		}
		if reSQLWhen.MatchString(l.upper) {
			label := sqlCaseLabel(l.upper)
			body, after := parseSQLStmts(lines, i+1)
			s.cases = append(s.cases, &caseClause{
				labels: []string{label},
				line:   l.lineNo, endLine: lastSQLLine(lines, after-1, l.lineNo),
				body: body,
			})
			i = after
			continue
		}
		i++
	}
	s.endLine = lastSQLLine(lines, i-1, s.endLine)
	return s, i
}

// sqlCaseLabel extracts the `WHEN <label>` text, dropping a trailing `THEN`.
func sqlCaseLabel(upper string) string {
	h := strings.TrimSpace(upper)
	if i := strings.Index(h, "THEN"); i >= 0 {
		h = h[:i]
	}
	return strings.TrimSpace(h)
}

// ---------------------------------------------------------------------------
// Loops: LOOP / WHILE..LOOP / WHILE..DO / FOR..LOOP / REPEAT..UNTIL
// ---------------------------------------------------------------------------

func parseSQLLoop(lines []sqlLine, idx int) (*stmt, int) {
	open := lines[idx]
	t := open.upper

	var cond string
	bodyStart := idx + 1
	switch {
	case reSQLWhile.MatchString(t):
		// WHILE cond LOOP (PL/pgSQL) or WHILE cond DO (MySQL).
		kw := "LOOP"
		if !strings.Contains(t, "LOOP") && strings.Contains(t, "DO") {
			kw = "DO"
		}
		cond, bodyStart = sqlScanCond(lines, idx, kw)
		cond = strings.TrimSpace(strings.TrimPrefix(cond, "WHILE"))
	case reSQLFor.MatchString(t):
		cond, bodyStart = sqlScanCond(lines, idx, "LOOP")
		cond = strings.TrimSpace(cond)
	default:
		// Bare infinite LOOP.
		cond = "loop"
		bodyStart = idx + 1
	}
	if cond == "" {
		cond = "loop"
	}

	s := &stmt{
		kind: skLoop, line: open.lineNo, endLine: open.lineNo,
		header: strings.TrimSpace(open.raw), cond: cond,
	}
	body, next := parseSQLStmts(lines, bodyStart)
	s.body = body

	if next < len(lines) && (reSQLEndLoop.MatchString(lines[next].upper) ||
		reSQLEndWhile.MatchString(lines[next].upper)) {
		s.endLine = lines[next].lineNo
		return s, next + 1
	}
	s.endLine = lastSQLLine(lines, next, open.lineNo)
	return s, next
}

func parseSQLRepeat(lines []sqlLine, idx int) (*stmt, int) {
	open := lines[idx]
	body, next := parseSQLStmts(lines, idx+1)

	cond := "repeat"
	endLine := open.lineNo
	if next < len(lines) && reSQLUntil.MatchString(lines[next].upper) {
		cond = strings.TrimSpace(strings.TrimPrefix(lines[next].upper, "UNTIL"))
		endLine = lines[next].lineNo
		next++
	}
	if cond == "" {
		cond = "repeat"
	}
	// Consume END REPEAT.
	if next < len(lines) && reSQLEndRep.MatchString(lines[next].upper) {
		endLine = lines[next].lineNo
		next++
	}
	s := &stmt{
		kind: skLoop, line: open.lineNo, endLine: endLine,
		header: strings.TrimSpace(open.raw), cond: cond,
		body: body, isDoLoop: true,
	}
	return s, next
}

// ---------------------------------------------------------------------------
// Jumps: EXIT / LEAVE (break) and CONTINUE / ITERATE (continue)
//
// PL/pgSQL allows a conditional form `EXIT WHEN cond;` / `CONTINUE WHEN cond;`,
// which we model as `if cond then <jump>` so the branch appears in the CFG.
// ---------------------------------------------------------------------------

func parseSQLJump(lines []sqlLine, idx int, kind stmtKind) (*stmt, int) {
	l := lines[idx]
	if i := strings.Index(l.upper, "WHEN"); i >= 0 {
		cond := strings.TrimSpace(l.upper[i+len("WHEN"):])
		cond = strings.TrimSuffix(strings.TrimSpace(cond), ";")
		return &stmt{
			kind: skIf, line: l.lineNo, endLine: l.lineNo,
			header: strings.TrimSpace(l.raw), cond: cond,
			then: []*stmt{sqlTermStmt(l, kind)},
		}, idx + 1
	}
	return sqlTermStmt(l, kind), idx + 1
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// sqlScanCond collects the condition text of a header that ends with the given
// terminator keyword (e.g. THEN, LOOP, DO), spanning multiple lines if needed.
// It returns the joined condition text and the index of the first body line
// (the line after the one containing the terminator).
func sqlScanCond(lines []sqlLine, idx int, term string) (string, int) {
	var parts []string
	for i := idx; i < len(lines); i++ {
		u := lines[i].upper
		if j := sqlKeywordIndex(u, term); j >= 0 {
			parts = append(parts, strings.TrimSpace(u[:j]))
			return strings.TrimSpace(strings.Join(parts, " ")), i + 1
		}
		// Stop runaway scans: if a later line opens its own construct or
		// closes the enclosing one, the terminator is absent (e.g. T-SQL
		// `IF cond` has no THEN). Treat the header collected so far as the
		// condition and let the body parser start at this line.
		if i > idx && (isSQLControlStart(u) || isSQLBoundary(u)) {
			return strings.TrimSpace(strings.Join(parts, " ")), i
		}
		parts = append(parts, strings.TrimSpace(u))
	}
	// Terminator not found — treat the single header line as the condition.
	return strings.TrimSpace(lines[idx].upper), idx + 1
}

// sqlKeywordIndex returns the byte index of keyword `kw` in `upper` as a whole
// word, or -1. `upper` is already upper-cased.
func sqlKeywordIndex(upper, kw string) int {
	from := 0
	for {
		j := strings.Index(upper[from:], kw)
		if j < 0 {
			return -1
		}
		j += from
		before := j == 0 || !isWordByte(upper[j-1])
		afterIdx := j + len(kw)
		after := afterIdx >= len(upper) || !isWordByte(upper[afterIdx])
		if before && after {
			return j
		}
		from = j + len(kw)
		if from >= len(upper) {
			return -1
		}
	}
}

func lastSQLLine(lines []sqlLine, k, fallback int) int {
	if k < 0 || k >= len(lines) {
		return fallback
	}
	return lines[k].lineNo
}
