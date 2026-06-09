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
// Shell parser (sh / bash / zsh / ksh)
//
// Bash function bodies use either a brace block `name() { ... }` or a
// `name() ( ... )` subshell. Inside the body, control flow uses three
// terminator pairs:
//
//   - if / elif / else / fi
//   - for / while / until ... do ... done
//   - case ... in    pat) ... ;; ... esac
//
// We treat statement separation as line-based; `;` separators on a single
// line are not split further (a Bash one-liner is collapsed into one linear
// statement, which still produces a usable CFG for path enumeration).
//
// String / heredoc handling:
//   - `'..'` and `".."` are stripped to spaces.
//   - Heredocs (`<< 'EOF'`, `<<-EOF`, `<< EOF`) are detected and their body
//     lines are not parsed as control flow — they're rolled into the
//     opening line's linear statement.
// ---------------------------------------------------------------------------

type shLine struct {
	lineNo   int
	raw      string
	stripped string
}

func preprocessShell(body string, startLine int) []shLine {
	rawLines := strings.Split(body, "\n")
	out := make([]shLine, 0, len(rawLines))
	heredocTerm := ""
	for i, raw := range rawLines {
		if heredocTerm != "" {
			out = append(out, shLine{
				lineNo: startLine + i, raw: raw, stripped: "",
			})
			if strings.TrimSpace(raw) == heredocTerm {
				heredocTerm = ""
			}
			continue
		}
		stripped, hd := stripShell(raw)
		out = append(out, shLine{
			lineNo: startLine + i, raw: raw, stripped: stripped,
		})
		if hd != "" {
			heredocTerm = hd
		}
	}
	return out
}

// reShHeredoc captures the heredoc terminator.
var reShHeredoc = regexp.MustCompile(`<<[-]?\s*['"]?(\w+)['"]?`)

func stripShell(raw string) (string, string) {
	var b strings.Builder
	b.Grow(len(raw))
	i, n := 0, len(raw)
	for i < n {
		c := raw[i]
		if c == '#' {
			break
		}
		if c == '"' || c == '\'' {
			quote := c
			b.WriteByte(' ')
			i++
			for i < n {
				ch := raw[i]
				if quote == '"' && ch == '\\' && i+1 < n {
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
		b.WriteByte(c)
		i++
	}
	stripped := b.String()
	var heredoc string
	if m := reShHeredoc.FindStringSubmatch(stripped); m != nil {
		heredoc = m[1]
	}
	return stripped, heredoc
}

// ---------------------------------------------------------------------------
// Statement parser
// ---------------------------------------------------------------------------

var (
	reShIf      = regexp.MustCompile(`^if\b`)
	reShElif    = regexp.MustCompile(`^elif\b`)
	reShElse    = regexp.MustCompile(`^else\b`)
	reShFi      = regexp.MustCompile(`^fi\b`)
	reShFor     = regexp.MustCompile(`^for\b`)
	reShWhile   = regexp.MustCompile(`^while\b`)
	reShUntil   = regexp.MustCompile(`^until\b`)
	reShDoLine  = regexp.MustCompile(`^do\b`)
	reShDone    = regexp.MustCompile(`^done\b`)
	reShCase    = regexp.MustCompile(`^case\b`)
	reShEsac    = regexp.MustCompile(`^esac\b`)
	reShReturn  = regexp.MustCompile(`^return\b`)
	reShExit    = regexp.MustCompile(`^exit\b`)
	reShBreak   = regexp.MustCompile(`^break\b`)
	reShContinue = regexp.MustCompile(`^continue\b`)
	reShFuncBrace = regexp.MustCompile(`^\s*(?:function\s+)?\w+\s*\(\s*\)\s*\{`)
	// case pattern line: ends with `)` and not preceded by `(` mismatch
	reShCasePat = regexp.MustCompile(`\)\s*$`)
	// case branch end token `;;` may appear at end of line or on its own line
	reShCaseEnd = regexp.MustCompile(`;;\s*$`)
)

// parseShellFunctionBody is the entry point for shell function bodies.
// It locates the opening brace of the function (which may be on the same
// line as the declaration) and parses until the matching closing brace.
func parseShellFunctionBody(body string, startLine int) ([]*stmt, []string) {
	lines := preprocessShell(body, startLine)
	// Locate `{` — either at end of the first line (`foo() {`) or the next
	// non-empty line.
	startIdx := -1
	for i, l := range lines {
		t := strings.TrimSpace(l.stripped)
		if reShFuncBrace.MatchString(t) && strings.HasSuffix(t, "{") {
			startIdx = i + 1
			break
		}
		if t == "{" {
			startIdx = i + 1
			break
		}
		// Header may end with `{` on a later line in `foo()` style with
		// the brace on its own line.
	}
	if startIdx < 0 {
		// Headerless input — parse all of it.
		stmts, _ := parseShStmts(lines, 0)
		return stmts, []string{"no function header `name() {` found; parsing whole input"}
	}
	stmts, _ := parseShStmts(lines, startIdx)
	return stmts, nil
}

// parseShStmts parses statements until it hits a `}`/`fi`/`done`/`esac`/`;;`
// or end of input.
func parseShStmts(lines []shLine, idx int) ([]*stmt, int) {
	var out []*stmt
	for idx < len(lines) {
		l := lines[idx]
		t := strings.TrimSpace(l.stripped)
		if t == "" {
			idx++
			continue
		}
		if t == "}" || reShFi.MatchString(t) || reShDone.MatchString(t) || reShEsac.MatchString(t) ||
			reShElif.MatchString(t) || reShElse.MatchString(t) || reShDoLine.MatchString(t) ||
			t == ";;" || reShCaseEnd.MatchString(t) {
			return out, idx
		}
		switch {
		case reShIf.MatchString(t):
			s, next := parseShIf(lines, idx)
			out = append(out, s)
			idx = next
		case reShFor.MatchString(t), reShWhile.MatchString(t), reShUntil.MatchString(t):
			s, next := parseShLoop(lines, idx)
			out = append(out, s)
			idx = next
		case reShCase.MatchString(t):
			s, next := parseShCase(lines, idx)
			out = append(out, s)
			idx = next
		case reShReturn.MatchString(t), reShExit.MatchString(t):
			out = append(out, &stmt{
				kind: skReturn, line: l.lineNo, endLine: l.lineNo,
				header: strings.TrimSpace(l.raw),
				code:   []string{l.raw}, lines: []int{l.lineNo},
			})
			idx++
		case reShBreak.MatchString(t):
			out = append(out, &stmt{
				kind: skBreak, line: l.lineNo, endLine: l.lineNo,
				header: strings.TrimSpace(l.raw),
				code:   []string{l.raw}, lines: []int{l.lineNo},
			})
			idx++
		case reShContinue.MatchString(t):
			out = append(out, &stmt{
				kind: skContinue, line: l.lineNo, endLine: l.lineNo,
				header: strings.TrimSpace(l.raw),
				code:   []string{l.raw}, lines: []int{l.lineNo},
			})
			idx++
		default:
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
				if isShControlStart(ts) {
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

func isShControlStart(trimmed string) bool {
	if trimmed == "}" || trimmed == ";;" {
		return true
	}
	return reShIf.MatchString(trimmed) ||
		reShElif.MatchString(trimmed) ||
		reShElse.MatchString(trimmed) ||
		reShFi.MatchString(trimmed) ||
		reShFor.MatchString(trimmed) ||
		reShWhile.MatchString(trimmed) ||
		reShUntil.MatchString(trimmed) ||
		reShDoLine.MatchString(trimmed) ||
		reShDone.MatchString(trimmed) ||
		reShCase.MatchString(trimmed) ||
		reShEsac.MatchString(trimmed) ||
		reShReturn.MatchString(trimmed) ||
		reShExit.MatchString(trimmed) ||
		reShBreak.MatchString(trimmed) ||
		reShContinue.MatchString(trimmed) ||
		reShCaseEnd.MatchString(trimmed)
}

// parseShIf parses `if cmds; then ... [elif ...; then ...]* [else ...] fi`.
//
// `then` may be on the same line as `if` (after `;`) or on its own line.
// We don't try to interpret the test expression — we record everything up
// to (but not including) `then` as the cond.
//
// elif chains are modelled by nesting: each elif becomes the only `els`
// statement of its predecessor, mirroring the brace parser's strategy.
func parseShIf(lines []shLine, idx int) (*stmt, int) {
	l := lines[idx]
	cond := extractShCond(lines, idx, "if", "then")
	outer := &stmt{
		kind: skIf, line: l.lineNo, endLine: l.lineNo,
		header: strings.TrimSpace(l.raw), cond: cond,
	}
	bodyStart := nextAfterToken(lines, idx, "then")
	thenBody, next := parseShStmts(lines, bodyStart)
	outer.then = thenBody

	// `tail` is the innermost if that future `elif` / `else` clauses
	// attach to. It starts equal to outer and walks down the els chain
	// each time we add an elif.
	tail := outer

	for next < len(lines) {
		ll := lines[next]
		t := strings.TrimSpace(ll.stripped)
		switch {
		case reShElif.MatchString(t):
			elifCond := extractShCond(lines, next, "elif", "then")
			elifBodyStart := nextAfterToken(lines, next, "then")
			elifBody, after := parseShStmts(lines, elifBodyStart)
			elif := &stmt{
				kind: skIf, line: ll.lineNo,
				endLine: lastShLine(lines, after-1, ll.lineNo),
				header:  strings.TrimSpace(ll.raw), cond: elifCond,
				then: elifBody,
			}
			tail.els = []*stmt{elif}
			tail = elif
			next = after
			continue
		case reShElse.MatchString(t):
			elseBody, after := parseShStmts(lines, next+1)
			tail.els = elseBody
			next = after
		}
		break
	}
	// Consume the `fi`.
	if next < len(lines) && reShFi.MatchString(strings.TrimSpace(lines[next].stripped)) {
		outer.endLine = lines[next].lineNo
		return outer, next + 1
	}
	outer.endLine = lastShLine(lines, next, outer.endLine)
	return outer, next
}

func parseShLoop(lines []shLine, idx int) (*stmt, int) {
	l := lines[idx]
	t := strings.TrimSpace(l.stripped)
	kw := "while"
	switch {
	case reShFor.MatchString(t):
		kw = "for"
	case reShUntil.MatchString(t):
		kw = "until"
	}
	cond := extractShCond(lines, idx, kw, "do")
	s := &stmt{
		kind: skLoop, line: l.lineNo, endLine: l.lineNo,
		header: strings.TrimSpace(l.raw), cond: cond,
	}
	bodyStart := nextAfterToken(lines, idx, "do")
	body, next := parseShStmts(lines, bodyStart)
	s.body = body
	if next < len(lines) && reShDone.MatchString(strings.TrimSpace(lines[next].stripped)) {
		s.endLine = lines[next].lineNo
		return s, next + 1
	}
	s.endLine = lastShLine(lines, next, l.lineNo)
	return s, next
}

func parseShCase(lines []shLine, idx int) (*stmt, int) {
	l := lines[idx]
	s := &stmt{
		kind: skSwitch, line: l.lineNo, endLine: l.lineNo,
		header: strings.TrimSpace(l.raw),
		cond:   extractShCond(lines, idx, "case", "in"),
	}
	// Body starts after `in`.
	i := nextAfterToken(lines, idx, "in")
	for i < len(lines) {
		ll := lines[i]
		t := strings.TrimSpace(ll.stripped)
		if t == "" {
			i++
			continue
		}
		if reShEsac.MatchString(t) {
			s.endLine = ll.lineNo
			return s, i + 1
		}
		if reShCasePat.MatchString(t) {
			// Strip optional leading `(` and the trailing `)`.
			label := strings.TrimSuffix(t, ")")
			label = strings.TrimPrefix(label, "(")
			label = strings.TrimSpace(label)
			cl := &caseClause{
				labels:    []string{label},
				isDefault: label == "*",
				line:      ll.lineNo,
				endLine:   ll.lineNo,
			}
			// Body runs until `;;` or end of pattern.
			body, after := parseShCaseBody(lines, i+1)
			cl.body = body
			cl.endLine = lastShLine(lines, after-1, ll.lineNo)
			s.cases = append(s.cases, cl)
			i = after
			continue
		}
		i++
	}
	s.endLine = lastShLine(lines, i-1, s.endLine)
	return s, i
}

// parseShCaseBody parses statements inside a single `case` arm. Returns
// when it hits `;;` (consumed) or `esac` (not consumed).
func parseShCaseBody(lines []shLine, idx int) ([]*stmt, int) {
	out, next := parseShStmts(lines, idx)
	// Consume `;;` if present.
	if next < len(lines) {
		t := strings.TrimSpace(lines[next].stripped)
		if t == ";;" || reShCaseEnd.MatchString(t) {
			return out, next + 1
		}
	}
	return out, next
}

// extractShCond joins the lines of `if cmds; then` / `while cmds; do` /
// `case expr in` into a single condition string (cmds / expr).
func extractShCond(lines []shLine, idx int, keyword, terminator string) string {
	var parts []string
	for j := idx; j < len(lines); j++ {
		s := strings.TrimSpace(lines[j].stripped)
		// Strip leading keyword on first line.
		if j == idx {
			s = strings.TrimSpace(strings.TrimPrefix(s, keyword))
		}
		// Find terminator (e.g. `then`, `do`, `in`).
		if k := wordIndex(s, terminator); k >= 0 {
			parts = append(parts, strings.TrimSpace(s[:k]))
			break
		}
		parts = append(parts, s)
	}
	cond := strings.Join(parts, " ")
	cond = strings.TrimRight(cond, "; ")
	return strings.TrimSpace(cond)
}

// nextAfterToken returns the line index immediately after the line that
// contains `token` as a whole word. Used to find where a `then`/`do`/`in`
// body begins.
func nextAfterToken(lines []shLine, idx int, token string) int {
	for j := idx; j < len(lines); j++ {
		if wordIndex(lines[j].stripped, token) >= 0 {
			return j + 1
		}
	}
	return idx + 1
}

// wordIndex returns the byte offset of `word` in `s` if it appears as a
// whole word (surrounded by non-word chars or string boundaries).
func wordIndex(s, word string) int {
	i := 0
	for {
		k := strings.Index(s[i:], word)
		if k < 0 {
			return -1
		}
		pos := i + k
		before := byte(' ')
		if pos > 0 {
			before = s[pos-1]
		}
		after := byte(' ')
		if pos+len(word) < len(s) {
			after = s[pos+len(word)]
		}
		if !isWordChar(before) && !isWordChar(after) {
			return pos
		}
		i = pos + 1
		if i >= len(s) {
			return -1
		}
	}
}

func isWordChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') || c == '_'
}

func lastShLine(lines []shLine, k, fallback int) int {
	if k < 0 || k >= len(lines) {
		return fallback
	}
	return lines[k].lineNo
}
