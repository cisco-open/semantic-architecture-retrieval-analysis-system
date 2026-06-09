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
// Lexer — strips strings/comments, tracks brace depth per source line
// ---------------------------------------------------------------------------

// sourceLine holds a single logical source line with its surrounding context.
type sourceLine struct {
	lineNo    int    // 1-indexed source line number
	raw       string // original text (for display)
	stripped  string // text with strings and inline comments removed
	openAt    int    // brace depth at the *start* of the line (before content)
	closeAt   int    // brace depth at the *end* of the line (after content)
	minDepth  int    // minimum depth reached anywhere within this line
	openParen int    // parenthesis depth at end of line (for multi-line headers)
}

// preprocess walks the function body and produces a slice of sourceLine
// records. It handles single-line `//` `#`, block `/* */`, and `"`/`'`/`` ` ``
// string literals so brace counting is not fooled by braces in strings or
// comments.
//
// startLine is the 1-indexed line number of the first line in body.
func preprocess(body string, startLine int) []sourceLine {
	rawLines := strings.Split(body, "\n")
	out := make([]sourceLine, 0, len(rawLines))

	depth := 0
	parenDepth := 0
	inBlockComment := false

	for i, raw := range rawLines {
		open := depth
		openP := parenDepth
		stripped, newDepth, newParenDepth, newBlock, minDepth := stripAndCount(raw, depth, parenDepth, inBlockComment)
		depth = newDepth
		parenDepth = newParenDepth
		inBlockComment = newBlock

		out = append(out, sourceLine{
			lineNo:    startLine + i,
			raw:       raw,
			stripped:  stripped,
			openAt:    open,
			closeAt:   depth,
			minDepth:  minDepth,
			openParen: openP,
		})
	}
	return out
}

// stripAndCount removes string literals, line comments, and block comments
// from `raw` and updates brace and paren depth as it scans. Returns the
// stripped text, the new brace/paren depths, whether we are still inside a
// block comment after this line, and the minimum brace depth reached during
// the line (used to detect `} else { ... }` constructs where a single line
// both closes and opens braces).
func stripAndCount(raw string, depth, paren int, inBlockComment bool) (string, int, int, bool, int) {
	var b strings.Builder
	b.Grow(len(raw))

	i := 0
	n := len(raw)
	minDepth := depth

	flushChar := func(c byte) {
		b.WriteByte(c)
	}

	for i < n {
		c := raw[i]

		// Inside a block comment — look for closing `*/`
		if inBlockComment {
			if c == '*' && i+1 < n && raw[i+1] == '/' {
				inBlockComment = false
				i += 2
				b.WriteByte(' ') // preserve column-ish layout
				b.WriteByte(' ')
				continue
			}
			b.WriteByte(' ')
			i++
			continue
		}

		// Start of a block comment
		if c == '/' && i+1 < n && raw[i+1] == '*' {
			inBlockComment = true
			i += 2
			b.WriteByte(' ')
			b.WriteByte(' ')
			continue
		}

		// Line comments (//, #) — rest of line is comment
		if c == '/' && i+1 < n && raw[i+1] == '/' {
			break
		}
		if c == '#' {
			// `#` is a comment in Python/Ruby/shell/Perl/etc. but in C/C++ it
			// denotes a preprocessor directive at column 0. Treat as comment
			// for simplicity — preprocessor lines don't contain control flow
			// keywords we care about.
			break
		}

		// String literals
		if c == '"' || c == '\'' || c == '`' {
			quote := c
			b.WriteByte(' ')
			i++
			for i < n {
				ch := raw[i]
				if ch == '\\' && i+1 < n {
					b.WriteByte(' ')
					b.WriteByte(' ')
					i += 2
					continue
				}
				if ch == quote {
					b.WriteByte(' ')
					i++
					break
				}
				b.WriteByte(' ')
				i++
			}
			continue
		}

		// Brace / paren tracking
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth < minDepth {
				minDepth = depth
			}
		case '(':
			paren++
		case ')':
			paren--
		}
		flushChar(c)
		i++
	}

	return b.String(), depth, paren, inBlockComment, minDepth
}

// ---------------------------------------------------------------------------
// Statement tree
// ---------------------------------------------------------------------------

type stmtKind int

const (
	skLinear   stmtKind = iota // plain statement(s)
	skIf                       // if (cond) { ... } [else if ...] [else { ... }]
	skLoop                     // for / while / do-while
	skSwitch                   // switch / select
	skReturn                   // return / panic / throw / os.Exit
	skBreak                    // break
	skContinue                 // continue
	skBlock                    // bare `{ ... }` block
)

type stmt struct {
	kind    stmtKind
	line    int    // first source line
	endLine int    // last source line (inclusive of body)
	header  string // for compound: text of the header line(s) joined
	cond    string // extracted condition expression
	code    []string
	lines   []int

	// Compound bodies
	then     []*stmt
	els      []*stmt // for if: list of statements in the `else` block
	body     []*stmt // for loops
	cases    []*caseClause
	isDoLoop bool // true for do-while: body executes at least once
}

type caseClause struct {
	labels    []string // e.g. ["case 1", "case 2"]
	isDefault bool
	line      int
	endLine   int
	body      []*stmt
}

// ---------------------------------------------------------------------------
// Parser — converts the flat sourceLine list into a statement tree
// ---------------------------------------------------------------------------

// Control-flow keyword detection. We only require word-boundary matches; the
// regex must succeed on the line's first non-whitespace token. We strip
// labels (`mylabel:`) and `}` prefixes before testing because in C-family
// languages constructs like `} else if` and `} else` are common.
var (
	reIf       = regexp.MustCompile(`^\s*(?:\}\s*)?if\b`)
	reElseIf   = regexp.MustCompile(`^\s*(?:\}\s*)?else\s+if\b`)
	reElse     = regexp.MustCompile(`^\s*(?:\}\s*)?else\b`)
	reFor      = regexp.MustCompile(`^\s*for\b`)
	reForeach  = regexp.MustCompile(`^\s*foreach\b`)
	reWhile    = regexp.MustCompile(`^\s*while\b`)
	reDo       = regexp.MustCompile(`^\s*do\b`)
	reSwitch   = regexp.MustCompile(`^\s*switch\b`)
	reSelect   = regexp.MustCompile(`^\s*select\s*\{`) // Go select
	reCase     = regexp.MustCompile(`^\s*case\b`)
	reDefault  = regexp.MustCompile(`^\s*default\s*:`)
	reReturn   = regexp.MustCompile(`^\s*return\b`)
	rePanic    = regexp.MustCompile(`\bpanic\s*\(`)
	reThrow    = regexp.MustCompile(`^\s*throw\b`)
	reOsExit   = regexp.MustCompile(`\b(?:os\.Exit|sys\.exit|System\.exit|exit|process\.exit)\s*\(`)
	reBreak    = regexp.MustCompile(`^\s*break\b`)
	reContinue = regexp.MustCompile(`^\s*continue\b`)
)

// parseFunctionBody parses the body of a function. The lines slice must cover
// the entire function (including the signature line and the trailing `}`).
// We locate the body's opening brace and parse statements inside it.
func parseFunctionBody(lines []sourceLine) []*stmt {
	bodyStart := findBodyStart(lines)
	if bodyStart < 0 {
		return nil
	}
	stmts, _ := parseStmts(lines, bodyStart+1, 1)
	return stmts
}

// findBodyStart returns the index of the line whose stripped content contains
// the opening `{` that starts the function body (i.e. where brace depth first
// transitions from 0 to 1). Returns -1 if no body found.
func findBodyStart(lines []sourceLine) int {
	for i, l := range lines {
		if l.openAt == 0 && l.closeAt >= 1 {
			return i
		}
	}
	return -1
}

// parseStmts parses statements at the given brace depth starting from line
// index `idx`. It stops when a line decreases the depth below `depth` (i.e.
// the enclosing `}` is reached). Returns the parsed statements and the index
// of the line where parsing stopped (the closing `}` line).
func parseStmts(lines []sourceLine, idx int, depth int) ([]*stmt, int) {
	var out []*stmt
	for idx < len(lines) {
		l := lines[idx]
		// Stop if we've reached the closing `}` of the enclosing block.
		// We check both the final depth (pure close-brace lines) and the
		// minimum depth seen during the line (handles `} else { ... }` where
		// the line both closes and reopens braces).
		if l.closeAt < depth {
			return out, idx
		}
		if l.minDepth < depth {
			return out, idx
		}
		// Stop on `case`/`default` keywords. These only appear inside a
		// switch body and signal the end of the current case's statements.
		ts0 := strings.TrimSpace(l.stripped)
		if reCase.MatchString(ts0) || reDefault.MatchString(ts0) {
			return out, idx
		}
		// Skip blank/comment-only lines.
		if strings.TrimSpace(l.stripped) == "" && strings.TrimSpace(l.raw) == "" {
			idx++
			continue
		}
		// Skip lines that are only the enclosing `}` (depth drop without
		// content). When openAt == depth and closeAt == depth-1, this is a
		// pure close brace, which we've already accounted for above.
		txt := strings.TrimSpace(l.stripped)
		if txt == "}" || txt == "" {
			idx++
			continue
		}

		// Identify the statement kind by its first keyword.
		switch {
		case reIf.MatchString(l.stripped):
			s, next := parseIf(lines, idx, depth)
			out = append(out, s)
			idx = next
		case reFor.MatchString(l.stripped),
			reForeach.MatchString(l.stripped),
			reWhile.MatchString(l.stripped):
			s, next := parseLoop(lines, idx, depth)
			out = append(out, s)
			idx = next
		case reDo.MatchString(l.stripped):
			s, next := parseDoWhile(lines, idx, depth)
			out = append(out, s)
			idx = next
		case reSwitch.MatchString(l.stripped), reSelect.MatchString(l.stripped):
			s, next := parseSwitch(lines, idx, depth)
			out = append(out, s)
			idx = next
		case reReturn.MatchString(l.stripped),
			reThrow.MatchString(l.stripped),
			rePanic.MatchString(l.stripped),
			reOsExit.MatchString(l.stripped):
			out = append(out, &stmt{
				kind:    skReturn,
				line:    l.lineNo,
				endLine: l.lineNo,
				header:  strings.TrimSpace(l.raw),
				code:    []string{l.raw},
				lines:   []int{l.lineNo},
			})
			idx++
		case reBreak.MatchString(l.stripped):
			out = append(out, &stmt{
				kind:    skBreak,
				line:    l.lineNo,
				endLine: l.lineNo,
				header:  strings.TrimSpace(l.raw),
				code:    []string{l.raw},
				lines:   []int{l.lineNo},
			})
			idx++
		case reContinue.MatchString(l.stripped):
			out = append(out, &stmt{
				kind:    skContinue,
				line:    l.lineNo,
				endLine: l.lineNo,
				header:  strings.TrimSpace(l.raw),
				code:    []string{l.raw},
				lines:   []int{l.lineNo},
			})
			idx++
		default:
			// Linear statement — accumulate consecutive linear lines into one
			// block for compactness. We stop when we hit a control keyword or
			// a depth change.
			start := idx
			var collected []string
			var cLines []int
			startLine := l.lineNo
			endLine := l.lineNo
			for idx < len(lines) {
				ll := lines[idx]
				if ll.closeAt < depth {
					break
				}
				ts := strings.TrimSpace(ll.stripped)
				if ts == "" || ts == "}" {
					idx++
					continue
				}
				if isControlStart(ll.stripped) {
					break
				}
				collected = append(collected, ll.raw)
				cLines = append(cLines, ll.lineNo)
				endLine = ll.lineNo
				idx++
				// If this line opens a nested block (`closeAt > depth`) we
				// must not gobble the whole nested body — but linear
				// statements at the current depth never open new blocks
				// without a control keyword (we already handled all
				// control keywords above). Defensive guard:
				if ll.closeAt > depth {
					// Treat as a bare `{` block — consume contents.
					nested, after := parseStmts(lines, idx, ll.closeAt)
					// Wrap as a child block under this linear stmt's tail.
					blk := &stmt{
						kind: skBlock, line: ll.lineNo, endLine: ll.lineNo,
						body: nested,
					}
					out = append(out, &stmt{
						kind: skLinear, line: startLine, endLine: endLine,
						code: collected, lines: cLines,
					})
					out = append(out, blk)
					idx = after + 1
					collected, cLines = nil, nil
					startLine = -1
					break
				}
			}
			if len(collected) > 0 {
				out = append(out, &stmt{
					kind:    skLinear,
					line:    startLine,
					endLine: endLine,
					code:    collected,
					lines:   cLines,
				})
			}
			// Safety: ensure forward progress.
			if idx == start {
				idx++
			}
		}
	}
	return out, idx
}

func isControlStart(stripped string) bool {
	return reIf.MatchString(stripped) ||
		reElseIf.MatchString(stripped) ||
		reElse.MatchString(stripped) ||
		reFor.MatchString(stripped) ||
		reForeach.MatchString(stripped) ||
		reWhile.MatchString(stripped) ||
		reDo.MatchString(stripped) ||
		reSwitch.MatchString(stripped) ||
		reSelect.MatchString(stripped) ||
		reReturn.MatchString(stripped) ||
		reThrow.MatchString(stripped) ||
		reBreak.MatchString(stripped) ||
		reContinue.MatchString(stripped) ||
		reCase.MatchString(stripped) ||
		reDefault.MatchString(stripped)
}

// parseIf parses an if/else-if/else chain.
//
// We expect the line at lines[idx] to start with `if` (possibly after a
// trailing `}` from a previous block). The function consumes through the
// closing `}` of the final else (or the if itself if no else).
func parseIf(lines []sourceLine, idx, depth int) (*stmt, int) {
	headerLine := lines[idx]
	header, blockStart := readHeader(lines, idx)
	cond := extractCond(header, "if")

	s := &stmt{
		kind:    skIf,
		line:    headerLine.lineNo,
		endLine: headerLine.lineNo,
		header:  header,
		cond:    cond,
	}

	// If the `if` is brace-less (e.g. `if (x) doFoo();`), there's no nested
	// block. Treat the next single statement as the `then` body.
	if !openedBlock(lines, idx, blockStart) {
		next := blockStart // first line after header
		thenStmts, after := parseSingleStmt(lines, next, depth)
		s.then = thenStmts
		if len(thenStmts) > 0 {
			s.endLine = thenStmts[len(thenStmts)-1].endLine
		}
		// Check for an `else` after the single statement.
		if after < len(lines) {
			ts := strings.TrimSpace(lines[after].stripped)
			if reElseIf.MatchString(ts) || reElse.MatchString(ts) {
				return parseElseTail(lines, after, depth, s)
			}
		}
		return s, after
	}

	// Braced `then` body: parse at depth+1.
	thenStmts, closeIdx := parseStmts(lines, blockStart, depth+1)
	s.then = thenStmts
	if closeIdx < len(lines) {
		s.endLine = lines[closeIdx].lineNo
	}

	// Look for `else` / `else if` on the line of the closing `}` or the next
	// non-empty line.
	after := closeIdx + 1
	// The closing-brace line may itself contain `} else if (...) {` or `} else {`.
	// In that case, parseStmts already returned at the closing depth, so we
	// re-examine the close line.
	if closeIdx < len(lines) {
		ts := strings.TrimSpace(lines[closeIdx].stripped)
		if reElseIf.MatchString(ts) || reElse.MatchString(ts) {
			return parseElseTail(lines, closeIdx, depth, s)
		}
	}
	// Otherwise check the next line.
	for after < len(lines) {
		ts := strings.TrimSpace(lines[after].stripped)
		if ts == "" {
			after++
			continue
		}
		if reElseIf.MatchString(ts) || reElse.MatchString(ts) {
			return parseElseTail(lines, after, depth, s)
		}
		break
	}
	return s, after
}

// parseElseTail handles `else { ... }` and `else if (...) { ... }` chained
// onto an existing if statement. The line at `idx` is the header line — it
// typically looks like `} else if (cond) {` or `} else {` and both closes
// the previous body and opens a new one.
//
// The `depth` argument is the depth of the outer if statement (so the new
// body is at depth+1).
func parseElseTail(lines []sourceLine, idx, depth int, ifStmt *stmt) (*stmt, int) {
	l := lines[idx]
	ts := strings.TrimSpace(l.stripped)
	if reElseIf.MatchString(ts) {
		// The else-if line opens a new block at depth+1. Build a nested
		// if statement directly.
		condText := strings.TrimSpace(l.raw)
		cond := extractCond(condText, "if") // extractCond strips `else` first
		nestedIf := &stmt{
			kind:    skIf,
			line:    l.lineNo,
			endLine: l.lineNo,
			header:  condText,
			cond:    cond,
		}
		// Body starts on the next line at depth+1.
		bodyStart := idx + 1
		thenStmts, closeIdx := parseStmts(lines, bodyStart, depth+1)
		nestedIf.then = thenStmts
		if closeIdx < len(lines) {
			nestedIf.endLine = lines[closeIdx].lineNo
		}
		// Look for further `else`/`else if` on the close line.
		after := closeIdx + 1
		if closeIdx < len(lines) {
			cts := strings.TrimSpace(lines[closeIdx].stripped)
			if reElseIf.MatchString(cts) || reElse.MatchString(cts) {
				_, nextAfter := parseElseTail(lines, closeIdx, depth, nestedIf)
				after = nextAfter
			}
		}
		ifStmt.els = []*stmt{nestedIf}
		if nestedIf.endLine > ifStmt.endLine {
			ifStmt.endLine = nestedIf.endLine
		}
		return ifStmt, after
	}
	if reElse.MatchString(ts) {
		// Plain else { ... } — body starts next line.
		bodyStart := idx + 1
		elseStmts, closeIdx := parseStmts(lines, bodyStart, depth+1)
		ifStmt.els = elseStmts
		if closeIdx < len(lines) {
			ifStmt.endLine = lines[closeIdx].lineNo
		}
		return ifStmt, closeIdx + 1
	}
	return ifStmt, idx
}

func parseLoop(lines []sourceLine, idx, depth int) (*stmt, int) {
	headerLine := lines[idx]
	header, blockStart := readHeader(lines, idx)

	kw := "loop"
	switch {
	case reFor.MatchString(headerLine.stripped):
		kw = "for"
	case reForeach.MatchString(headerLine.stripped):
		kw = "foreach"
	case reWhile.MatchString(headerLine.stripped):
		kw = "while"
	}
	cond := extractCond(header, kw)

	s := &stmt{
		kind:    skLoop,
		line:    headerLine.lineNo,
		endLine: headerLine.lineNo,
		header:  header,
		cond:    cond,
	}

	if !openedBlock(lines, idx, blockStart) {
		body, after := parseSingleStmt(lines, blockStart, depth)
		s.body = body
		if len(body) > 0 {
			s.endLine = body[len(body)-1].endLine
		}
		return s, after
	}
	body, closeIdx := parseStmts(lines, blockStart, depth+1)
	s.body = body
	if closeIdx < len(lines) {
		s.endLine = lines[closeIdx].lineNo
	}
	return s, closeIdx + 1
}

func parseDoWhile(lines []sourceLine, idx, depth int) (*stmt, int) {
	headerLine := lines[idx]
	// Find the opening `{`
	blockStart := idx + 1
	if !openedBlock(lines, idx, blockStart) {
		// Brace-less do — treat as a single linear statement (rare).
		body, after := parseSingleStmt(lines, blockStart, depth)
		s := &stmt{
			kind:     skLoop,
			line:     headerLine.lineNo,
			endLine:  headerLine.lineNo,
			header:   strings.TrimSpace(headerLine.raw),
			body:     body,
			isDoLoop: true,
		}
		if len(body) > 0 {
			s.endLine = body[len(body)-1].endLine
		}
		return s, after
	}

	body, closeIdx := parseStmts(lines, blockStart, depth+1)
	endLine := headerLine.lineNo
	if closeIdx < len(lines) {
		endLine = lines[closeIdx].lineNo
	}

	// Look for `while (...)` on the close line or next line.
	cond := ""
	after := closeIdx + 1
	for j := closeIdx; j < len(lines) && j <= closeIdx+2; j++ {
		ts := strings.TrimSpace(lines[j].stripped)
		if reWhile.MatchString(ts) {
			cond = extractCond(lines[j].raw, "while")
			endLine = lines[j].lineNo
			after = j + 1
			break
		}
	}

	return &stmt{
		kind:     skLoop,
		line:     headerLine.lineNo,
		endLine:  endLine,
		header:   strings.TrimSpace(headerLine.raw) + " ... while " + cond,
		cond:     cond,
		body:     body,
		isDoLoop: true,
	}, after
}

func parseSwitch(lines []sourceLine, idx, depth int) (*stmt, int) {
	headerLine := lines[idx]
	header, blockStart := readHeader(lines, idx)
	cond := extractCond(header, "switch")
	if cond == "" {
		cond = extractCond(header, "select")
	}

	s := &stmt{
		kind:    skSwitch,
		line:    headerLine.lineNo,
		endLine: headerLine.lineNo,
		header:  header,
		cond:    cond,
	}

	if !openedBlock(lines, idx, blockStart) {
		// Unusual: switch without `{`. Bail out gracefully.
		return s, blockStart
	}

	// Inside the switch body, parse case clauses at depth+1.
	caseDepth := depth + 1
	i := blockStart
	var cases []*caseClause
	var cur *caseClause
	for i < len(lines) {
		l := lines[i]
		if l.closeAt < caseDepth {
			break
		}
		ts := strings.TrimSpace(l.stripped)
		if ts == "" || ts == "}" {
			i++
			continue
		}
		if reCase.MatchString(ts) || reDefault.MatchString(ts) {
			// Start (or continue) a case clause. Multiple consecutive
			// `case` labels with no body in between fall through to the
			// same body in C/Java; we group them.
			label := strings.TrimSpace(stripTrailing(l.raw, ':'))
			if cur != nil && len(cur.body) == 0 {
				// Stacked labels — append.
				cur.labels = append(cur.labels, label)
				cur.endLine = l.lineNo
				if reDefault.MatchString(ts) {
					cur.isDefault = true
				}
				i++
				continue
			}
			cur = &caseClause{
				labels:    []string{label},
				isDefault: reDefault.MatchString(ts),
				line:      l.lineNo,
				endLine:   l.lineNo,
			}
			cases = append(cases, cur)
			i++
			// Parse the case body at the next depth. Some languages (Go,
			// JS) put bodies at the same brace depth as the case label,
			// others (Java/C/C++) require an inner `{}` for scoping.
			// We approximate: parse statements at depth+1 until we hit
			// another `case`/`default` at the same depth or the closing `}`.
			bodyStmts, next := parseCaseBody(lines, i, caseDepth)
			cur.body = bodyStmts
			if next > i {
				if next-1 < len(lines) {
					cur.endLine = lines[next-1].lineNo
				}
				i = next
			}
			continue
		}
		// Statements before the first `case` (rare). Skip — they're
		// unreachable in real code.
		i++
	}
	s.cases = cases
	if i < len(lines) {
		s.endLine = lines[i].lineNo
	}
	return s, i + 1
}

func parseCaseBody(lines []sourceLine, idx, caseDepth int) ([]*stmt, int) {
	var out []*stmt
	for idx < len(lines) {
		l := lines[idx]
		if l.closeAt < caseDepth || l.minDepth < caseDepth {
			return out, idx
		}
		ts := strings.TrimSpace(l.stripped)
		if reCase.MatchString(ts) || reDefault.MatchString(ts) {
			return out, idx
		}
		stmts, next := parseStmts(lines, idx, caseDepth)
		out = append(out, stmts...)
		if next == idx {
			idx++
		} else {
			return out, next
		}
	}
	return out, idx
}

// parseSingleStmt parses one logical statement (used for brace-less if/for
// bodies). It is conservative: if the next line is a compound statement
// (if/for/while/etc.) we delegate to parseStmts with a virtual depth that
// matches just one statement.
func parseSingleStmt(lines []sourceLine, idx, depth int) ([]*stmt, int) {
	if idx >= len(lines) {
		return nil, idx
	}
	stmts, next := parseStmts(lines, idx, depth)
	if len(stmts) > 0 {
		return stmts[:1], advanceAfter(lines, idx, stmts[0])
	}
	return stmts, next
}

func advanceAfter(lines []sourceLine, idx int, s *stmt) int {
	for j := idx; j < len(lines); j++ {
		if lines[j].lineNo > s.endLine {
			return j
		}
	}
	return len(lines)
}

// readHeader walks forward from `idx` and joins all lines belonging to the
// statement header — that is, until brace depth increases (opening `{`) or
// the statement terminates with `;` at paren depth 0. Returns the joined
// header text and the index of the first body line (the line after `{` or
// the next line for brace-less constructs).
func readHeader(lines []sourceLine, idx int) (string, int) {
	var parts []string
	startDepth := lines[idx].openAt
	for j := idx; j < len(lines); j++ {
		l := lines[j]
		parts = append(parts, strings.TrimSpace(l.raw))
		if l.closeAt > startDepth {
			// Opened the body — header ends here, body starts next line.
			return strings.Join(parts, " "), j + 1
		}
		// Brace-less: terminate at end of statement (single line in practice).
		if strings.HasSuffix(strings.TrimSpace(l.stripped), ";") ||
			(strings.TrimSpace(l.stripped) != "" && !strings.HasSuffix(strings.TrimSpace(l.stripped), ",") && l.openParen == 0) {
			// Cheap heuristic: if no open `(` continues and the line doesn't
			// end with a comma, treat as the entire single-line statement.
			return strings.Join(parts, " "), j + 1
		}
	}
	return strings.Join(parts, " "), len(lines)
}

func openedBlock(lines []sourceLine, headerIdx, blockStart int) bool {
	if blockStart-1 < 0 || blockStart-1 >= len(lines) {
		return false
	}
	headerEnd := lines[blockStart-1]
	return headerEnd.closeAt > headerEnd.openAt
}

// extractCond pulls the condition expression out of a header line like
// `if (x > 0) {` → "x > 0", or `for i := 0; i < 10; i++ {` → "i := 0; i < 10; i++".
func extractCond(header, keyword string) string {
	h := strings.TrimSpace(header)
	// Drop leading `}` if any.
	h = strings.TrimPrefix(h, "}")
	h = strings.TrimSpace(h)
	// Drop leading `else` (for `else if`).
	h = strings.TrimPrefix(h, "else")
	h = strings.TrimSpace(h)
	// Drop the keyword.
	h = strings.TrimPrefix(h, keyword)
	h = strings.TrimSpace(h)
	// Drop trailing `{` and `:` (Python-like).
	h = strings.TrimSuffix(h, "{")
	h = strings.TrimSpace(h)
	h = strings.TrimSuffix(h, ":")
	h = strings.TrimSpace(h)
	// Drop balanced surrounding `()`.
	if len(h) >= 2 && h[0] == '(' && h[len(h)-1] == ')' {
		// Only strip if the parens are balanced for the whole expression.
		level := 0
		balanced := true
		for i := 0; i < len(h); i++ {
			switch h[i] {
			case '(':
				level++
			case ')':
				level--
				if level == 0 && i != len(h)-1 {
					balanced = false
				}
			}
		}
		if balanced {
			h = strings.TrimSpace(h[1 : len(h)-1])
		}
	}
	return h
}

func stripTrailing(s string, c byte) string {
	s = strings.TrimSpace(s)
	if len(s) > 0 && s[len(s)-1] == c {
		return strings.TrimSpace(s[:len(s)-1])
	}
	return s
}
