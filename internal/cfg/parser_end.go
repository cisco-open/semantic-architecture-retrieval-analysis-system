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
// End-keyword parser (Ruby)
//
// Ruby blocks open with `def` / `if` / `unless` / `while` / `until` / `for` /
// `case` / `begin` / `do` and close with `end`. The challenge is the
// "modifier" form: `puts "yes" if condition` is a single linear statement
// even though it contains `if`. We disambiguate by requiring the opener
// keyword to be at the START of the stripped logical line.
//
// We model `unless cond` as `if !cond`, and `until cond` as `while !cond`.
// Heredocs are not fully supported — when we see `<<-EOF` we skip lines
// until the matching terminator, treating the whole block as the start
// line's content. String interpolation is stripped along with the string.
// ---------------------------------------------------------------------------

type rbLine struct {
	lineNo   int
	raw      string
	stripped string
	indent   int
}

// preprocessEnd lexes Ruby source: removes `"`/`'`/`%w()`/`#{...}` strings
// (best-effort) and `#` line comments. Heredocs are recognised by `<<-`/`<<~`
// and the matching terminator line is consumed without further analysis.
func preprocessEnd(body string, startLine int) []rbLine {
	rawLines := strings.Split(body, "\n")
	out := make([]rbLine, 0, len(rawLines))

	heredocTerminator := ""
	for i, raw := range rawLines {
		// In heredoc body: skip until terminator.
		if heredocTerminator != "" {
			t := strings.TrimSpace(raw)
			out = append(out, rbLine{
				lineNo: startLine + i, raw: raw, stripped: "", indent: indentWidth(raw),
			})
			if t == heredocTerminator {
				heredocTerminator = ""
			}
			continue
		}

		stripped, hd := stripRuby(raw)
		out = append(out, rbLine{
			lineNo:   startLine + i,
			raw:      raw,
			stripped: stripped,
			indent:   indentWidth(raw),
		})
		if hd != "" {
			heredocTerminator = hd
		}
	}
	return out
}

// reHeredoc matches `<<-EOF`, `<<~EOF`, `<<EOF` (no leading-whitespace strip).
var reHeredoc = regexp.MustCompile(`<<[-~]?\s*['"]?(\w+)['"]?`)

// stripRuby removes string literals and line comments. Returns the
// stripped text plus the terminator of an opened heredoc, if any.
func stripRuby(raw string) (string, string) {
	var b strings.Builder
	b.Grow(len(raw))
	i, n := 0, len(raw)
	for i < n {
		c := raw[i]

		// Comment to end of line.
		if c == '#' {
			break
		}

		// String literal.
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
				// Naive: don't try to parse #{...} interpolation depth.
				b.WriteByte(' ')
				i++
			}
			continue
		}

		b.WriteByte(c)
		i++
	}

	stripped := b.String()
	// Detect heredoc opener in the stripped text. We only need the
	// terminator; the body lines are skipped wholesale.
	var heredoc string
	if m := reHeredoc.FindStringSubmatch(stripped); m != nil {
		heredoc = m[1]
	}
	return stripped, heredoc
}

// ---------------------------------------------------------------------------
// Statement parser
// ---------------------------------------------------------------------------

var (
	reRbDef      = regexp.MustCompile(`^def\s+\S+`)
	reRbIf       = regexp.MustCompile(`^if\b`)
	reRbUnless   = regexp.MustCompile(`^unless\b`)
	reRbElsif    = regexp.MustCompile(`^elsif\b`)
	reRbElse     = regexp.MustCompile(`^else\b`)
	reRbWhile    = regexp.MustCompile(`^while\b`)
	reRbUntil    = regexp.MustCompile(`^until\b`)
	reRbFor      = regexp.MustCompile(`^for\b`)
	reRbCase     = regexp.MustCompile(`^case\b`)
	reRbWhen     = regexp.MustCompile(`^when\b`)
	reRbBegin    = regexp.MustCompile(`^begin\b`)
	reRbRescue   = regexp.MustCompile(`^rescue\b`)
	reRbEnsure   = regexp.MustCompile(`^ensure\b`)
	reRbEnd      = regexp.MustCompile(`^end\b`)
	reRbReturn   = regexp.MustCompile(`^return\b`)
	reRbRaise    = regexp.MustCompile(`^raise\b`)
	reRbBreak    = regexp.MustCompile(`^break\b`)
	reRbNext     = regexp.MustCompile(`^next\b`)
	reRbExit     = regexp.MustCompile(`\b(?:exit|exit!|abort)\b`)
	// `do` opens a block when it follows a method call (each/map/...);
	// detect any `do` token at end-of-line.
	reRbDoTail = regexp.MustCompile(`\bdo\b\s*(\|[^|]*\|)?\s*$`)
)

// parseEndFunctionBody is the entry point for Ruby. It finds the `def`
// header, then parses statements inside the function body. The `end` that
// matches the `def` terminates parsing.
func parseEndFunctionBody(body string, startLine int) ([]*stmt, []string) {
	lines := preprocessEnd(body, startLine)
	// Find the def line.
	defIdx := -1
	for i, l := range lines {
		if reRbDef.MatchString(strings.TrimSpace(l.stripped)) {
			defIdx = i
			break
		}
	}
	if defIdx < 0 {
		// No `def` — parse the whole body as if it were a function body.
		stmts, _ := parseRbStmts(lines, 0)
		return stmts, []string{"no `def` header found; parsing whole input"}
	}
	stmts, _ := parseRbStmts(lines, defIdx+1)
	return stmts, nil
}

// parseRbStmts parses statements starting at lines[idx] until a matching
// `end` (or end-of-input) is reached. Returns the parsed statements and
// the index of the line AFTER the closing `end`.
func parseRbStmts(lines []rbLine, idx int) ([]*stmt, int) {
	var out []*stmt
	for idx < len(lines) {
		l := lines[idx]
		t := strings.TrimSpace(l.stripped)

		if t == "" {
			idx++
			continue
		}

		// Closing keyword — caller handles consumption.
		if reRbEnd.MatchString(t) || reRbElsif.MatchString(t) ||
			reRbElse.MatchString(t) || reRbWhen.MatchString(t) ||
			reRbRescue.MatchString(t) || reRbEnsure.MatchString(t) {
			return out, idx
		}

		switch {
		case reRbIf.MatchString(t):
			s, next := parseRbIf(lines, idx, false)
			out = append(out, s)
			idx = next
		case reRbUnless.MatchString(t):
			s, next := parseRbIf(lines, idx, true)
			out = append(out, s)
			idx = next
		case reRbWhile.MatchString(t), reRbFor.MatchString(t):
			s, next := parseRbLoop(lines, idx, false)
			out = append(out, s)
			idx = next
		case reRbUntil.MatchString(t):
			s, next := parseRbLoop(lines, idx, true)
			out = append(out, s)
			idx = next
		case reRbCase.MatchString(t):
			s, next := parseRbCase(lines, idx)
			out = append(out, s)
			idx = next
		case reRbBegin.MatchString(t):
			s, next := parseRbBegin(lines, idx)
			out = append(out, s)
			idx = next
		case reRbReturn.MatchString(t), reRbRaise.MatchString(t), reRbExit.MatchString(l.stripped):
			out = append(out, &stmt{
				kind: skReturn, line: l.lineNo, endLine: l.lineNo,
				header: strings.TrimSpace(l.raw),
				code:   []string{l.raw}, lines: []int{l.lineNo},
			})
			idx++
		case reRbBreak.MatchString(t):
			out = append(out, &stmt{
				kind: skBreak, line: l.lineNo, endLine: l.lineNo,
				header: strings.TrimSpace(l.raw),
				code:   []string{l.raw}, lines: []int{l.lineNo},
			})
			idx++
		case reRbNext.MatchString(t):
			out = append(out, &stmt{
				kind: skContinue, line: l.lineNo, endLine: l.lineNo,
				header: strings.TrimSpace(l.raw),
				code:   []string{l.raw}, lines: []int{l.lineNo},
			})
			idx++
		default:
			// Linear: includes modifier forms (`x if y`) and `xs.each do |x| ... end`.
			// If the line ends with `do |...|` it opens a block — consume up to `end`.
			if reRbDoTail.MatchString(t) {
				body, next := parseRbStmts(lines, idx+1)
				out = append(out, &stmt{
					kind: skLoop, line: l.lineNo,
					endLine: lastRbLine(lines, next, l.lineNo),
					header:  strings.TrimSpace(l.raw),
					cond:    "each", body: body,
				})
				idx = next + 1
				continue
			}
			start := idx
			startLine := l.lineNo
			endLine := l.lineNo
			var code []string
			var ln []int
			for idx < len(lines) {
				ll := lines[idx]
				ts := strings.TrimSpace(ll.stripped)
				if ts == "" {
					idx++
					continue
				}
				if isRbControlStart(ts, ll.stripped) || isRbBlockBoundary(ts) {
					break
				}
				code = append(code, ll.raw)
				ln = append(ln, ll.lineNo)
				endLine = ll.lineNo
				idx++
			}
			if len(code) > 0 {
				out = append(out, &stmt{
					kind: skLinear, line: startLine, endLine: endLine,
					code: code, lines: ln,
				})
			}
			if idx == start {
				idx++
			}
		}
	}
	return out, idx
}

func isRbControlStart(trimmed, stripped string) bool {
	return reRbDef.MatchString(trimmed) ||
		reRbIf.MatchString(trimmed) ||
		reRbUnless.MatchString(trimmed) ||
		reRbWhile.MatchString(trimmed) ||
		reRbUntil.MatchString(trimmed) ||
		reRbFor.MatchString(trimmed) ||
		reRbCase.MatchString(trimmed) ||
		reRbBegin.MatchString(trimmed) ||
		reRbReturn.MatchString(trimmed) ||
		reRbRaise.MatchString(trimmed) ||
		reRbBreak.MatchString(trimmed) ||
		reRbNext.MatchString(trimmed) ||
		reRbExit.MatchString(stripped) ||
		reRbDoTail.MatchString(trimmed)
}

// isRbBlockBoundary reports whether the given trimmed line is a token that
// closes or sub-divides the enclosing block (end / elsif / else / when /
// rescue / ensure). These are NOT control-flow "starts", but they are
// stop conditions for the linear-statement accumulator: without breaking
// here, a linear block would eat its way past the loop's `end` and gobble
// the rest of the function.
func isRbBlockBoundary(trimmed string) bool {
	return reRbEnd.MatchString(trimmed) ||
		reRbElsif.MatchString(trimmed) ||
		reRbElse.MatchString(trimmed) ||
		reRbWhen.MatchString(trimmed) ||
		reRbRescue.MatchString(trimmed) ||
		reRbEnsure.MatchString(trimmed)
}

// parseRbIf parses `if cond ... elsif cond ... else ... end` (or `unless`).
func parseRbIf(lines []rbLine, idx int, negate bool) (*stmt, int) {
	l := lines[idx]
	kw := "if"
	if negate {
		kw = "unless"
	}
	cond := extractRbCond(l.stripped, kw)
	if negate {
		cond = "!(" + cond + ")"
	}
	s := &stmt{
		kind: skIf, line: l.lineNo, endLine: l.lineNo,
		header: strings.TrimSpace(l.raw), cond: cond,
	}
	thenBody, next := parseRbStmts(lines, idx+1)
	s.then = thenBody

	// At most one `elsif` (which recurses to capture further chained
	// `elsif`s) OR one `else` follows the then-body.
	if next < len(lines) {
		ll := lines[next]
		t := strings.TrimSpace(ll.stripped)
		switch {
		case reRbElsif.MatchString(t):
			inner, after := parseRbIf(lines, next, false)
			inner.cond = extractRbCond(ll.stripped, "elsif")
			s.els = []*stmt{inner}
			next = after
		case reRbElse.MatchString(t):
			elseBody, after := parseRbStmts(lines, next+1)
			s.els = elseBody
			next = after
		}
	}

	// Consume the `end` that closes this if.
	if next < len(lines) && reRbEnd.MatchString(strings.TrimSpace(lines[next].stripped)) {
		s.endLine = lines[next].lineNo
		return s, next + 1
	}
	s.endLine = lastRbLine(lines, next, l.lineNo)
	return s, next
}

// parseRbLoop parses `while`/`until`/`for`. `negate` is true for `until`.
func parseRbLoop(lines []rbLine, idx int, negate bool) (*stmt, int) {
	l := lines[idx]
	kw := "while"
	switch {
	case reRbUntil.MatchString(strings.TrimSpace(l.stripped)):
		kw = "until"
	case reRbFor.MatchString(strings.TrimSpace(l.stripped)):
		kw = "for"
	}
	cond := extractRbCond(l.stripped, kw)
	if negate {
		cond = "!(" + cond + ")"
	}
	s := &stmt{
		kind: skLoop, line: l.lineNo, endLine: l.lineNo,
		header: strings.TrimSpace(l.raw), cond: cond,
	}
	body, next := parseRbStmts(lines, idx+1)
	s.body = body
	if next < len(lines) && reRbEnd.MatchString(strings.TrimSpace(lines[next].stripped)) {
		s.endLine = lines[next].lineNo
		return s, next + 1
	}
	s.endLine = lastRbLine(lines, next, l.lineNo)
	return s, next
}

// parseRbCase parses `case expr ... when x ... when y ... else ... end`.
func parseRbCase(lines []rbLine, idx int) (*stmt, int) {
	l := lines[idx]
	s := &stmt{
		kind: skSwitch, line: l.lineNo, endLine: l.lineNo,
		header: strings.TrimSpace(l.raw), cond: extractRbCond(l.stripped, "case"),
	}
	i := idx + 1
	for i < len(lines) {
		ll := lines[i]
		t := strings.TrimSpace(ll.stripped)
		if t == "" {
			i++
			continue
		}
		if reRbEnd.MatchString(t) {
			s.endLine = ll.lineNo
			return s, i + 1
		}
		if reRbElse.MatchString(t) {
			body, after := parseRbStmts(lines, i+1)
			s.cases = append(s.cases, &caseClause{
				labels:    []string{"else"},
				isDefault: true,
				line:      ll.lineNo,
				endLine:   lastRbLine(lines, after-1, ll.lineNo),
				body:      body,
			})
			i = after
			continue
		}
		if reRbWhen.MatchString(t) {
			label := strings.TrimSpace(t)
			body, after := parseRbStmts(lines, i+1)
			s.cases = append(s.cases, &caseClause{
				labels:  []string{label},
				line:    ll.lineNo,
				endLine: lastRbLine(lines, after-1, ll.lineNo),
				body:    body,
			})
			i = after
			continue
		}
		// Anything else inside case but outside a when is unusual; skip.
		i++
	}
	s.endLine = lastRbLine(lines, i-1, s.endLine)
	return s, i
}

// parseRbBegin handles `begin ... rescue ... ensure ... end` as a branch.
func parseRbBegin(lines []rbLine, idx int) (*stmt, int) {
	l := lines[idx]
	body, next := parseRbStmts(lines, idx+1)
	s := &stmt{
		kind: skIf, line: l.lineNo, endLine: l.lineNo,
		header: "begin", cond: "no rescue?", then: body,
	}
	var tail []*stmt
	for next < len(lines) {
		ll := lines[next]
		t := strings.TrimSpace(ll.stripped)
		if reRbRescue.MatchString(t) || reRbEnsure.MatchString(t) {
			b, after := parseRbStmts(lines, next+1)
			tail = append(tail, &stmt{
				kind: skBlock, line: ll.lineNo,
				endLine: lastRbLine(lines, after-1, ll.lineNo),
				header:  strings.TrimSpace(ll.raw),
				body:    b,
			})
			next = after
			continue
		}
		if reRbEnd.MatchString(t) {
			s.endLine = ll.lineNo
			s.els = tail
			return s, next + 1
		}
		break
	}
	if len(tail) > 0 {
		s.els = tail
	}
	s.endLine = lastRbLine(lines, next, s.endLine)
	return s, next
}

func extractRbCond(stripped, keyword string) string {
	h := strings.TrimSpace(stripped)
	h = strings.TrimPrefix(h, keyword)
	h = strings.TrimSpace(h)
	// Drop trailing `then` / `do` (Ruby's optional one-line block opener).
	h = strings.TrimSuffix(h, "then")
	h = strings.TrimSpace(h)
	h = strings.TrimSuffix(h, "do")
	return strings.TrimSpace(h)
}

func lastRbLine(lines []rbLine, k, fallback int) int {
	if k < 0 || k >= len(lines) {
		return fallback
	}
	return lines[k].lineNo
}
