/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cfg

import "fmt"

// DefaultMaxPaths caps path enumeration to avoid combinatorial explosion on
// complex functions. Functions with >50 paths are typically refactor
// candidates; we surface a warning rather than enumerate them all.
const DefaultMaxPaths = 64

// EnumeratePaths returns every simple acyclic path from the entry block to
// the exit block. Loop back-edges (Edge.Back == true) are excluded so the
// enumeration is finite. Each path represents one unique combination of
// branch outcomes — i.e. one test case for water-tight coverage.
//
// If `max` is <= 0, DefaultMaxPaths is used. When the limit is reached, the
// CFG's Notes is appended with a truncation warning and the partial result is
// returned.
func (c *CFG) EnumeratePaths(max int) []Path {
	if max <= 0 {
		max = DefaultMaxPaths
	}
	// Index edges by source for fast forward traversal, skipping back-edges.
	type successor struct {
		edge Edge
	}
	adj := make(map[int][]successor)
	for _, e := range c.Edges {
		if e.Back {
			continue
		}
		adj[e.From] = append(adj[e.From], successor{edge: e})
	}

	var paths []Path
	var visit func(node int, pathBlocks []int, pathEdges []Edge, visited map[int]bool)
	truncated := false
	visit = func(node int, pathBlocks []int, pathEdges []Edge, visited map[int]bool) {
		if len(paths) >= max {
			truncated = true
			return
		}
		pathBlocks = append(pathBlocks, node)
		if node == c.ExitID {
			final := make([]int, len(pathBlocks))
			copy(final, pathBlocks)
			fe := make([]Edge, len(pathEdges))
			copy(fe, pathEdges)
			paths = append(paths, Path{
				Blocks:    final,
				Edges:     fe,
				Decisions: c.decisionsFor(final, fe),
			})
			return
		}
		visited[node] = true
		defer func() { visited[node] = false }()
		for _, succ := range adj[node] {
			if visited[succ.edge.To] {
				// Skip cycles created by non-back edges (rare but possible
				// with goto / fallthrough).
				continue
			}
			visit(succ.edge.To, pathBlocks, append(pathEdges, succ.edge), visited)
			if len(paths) >= max {
				truncated = true
				return
			}
		}
	}

	visit(c.EntryID, nil, nil, map[int]bool{})

	if truncated {
		c.Notes = appendNote(c.Notes, fmt.Sprintf(
			"path enumeration truncated at %d paths; function may benefit from refactoring",
			max))
	}
	return paths
}

// decisionsFor produces a human-readable summary of the branch outcomes
// taken along a path. Used for test-case naming and for the LLM prompt.
func (c *CFG) decisionsFor(blocks []int, edges []Edge) []string {
	var out []string
	for i, e := range edges {
		fromBlk := c.blockByID(e.From)
		if fromBlk == nil {
			continue
		}
		switch fromBlk.Kind {
		case KindBranch:
			if e.Label != "" && fromBlk.Cond != "" {
				out = append(out, fmt.Sprintf("if %s = %s", fromBlk.Cond, e.Label))
			}
		case KindLoopHead:
			if e.Label == "false" && fromBlk.Cond != "" {
				out = append(out, fmt.Sprintf("loop %s: 0 iterations (skip)", fromBlk.Cond))
			} else if e.Label == "true" && fromBlk.Cond != "" {
				out = append(out, fmt.Sprintf("loop %s: at least 1 iteration", fromBlk.Cond))
			}
		case KindSwitch:
			if e.Label != "" && fromBlk.Cond != "" {
				out = append(out, fmt.Sprintf("switch %s = %s", fromBlk.Cond, e.Label))
			}
		}
		_ = i
	}
	// If the last block before exit is a return/throw, annotate with that.
	if len(blocks) >= 2 {
		last := c.blockByID(blocks[len(blocks)-2])
		if last != nil && last.Kind == KindReturn && last.Label != "" {
			out = append(out, last.Label)
		}
	}
	return out
}
