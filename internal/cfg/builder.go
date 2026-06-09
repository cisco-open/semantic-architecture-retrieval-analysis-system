/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cfg

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/lang"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/trace"
)

// ErrUnsupportedLanguage indicates that the heuristic builder cannot
// produce a CFG for this language because it lacks function-level control
// flow (markup, config, data) — examples: JSON, YAML, Markdown. Callers
// can fall back to the LLM-based `cfg explain` command which is
// language-agnostic.
var ErrUnsupportedLanguage = errors.New("language not supported by heuristic CFG builder")

// BuildFromFunction builds a CFG for the named function in `projectRoot`.
//
// When the project contains more than one function with this name (across
// files, languages, classes, or method receivers) the call returns an
// *AmbiguousFunctionError listing every candidate. Use BuildFromFunctionWith
// to pass disambiguators (file substring, language, parent type/class).
func BuildFromFunction(ctx context.Context, projectRoot string, funcName string, ignoreList []string) (*CFG, error) {
	return BuildFromFunctionWith(ctx, projectRoot, funcName, ignoreList, SelectOptions{})
}

// BuildFromFunctionWith is the disambiguating variant of BuildFromFunction.
// `opts` may filter candidates by file path substring, language, and/or
// parent (Go receiver type / Python class / etc.). When more than one
// candidate remains an *AmbiguousFunctionError is returned listing every
// match so the caller can guide the user to a unique filter.
func BuildFromFunctionWith(
	ctx context.Context,
	projectRoot string,
	funcName string,
	ignoreList []string,
	opts SelectOptions,
) (*CFG, error) {
	cands, err := FindFunctionSymbols(ctx, projectRoot, ignoreList, funcName, opts)
	if err != nil {
		return nil, err
	}
	pick, err := SelectOne(funcName, cands, opts)
	if err != nil {
		return nil, err
	}
	return BuildFromSymbol(projectRoot, pick.Symbol)
}

// BuildFromSymbol builds a CFG given an already-resolved function symbol.
// The parsing front-end is chosen by the language's registered Style;
// see internal/cfg/registry.go.
func BuildFromSymbol(projectRoot string, sym trace.Symbol) (*CFG, error) {
	if sym.EndLine <= 0 || sym.EndLine < sym.Line {
		return nil, fmt.Errorf("symbol %q has invalid line range %d-%d",
			sym.Name, sym.Line, sym.EndLine)
	}
	absPath := filepath.Join(projectRoot, sym.FilePath)

	// Determine language name and parsing style.
	langName := ""
	if p := lang.ParserForFile(absPath); p != nil {
		langName = p.Name()
	}
	style := StyleForLanguage(langName)
	if style == StyleUnsupported {
		ext := filepath.Ext(absPath)
		return nil, fmt.Errorf(
			"%w: %s (file %s) — try `saras cfg %s explain` for LLM-based analysis",
			ErrUnsupportedLanguage, langName, ext, sym.Name)
	}

	body, err := readLineRange(absPath, sym.Line, sym.EndLine)
	if err != nil {
		return nil, fmt.Errorf("read function body: %w", err)
	}

	c := &CFG{
		Function:  sym.Name,
		File:      sym.FilePath,
		Language:  langName,
		Parent:    sym.Parent,
		StartLine: sym.Line,
		EndLine:   sym.EndLine,
	}

	// Front-end: turn source text into the style-agnostic []*stmt tree.
	stmts, parseNotes, err := parseFunctionForStyle(style, body, sym.Line)
	if err != nil {
		return nil, fmt.Errorf("parse %s body: %w", langName, err)
	}
	for _, n := range parseNotes {
		c.Notes = appendNote(c.Notes, n)
	}

	// Back-end: walk the stmt tree to materialise blocks and edges.
	b := newBuilder(c)
	entry := b.addBlock(KindEntry, "entry: "+sym.Name, nil, "")
	exit := b.addBlock(KindExit, "exit", nil, "")
	c.EntryID = entry.ID
	c.ExitID = exit.ID

	// Build body. The "current" frontier is the entry block; after the body
	// any unterminated frontier flows into the exit block.
	frontier := b.build(stmts, []int{entry.ID}, builderCtx{
		breakTarget:    -1,
		continueTarget: -1,
		exitID:         exit.ID,
	})
	for _, fid := range frontier {
		b.addEdge(fid, exit.ID, b.resolveLabel(fid, ""), false)
	}

	return c, nil
}

// parseFunctionForStyle dispatches the function body source to the
// per-style front-end parser. Each front-end returns a uniform `[]*stmt`
// tree, plus optional notes describing approximations or limitations.
func parseFunctionForStyle(style Style, body string, startLine int) ([]*stmt, []string, error) {
	switch style {
	case StyleBrace:
		lines := preprocess(body, startLine)
		return parseFunctionBody(lines), nil, nil
	case StyleIndent:
		stmts, notes := parseIndentFunctionBody(body, startLine)
		return stmts, notes, nil
	case StyleEnd:
		stmts, notes := parseEndFunctionBody(body, startLine)
		return stmts, notes, nil
	case StyleShell:
		stmts, notes := parseShellFunctionBody(body, startLine)
		return stmts, notes, nil
	default:
		return nil, nil, fmt.Errorf("no parser for style %s", style)
	}
}

// ---------------------------------------------------------------------------
// Builder state
// ---------------------------------------------------------------------------

type builder struct {
	cfg     *CFG
	nextID  int
	pending []pendingLabel
}

// pendingLabel is a label deferred to the next outgoing edge from a block.
// Used when an `if` has no `else` — the "false" label belongs to the next
// edge created from the branch block.
type pendingLabel struct {
	blockID int
	label   string
}

func newBuilder(c *CFG) *builder {
	return &builder{cfg: c, nextID: 0}
}

func (b *builder) addBlock(kind BlockKind, label string, s *stmt, cond string) *Block {
	blk := &Block{
		ID:    b.nextID,
		Kind:  kind,
		Label: label,
		Cond:  cond,
	}
	if s != nil {
		if len(s.lines) > 0 {
			blk.Lines = append([]int(nil), s.lines...)
		}
		if len(s.code) > 0 {
			blk.Code = append([]string(nil), s.code...)
		}
		if len(blk.Lines) == 0 && s.line > 0 {
			blk.Lines = []int{s.line}
			if s.header != "" {
				blk.Code = []string{s.header}
			}
		}
	}
	b.nextID++
	b.cfg.Blocks = append(b.cfg.Blocks, blk)
	return blk
}

func (b *builder) addEdge(from, to int, label string, back bool) {
	b.cfg.Edges = append(b.cfg.Edges, Edge{From: from, To: to, Label: label, Back: back})
}

// builderCtx carries the targets that `break` / `continue` resolve to.
type builderCtx struct {
	breakTarget    int // -1 if not in a loop/switch
	continueTarget int // -1 if not in a loop
	exitID         int
}

// build threads `frontier` through the statement list and returns the new
// frontier — the set of block IDs that have no successor yet and should be
// connected to whatever follows this list.
//
// If the list ends with a terminator (return/throw/break/continue), the
// returned frontier is empty.
func (b *builder) build(stmts []*stmt, frontier []int, ctx builderCtx) []int {
	for _, s := range stmts {
		frontier = b.buildOne(s, frontier, ctx)
	}
	return frontier
}

func (b *builder) buildOne(s *stmt, frontier []int, ctx builderCtx) []int {
	switch s.kind {
	case skLinear:
		blk := b.addBlock(KindLinear, summarizeLines(s.code), s, "")
		b.connectAll(frontier, blk.ID, "")
		return []int{blk.ID}

	case skIf:
		return b.buildIf(s, frontier, ctx)

	case skLoop:
		return b.buildLoop(s, frontier, ctx)

	case skSwitch:
		return b.buildSwitch(s, frontier, ctx)

	case skReturn:
		blk := b.addBlock(KindReturn, summarizeOne(s.header), s, "")
		b.connectAll(frontier, blk.ID, "")
		b.addEdge(blk.ID, ctx.exitID, "", false)
		return nil

	case skBreak:
		blk := b.addBlock(KindBreak, summarizeOne(s.header), s, "")
		b.connectAll(frontier, blk.ID, "")
		if ctx.breakTarget >= 0 {
			b.addEdge(blk.ID, ctx.breakTarget, "", false)
		} else {
			b.cfg.Notes = appendNote(b.cfg.Notes, "stray `break` outside loop/switch")
			b.addEdge(blk.ID, ctx.exitID, "", false)
		}
		return nil

	case skContinue:
		blk := b.addBlock(KindContinue, summarizeOne(s.header), s, "")
		b.connectAll(frontier, blk.ID, "")
		if ctx.continueTarget >= 0 {
			b.addEdge(blk.ID, ctx.continueTarget, "", true)
		} else {
			b.cfg.Notes = appendNote(b.cfg.Notes, "stray `continue` outside loop")
			b.addEdge(blk.ID, ctx.exitID, "", false)
		}
		return nil

	case skBlock:
		return b.build(s.body, frontier, ctx)
	}
	return frontier
}

// ---------------------------------------------------------------------------
// If / Else
// ---------------------------------------------------------------------------

func (b *builder) buildIf(s *stmt, frontier []int, ctx builderCtx) []int {
	cond := s.cond
	if cond == "" {
		cond = "if"
	}
	branch := b.addBlock(KindBranch, "if "+truncate(cond, 50), nil, cond)
	branch.Lines = []int{s.line}
	branch.Code = []string{strings.TrimSpace(s.header)}
	b.connectAll(frontier, branch.ID, "")

	// True branch.
	trueFrontier := b.build(s.then, []int{branch.ID}, ctx)
	b.labelFirstEdgeFrom(branch.ID, "true")

	// False / else branch.
	var falseFrontier []int
	if len(s.els) > 0 {
		falseFrontier = b.build(s.els, []int{branch.ID}, ctx)
		b.labelNthEdgeFrom(branch.ID, 1, "false")
	} else {
		// No else — the "false" outcome flows through to whatever comes next.
		// Keep the branch in the frontier and queue a "false" label for the
		// next edge created from it.
		falseFrontier = []int{branch.ID}
		b.pendingLabelFrom(branch.ID, "false")
	}

	out := append([]int{}, trueFrontier...)
	out = append(out, falseFrontier...)
	return out
}

// ---------------------------------------------------------------------------
// Loop (for / while / do-while)
// ---------------------------------------------------------------------------

func (b *builder) buildLoop(s *stmt, frontier []int, ctx builderCtx) []int {
	cond := s.cond
	if cond == "" {
		cond = "loop"
	}
	head := b.addBlock(KindLoopHead, "loop "+truncate(cond, 50), nil, cond)
	head.Lines = []int{s.line}
	head.Code = []string{strings.TrimSpace(s.header)}

	// Create the loop-exit merge block up front so `break` statements have a
	// concrete target.
	exitMerge := b.addBlock(KindMerge, "loop exit", nil, "")

	innerCtx := builderCtx{
		breakTarget:    exitMerge.ID,
		continueTarget: head.ID,
		exitID:         ctx.exitID,
	}

	if s.isDoLoop {
		// do-while: body executes first, then head (the while-condition).
		bodyFrontier := b.build(s.body, frontier, innerCtx)
		// After body, flow into head.
		for _, f := range bodyFrontier {
			b.addEdge(f, head.ID, b.resolveLabel(f, ""), false)
		}
		// head -true→ first body block (back-edge), head -false→ exitMerge.
		firstBody := b.firstBodyOf(s.body)
		if firstBody >= 0 {
			b.addEdge(head.ID, firstBody, "true (continue)", true)
		}
		b.addEdge(head.ID, exitMerge.ID, "false", false)
		return []int{exitMerge.ID}
	}

	// Regular loop: head checks condition first.
	b.connectAll(frontier, head.ID, "")
	bodyFrontier := b.build(s.body, []int{head.ID}, innerCtx)
	b.labelFirstEdgeFrom(head.ID, "true")
	// Body falls back to head (back-edge).
	for _, f := range bodyFrontier {
		if f == head.ID {
			continue
		}
		b.addEdge(f, head.ID, b.resolveLabel(f, ""), true)
	}
	// Loop falsifies → exit merge.
	b.addEdge(head.ID, exitMerge.ID, "false", false)
	return []int{exitMerge.ID}
}

// firstBodyOf returns the ID of the first block created when the given body
// statements were materialized. Heuristic: the lowest block ID greater than
// `headID` that is a member of `bodyFrontier`'s upstream is our first body
// block. We approximate by scanning the most recent blocks for the first
// non-Merge/non-LoopHead block whose ID is greater than our head's ID.
func (b *builder) firstBodyOf(body []*stmt) int {
	if len(body) == 0 {
		return -1
	}
	firstLine := body[0].line
	// Find the most recently added block whose first source line matches.
	for i := len(b.cfg.Blocks) - 1; i >= 0; i-- {
		blk := b.cfg.Blocks[i]
		if len(blk.Lines) > 0 && blk.Lines[0] == firstLine {
			return blk.ID
		}
	}
	// Fallback: just pick the most recently added linear/branch block.
	for i := len(b.cfg.Blocks) - 1; i >= 0; i-- {
		blk := b.cfg.Blocks[i]
		switch blk.Kind {
		case KindLinear, KindBranch, KindLoopHead, KindSwitch:
			return blk.ID
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// Switch
// ---------------------------------------------------------------------------

func (b *builder) buildSwitch(s *stmt, frontier []int, ctx builderCtx) []int {
	cond := s.cond
	if cond == "" {
		cond = "switch"
	}
	disc := b.addBlock(KindSwitch, "switch "+truncate(cond, 50), nil, cond)
	disc.Lines = []int{s.line}
	disc.Code = []string{strings.TrimSpace(s.header)}
	b.connectAll(frontier, disc.ID, "")

	exitMerge := b.addBlock(KindMerge, "switch exit", nil, "")
	hasDefault := false

	innerCtx := builderCtx{
		breakTarget:    exitMerge.ID,
		continueTarget: ctx.continueTarget,
		exitID:         ctx.exitID,
	}

	var prevFallthrough []int
	for _, cl := range s.cases {
		labelText := strings.Join(cl.labels, ", ")
		caseEntry := b.addBlock(KindLinear, labelText, nil, "")
		caseEntry.Lines = []int{cl.line}
		caseEntry.Code = []string{labelText}
		edgeLabel := labelText
		if cl.isDefault {
			edgeLabel = "default"
			hasDefault = true
		}
		b.addEdge(disc.ID, caseEntry.ID, edgeLabel, false)

		// Connect previous case's fallthrough to this case entry.
		for _, f := range prevFallthrough {
			b.addEdge(f, caseEntry.ID, "fallthrough", false)
		}

		caseFrontier := b.build(cl.body, []int{caseEntry.ID}, innerCtx)
		// If caseFrontier is non-empty, the case did NOT terminate — it
		// falls through (C-style) or implicitly breaks (Go/C#-style). We
		// can't tell from the heuristic parser, so we record both
		// possibilities by treating it as fallthrough AND a path to exit.
		// To keep the CFG simple and useful for test design, we treat
		// non-terminated cases as falling into the exitMerge (the common
		// behavior in Go, C#, JavaScript implicit break, Rust match).
		for _, f := range caseFrontier {
			b.addEdge(f, exitMerge.ID, b.resolveLabel(f, ""), false)
		}
		prevFallthrough = nil
	}

	// If there's no `default` clause, the discriminator can flow directly to
	// the merge (no case matched).
	if !hasDefault {
		b.addEdge(disc.ID, exitMerge.ID, "no match", false)
	}

	return []int{exitMerge.ID}
}

// ---------------------------------------------------------------------------
// Edge helpers
// ---------------------------------------------------------------------------

func (b *builder) connectAll(frontier []int, to int, label string) {
	for _, f := range frontier {
		b.addEdge(f, to, b.resolveLabel(f, label), false)
	}
}

func (b *builder) pendingLabelFrom(blockID int, label string) {
	b.pending = append(b.pending, pendingLabel{blockID: blockID, label: label})
}

func (b *builder) resolveLabel(from int, defaultLabel string) string {
	for i, pl := range b.pending {
		if pl.blockID == from {
			b.pending = append(b.pending[:i], b.pending[i+1:]...)
			return pl.label
		}
	}
	return defaultLabel
}

func (b *builder) labelFirstEdgeFrom(from int, label string) {
	for i := range b.cfg.Edges {
		if b.cfg.Edges[i].From == from && b.cfg.Edges[i].Label == "" {
			b.cfg.Edges[i].Label = label
			return
		}
	}
}

func (b *builder) labelNthEdgeFrom(from, n int, label string) {
	count := 0
	for i := range b.cfg.Edges {
		if b.cfg.Edges[i].From == from {
			if count == n {
				b.cfg.Edges[i].Label = label
				return
			}
			count++
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func summarizeLines(lines []string) string {
	if len(lines) == 0 {
		return "block"
	}
	first := strings.TrimSpace(lines[0])
	if len(lines) == 1 {
		return truncate(first, 60)
	}
	return fmt.Sprintf("%s … (+%d lines)", truncate(first, 40), len(lines)-1)
}

func summarizeOne(s string) string {
	return truncate(strings.TrimSpace(s), 60)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	if n < 1 {
		return s
	}
	return s[:n-1] + "…"
}

func appendNote(notes []string, note string) []string {
	for _, n := range notes {
		if n == note {
			return notes
		}
	}
	return append(notes, note)
}

// readLineRange reads lines [start, end] (1-indexed, inclusive) from path.
func readLineRange(path string, start, end int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	var buf strings.Builder
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		if lineNo < start {
			continue
		}
		if lineNo > end {
			break
		}
		buf.WriteString(scanner.Text())
		buf.WriteByte('\n')
	}
	return buf.String(), scanner.Err()
}
