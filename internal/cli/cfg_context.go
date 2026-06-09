/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/cfg"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/trace"
)

// ProjectContext is the structured "surrounding code" view that the
// `saras cfg ... --with-context` family attaches to a CFG. It is
// intentionally shaped for two consumption modes:
//
//   - JSON: serialized verbatim under a `"context"` field on the CFG
//     output, so downstream tooling (other AI assistants, scripts,
//     dashboards) can pick out individual pieces without re-parsing.
//   - Text / paths: rendered via Text() into a markdown-formatted
//     appendix that human reviewers can scan inline.
//
// All fields are optional and may be empty when no useful information
// could be gathered (free-standing functions have no ParentDecl,
// markup files have no callee signatures, etc.). Callers should treat
// an empty struct as "context unavailable" rather than an error.
type ProjectContext struct {
	// Language carries the source language name (go, python, …) so
	// renderers can pick the right code fence in the markdown
	// appendix. Empty when the language couldn't be resolved.
	Language string `json:"language,omitempty"`
	// FileHeader is the literal source from line 1 up to the
	// function's start line, capped at MaxHeaderLines. Captures the
	// package declaration, import block, and any sibling
	// type/constant declarations near the top of the file.
	FileHeader string `json:"file_header,omitempty"`
	// ParentDecl is the source of the receiver / class / module type
	// that owns the function (e.g. the Go receiver, Python class,
	// Ruby module). Empty for free-standing functions.
	ParentDecl string `json:"parent_decl,omitempty"`
	// ReferencedTypes contains the source of every type / interface
	// referenced by the function body, the receiver, or any callee
	// signature — one hop deep. Sorted alphabetically for
	// determinism.
	ReferencedTypes []string `json:"referenced_types,omitempty"`
	// CalleeSignatures lists the signature lines (with `// file:line`
	// suffixes) of every function or method called by the target.
	// Bodies are intentionally omitted to keep the payload
	// lightweight; users who need the body can grep the file.
	CalleeSignatures []string `json:"callee_signatures,omitempty"`
}

// Empty reports whether the context contains nothing useful. When
// true, callers should suppress the entire context section rather
// than emit empty markdown headers.
func (p *ProjectContext) Empty() bool {
	return p == nil ||
		(p.FileHeader == "" &&
			p.ParentDecl == "" &&
			len(p.ReferencedTypes) == 0 &&
			len(p.CalleeSignatures) == 0)
}

// Text renders the context as a markdown appendix suitable for
// concatenating after a text/paths CFG render. Empty sections are
// omitted entirely so trivial functions get a small footprint.
func (p *ProjectContext) Text() string {
	if p.Empty() {
		return ""
	}
	lang := p.Language
	var b strings.Builder
	b.WriteString(strings.Repeat("=", 80))
	b.WriteString("\nProject context (--with-context)\n")
	b.WriteString(strings.Repeat("=", 80))
	b.WriteString("\n\n")
	if p.FileHeader != "" {
		fmt.Fprintf(&b, "File header (package, imports, top-of-file decls):\n```%s\n%s\n```\n\n",
			lang, p.FileHeader)
	}
	if p.ParentDecl != "" {
		fmt.Fprintf(&b, "Receiver / parent type definition:\n```%s\n%s\n```\n\n",
			lang, p.ParentDecl)
	}
	if len(p.ReferencedTypes) > 0 {
		fmt.Fprintf(&b, "Referenced types (one hop deep):\n```%s\n%s\n```\n\n",
			lang, strings.Join(p.ReferencedTypes, "\n\n"))
	}
	if len(p.CalleeSignatures) > 0 {
		fmt.Fprintf(&b, "Functions / methods called by the target (signatures only):\n```%s\n%s\n```\n",
			lang, strings.Join(p.CalleeSignatures, "\n"))
	}
	return b.String()
}

// projectContextOptions tunes the cost of gathering context. Defaults
// keep the appendix bounded for typical functions (≤ 50 header lines,
// ≤ 8 referenced types × 30 lines, ≤ 12 callee signatures).
type projectContextOptions struct {
	MaxHeaderLines     int
	MaxTypeDeclLines   int
	MaxReferencedTypes int
	MaxCallees         int
}

func defaultProjectContextOptions() projectContextOptions {
	return projectContextOptions{
		MaxHeaderLines:     50,
		MaxTypeDeclLines:   30,
		MaxReferencedTypes: 8,
		MaxCallees:         12,
	}
}

// gatherCFGContext walks the project's symbol table to assemble the
// surrounding-code view of `c`. It is intentionally best-effort: any
// single lookup that fails (config load, missing symbols, parse
// hiccup) is silently dropped rather than failing the whole command,
// so `--with-context` never blocks the primary CFG output.
//
// Returns a non-nil pointer; callers should use ProjectContext.Empty
// to test for usefulness.
//
// The function is named `gatherCFGContext` rather than the more
// natural `gatherProjectContext` to avoid colliding with the
// AGENTS.md generator, which has its own (different) helper of that
// name in `agentsmd.go`. Both live in package `cli`.
func gatherCFGContext(ctx context.Context, c *cfg.CFG) *ProjectContext {
	return gatherCFGContextWith(ctx, c, defaultProjectContextOptions())
}

// gatherCFGContextWith is gatherCFGContext with tunables; exposed for
// tests.
func gatherCFGContextWith(
	ctx context.Context,
	c *cfg.CFG,
	opts projectContextOptions,
) *ProjectContext {
	pc := &ProjectContext{Language: c.Language}

	projectRoot, err := config.FindProjectRoot()
	if err != nil {
		return pc
	}
	cfgRoot, err := config.Load(projectRoot)
	if err != nil {
		return pc
	}
	tracer := trace.NewTracer(projectRoot, cfgRoot.Ignore)

	// Body is the function source minus signature pre-amble; we use
	// it both for identifier extraction below and (transparently)
	// when readSymbolSource walks symbol bodies.
	body, _ := readFunctionSource(c)

	pc.FileHeader = readFileHeader(filepath.Join(projectRoot, c.File),
		c.StartLine, opts.MaxHeaderLines)

	pc.ParentDecl = lookupSymbolSource(ctx, tracer, projectRoot, c.Parent,
		[]trace.SymbolKind{trace.KindType, trace.KindInterface},
		opts.MaxTypeDeclLines)

	pc.CalleeSignatures = gatherCalleeSignatures(ctx, tracer, c, opts.MaxCallees)

	// Capitalised identifiers in the body alone miss types like
	// CodebaseMap whose only mention in a Go function is the
	// implicit return type of a callee. Including the parent decl
	// and callee signatures in the corpus catches those — one hop
	// deep, no further (recursing would explode prompt size on
	// real codebases).
	corpus := body + "\n" + pc.ParentDecl + "\n" + strings.Join(pc.CalleeSignatures, "\n")
	pc.ReferencedTypes = gatherReferencedTypes(ctx, tracer, projectRoot, corpus,
		c.Parent, opts.MaxReferencedTypes, opts.MaxTypeDeclLines)

	return pc
}

// readFileHeader returns the source content from line 1 up to (but
// not including) `untilLine`, capped at `maxLines`.
func readFileHeader(absPath string, untilLine, maxLines int) string {
	f, err := os.Open(absPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	var b strings.Builder
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<16), 1<<20)
	line := 0
	for scanner.Scan() {
		line++
		if untilLine > 0 && line >= untilLine {
			break
		}
		if line > maxLines {
			b.WriteString("// ... header truncated ...\n")
			break
		}
		b.WriteString(scanner.Text())
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// readSymbolSource reads the source for a symbol, capped at
// maxLines. Some lang parsers (Go especially) report types/structs
// with EndLine == StartLine — only the signature line is recorded.
// To still surface useful context we fall back to a brace-balanced
// read: starting at StartLine we accumulate lines and track `{` /
// `}` depth, stopping when we return to balance or hit maxLines.
// For non-brace languages the brace counter never increments and we
// fall through to the declared end line.
func readSymbolSource(projectRoot string, s trace.Symbol, maxLines int) string {
	if s.Line <= 0 {
		return s.Signature
	}
	abs := filepath.Join(projectRoot, s.FilePath)
	f, err := os.Open(abs)
	if err != nil {
		return s.Signature
	}
	defer f.Close()

	declaredEnd := s.EndLine
	useBraceBalance := declaredEnd <= s.Line
	hardEnd := s.Line + maxLines - 1
	if maxLines <= 0 {
		hardEnd = 1 << 30
	}

	var b strings.Builder
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<16), 1<<20)
	line := 0
	depth := 0
	openedOnce := false
	truncated := false
	for scanner.Scan() {
		line++
		if line < s.Line {
			continue
		}
		text := scanner.Text()
		b.WriteString(text)
		b.WriteString("\n")

		switch {
		case useBraceBalance:
			for _, r := range stripStringsAndComments(text) {
				switch r {
				case '{':
					depth++
					openedOnce = true
				case '}':
					if depth > 0 {
						depth--
					}
				}
			}
			if openedOnce && depth == 0 {
				return strings.TrimRight(b.String(), "\n")
			}
		default:
			if line >= declaredEnd {
				return strings.TrimRight(b.String(), "\n")
			}
		}
		if line >= hardEnd {
			truncated = true
			break
		}
	}
	if truncated {
		b.WriteString("// ... truncated ...\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// stripStringsAndComments removes the contents of double-quoted
// strings, single-quoted strings, back-tick raw strings, and `//`
// line comments from a single line so the brace-balance walker isn't
// confused by `{` / `}` inside string literals or commented-out code.
// It is intentionally simple; a full lexer would be overkill for
// header / type-definition extraction.
func stripStringsAndComments(line string) string {
	var b strings.Builder
	b.Grow(len(line))
	inStr := false
	inRaw := false
	var quote byte
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if !inStr && !inRaw {
			if ch == '/' && i+1 < len(line) && line[i+1] == '/' {
				break
			}
			switch ch {
			case '"', '\'':
				inStr = true
				quote = ch
				continue
			case '`':
				inRaw = true
				continue
			}
			b.WriteByte(ch)
			continue
		}
		if inStr {
			if ch == '\\' && i+1 < len(line) {
				i++
				continue
			}
			if ch == quote {
				inStr = false
			}
			continue
		}
		if ch == '`' {
			inRaw = false
		}
	}
	return b.String()
}

// lookupSymbolSource resolves a single symbol by name + kind set and
// returns its source. Returns "" when name is empty, the lookup fails,
// or no candidate matches. Multi-match cases pick the first
// alphabetically-sorted candidate (FindCandidates already sorts).
func lookupSymbolSource(
	ctx context.Context,
	tracer *trace.Tracer,
	projectRoot, name string,
	kinds []trace.SymbolKind,
	maxLines int,
) string {
	if name == "" {
		return ""
	}
	cands, err := tracer.FindCandidates(ctx, name, trace.SelectOptions{Kinds: kinds})
	if err != nil || len(cands) == 0 {
		return ""
	}
	return readSymbolSource(projectRoot, cands[0].Symbol, maxLines)
}

// gatherCalleeSignatures returns up to `cap` callee signature lines
// (formatted as "<signature>  // <file>:<line>") for the function
// described by c. Deduplicated by callee name.
func gatherCalleeSignatures(
	ctx context.Context,
	tracer *trace.Tracer,
	c *cfg.CFG,
	cap int,
) []string {
	edges, err := tracer.FindCallees(ctx, c.Function)
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{}, len(edges))
	var lines []string
	for _, e := range edges {
		if _, ok := seen[e.Callee]; ok {
			continue
		}
		seen[e.Callee] = struct{}{}
		cands, _ := tracer.FindCandidates(ctx, e.Callee, trace.SelectOptions{
			Kinds: []trace.SymbolKind{trace.KindFunction, trace.KindMethod},
		})
		for _, cd := range cands {
			sig := strings.TrimSpace(cd.Symbol.Signature)
			if sig == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("%s  // %s:%d",
				sig, cd.Symbol.FilePath, cd.Symbol.Line))
		}
		if cap > 0 && len(lines) >= cap {
			break
		}
	}
	sort.Strings(lines)
	if cap > 0 && len(lines) > cap {
		lines = lines[:cap]
	}
	return lines
}

// gatherReferencedTypes scans the corpus for capitalised identifiers,
// resolves each against the project's type/interface symbol table,
// and returns at most `cap` source-snippet decls (sorted
// alphabetically for determinism). The receiver/parent type is
// excluded since it's surfaced separately.
func gatherReferencedTypes(
	ctx context.Context,
	tracer *trace.Tracer,
	projectRoot, corpus, parent string,
	cap, maxLines int,
) []string {
	idents := extractCapIdentifiers(corpus)
	skip := map[string]struct{}{}
	if parent != "" {
		skip[parent] = struct{}{}
	}
	var decls []string
	for _, name := range idents {
		if _, ok := skip[name]; ok {
			continue
		}
		skip[name] = struct{}{}
		cands, _ := tracer.FindCandidates(ctx, name, trace.SelectOptions{
			Kinds: []trace.SymbolKind{trace.KindType, trace.KindInterface},
		})
		for _, cd := range cands {
			src := readSymbolSource(projectRoot, cd.Symbol, maxLines)
			if src == "" {
				continue
			}
			decls = append(decls, src)
		}
		if cap > 0 && len(decls) >= cap {
			break
		}
	}
	sort.Strings(decls)
	if cap > 0 && len(decls) > cap {
		decls = decls[:cap]
	}
	return decls
}

// reCapIdent matches identifiers that begin with an uppercase
// letter — Go-style exported types, Java/C# classes, Python class
// names, etc. Lowercase callees are picked up via the call graph in
// gatherCalleeSignatures.
var reCapIdent = regexp.MustCompile(`\b[A-Z][a-zA-Z0-9_]*\b`)

// extractCapIdentifiers returns the set of capitalised identifiers
// in the source, sorted alphabetically. Duplicates are removed.
// False positives like `TODO` are cheap because they simply return
// zero symbol matches and get dropped.
func extractCapIdentifiers(source string) []string {
	matches := reCapIdent.FindAllString(source, -1)
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}
