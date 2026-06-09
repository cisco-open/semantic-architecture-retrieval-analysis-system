/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

// Package cfg builds intra-procedural Control Flow Graphs (CFGs) from a
// function's source code and enumerates the execution paths through it.
//
// The primary use case is "water-tight test generation": every enumerated
// path corresponds to one branch/decision in the function, so unit tests can
// be written one-per-path to maximize branch coverage.
//
// The CFG builder is line/brace based and supports C-family languages
// (Go, JavaScript, TypeScript, Java, C, C++, C#, Rust, Kotlin, PHP, Scala,
// Dart). Python and other indentation-based languages are not yet supported
// by the heuristic builder; for those, callers can fall back to the LLM
// "explain"/"tests" subcommands which work language-agnostically.
package cfg

// BlockKind classifies a CFG basic block.
type BlockKind string

const (
	KindEntry    BlockKind = "entry"    // synthetic function entry
	KindExit     BlockKind = "exit"     // synthetic function exit
	KindLinear   BlockKind = "linear"   // straight-line statements
	KindBranch   BlockKind = "branch"   // `if`/`else if` condition
	KindLoopHead BlockKind = "loop"     // `for`/`while`/`do-while` head
	KindSwitch   BlockKind = "switch"   // `switch` discriminator
	KindReturn   BlockKind = "return"   // terminator (return/panic/throw/exit)
	KindBreak    BlockKind = "break"    // break statement
	KindContinue BlockKind = "continue" // continue statement
	KindMerge    BlockKind = "merge"    // synthetic join after branch
)

// Block is one node in the CFG.
type Block struct {
	ID    int       `json:"id"`
	Kind  BlockKind `json:"kind"`
	Label string    `json:"label"`           // short human-readable description
	Lines []int     `json:"lines,omitempty"` // 1-indexed source line numbers
	Code  []string  `json:"code,omitempty"`  // raw source text for Lines
	Cond  string    `json:"cond,omitempty"`  // condition expression (branch/loop/switch)
}

// Edge connects two blocks.
type Edge struct {
	From  int    `json:"from"`
	To    int    `json:"to"`
	Label string `json:"label,omitempty"` // e.g. "true", "false", "case X", "default", "loop"
	Back  bool   `json:"back,omitempty"`  // true for loop back-edges (excluded from path enumeration)
}

// CFG is the control flow graph for a single function.
type CFG struct {
	Function string `json:"function"`
	File     string `json:"file"`     // relative path
	Language string `json:"language"` // parser name (go, javascript, ...) or "" if unknown
	// Parent carries the receiver / class / module the function lives
	// on (Symbol.Parent in the trace package). Empty for free-standing
	// functions. Consumers of `--with-context` use it to surface the
	// receiver type definition alongside the CFG.
	Parent    string   `json:"parent,omitempty"`
	StartLine int      `json:"start_line"`
	EndLine   int      `json:"end_line"`
	Blocks    []*Block `json:"blocks"`
	Edges     []Edge   `json:"edges"`
	EntryID   int      `json:"entry_id"`
	ExitID    int      `json:"exit_id"`
	// Notes records any heuristic limitations (e.g. "do-while approximated as while").
	Notes []string `json:"notes,omitempty"`

	// cachedPaths / cachedMax store the result of the most recent
	// EnumeratePaths call so that format helpers (ToText, ToPathsText,
	// ToJSON) don't silently re-enumerate with DefaultMaxPaths and
	// discard a higher --max-paths the caller already requested.
	cachedPaths []Path `json:"-"`
	cachedMax   int    `json:"-"`
}

// Path is a single execution path from entry to exit, expressed as a sequence
// of block IDs and the labeled edges taken between them.
type Path struct {
	Blocks []int  `json:"blocks"`
	Edges  []Edge `json:"edges"`
	// Decisions summarizes the branch outcomes taken along this path, e.g.
	// ["if x > 0 = true", "switch op = case +", "return"]. Useful for naming
	// test cases.
	Decisions []string `json:"decisions"`
}

// blockByID is a small helper used internally by builders and formatters.
func (c *CFG) blockByID(id int) *Block {
	for _, b := range c.Blocks {
		if b.ID == id {
			return b
		}
	}
	return nil
}
