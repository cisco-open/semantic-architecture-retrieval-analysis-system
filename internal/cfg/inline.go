/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cfg

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/trace"
)

// InlineOptions tunes callee inlining for `BuildFromFunctionInlined`.
//
// Inlining is intentionally **post-processing**: we first build the
// caller's CFG using the per-language heuristic parser, then walk
// the saras call graph (via trace.Tracer.FindCallees) to find every
// block that contains a call to a known project-internal function,
// and splice that callee's CFG into the host so its branches show
// up on every enumerated path. This lets us reuse all four language
// strategies (brace / indent / end / shell) unchanged.
//
// The feature is bounded:
//
//   - MaxDepth caps recursion so a deeply nested call graph doesn't
//     blow up CFG size and Mermaid render time. The default
//     (DefaultMaxInlineDepth) is conservative.
//
//   - Functions reached via mutual or self recursion are detected
//     via an in-progress set and left as call-site blocks with a
//     "recursive call to X not inlined" note, so paths terminate
//     instead of looping forever.
//
//   - Callees that don't resolve to a unique project symbol —
//     standard library functions, third-party calls, ambiguous
//     overloads, indirect calls via function variables — are
//     left as plain call-site blocks. They get a "(external)" or
//     "(ambiguous)" note where applicable.
type InlineOptions struct {
	// Enabled toggles the entire inlining pass. When false (the
	// default) BuildFromFunctionInlined is functionally equivalent
	// to BuildFromFunctionWith.
	Enabled bool
	// MaxDepth caps recursion. 0 means use DefaultMaxInlineDepth.
	// Negative values are treated as 0.
	MaxDepth int
}

// DefaultMaxInlineDepth is the conservative default depth used when
// `InlineOptions.MaxDepth == 0`. Empirically depth 2 is enough for
// "open up the helper called by my function" and small enough that
// the resulting Mermaid renders cleanly even on deeply layered
// codebases. Users who want to drill deeper can pass --max-inline-depth.
const DefaultMaxInlineDepth = 2

// BuildFromFunctionInlined is the inline-aware variant of
// BuildFromFunctionWith. When `inline.Enabled` is false the result
// is byte-identical to BuildFromFunctionWith — callers can wire the
// new function unconditionally without affecting existing behaviour.
//
// On error during the inline pass we return the un-inlined CFG with
// a Notes entry rather than failing the whole build, because a
// partial CFG is still useful (the user can retry with a different
// flag, etc.).
func BuildFromFunctionInlined(
	ctx context.Context,
	projectRoot string,
	funcName string,
	ignoreList []string,
	sel SelectOptions,
	inline InlineOptions,
) (*CFG, error) {
	c, err := BuildFromFunctionWith(ctx, projectRoot, funcName, ignoreList, sel)
	if err != nil {
		return nil, err
	}
	if !inline.Enabled {
		return c, nil
	}
	maxDepth := inline.MaxDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxInlineDepth
	}

	tracer := trace.NewTracer(projectRoot, ignoreList)
	inProgress := map[string]bool{c.Function: true}
	inlineExpand(ctx, tracer, projectRoot, ignoreList, c, maxDepth, inProgress)
	return c, nil
}

// inlineExpand walks every callee of c.Function (via the saras call
// graph), identifies call sites in c, builds each callee's CFG, and
// splices it in. Recurses into each spliced sub-CFG with depth - 1
// so chains of helpers inline correctly within the depth budget.
//
// Returns nothing — all mutations happen on `c` in place. Best-effort:
// errors building a single callee's CFG add a Note and skip that
// callee rather than aborting the inlining of the rest.
func inlineExpand(
	ctx context.Context,
	tracer *trace.Tracer,
	projectRoot string,
	ignoreList []string,
	c *CFG,
	depth int,
	inProgress map[string]bool,
) {
	if depth <= 0 {
		return
	}

	edges, err := tracer.FindCallees(ctx, c.Function)
	if err != nil {
		// FindCallees errors when the function isn't in the symbol
		// table — that's fine for inlining (nothing to expand).
		return
	}

	// Stable sort + dedupe so the inline pass is deterministic
	// regardless of how the trace package orders raw call edges.
	calleeNames := uniqueCalleeNames(edges)

	for _, callee := range calleeNames {
		if inProgress[callee] {
			c.Notes = appendUniqueNote(c.Notes,
				fmt.Sprintf("recursive call to %s not inlined", callee))
			continue
		}

		// Resolve the callee through the saras symbol graph.
		// Constraint: must be a unique function/method match.
		// Ambiguous matches are left as call sites with a note,
		// because picking the "wrong" one would silently lie about
		// which branches the caller can take.
		cands, err := FindFunctionSymbols(ctx, projectRoot, ignoreList, callee, SelectOptions{})
		if err != nil || len(cands) == 0 {
			// External / unknown — skip silently. A note here
			// would fire on every print/Println/strconv.Itoa,
			// which is more noise than signal.
			continue
		}
		if len(cands) > 1 {
			c.Notes = appendUniqueNote(c.Notes,
				fmt.Sprintf("call to %s not inlined: %d candidates (use --inline-callees on the resolved function directly)",
					callee, len(cands)))
			continue
		}

		// Build the callee's CFG. This also resolves its language
		// style — markup/config callees won't have one and end up
		// here as ErrUnsupportedLanguage; treat that as a soft skip.
		sub, err := BuildFromSymbol(projectRoot, cands[0].Symbol)
		if err != nil {
			continue
		}

		// Recurse before splicing so the spliced sub-graph already
		// contains its own inlined helpers. Doing it after splice
		// would require re-finding the now-renumbered blocks.
		inProgress[callee] = true
		inlineExpand(ctx, tracer, projectRoot, ignoreList, sub, depth-1, inProgress)
		delete(inProgress, callee)

		// Splice every call-site in the host. We can't dedupe
		// across call sites — each one is a distinct location in
		// the host's control flow and may take different paths
		// through the callee.
		sites := findCallSites(c, callee)
		for _, siteID := range sites {
			spliceCallee(c, siteID, sub, callee)
		}

		if depth-1 == 0 && len(sites) > 0 {
			c.Notes = appendUniqueNote(c.Notes,
				"max inline depth reached: deeper callees not expanded")
		}
	}
}

// uniqueCalleeNames returns the alphabetically-sorted unique callee
// names from a slice of CallEdge. Sorting makes the inline pass
// deterministic so two runs over the same project produce identical
// CFGs (important for diffing JSON output).
func uniqueCalleeNames(edges []trace.CallEdge) []string {
	seen := make(map[string]struct{}, len(edges))
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		if _, ok := seen[e.Callee]; ok {
			continue
		}
		seen[e.Callee] = struct{}{}
		out = append(out, e.Callee)
	}
	sort.Strings(out)
	return out
}

// callSiteRegex returns a regexp that matches a literal call to
// funcName — i.e. the identifier followed (possibly after whitespace)
// by an opening parenthesis. We anchor on a word boundary so
// `Run`-the-callee doesn't match `Rerun(`.
//
// The match is intentionally line-oriented and language-agnostic.
// It will produce false positives in pathological cases (`Foo` in a
// comment that happens to be `Foo(`), but those are harmless
// because the resolved callee CFG is still semantically correct;
// the result is just an over-zealous inline.
func callSiteRegex(funcName string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(funcName) + `\s*\(`)
}

// findCallSites returns the IDs of every block in `c` that mentions
// a call to `funcName`. We search both the block's Code (the literal
// source lines for the block) and Label (which captures linear
// stmt summaries that may not have full source attached).
//
// Entry/exit/merge synthetic blocks are skipped — they never carry
// real source.
func findCallSites(c *CFG, funcName string) []int {
	if funcName == "" {
		return nil
	}
	re := callSiteRegex(funcName)
	var out []int
	for _, b := range c.Blocks {
		if b.Kind == KindEntry || b.Kind == KindExit || b.Kind == KindMerge {
			continue
		}
		hit := false
		for _, line := range b.Code {
			if re.MatchString(line) {
				hit = true
				break
			}
		}
		if !hit && re.MatchString(b.Label) {
			hit = true
		}
		if hit {
			out = append(out, b.ID)
		}
	}
	return out
}

// spliceCallee rewires the host CFG so the call-site block flows
// through the callee's CFG before continuing.
//
// Concretely, given:
//
//	... → siteID → succA
//	            → succB
//
// after splicing the callee (entry e, exit x) we have:
//
//	... → siteID --call→ e → ... → x → succA
//	                                  → succB
//
// The siteID block is preserved as the marker of the call site so
// users can still see "this is where outer() called helper()".
// Edges that previously left siteID are detached and re-attached to
// the callee's exit, preserving their original labels (true/false,
// case/default, loop) so existing path enumeration semantics still
// hold.
func spliceCallee(host *CFG, siteID int, callee *CFG, calleeName string) {
	if callee == nil || len(callee.Blocks) == 0 {
		return
	}

	offset := nextBlockID(host)
	idMap := make(map[int]int, len(callee.Blocks))

	for _, b := range callee.Blocks {
		// Copy by value, then take the address — keeps host and
		// callee CFGs independent so further inlining of the same
		// callee elsewhere in the host doesn't share mutable state.
		nb := *b
		nb.ID = b.ID + offset
		nb.Label = "[" + calleeName + "] " + b.Label
		host.Blocks = append(host.Blocks, &nb)
		idMap[b.ID] = nb.ID
	}

	for _, e := range callee.Edges {
		host.Edges = append(host.Edges, Edge{
			From:  idMap[e.From],
			To:    idMap[e.To],
			Label: e.Label,
			Back:  e.Back,
		})
	}

	subEntry := idMap[callee.EntryID]
	subExit := idMap[callee.ExitID]

	// Detach siteID's outgoing edges; we'll rebuild them off the
	// callee's exit. Two-pass over host.Edges avoids slice-copy
	// surprises while we rewrite in place.
	var detached []Edge
	kept := host.Edges[:0]
	for _, e := range host.Edges {
		if e.From == siteID && !e.Back {
			detached = append(detached, e)
			continue
		}
		kept = append(kept, e)
	}
	host.Edges = kept

	// site → callee entry. Labelled "call <name>" so Mermaid /
	// text / json all communicate the boundary clearly.
	host.Edges = append(host.Edges, Edge{
		From:  siteID,
		To:    subEntry,
		Label: "call " + calleeName,
	})

	// callee exit → each original successor, preserving the
	// original edge's label/back-edge state.
	for _, e := range detached {
		host.Edges = append(host.Edges, Edge{
			From:  subExit,
			To:    e.To,
			Label: e.Label,
			Back:  e.Back,
		})
	}

	// Annotate the call-site block so renderers can show "calls X"
	// even for users skimming text/JSON output. We *augment* the
	// label rather than replace it because the original label often
	// carries the surrounding statement context (assignment LHS,
	// branch condition, etc.).
	if siteBlk := host.blockByID(siteID); siteBlk != nil {
		if !strings.Contains(siteBlk.Label, "calls "+calleeName) {
			siteBlk.Label = strings.TrimSpace(siteBlk.Label) +
				"  // calls " + calleeName
		}
	}

	// Propagate inlined notes upward so the top-level CFG surfaces
	// every limitation hit during the inline pass — most importantly
	// mutual-recursion bail-outs that get attached to the inlined
	// sub's Notes (not the host's) when inlineExpand recurses.
	for _, n := range callee.Notes {
		host.Notes = appendUniqueNote(host.Notes, "["+calleeName+"] "+n)
	}
}

// nextBlockID returns one past the largest block ID in host. Using
// max+1 (rather than len(host.Blocks)) survives any future edits
// that delete or reorder blocks.
func nextBlockID(host *CFG) int {
	maxID := -1
	for _, b := range host.Blocks {
		if b.ID > maxID {
			maxID = b.ID
		}
	}
	return maxID + 1
}

// appendUniqueNote adds note to notes if it isn't already present.
// Cuts down on duplicate "recursive call to X not inlined" lines
// when the same callee is referenced from multiple call sites.
func appendUniqueNote(notes []string, note string) []string {
	for _, n := range notes {
		if n == note {
			return notes
		}
	}
	return append(notes, note)
}
