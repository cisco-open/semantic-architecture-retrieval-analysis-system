/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cfg

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/trace"

	// Importing lang registers all bundled language parsers via init() —
	// the Go parser among them is what we need for these tests.
	_ "github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/lang"
)

// writeTempGo writes content to a fresh temp file named test.go and returns
// its directory and base path.
func writeTempGo(t *testing.T, content string) (dir string, filename string) {
	t.Helper()
	dir = t.TempDir()
	filename = "test.go"
	full := filepath.Join(dir, filename)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return dir, filename
}

func TestBuildFromFunction_Linear(t *testing.T) {
	src := `package p

func add(a int, b int) int {
	c := a + b
	return c
}
`
	dir, _ := writeTempGo(t, src)
	c, err := BuildFromFunction(context.Background(), dir, "add", nil)
	if err != nil {
		t.Fatalf("BuildFromFunction: %v", err)
	}
	if c.Function != "add" {
		t.Errorf("function = %q, want add", c.Function)
	}
	// Expected blocks: entry, exit, one linear (c := a + b), one return.
	wantKinds := []BlockKind{KindEntry, KindExit, KindLinear, KindReturn}
	if diff := blockKindDiff(c.Blocks, wantKinds); diff != "" {
		t.Errorf("block kinds: %s\n%s", diff, c.ToText())
	}
	paths := c.EnumeratePaths(0)
	if len(paths) != 1 {
		t.Errorf("paths = %d, want 1\n%s", len(paths), c.ToText())
	}
}

func TestBuildFromFunction_IfElse(t *testing.T) {
	src := `package p

func sign(x int) string {
	if x > 0 {
		return "pos"
	} else if x < 0 {
		return "neg"
	} else {
		return "zero"
	}
}
`
	dir, _ := writeTempGo(t, src)
	c, err := BuildFromFunction(context.Background(), dir, "sign", nil)
	if err != nil {
		t.Fatalf("BuildFromFunction: %v", err)
	}
	paths := c.EnumeratePaths(0)
	if len(paths) != 3 {
		t.Errorf("paths = %d, want 3 (one per branch)\n%s", len(paths), c.ToText())
	}
	// Each path should have at least one Decision.
	for i, p := range paths {
		if len(p.Decisions) == 0 {
			t.Errorf("path %d has no decisions:\n%s", i+1, c.ToText())
		}
	}
}

func TestBuildFromFunction_IfNoElse(t *testing.T) {
	src := `package p

func clamp(x int) int {
	if x < 0 {
		x = 0
	}
	return x
}
`
	dir, _ := writeTempGo(t, src)
	c, err := BuildFromFunction(context.Background(), dir, "clamp", nil)
	if err != nil {
		t.Fatalf("BuildFromFunction: %v", err)
	}
	paths := c.EnumeratePaths(0)
	if len(paths) != 2 {
		t.Errorf("paths = %d, want 2 (true/false of the if)\n%s",
			len(paths), c.ToText())
	}
	// Verify "true" and "false" labels show up on edges from the branch.
	var hasTrue, hasFalse bool
	for _, e := range c.Edges {
		blk := c.blockByID(e.From)
		if blk == nil || blk.Kind != KindBranch {
			continue
		}
		if e.Label == "true" {
			hasTrue = true
		}
		if e.Label == "false" {
			hasFalse = true
		}
	}
	if !hasTrue || !hasFalse {
		t.Errorf("branch edge labels: hasTrue=%v hasFalse=%v\n%s",
			hasTrue, hasFalse, c.ToText())
	}
}

func TestBuildFromFunction_ForLoop(t *testing.T) {
	src := `package p

func sumTo(n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += i
	}
	return s
}
`
	dir, _ := writeTempGo(t, src)
	c, err := BuildFromFunction(context.Background(), dir, "sumTo", nil)
	if err != nil {
		t.Fatalf("BuildFromFunction: %v", err)
	}
	// At minimum we should have a KindLoopHead block and a back-edge.
	var hasLoop, hasBack bool
	for _, blk := range c.Blocks {
		if blk.Kind == KindLoopHead {
			hasLoop = true
		}
	}
	for _, e := range c.Edges {
		if e.Back {
			hasBack = true
		}
	}
	if !hasLoop {
		t.Errorf("no loop-head block found\n%s", c.ToText())
	}
	if !hasBack {
		t.Errorf("no back-edge found\n%s", c.ToText())
	}
	// Paths: loop-skipped (0 iterations) and loop-entered (>=1 iteration).
	paths := c.EnumeratePaths(0)
	if len(paths) < 1 {
		t.Errorf("paths = %d, want >=1\n%s", len(paths), c.ToText())
	}
}

func TestBuildFromFunction_EarlyReturn(t *testing.T) {
	src := `package p

func find(items []int, target int) int {
	if items == nil {
		return -1
	}
	for i, v := range items {
		if v == target {
			return i
		}
	}
	return -1
}
`
	dir, _ := writeTempGo(t, src)
	c, err := BuildFromFunction(context.Background(), dir, "find", nil)
	if err != nil {
		t.Fatalf("BuildFromFunction: %v", err)
	}
	paths := c.EnumeratePaths(0)
	if len(paths) < 3 {
		t.Errorf("paths = %d, want >=3 (nil-input, found, not-found)\n%s",
			len(paths), c.ToText())
	}
}

func TestBuildFromFunction_Mermaid(t *testing.T) {
	src := `package p

func twoBranch(x int) int {
	if x > 0 {
		return 1
	}
	return -1
}
`
	dir, _ := writeTempGo(t, src)
	c, err := BuildFromFunction(context.Background(), dir, "twoBranch", nil)
	if err != nil {
		t.Fatalf("BuildFromFunction: %v", err)
	}
	out := c.ToMermaid()
	if !strings.HasPrefix(out, "flowchart TD") {
		t.Errorf("mermaid output missing header:\n%s", out)
	}
	if !strings.Contains(out, "true") || !strings.Contains(out, "false") {
		t.Errorf("mermaid output missing branch labels:\n%s", out)
	}
}

func TestBuildFromFunction_JSONRoundtrip(t *testing.T) {
	src := `package p

func id(x int) int { return x }
`
	dir, _ := writeTempGo(t, src)
	c, err := BuildFromFunction(context.Background(), dir, "id", nil)
	if err != nil {
		t.Fatalf("BuildFromFunction: %v", err)
	}
	js, err := c.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	if !strings.Contains(js, `"function": "id"`) {
		t.Errorf("JSON missing function name:\n%s", js)
	}
	if !strings.Contains(js, `"blocks"`) || !strings.Contains(js, `"edges"`) {
		t.Errorf("JSON missing blocks/edges:\n%s", js)
	}
}

func TestBuildFromFunction_Unsupported(t *testing.T) {
	dir := t.TempDir()
	// Symbol points at a markup file (Markdown) — no function-level
	// control flow, so the heuristic builder must decline.
	sym := trace.Symbol{
		Name:     "intro",
		FilePath: "README.md",
		Line:     1,
		EndLine:  3,
	}
	full := filepath.Join(dir, "README.md")
	if err := os.WriteFile(full, []byte("# heading\n\nsome text\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := BuildFromSymbol(dir, sym)
	if err == nil {
		t.Fatalf("expected ErrUnsupportedLanguage, got nil")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("expected unsupported-language error, got: %v", err)
	}
}

// blockKindDiff compares observed block kinds against a multiset of expected
// kinds. Returns an empty string when they match.
func blockKindDiff(blocks []*Block, want []BlockKind) string {
	have := make(map[BlockKind]int)
	for _, b := range blocks {
		have[b.Kind]++
	}
	wantMap := make(map[BlockKind]int)
	for _, k := range want {
		wantMap[k]++
	}
	var diffs []string
	for k, n := range wantMap {
		if have[k] < n {
			diffs = append(diffs, k.string()+" missing")
		}
	}
	return strings.Join(diffs, "; ")
}

// string is a method on BlockKind for use in test diff. We avoid adding a
// production String() because BlockKind is already a string-backed type.
func (k BlockKind) string() string { return string(k) }
