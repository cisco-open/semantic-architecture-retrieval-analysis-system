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
// Indentation-based parser (Python 2 & 3)
//
// Strategy:
//   1. Lex each source line, stripping `#` comments and `"""`, `'''`, `"`, `'`
//      strings so we never mistake their contents for control-flow keywords.
//   2. Skip blank/decorator/docstring lines and lines that are continuations
//      of an open parenthesis or end with `\`.
//   3. For each "logical" line, record:
//        - its indent (in spaces; tabs count as 8 per PEP 8 §3)
//        - the stripped content
//   4. Identify the function body: it's the contiguous block of lines whose
//      indent is strictly greater than the `def` / `async def` header.
//   5. Within the body, parse statements recursively: when we see a header
//      ending with `:` (if/elif/else/for/while/try/except/finally/with/match/
//      case), the body is all subsequent lines at indent > header indent.
//
// One-liner `if cond: stmt` and `for x in y: stmt` are recognised but the
// inline body is treated as a single linear statement — sufficient for path
// enumeration since there's only one branch outcome.
// ---------------------------------------------------------------------------

// pyLine is a Python logical source line after string/comment stripping.
type pyLine struct {
	lineNo   int    // 1-indexed
	raw      string // original
	stripped string // strings/comments removed
	indent   int    // leading whitespace width (tab=8)
	blank    bool   // empty after stripping
	cont     bool   // line is a continuation of the previous (open paren / `\`)
}

// indentWidth returns the visual indent of `s` (tabs expanded to 8 spaces).
// We deliberately use 8 (not 4) so a line indented with a mix of tabs and
// spaces consistently sorts above or below its siblings.
func indentWidth(s string) int {
	w := 0
	for _, c := range s {
		switch c {
		case ' ':
			w++
		case '\t':
			w += 8
		default:
			return w
		}
	}
	return w
}

// preprocessIndent tokenises Python source into pyLines and tracks open
// parenthesis depth and explicit line continuations so multi-line
// expressions are collapsed into a single logical line.
func preprocessIndent(body string, startLine int) []pyLine {
	rawLines := strings.Split(body, "\n")
	out := make([]pyLine, 0, len(rawLines))

	inTriple := false       // inside """ or ''' block
	tripleDelim := byte('"') // delimiter of the open triple string
	parenDepth := 0
	contFromBackslash := false

	for i, raw := range rawLines {
		stripped, newTriple, newDelim, newParenDepth, _ := stripPython(
			raw, inTriple, tripleDelim, parenDepth)

		wasCont := contFromBackslash || (i > 0 && (parenDepth > 0))
		inTriple = newTriple
		tripleDelim = newDelim
		parenDepth = newParenDepth

		// A trailing backslash (outside strings) starts a continuation that
		// runs on the *next* line.
		contFromBackslash = strings.HasSuffix(strings.TrimRight(stripped, " \t"), "\\")
		if contFromBackslash {
			stripped = strings.TrimRight(stripped, "\\ \t")
		}

		trimmed := strings.TrimSpace(stripped)
		out = append(out, pyLine{
			lineNo:   startLine + i,
			raw:      raw,
			stripped: stripped,
			indent:   indentWidth(raw),
			blank:    trimmed == "",
			cont:     wasCont,
		})
	}
	return out
}

// stripPython removes string literals and `#` comments from a single Python
// source line, while tracking whether we're inside a triple-quoted string
// and the current parenthesis depth (only `()`, `[]`, `{}` change depth).
// Returns the stripped text, the new triple-string state, and the new
// paren depth. The boolean fifth return is reserved for future use.
func stripPython(raw string, inTriple bool, tripleDelim byte, paren int) (string, bool, byte, int, bool) {
	var b strings.Builder
	b.Grow(len(raw))

	i := 0
	n := len(raw)
	for i < n {
		c := raw[i]

		// Inside triple-string: look for matching closing delimiter.
		if inTriple {
			if i+2 < n && raw[i] == tripleDelim && raw[i+1] == tripleDelim && raw[i+2] == tripleDelim {
				inTriple = false
				b.WriteString("   ")
				i += 3
				continue
			}
			b.WriteByte(' ')
			i++
			continue
		}

		// Possible start of a triple-quoted string.
		if (c == '"' || c == '\'') && i+2 < n && raw[i+1] == c && raw[i+2] == c {
			inTriple = true
			tripleDelim = c
			b.WriteString("   ")
			i += 3
			continue
		}

		// Regular `"`/`'` string literal.
		if c == '"' || c == '\'' {
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

		// `#` line comment.
		if c == '#' {
			break
		}

		switch c {
		case '(', '[', '{':
			paren++
		case ')', ']', '}':
			paren--
		}
		b.WriteByte(c)
		i++
	}

	return b.String(), inTriple, tripleDelim, paren, false
}

// ---------------------------------------------------------------------------
// Statement parser
// ---------------------------------------------------------------------------

var (
	reIndentIf      = regexp.MustCompile(`^if\b`)
	reIndentElif    = regexp.MustCompile(`^elif\b`)
	reIndentElse    = regexp.MustCompile(`^else\s*:`)
	reIndentFor     = regexp.MustCompile(`^(?:async\s+)?for\b`)
	reIndentWhile   = regexp.MustCompile(`^while\b`)
	reIndentTry     = regexp.MustCompile(`^try\s*:`)
	reIndentExcept  = regexp.MustCompile(`^except\b`)
	reIndentFinally = regexp.MustCompile(`^finally\s*:`)
	reIndentWith    = regexp.MustCompile(`^(?:async\s+)?with\b`)
	reIndentMatch   = regexp.MustCompile(`^match\b`)
	reIndentCase    = regexp.MustCompile(`^case\b`)
	reIndentReturn  = regexp.MustCompile(`^return\b`)
	reIndentRaise   = regexp.MustCompile(`^raise\b`)
	reIndentBreak   = regexp.MustCompile(`^break\b`)
	reIndentContinue = regexp.MustCompile(`^continue\b`)
	reIndentExit    = regexp.MustCompile(`\b(?:sys\.exit|os\._exit|exit|quit)\s*\(`)
	reIndentDef     = regexp.MustCompile(`^(?:async\s+)?def\s+\w+`)
)

// parseIndentFunctionBody is the entry point for indentation-based languages.
// It returns the parsed statements of the function body (excluding the `def`
// header line itself) plus diagnostic notes.
func parseIndentFunctionBody(body string, startLine int) ([]*stmt, []string) {
	lines := preprocessIndent(body, startLine)
	// Find the def/async-def header.
	headerIdx := -1
	for i, l := range lines {
		t := strings.TrimSpace(l.stripped)
		if reIndentDef.MatchString(t) {
			headerIdx = i
			break
		}
	}
	if headerIdx < 0 {
		// No header found — parse what we have at indent 0 (allows tests
		// that pass bare function bodies).
		return parseIndentStmts(lines, 0, 0), nil
	}
	headerIndent := lines[headerIdx].indent
	bodyStart := headerIdx + 1
	// The body's indent is the indent of the first non-blank, non-continuation
	// line at indent > headerIndent.
	bodyIndent := -1
	for i := bodyStart; i < len(lines); i++ {
		if lines[i].blank || lines[i].cont {
			continue
		}
		if lines[i].indent > headerIndent {
			bodyIndent = lines[i].indent
			break
		}
		// Function with no body (`def foo(): pass` on a single line) — bail.
		break
	}
	if bodyIndent < 0 {
		return nil, []string{"function body is empty or single-line"}
	}
	stmts := parseIndentStmts(lines[bodyStart:], bodyIndent, 0)
	return stmts, nil
}

// parseIndentStmts parses sibling statements at exactly `indent` width and
// returns when a line with smaller indent is reached. `startIdx` is the
// index into `lines` to start scanning from.
func parseIndentStmts(lines []pyLine, indent int, startIdx int) []*stmt {
	var out []*stmt
	i := startIdx
	for i < len(lines) {
		l := lines[i]
		if l.blank || l.cont {
			i++
			continue
		}
		if l.indent < indent {
			return out
		}
		if l.indent > indent {
			// Stray over-indented line — skip (rare, defensive).
			i++
			continue
		}

		text := strings.TrimSpace(l.stripped)
		switch {
		case reIndentIf.MatchString(text):
			s, next := parseIndentIf(lines, i, indent)
			out = append(out, s)
			i = next
		case reIndentFor.MatchString(text), reIndentWhile.MatchString(text):
			s, next := parseIndentLoop(lines, i, indent)
			out = append(out, s)
			i = next
		case reIndentTry.MatchString(text):
			s, next := parseIndentTry(lines, i, indent)
			out = append(out, s)
			i = next
		case reIndentMatch.MatchString(text):
			s, next := parseIndentMatch(lines, i, indent)
			out = append(out, s)
			i = next
		case reIndentWith.MatchString(text):
			// `with` doesn't fork control flow — treat as a single block
			// container whose body executes linearly.
			body, next := readIndentBody(lines, i, indent)
			out = append(out, &stmt{
				kind:    skBlock,
				line:    l.lineNo,
				endLine: lastLineNo(lines, next-1, l.lineNo),
				header:  text,
				body:    body,
			})
			i = next
		case reIndentReturn.MatchString(text), reIndentRaise.MatchString(text), reIndentExit.MatchString(l.stripped):
			out = append(out, &stmt{
				kind:    skReturn,
				line:    l.lineNo,
				endLine: l.lineNo,
				header:  strings.TrimSpace(l.raw),
				code:    []string{l.raw},
				lines:   []int{l.lineNo},
			})
			i++
		case reIndentBreak.MatchString(text):
			out = append(out, &stmt{
				kind: skBreak, line: l.lineNo, endLine: l.lineNo,
				header: strings.TrimSpace(l.raw),
				code:   []string{l.raw}, lines: []int{l.lineNo},
			})
			i++
		case reIndentContinue.MatchString(text):
			out = append(out, &stmt{
				kind: skContinue, line: l.lineNo, endLine: l.lineNo,
				header: strings.TrimSpace(l.raw),
				code:   []string{l.raw}, lines: []int{l.lineNo},
			})
			i++
		default:
			// Linear: greedily accumulate consecutive same-indent
			// non-control lines.
			start := i
			startLine := l.lineNo
			endLine := l.lineNo
			var code []string
			var ln []int
			for i < len(lines) {
				ll := lines[i]
				if ll.blank || ll.cont {
					i++
					continue
				}
				if ll.indent != indent {
					break
				}
				ts := strings.TrimSpace(ll.stripped)
				if isIndentControlStart(ts, ll.stripped) {
					break
				}
				code = append(code, ll.raw)
				ln = append(ln, ll.lineNo)
				endLine = ll.lineNo
				i++
			}
			if len(code) > 0 {
				out = append(out, &stmt{
					kind: skLinear, line: startLine, endLine: endLine,
					code: code, lines: ln,
				})
			}
			if i == start {
				i++
			}
		}
	}
	return out
}

func isIndentControlStart(trimmed, stripped string) bool {
	return reIndentIf.MatchString(trimmed) ||
		reIndentElif.MatchString(trimmed) ||
		reIndentElse.MatchString(trimmed) ||
		reIndentFor.MatchString(trimmed) ||
		reIndentWhile.MatchString(trimmed) ||
		reIndentTry.MatchString(trimmed) ||
		reIndentExcept.MatchString(trimmed) ||
		reIndentFinally.MatchString(trimmed) ||
		reIndentWith.MatchString(trimmed) ||
		reIndentMatch.MatchString(trimmed) ||
		reIndentReturn.MatchString(trimmed) ||
		reIndentRaise.MatchString(trimmed) ||
		reIndentBreak.MatchString(trimmed) ||
		reIndentContinue.MatchString(trimmed) ||
		reIndentExit.MatchString(stripped)
}

// readIndentBody reads the indented body of a header line at index
// `headerIdx`. Returns the parsed body statements and the index of the
// first line that is NOT part of the body (caller resumes from there).
func readIndentBody(lines []pyLine, headerIdx, headerIndent int) ([]*stmt, int) {
	// Find the body's indent: first non-blank, non-continuation line after
	// the header whose indent > headerIndent.
	bodyIndent := -1
	bodyStart := headerIdx + 1
	for i := bodyStart; i < len(lines); i++ {
		if lines[i].blank || lines[i].cont {
			continue
		}
		if lines[i].indent > headerIndent {
			bodyIndent = lines[i].indent
			bodyStart = i
			break
		}
		break
	}
	if bodyIndent < 0 {
		// One-liner like `if x: y = 1` — synthesise a single linear stmt
		// from whatever follows the `:` on the same line.
		l := lines[headerIdx]
		s := strings.TrimSpace(l.stripped)
		if idx := strings.Index(s, ":"); idx >= 0 && idx < len(s)-1 {
			tail := strings.TrimSpace(s[idx+1:])
			if tail != "" {
				return []*stmt{{
					kind: skLinear, line: l.lineNo, endLine: l.lineNo,
					code: []string{l.raw}, lines: []int{l.lineNo},
				}}, headerIdx + 1
			}
		}
		return nil, headerIdx + 1
	}

	// Parse all sibling statements at bodyIndent.
	body := parseIndentStmts(lines, bodyIndent, bodyStart)
	// Find the index past the last body line: first line with indent <= headerIndent.
	end := bodyStart
	for end < len(lines) {
		if lines[end].blank || lines[end].cont {
			end++
			continue
		}
		if lines[end].indent <= headerIndent {
			break
		}
		end++
	}
	return body, end
}

// parseIndentIf parses `if`/`elif*`/`else` chains. Multiple `elif` clauses
// are modelled by nesting them inside the parent if's `els`, mirroring the
// existing brace parser's representation.
func parseIndentIf(lines []pyLine, idx, indent int) (*stmt, int) {
	l := lines[idx]
	cond := extractIndentCond(l.stripped, "if")
	s := &stmt{
		kind: skIf, line: l.lineNo, endLine: l.lineNo,
		header: strings.TrimSpace(l.raw), cond: cond,
	}
	then, next := readIndentBody(lines, idx, indent)
	s.then = then
	s.endLine = lastLineNo(lines, next-1, l.lineNo)

	// elif / else attached at the same indent.
	for next < len(lines) {
		ll := lines[next]
		if ll.blank || ll.cont {
			next++
			continue
		}
		if ll.indent != indent {
			break
		}
		t := strings.TrimSpace(ll.stripped)
		if reIndentElif.MatchString(t) {
			elifStmt := &stmt{
				kind: skIf, line: ll.lineNo, endLine: ll.lineNo,
				header: strings.TrimSpace(ll.raw),
				cond:   extractIndentCond(ll.stripped, "elif"),
			}
			elifThen, after := readIndentBody(lines, next, indent)
			elifStmt.then = elifThen
			elifStmt.endLine = lastLineNo(lines, after-1, ll.lineNo)
			// Recursively attach any further elif/else to this elif.
			tailIdx := after
			for tailIdx < len(lines) {
				ll2 := lines[tailIdx]
				if ll2.blank || ll2.cont {
					tailIdx++
					continue
				}
				if ll2.indent != indent {
					break
				}
				t2 := strings.TrimSpace(ll2.stripped)
				if reIndentElif.MatchString(t2) {
					innerElif, innerAfter := parseIndentElifChain(lines, tailIdx, indent)
					elifStmt.els = []*stmt{innerElif}
					tailIdx = innerAfter
					break
				}
				if reIndentElse.MatchString(t2) {
					elseBody, innerAfter := readIndentBody(lines, tailIdx, indent)
					elifStmt.els = elseBody
					elifStmt.endLine = lastLineNo(lines, innerAfter-1, elifStmt.endLine)
					tailIdx = innerAfter
				}
				break
			}
			s.els = []*stmt{elifStmt}
			next = tailIdx
			break
		}
		if reIndentElse.MatchString(t) {
			elseBody, after := readIndentBody(lines, next, indent)
			s.els = elseBody
			s.endLine = lastLineNo(lines, after-1, s.endLine)
			next = after
		}
		break
	}
	return s, next
}

// parseIndentElifChain helps parseIndentIf attach `elif x: ... elif y: ...`
// chains as nested ifs.
func parseIndentElifChain(lines []pyLine, idx, indent int) (*stmt, int) {
	l := lines[idx]
	s := &stmt{
		kind: skIf, line: l.lineNo, endLine: l.lineNo,
		header: strings.TrimSpace(l.raw),
		cond:   extractIndentCond(l.stripped, "elif"),
	}
	then, next := readIndentBody(lines, idx, indent)
	s.then = then
	s.endLine = lastLineNo(lines, next-1, l.lineNo)

	for next < len(lines) {
		ll := lines[next]
		if ll.blank || ll.cont {
			next++
			continue
		}
		if ll.indent != indent {
			break
		}
		t := strings.TrimSpace(ll.stripped)
		if reIndentElif.MatchString(t) {
			inner, after := parseIndentElifChain(lines, next, indent)
			s.els = []*stmt{inner}
			next = after
			break
		}
		if reIndentElse.MatchString(t) {
			elseBody, after := readIndentBody(lines, next, indent)
			s.els = elseBody
			next = after
		}
		break
	}
	return s, next
}

// parseIndentLoop handles `for` and `while`.
func parseIndentLoop(lines []pyLine, idx, indent int) (*stmt, int) {
	l := lines[idx]
	kw := "while"
	if reIndentFor.MatchString(strings.TrimSpace(l.stripped)) {
		kw = "for"
	}
	cond := extractIndentCond(l.stripped, kw)
	s := &stmt{
		kind: skLoop, line: l.lineNo, endLine: l.lineNo,
		header: strings.TrimSpace(l.raw), cond: cond,
	}
	body, next := readIndentBody(lines, idx, indent)
	s.body = body
	s.endLine = lastLineNo(lines, next-1, l.lineNo)

	// Python `for`/`while` can have an `else:` clause that runs when the
	// loop exits normally (no break). We don't model that branch
	// explicitly; we record a note so the user knows.
	for next < len(lines) {
		ll := lines[next]
		if ll.blank || ll.cont {
			next++
			continue
		}
		if ll.indent != indent {
			break
		}
		t := strings.TrimSpace(ll.stripped)
		if reIndentElse.MatchString(t) {
			elseBody, after := readIndentBody(lines, next, indent)
			// Attach as a successor sibling block so its statements
			// still appear in the CFG (linearly after the loop).
			s.els = elseBody
			s.endLine = lastLineNo(lines, after-1, s.endLine)
			next = after
		}
		break
	}
	return s, next
}

// parseIndentTry models try/except/finally as a branch where each `except`
// is an alternative continuation. We use skIf with a synthetic cond
// (`exception?`) because skIf already supports n-way else chains and
// produces sensible paths for tests.
func parseIndentTry(lines []pyLine, idx, indent int) (*stmt, int) {
	l := lines[idx]
	tryBody, next := readIndentBody(lines, idx, indent)

	s := &stmt{
		kind:   skIf,
		line:   l.lineNo,
		endLine: l.lineNo,
		header: strings.TrimSpace(l.raw),
		cond:   "no exception?",
		then:   tryBody,
	}
	s.endLine = lastLineNo(lines, next-1, l.lineNo)

	var exceptBlocks []*stmt
	for next < len(lines) {
		ll := lines[next]
		if ll.blank || ll.cont {
			next++
			continue
		}
		if ll.indent != indent {
			break
		}
		t := strings.TrimSpace(ll.stripped)
		// `except` is an alternative continuation; `finally` runs on every
		// path. Both attach as sibling blocks — distinguishing the two is
		// left to display/LLM analysis since the heuristic CFG only needs
		// the source lines to enumerate paths.
		if !reIndentExcept.MatchString(t) && !reIndentFinally.MatchString(t) {
			break
		}
		body, after := readIndentBody(lines, next, indent)
		exceptBlocks = append(exceptBlocks, &stmt{
			kind: skBlock, line: ll.lineNo,
			endLine: lastLineNo(lines, after-1, ll.lineNo),
			header:  strings.TrimSpace(ll.raw),
			body:    body,
		})
		next = after
	}
	if len(exceptBlocks) > 0 {
		s.els = exceptBlocks
		s.endLine = lastLineNo(lines, next-1, s.endLine)
	}
	return s, next
}

// parseIndentMatch handles Python 3.10+ structural pattern matching by
// modelling it as a switch statement.
func parseIndentMatch(lines []pyLine, idx, indent int) (*stmt, int) {
	l := lines[idx]
	cond := extractIndentCond(l.stripped, "match")
	s := &stmt{
		kind: skSwitch, line: l.lineNo, endLine: l.lineNo,
		header: strings.TrimSpace(l.raw), cond: cond,
	}

	// Cases live at indent + visible step (4 by convention; we discover it).
	caseIndent := -1
	idx2 := idx + 1
	for idx2 < len(lines) {
		if lines[idx2].blank || lines[idx2].cont {
			idx2++
			continue
		}
		if lines[idx2].indent > indent {
			caseIndent = lines[idx2].indent
		}
		break
	}
	if caseIndent < 0 {
		return s, idx + 1
	}

	i := idx2
	for i < len(lines) {
		ll := lines[i]
		if ll.blank || ll.cont {
			i++
			continue
		}
		if ll.indent < caseIndent {
			break
		}
		if ll.indent != caseIndent {
			i++
			continue
		}
		t := strings.TrimSpace(ll.stripped)
		if !reIndentCase.MatchString(t) {
			break
		}
		caseLabel := strings.TrimSpace(strings.TrimSuffix(t, ":"))
		body, after := readIndentBody(lines, i, caseIndent)
		s.cases = append(s.cases, &caseClause{
			labels:    []string{caseLabel},
			isDefault: caseLabel == "case _",
			line:      ll.lineNo,
			endLine:   lastLineNo(lines, after-1, ll.lineNo),
			body:      body,
		})
		i = after
	}
	s.endLine = lastLineNo(lines, i-1, s.endLine)
	return s, i
}

// extractIndentCond pulls the condition expression out of a header line
// such as `if x > 0:`, `for i in range(10):`, `while not done:`.
func extractIndentCond(stripped, keyword string) string {
	h := strings.TrimSpace(stripped)
	h = strings.TrimPrefix(h, keyword)
	h = strings.TrimSpace(h)
	h = strings.TrimSuffix(h, ":")
	return strings.TrimSpace(h)
}

// lastLineNo returns the source line number of lines[k] if k is in range,
// else fallback.
func lastLineNo(lines []pyLine, k, fallback int) int {
	if k < 0 || k >= len(lines) {
		return fallback
	}
	if lines[k].lineNo == 0 {
		return fallback
	}
	return lines[k].lineNo
}
