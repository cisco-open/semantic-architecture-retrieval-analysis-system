/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package architect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/trace"
)

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	os.MkdirAll(filepath.Dir(abs), 0755)
	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// testFlowMapper creates a FlowMapper suitable for unit tests.
func testFlowMapper(root string) *FlowMapper {
	return NewFlowMapper(root, nil, defaultMaxDepth)
}

// ---------------------------------------------------------------------------
// symbolIndex tests
// ---------------------------------------------------------------------------

func TestSymbolIndexResolveUnique(t *testing.T) {
	idx := buildSymbolIndex([]trace.Symbol{
		{Name: "Foo", Kind: trace.KindFunction, FilePath: "a/foo.go", Line: 10},
	})

	sym, ok := idx.resolve("Foo", "")
	if !ok {
		t.Fatal("expected Foo to resolve")
	}
	if sym.Line != 10 {
		t.Errorf("expected line 10, got %d", sym.Line)
	}
}

func TestSymbolIndexResolveSamePackage(t *testing.T) {
	idx := buildSymbolIndex([]trace.Symbol{
		{Name: "Close", Kind: trace.KindMethod, FilePath: "a/conn.go", Line: 5},
		{Name: "Close", Kind: trace.KindMethod, FilePath: "b/file.go", Line: 20},
	})

	sym, ok := idx.resolve("Close", "b/handler.go")
	if !ok {
		t.Fatal("expected Close to resolve")
	}
	if sym.FilePath != "b/file.go" {
		t.Errorf("expected same-package resolution to b/file.go, got %s", sym.FilePath)
	}
}

func TestSymbolIndexResolveUnknown(t *testing.T) {
	idx := buildSymbolIndex([]trace.Symbol{})
	_, ok := idx.resolve("Missing", "")
	if ok {
		t.Error("expected Missing to not resolve")
	}
}

// ---------------------------------------------------------------------------
// findCallsInBody tests
// ---------------------------------------------------------------------------

func TestFindCallsInBody(t *testing.T) {
	idx := buildSymbolIndex([]trace.Symbol{
		{Name: "Foo", Kind: trace.KindFunction, FilePath: "a.go"},
		{Name: "Bar", Kind: trace.KindFunction, FilePath: "a.go"},
		{Name: "Baz", Kind: trace.KindFunction, FilePath: "a.go"},
	})

	body := `func doStuff() {
	x := Foo()
	y := Bar(x)
	// Baz() is commented out
	fmt.Println(y)
}`

	fm := testFlowMapper("")
	calls := fm.findCallsInBody(body, "doStuff", "a.go", idx)

	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d: %v", len(calls), calls)
	}
	if calls[0] != "Bar" || calls[1] != "Foo" {
		t.Errorf("expected [Bar, Foo], got %v", calls)
	}
}

func TestFindCallsSkipsStringLiterals(t *testing.T) {
	idx := buildSymbolIndex([]trace.Symbol{
		{Name: "RealCall", Kind: trace.KindFunction, FilePath: "a.go"},
		{Name: "FakeCall", Kind: trace.KindFunction, FilePath: "a.go"},
	})

	body := `func x() {
	RealCall()
	s := "FakeCall() should not match"
}`

	fm := testFlowMapper("")
	calls := fm.findCallsInBody(body, "x", "a.go", idx)

	if len(calls) != 1 || calls[0] != "RealCall" {
		t.Errorf("expected [RealCall], got %v", calls)
	}
}

func TestFindCallsSkipsKeywords(t *testing.T) {
	idx := buildSymbolIndex([]trace.Symbol{
		{Name: "make", Kind: trace.KindFunction, FilePath: "a.go"},
		{Name: "Real", Kind: trace.KindFunction, FilePath: "a.go"},
	})

	body := `func x() {
	s := make([]byte, 10)
	Real()
}`

	fm := testFlowMapper("")
	calls := fm.findCallsInBody(body, "x", "a.go", idx)
	if len(calls) != 1 || calls[0] != "Real" {
		t.Errorf("expected [Real], got %v", calls)
	}
}

func TestFindCallsSkipsSelf(t *testing.T) {
	idx := buildSymbolIndex([]trace.Symbol{
		{Name: "Recurse", Kind: trace.KindFunction, FilePath: "a.go"},
	})

	body := `func Recurse() {
	Recurse()
}`

	fm := testFlowMapper("")
	calls := fm.findCallsInBody(body, "Recurse", "a.go", idx)
	if len(calls) != 0 {
		t.Errorf("expected no calls (self-recursion filtered), got %v", calls)
	}
}

// ---------------------------------------------------------------------------
// buildCallTree tests
// ---------------------------------------------------------------------------

func TestBuildCallTreeSimple(t *testing.T) {
	root := t.TempDir()

	writeTestFile(t, root, "main.go", `package main

func main() {
	A()
}

func A() {
	B()
}

func B() {
}
`)

	fm := NewFlowMapper(root, nil, 8)

	symbols := []trace.Symbol{
		{Name: "main", Kind: trace.KindFunction, FilePath: "main.go", Line: 3, EndLine: 5},
		{Name: "A", Kind: trace.KindFunction, FilePath: "main.go", Line: 7, EndLine: 9},
		{Name: "B", Kind: trace.KindFunction, FilePath: "main.go", Line: 11, EndLine: 12},
	}
	idx := buildSymbolIndex(symbols)
	visiting := make(map[string]bool)
	expanded := make(map[string]bool)

	node := fm.buildCallTree("main", symbols[0], idx, visiting, expanded, 0)

	if node.Name != "main" {
		t.Fatalf("expected root node 'main', got %q", node.Name)
	}
	if len(node.Children) != 1 || node.Children[0].Name != "A" {
		t.Fatalf("expected main → [A], got %v", childNames(node))
	}
	aNode := node.Children[0]
	if len(aNode.Children) != 1 || aNode.Children[0].Name != "B" {
		t.Fatalf("expected A → [B], got %v", childNames(aNode))
	}
}

func TestBuildCallTreeCycleDetection(t *testing.T) {
	root := t.TempDir()

	writeTestFile(t, root, "main.go", `package main

func A() {
	B()
}

func B() {
	A()
}
`)

	fm := NewFlowMapper(root, nil, 8)

	symbols := []trace.Symbol{
		{Name: "A", Kind: trace.KindFunction, FilePath: "main.go", Line: 3, EndLine: 5},
		{Name: "B", Kind: trace.KindFunction, FilePath: "main.go", Line: 7, EndLine: 9},
	}
	idx := buildSymbolIndex(symbols)

	node := fm.buildCallTree("A", symbols[0], idx, make(map[string]bool), make(map[string]bool), 0)

	// A → B → A(cycle)
	if len(node.Children) != 1 || node.Children[0].Name != "B" {
		t.Fatalf("expected A → [B], got %v", childNames(node))
	}
	bNode := node.Children[0]
	if len(bNode.Children) != 1 {
		t.Fatalf("expected B → [A(cycle)], got %v", childNames(bNode))
	}
	if !bNode.Children[0].Cycle {
		t.Error("expected cycle marker on A under B")
	}
}

func TestBuildCallTreeDepthLimit(t *testing.T) {
	root := t.TempDir()

	writeTestFile(t, root, "main.go", `package main

func A() {
	B()
}

func B() {
	C()
}

func C() {
}
`)

	fm := NewFlowMapper(root, nil, 2) // depth limit = 2

	symbols := []trace.Symbol{
		{Name: "A", Kind: trace.KindFunction, FilePath: "main.go", Line: 3, EndLine: 5},
		{Name: "B", Kind: trace.KindFunction, FilePath: "main.go", Line: 7, EndLine: 9},
		{Name: "C", Kind: trace.KindFunction, FilePath: "main.go", Line: 11, EndLine: 12},
	}
	idx := buildSymbolIndex(symbols)

	node := fm.buildCallTree("A", symbols[0], idx, make(map[string]bool), make(map[string]bool), 0)

	// A(depth=0) → B(depth=1) → C(depth=2, capped)
	bNode := node.Children[0]
	if len(bNode.Children) != 1 {
		t.Fatalf("expected B → [C], got %v", childNames(bNode))
	}
	cNode := bNode.Children[0]
	if !cNode.DepthCap {
		t.Error("expected depth cap marker on C")
	}
	if len(cNode.Children) != 0 {
		t.Error("expected no children on depth-capped node")
	}
}

func TestBuildCallTreeRefMarker(t *testing.T) {
	root := t.TempDir()

	writeTestFile(t, root, "main.go", `package main

func A() {
	C()
}

func B() {
	C()
}

func C() {
}

func Top() {
	A()
	B()
}
`)

	fm := NewFlowMapper(root, nil, 8)

	symbols := []trace.Symbol{
		{Name: "Top", Kind: trace.KindFunction, FilePath: "main.go", Line: 14, EndLine: 17},
		{Name: "A", Kind: trace.KindFunction, FilePath: "main.go", Line: 3, EndLine: 5},
		{Name: "B", Kind: trace.KindFunction, FilePath: "main.go", Line: 7, EndLine: 9},
		{Name: "C", Kind: trace.KindFunction, FilePath: "main.go", Line: 11, EndLine: 12},
	}
	idx := buildSymbolIndex(symbols)

	node := fm.buildCallTree("Top", symbols[0], idx, make(map[string]bool), make(map[string]bool), 0)

	// Top → A → C, B → C(↩)
	aNode := node.Children[0]
	bNode := node.Children[1]
	if len(aNode.Children) != 1 || aNode.Children[0].Name != "C" {
		t.Fatalf("expected A → [C], got %v", childNames(aNode))
	}
	if aNode.Children[0].Ref {
		t.Error("C under A should NOT be a ref (first expansion)")
	}
	if len(bNode.Children) != 1 || bNode.Children[0].Name != "C" {
		t.Fatalf("expected B → [C], got %v", childNames(bNode))
	}
	if !bNode.Children[0].Ref {
		t.Error("C under B SHOULD be a ref (already expanded under A)")
	}
}

// ---------------------------------------------------------------------------
// Entry point detection tests
// ---------------------------------------------------------------------------

func TestDetectMainEntryPoint(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "cmd/app/main.go", `package main

func main() {
	run()
}
`)
	writeTestFile(t, root, "pkg/lib.go", `package pkg

func Helper() {}
`)

	symbols := []trace.Symbol{
		{Name: "main", Kind: trace.KindFunction, FilePath: "cmd/app/main.go", Line: 3, EndLine: 5},
		{Name: "Helper", Kind: trace.KindFunction, FilePath: "pkg/lib.go", Line: 3, EndLine: 3},
	}

	fm := NewFlowMapper(root, nil, 8)
	entries, err := fm.detectEntryPoints(nil, symbols)
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, e := range entries {
		if e.Kind == EntryMain && e.Symbol.Name == "main" {
			found = true
		}
	}
	if !found {
		t.Error("expected to detect main() entry point")
	}
}

func TestDetectCobraHandler(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "cli/cmd.go", `package cli

import "github.com/spf13/cobra"

var fooCmd = &cobra.Command{
	Use:   "foo",
	Short: "Do foo",
	RunE:  runFoo,
}
`)
	writeTestFile(t, root, "cli/foo.go", `package cli

func runFoo(cmd *cobra.Command, args []string) error {
	return nil
}
`)

	symbols := []trace.Symbol{
		{Name: "runFoo", Kind: trace.KindFunction, FilePath: "cli/foo.go", Line: 3, EndLine: 5},
	}

	fm := NewFlowMapper(root, nil, 8)
	entries := fm.detectCobraHandlers(symbols)

	if len(entries) != 1 {
		t.Fatalf("expected 1 cobra handler, got %d", len(entries))
	}
	if entries[0].Kind != EntryCommand {
		t.Errorf("expected command kind, got %s", entries[0].Kind)
	}
	if entries[0].Label != "foo" {
		t.Errorf("expected label 'foo', got %q", entries[0].Label)
	}
}

// ---------------------------------------------------------------------------
// Format tests
// ---------------------------------------------------------------------------

func TestFormatFlowTree(t *testing.T) {
	tree := &FlowTree{
		Root: &FlowNode{
			Name: "main", FilePath: "main.go", Line: 1,
			Children: []*FlowNode{
				{Name: "A", FilePath: "a.go", Line: 5, Children: []*FlowNode{
					{Name: "B", FilePath: "b.go", Line: 10},
				}},
				{Name: "C", FilePath: "c.go", Line: 15, Ref: true},
			},
		},
	}

	out := FormatFlowTree(tree)

	if !strings.Contains(out, "main [main.go:1]") {
		t.Error("expected root line")
	}
	if !strings.Contains(out, "├── A [a.go:5]") {
		t.Error("expected A node")
	}
	if !strings.Contains(out, "│   └── B [b.go:10]") {
		t.Error("expected B node under A")
	}
	if !strings.Contains(out, "└── C [c.go:15] (↩)") {
		t.Error("expected C node with ref marker")
	}
}

func TestFormatFlowTreeCycleMarker(t *testing.T) {
	tree := &FlowTree{
		Root: &FlowNode{
			Name: "A", FilePath: "a.go", Line: 1,
			Children: []*FlowNode{
				{Name: "B", FilePath: "b.go", Line: 5, Children: []*FlowNode{
					{Name: "A", FilePath: "a.go", Line: 1, Cycle: true},
				}},
			},
		},
	}

	out := FormatFlowTree(tree)
	if !strings.Contains(out, "(cycle)") {
		t.Error("expected cycle marker in output")
	}
}

func TestFormatFlowTreeDepthCap(t *testing.T) {
	tree := &FlowTree{
		Root: &FlowNode{
			Name: "X", FilePath: "x.go", Line: 1,
			Children: []*FlowNode{
				{Name: "Y", FilePath: "y.go", Line: 5, DepthCap: true},
			},
		},
	}

	out := FormatFlowTree(tree)
	if !strings.Contains(out, "(...)") {
		t.Error("expected depth cap marker in output")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func childNames(n *FlowNode) []string {
	var names []string
	for _, c := range n.Children {
		names = append(names, c.Name)
	}
	return names
}
