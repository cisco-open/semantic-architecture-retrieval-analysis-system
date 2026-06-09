/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/cfg"
	// Side-effect: register language parsers so the trace package can
	// extract types/methods from the seeded files below.
	_ "github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/lang"
)

// seedTestProject creates a tiny multi-file Go project under a fresh
// temp directory, installs a minimal .saras/config.yaml so
// config.FindProjectRoot resolves to it, and cd's the test process
// in. Returns the project root.
func seedTestProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeProjectFile(t, root, ".saras/config.yaml", "ignore: []\n")
	for rel, body := range files {
		writeProjectFile(t, root, rel, body)
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	return root
}

// architectMini is a small but realistic stand-in for the architect
// package: receiver type, referenced struct types, helper function
// called from the body, and a non-trivial import set.
const architectMini = `package architect

import (
	"context"
	"fmt"
	"strings"
)

// Mapper builds an architecture map for a project.
type Mapper struct {
	root       string
	ignoreList []string
}

// CodebaseMap is the structured output of GenerateMap.
type CodebaseMap struct {
	TotalFiles int
	TotalLines int
	Packages   []PackageInfo
	Symbols    []Symbol
}

// PackageInfo describes a single package.
type PackageInfo struct {
	Name string
	Path string
}

// Symbol is a placeholder symbol type.
type Symbol struct {
	Name string
	Kind string
}

// NewMapper constructs a Mapper.
func NewMapper(root string, ignore []string) *Mapper {
	return &Mapper{root: root, ignoreList: ignore}
}

// GenerateMap is a placeholder.
func (m *Mapper) GenerateMap(ctx context.Context) (*CodebaseMap, error) {
	return &CodebaseMap{}, nil
}

// filterSymbols filters by kind.
func filterSymbols(syms []Symbol, kind string) []Symbol {
	var out []Symbol
	for _, s := range syms {
		if s.Kind == kind {
			out = append(out, s)
		}
	}
	return out
}

// GenerateMarkdown is the function under test.
func (m *Mapper) GenerateMarkdown(ctx context.Context) (string, error) {
	cmap, err := m.GenerateMap(ctx)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Codebase\nFiles: %d\n", cmap.TotalFiles))
	for _, pkg := range cmap.Packages {
		b.WriteString(fmt.Sprintf("- %s (%s)\n", pkg.Name, pkg.Path))
	}
	funcs := filterSymbols(cmap.Symbols, "function")
	if len(funcs) > 0 {
		b.WriteString("## Functions\n")
		for _, s := range funcs {
			b.WriteString(fmt.Sprintf("- %s\n", s.Name))
		}
	}
	return b.String(), nil
}
`

// helperCFG runs the heuristic CFG builder against the project to
// produce a real *cfg.CFG for the named function.
func helperCFG(t *testing.T, root, fn string) *cfg.CFG {
	t.Helper()
	c, err := cfg.BuildFromFunction(context.Background(), root, fn, nil)
	if err != nil {
		t.Fatalf("BuildFromFunction(%s): %v", fn, err)
	}
	return c
}

// TestGatherCFGContext_PopulatesAllSections — happy path: every
// section the gather function knows how to produce is non-empty for
// a realistic method (Mapper.GenerateMarkdown).
func TestGatherCFGContext_PopulatesAllSections(t *testing.T) {
	seedTestProject(t, map[string]string{
		"internal/architect/architect.go": architectMini,
	})
	c := helperCFG(t, ".", "GenerateMarkdown")
	if c.Parent != "Mapper" {
		t.Fatalf("expected CFG.Parent=Mapper, got %q", c.Parent)
	}

	pc := gatherCFGContext(context.Background(), c)
	if pc.Empty() {
		t.Fatalf("ProjectContext should be populated; got empty")
	}
	for _, want := range []string{"package architect", `"context"`} {
		if !strings.Contains(pc.FileHeader, want) {
			t.Errorf("FileHeader missing %q:\n%s", want, pc.FileHeader)
		}
	}
	if !strings.Contains(pc.ParentDecl, "type Mapper struct") {
		t.Errorf("ParentDecl missing receiver type:\n%s", pc.ParentDecl)
	}
	if len(pc.ReferencedTypes) == 0 {
		t.Errorf("ReferencedTypes should not be empty")
	}
	foundCodebaseMap := false
	for _, d := range pc.ReferencedTypes {
		if strings.Contains(d, "type CodebaseMap struct") {
			foundCodebaseMap = true
		}
		if strings.Contains(d, "type Mapper struct") {
			t.Errorf("receiver type should not appear under ReferencedTypes:\n%s", d)
		}
	}
	if !foundCodebaseMap {
		t.Errorf("expected CodebaseMap among ReferencedTypes; got %v", pc.ReferencedTypes)
	}
	if len(pc.CalleeSignatures) == 0 {
		t.Errorf("CalleeSignatures should not be empty")
	}
}

// TestProjectContextText_OmitsEmptySections — the markdown
// renderer should skip sections that have no content rather than
// emitting empty headers, so trivial functions get a small footprint.
func TestProjectContextText_OmitsEmptySections(t *testing.T) {
	pc := &ProjectContext{
		Language:   "go",
		FileHeader: "package x",
	}
	got := pc.Text()
	if !strings.Contains(got, "File header") {
		t.Errorf("expected file-header section, missing in:\n%s", got)
	}
	for _, dont := range []string{
		"Receiver / parent type",
		"Referenced types",
		"Functions / methods called",
	} {
		if strings.Contains(got, dont) {
			t.Errorf("empty section %q should be omitted:\n%s", dont, got)
		}
	}
}

// TestProjectContextText_EmptyReturnsEmpty — an empty struct
// produces an empty string (not just whitespace headers) so the
// caller can use the value verbatim with no further nil-checks.
func TestProjectContextText_EmptyReturnsEmpty(t *testing.T) {
	pc := &ProjectContext{}
	if got := pc.Text(); got != "" {
		t.Errorf("Text() on empty pc should be \"\", got %q", got)
	}
	if pc2 := (*ProjectContext)(nil); !pc2.Empty() {
		t.Errorf("nil receiver should be Empty()=true")
	}
}

// renderWithContext builds a CFG for fn under root and runs the
// CLI's renderCFG with a freshly-gathered ProjectContext. This skips
// the cobra plumbing (which uses the package-level rootCmd singleton
// and is fiddly to drive in parallel-safe tests) but exercises the
// exact same render path runCFG goes through.
func renderWithContext(t *testing.T, root, fn, format string) string {
	t.Helper()
	c := helperCFG(t, root, fn)
	pc := gatherCFGContext(context.Background(), c)
	out, err := renderCFG(c, format, 0, pc)
	if err != nil {
		t.Fatalf("renderCFG(%s, with-context): %v", format, err)
	}
	return out
}

// TestRenderCFGWithContext_TextFormat — the text renderer must
// output the standard CFG block list AND the appended project
// context section when a non-empty ProjectContext is supplied.
func TestRenderCFGWithContext_TextFormat(t *testing.T) {
	seedTestProject(t, map[string]string{
		"internal/architect/architect.go": architectMini,
	})
	out := renderWithContext(t, ".", "GenerateMarkdown", "text")
	for _, want := range []string{
		"Control Flow Graph",
		"Project context (--with-context)",
		"package architect",
		"type Mapper struct",
		"Functions / methods called by the target",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text+context output missing %q:\n%s", want, out)
		}
	}
}

// TestRenderCFGWithContext_PathsFormat — same coverage for the
// `paths` format (the format reviewers use most when designing
// tests by hand).
func TestRenderCFGWithContext_PathsFormat(t *testing.T) {
	seedTestProject(t, map[string]string{
		"internal/architect/architect.go": architectMini,
	})
	out := renderWithContext(t, ".", "GenerateMarkdown", "paths")
	if !strings.Contains(out, "Function: GenerateMarkdown") {
		t.Errorf("missing CFG paths header:\n%s", out)
	}
	if !strings.Contains(out, "Project context (--with-context)") {
		t.Errorf("missing context section:\n%s", out)
	}
}

// TestRenderCFGWithContext_JSON — JSON output embeds the context
// under a `context` key. Verify the structure is well-formed
// (parses as JSON) and the expected fields exist.
func TestRenderCFGWithContext_JSON(t *testing.T) {
	seedTestProject(t, map[string]string{
		"internal/architect/architect.go": architectMini,
	})
	out := renderWithContext(t, ".", "GenerateMarkdown", "json")

	var got struct {
		Function string `json:"function"`
		Context  *struct {
			Language        string   `json:"language"`
			FileHeader      string   `json:"file_header"`
			ParentDecl      string   `json:"parent_decl"`
			ReferencedTypes []string `json:"referenced_types"`
		} `json:"context"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if got.Function != "GenerateMarkdown" {
		t.Errorf("function = %q, want GenerateMarkdown", got.Function)
	}
	if got.Context == nil {
		t.Fatalf("context object missing in JSON:\n%s", out)
	}
	if got.Context.Language != "go" {
		t.Errorf("context.language = %q, want go", got.Context.Language)
	}
	if !strings.Contains(got.Context.ParentDecl, "type Mapper struct") {
		t.Errorf("context.parent_decl missing Mapper:\n%s", got.Context.ParentDecl)
	}
}

// TestRenderCFGWithContext_MermaidIgnoresContext — Mermaid is a
// diagram, not a document. Even when a populated context is passed
// in, the renderer must not contaminate the flowchart with a
// markdown appendix (which would break mermaid.live, GitHub
// renderers, etc.). The user-facing warning is emitted by the
// runCFG wrapper; this test pins the lower-level behaviour.
func TestRenderCFGWithContext_MermaidIgnoresContext(t *testing.T) {
	seedTestProject(t, map[string]string{
		"internal/architect/architect.go": architectMini,
	})
	c := helperCFG(t, ".", "GenerateMarkdown")
	// Hand-craft a populated context to prove the renderer (not
	// gatherCFGContext) is what filters it out.
	pc := &ProjectContext{
		Language:   "go",
		FileHeader: "package architect",
		ParentDecl: "type Mapper struct {}",
	}
	out, err := renderCFG(c, "mermaid", 0, pc)
	if err != nil {
		t.Fatalf("renderCFG(mermaid): %v", err)
	}
	if !strings.Contains(out, "flowchart TD") {
		t.Errorf("Mermaid output corrupted:\n%s", out)
	}
	if strings.Contains(out, "Project context (--with-context)") {
		t.Errorf("Mermaid output must not include markdown appendix:\n%s", out)
	}
}

// TestRenderCFG_NoContextSchemaStable — without a context, the json
// output must keep its pre-feature shape (no `context` key). This
// pins the contract for downstream tooling that pre-dates
// --with-context.
func TestRenderCFG_NoContextSchemaStable(t *testing.T) {
	seedTestProject(t, map[string]string{
		"internal/architect/architect.go": architectMini,
	})
	c := helperCFG(t, ".", "GenerateMarkdown")
	out, err := renderCFG(c, "json", 0, nil)
	if err != nil {
		t.Fatalf("renderCFG(json, nil): %v", err)
	}
	if strings.Contains(out, `"context"`) {
		t.Errorf("default JSON must not contain context key:\n%s", out)
	}
	out, err = renderCFG(c, "text", 0, nil)
	if err != nil {
		t.Fatalf("renderCFG(text, nil): %v", err)
	}
	if strings.Contains(out, "Project context") {
		t.Errorf("default text must not contain context section:\n%s", out)
	}
}

// TestExtractCapIdentifiers — the extractor must dedupe, sort
// alphabetically, and ignore lowercase tokens (which are picked up
// via the call graph instead).
func TestExtractCapIdentifiers(t *testing.T) {
	src := `func (m *Mapper) Foo(x CodebaseMap) {
	cmap := getCmap()
	if cmap.TotalFiles > 0 {
		_ = PackageInfo{}
	}
	_ = Mapper{}
	// TODO: revisit
}`
	got := extractCapIdentifiers(src)
	want := []string{"CodebaseMap", "Foo", "Mapper", "PackageInfo", "TODO", "TotalFiles"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("extractCapIdentifiers got %v, want %v", got, want)
	}
}

// TestReadFileHeader_RespectsCaps — both bounds (untilLine and
// maxLines) must take effect; whichever is smaller wins.
func TestReadFileHeader_RespectsCaps(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "f.go")
	var b strings.Builder
	for i := 1; i <= 20; i++ {
		b.WriteString("// line\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := readFileHeader(path, 5, 100)
	if lines := strings.Count(got, "\n") + 1; lines > 5 {
		t.Errorf("untilLine cap broken: got %d lines, want <= 4", lines)
	}

	got = readFileHeader(path, 100, 3)
	if !strings.Contains(got, "header truncated") {
		t.Errorf("maxLines cap should append truncation marker:\n%s", got)
	}
}
