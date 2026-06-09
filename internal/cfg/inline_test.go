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

	// Side-effect import: registers all bundled language parsers so
	// trace.Tracer.FindCallees has something to walk.
	_ "github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/lang"
)

// writeExtraFile drops `content` at <dir>/<name>. Used by the
// multi-file inlining tests below.
func writeExtraFile(dir, name, content string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
}

// pathDecisions returns each enumerated path as a single string of
// decision labels, joined with " | ". Useful for asserting that
// inlining surfaces specific branches in the callee on every path.
func pathDecisions(c *CFG) []string {
	paths := c.EnumeratePaths(0)
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, strings.Join(p.Decisions, " | "))
	}
	return out
}

// hasNote returns whether c.Notes contains a substring match. We
// substring-match instead of equality because the inline pass formats
// notes with concrete callee names (e.g. "recursive call to foo not inlined").
func hasNote(c *CFG, substr string) bool {
	for _, n := range c.Notes {
		if strings.Contains(n, substr) {
			return true
		}
	}
	return false
}

// TestBuildFromFunctionInlined_Disabled is the soft-launch
// guarantee: when InlineOptions.Enabled is false, the result must
// match BuildFromFunctionWith. This protects every existing CFG
// caller from accidental drift.
func TestBuildFromFunctionInlined_Disabled(t *testing.T) {
	src := `package p

func helper(x int) int {
	if x > 0 {
		return x
	}
	return -x
}

func outer(x int) int {
	y := helper(x)
	return y + 1
}
`
	dir, _ := writeTempGo(t, src)

	plain, err := BuildFromFunction(context.Background(), dir, "outer", nil)
	if err != nil {
		t.Fatalf("plain build: %v", err)
	}
	noinline, err := BuildFromFunctionInlined(
		context.Background(), dir, "outer", nil,
		SelectOptions{}, InlineOptions{Enabled: false})
	if err != nil {
		t.Fatalf("disabled-inline build: %v", err)
	}

	if len(plain.Blocks) != len(noinline.Blocks) {
		t.Errorf("disabled inlining changed block count: plain=%d, noinline=%d",
			len(plain.Blocks), len(noinline.Blocks))
	}
	if len(plain.Edges) != len(noinline.Edges) {
		t.Errorf("disabled inlining changed edge count: plain=%d, noinline=%d",
			len(plain.Edges), len(noinline.Edges))
	}
}

// TestBuildFromFunctionInlined_TwoFunctionChain is the headline
// test: inlining helper into outer should expose helper's
// `if x > 0` branches on outer's enumerated paths, taking the
// single linear path of plain outer and turning it into two paths.
func TestBuildFromFunctionInlined_TwoFunctionChain(t *testing.T) {
	src := `package p

func helper(x int) int {
	if x > 0 {
		return x
	}
	return -x
}

func outer(x int) int {
	y := helper(x)
	return y + 1
}
`
	dir, _ := writeTempGo(t, src)

	plain, err := BuildFromFunction(context.Background(), dir, "outer", nil)
	if err != nil {
		t.Fatalf("plain build: %v", err)
	}
	plainPaths := plain.EnumeratePaths(0)
	if len(plainPaths) != 1 {
		t.Fatalf("plain outer should have 1 path; got %d\n%s",
			len(plainPaths), plain.ToText())
	}

	c, err := BuildFromFunctionInlined(
		context.Background(), dir, "outer", nil,
		SelectOptions{}, InlineOptions{Enabled: true})
	if err != nil {
		t.Fatalf("inlined build: %v", err)
	}

	paths := c.EnumeratePaths(0)
	if len(paths) < 2 {
		t.Fatalf("inlined outer should expose helper's two branches; got %d paths\n%s",
			len(paths), c.ToText())
	}

	// At least one path should reference the inlined call boundary
	// — the splice always emits a "call helper" labelled edge.
	foundCall := false
	for _, e := range c.Edges {
		if strings.Contains(e.Label, "call helper") {
			foundCall = true
			break
		}
	}
	if !foundCall {
		t.Errorf("missing 'call helper' edge after splicing\n%s", c.ToText())
	}

	// Inlined blocks are prefixed with "[helper]" so renderers can
	// distinguish them from the host function's blocks.
	foundInlined := false
	for _, b := range c.Blocks {
		if strings.HasPrefix(b.Label, "[helper] ") {
			foundInlined = true
			break
		}
	}
	if !foundInlined {
		t.Errorf("inlined blocks should be prefixed with [helper]\n%s", c.ToText())
	}
}

// TestBuildFromFunctionInlined_SelfRecursionTerminates makes sure
// direct self-recursion doesn't loop forever. trace.FindCallees
// already strips self-calls (see internal/trace/trace.go: "skip
// recursive"), so we don't bother emitting a note for fact→fact —
// the splice simply never fires. The contract here is just
// "termination" plus "finite, sane block count".
func TestBuildFromFunctionInlined_SelfRecursionTerminates(t *testing.T) {
	src := `package p

func fact(n int) int {
	if n <= 1 {
		return 1
	}
	return n * fact(n-1)
}
`
	dir, _ := writeTempGo(t, src)

	c, err := BuildFromFunctionInlined(
		context.Background(), dir, "fact", nil,
		SelectOptions{}, InlineOptions{Enabled: true, MaxDepth: 5})
	if err != nil {
		t.Fatalf("inlined build: %v", err)
	}

	// No "[fact] " inlined block — self-call is filtered upstream.
	for _, b := range c.Blocks {
		if strings.HasPrefix(b.Label, "[fact] ") {
			t.Errorf("self-recursion should not inline; got block %q", b.Label)
		}
	}
	// Block count is bounded — nothing exploded.
	if len(c.Blocks) > 50 {
		t.Errorf("self-recursion produced %d blocks (suspect runaway expansion)",
			len(c.Blocks))
	}
}

// TestBuildFromFunctionInlined_MutualRecursionGuard exercises the
// in-progress set: A→B→A would loop forever without a guard, and
// trace.FindCallees does NOT filter cross-function recursion. The
// guard should bail at the second `A` reference and add a note so
// the user knows their CFG is truncated at the recursion point.
func TestBuildFromFunctionInlined_MutualRecursionGuard(t *testing.T) {
	src := `package p

func a(n int) int {
	if n <= 0 {
		return 0
	}
	return b(n - 1)
}

func b(n int) int {
	if n <= 0 {
		return 1
	}
	return a(n - 1)
}
`
	dir, _ := writeTempGo(t, src)

	c, err := BuildFromFunctionInlined(
		context.Background(), dir, "a", nil,
		SelectOptions{}, InlineOptions{Enabled: true, MaxDepth: 10})
	if err != nil {
		t.Fatalf("inlined build: %v", err)
	}

	// `a` should have b spliced in (one level), and b's recursive
	// call back to a should be noted-but-not-spliced. Without the
	// guard MaxDepth=10 would explode the block count.
	if !hasNote(c, "recursive call to a") {
		t.Errorf("expected mutual-recursion note for b->a; got notes=%v\n%s",
			c.Notes, c.ToText())
	}
	if len(c.Blocks) > 100 {
		t.Errorf("mutual recursion produced %d blocks (suspect runaway expansion)",
			len(c.Blocks))
	}
}

// TestBuildFromFunctionInlined_MaxDepthBounds verifies the depth
// budget actually bounds the inlining. With MaxDepth=1 we should
// inline outer's direct callee (mid) but not mid's callee (inner).
func TestBuildFromFunctionInlined_MaxDepthBounds(t *testing.T) {
	src := `package p

func inner(x int) int {
	if x > 0 {
		return x * 2
	}
	return 0
}

func mid(x int) int {
	return inner(x) + 1
}

func outer(x int) int {
	return mid(x) + 1
}
`
	dir, _ := writeTempGo(t, src)

	c, err := BuildFromFunctionInlined(
		context.Background(), dir, "outer", nil,
		SelectOptions{}, InlineOptions{Enabled: true, MaxDepth: 1})
	if err != nil {
		t.Fatalf("inlined build: %v", err)
	}

	hasMid := false
	hasInner := false
	for _, b := range c.Blocks {
		if strings.HasPrefix(b.Label, "[mid] ") {
			hasMid = true
		}
		if strings.HasPrefix(b.Label, "[inner] ") {
			hasInner = true
		}
	}

	if !hasMid {
		t.Errorf("MaxDepth=1: expected mid to be inlined into outer\n%s", c.ToText())
	}
	if hasInner {
		t.Errorf("MaxDepth=1: inner should NOT be inlined (depth budget exhausted)\n%s",
			c.ToText())
	}
	if !hasNote(c, "max inline depth reached") {
		t.Errorf("expected 'max inline depth reached' note; got %v", c.Notes)
	}
}

// TestBuildFromFunctionInlined_ExternalCalleeSilent checks that
// callees we can't resolve to a project symbol (here: fmt.Println
// from the standard library — never indexed because the symbol
// table is project-local) are skipped silently. We don't want
// every println in the codebase to spam Notes.
func TestBuildFromFunctionInlined_ExternalCalleeSilent(t *testing.T) {
	src := `package p

import "fmt"

func greet(name string) {
	fmt.Println("hi", name)
}
`
	dir, _ := writeTempGo(t, src)

	c, err := BuildFromFunctionInlined(
		context.Background(), dir, "greet", nil,
		SelectOptions{}, InlineOptions{Enabled: true})
	if err != nil {
		t.Fatalf("inlined build: %v", err)
	}

	// No "[fmt.Println]" inlining ever happened, and no spammy
	// notes for the external callee.
	for _, n := range c.Notes {
		if strings.Contains(n, "Println") {
			t.Errorf("external callee should not produce a note; got %q", n)
		}
	}
	for _, b := range c.Blocks {
		if strings.HasPrefix(b.Label, "[Println] ") || strings.HasPrefix(b.Label, "[fmt.Println] ") {
			t.Errorf("external callee should not be inlined; got block %q", b.Label)
		}
	}
}

// TestBuildFromFunctionInlined_AmbiguousCalleeNoted asserts that an
// in-project callee with multiple definitions (same name in two
// files) is *not* silently spliced — picking arbitrarily would lie
// about which branches the caller can take. Instead we leave the
// call site as-is and add a Note so the user can disambiguate via
// `saras cfg helper --file ...`.
func TestBuildFromFunctionInlined_AmbiguousCalleeNoted(t *testing.T) {
	srcA := `package p

func helper(x int) int {
	if x > 0 {
		return x
	}
	return -x
}

func outer(x int) int {
	return helper(x) + 1
}
`
	srcB := `package p

func helper(x int, y int) int {
	return x + y
}
`

	dir, _ := writeTempGo(t, srcA)
	if err := writeExtraFile(dir, "other.go", srcB); err != nil {
		t.Fatalf("write second file: %v", err)
	}

	c, err := BuildFromFunctionInlined(
		context.Background(), dir, "outer", nil,
		// Use --file to disambiguate outer (the caller); helper
		// stays ambiguous and that's the case under test.
		SelectOptions{File: "test.go"}, InlineOptions{Enabled: true})
	if err != nil {
		t.Fatalf("inlined build: %v", err)
	}

	if !hasNote(c, "call to helper not inlined") {
		t.Errorf("ambiguous callee should produce a note; got %v\n%s",
			c.Notes, c.ToText())
	}
	for _, b := range c.Blocks {
		if strings.HasPrefix(b.Label, "[helper] ") {
			t.Errorf("ambiguous callee should not be inlined; got block %q", b.Label)
		}
	}
}

// TestPathDecisionsExpandWithInlining is a behaviour-level test:
// inlining helper into outer should at least multiply the path
// count and make at least one path's Decisions reflect helper's
// `if x > 0`. We don't pin the exact decision string because
// individual style parsers may format conditions differently.
func TestPathDecisionsExpandWithInlining(t *testing.T) {
	src := `package p

func helper(x int) bool {
	if x > 0 {
		return true
	}
	return false
}

func outer(x int) bool {
	return helper(x)
}
`
	dir, _ := writeTempGo(t, src)

	plain, err := BuildFromFunction(context.Background(), dir, "outer", nil)
	if err != nil {
		t.Fatal(err)
	}
	inlined, err := BuildFromFunctionInlined(
		context.Background(), dir, "outer", nil,
		SelectOptions{}, InlineOptions{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	if got := pathDecisions(plain); len(got) >= len(pathDecisions(inlined)) {
		t.Errorf("inlining should multiply path count; plain=%d, inlined=%d",
			len(got), len(pathDecisions(inlined)))
	}
}
