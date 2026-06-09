/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package trace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	// Side-effect: register the bundled language parsers (go, python,
	// ruby, …) so symbol extraction recognises the test sources below.
	_ "github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/lang"
)

// writeAt writes contents under root/rel, creating intermediate dirs.
// Distinct from the writeFile helper in trace_test.go which expects
// a flat path.
func writeAt(t *testing.T, root, rel, contents string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

const goLoginSrc = `package main

func login(ok bool) string {
	if ok {
		return "ok"
	}
	return "no"
}

type Login struct {
	Token string
}
`

const pyLoginSrc = `def login(ok):
    if ok:
        return "ok"
    return "no"
`

const rubyLoginSrc = `def login(ok)
  if ok
    return "ok"
  end
  return "no"
end
`

// TestFindCandidates_KindFilter — restricting to functions/methods
// hides the same-named struct/type from the result set.
func TestFindCandidates_KindFilter(t *testing.T) {
	root := t.TempDir()
	writeAt(t, root, "src/login.go", goLoginSrc)

	tracer := NewTracer(root, nil)

	// Functions only.
	cands, err := tracer.FindCandidates(context.Background(), "login",
		SelectOptions{Kinds: []SymbolKind{KindFunction, KindMethod}})
	if err != nil {
		t.Fatalf("FindCandidates: %v", err)
	}
	if len(cands) != 1 || cands[0].Symbol.Kind != KindFunction {
		t.Fatalf("expected 1 function candidate, got %d: %+v", len(cands), cands)
	}

	// Now hunt for the type (different name to avoid colliding).
	typeCands, err := tracer.FindCandidates(context.Background(), "Login",
		SelectOptions{Kinds: []SymbolKind{KindType}})
	if err != nil {
		t.Fatalf("FindCandidates(Login type): %v", err)
	}
	if len(typeCands) != 1 || typeCands[0].Symbol.Kind != KindType {
		t.Fatalf("expected 1 type candidate, got %d: %+v", len(typeCands), typeCands)
	}
}

// TestFindCandidates_NoKindFilterReturnsAll — without a Kinds filter
// every match is returned regardless of SymbolKind. (We use distinct
// names to avoid ambiguity with the lang parser's lower/upper case
// conventions.)
func TestFindCandidates_NoKindFilterReturnsAll(t *testing.T) {
	root := t.TempDir()
	writeAt(t, root, "src/login.go", goLoginSrc)

	tracer := NewTracer(root, nil)
	cands, err := tracer.FindCandidates(context.Background(), "login",
		SelectOptions{})
	if err != nil {
		t.Fatalf("FindCandidates: %v", err)
	}
	if len(cands) == 0 {
		t.Fatalf("expected at least one candidate, got 0")
	}
}

// TestFindCandidates_FileFilter — same name in two Go files; the file
// substring narrows the result to the matching one.
func TestFindCandidates_FileFilter(t *testing.T) {
	root := t.TempDir()
	writeAt(t, root, "pkg/auth/a.go", goLoginSrc)
	writeAt(t, root, "pkg/admin/b.go", goLoginSrc)

	tracer := NewTracer(root, nil)
	cands, err := tracer.FindCandidates(context.Background(), "login",
		SelectOptions{
			File:  "pkg/auth",
			Kinds: []SymbolKind{KindFunction},
		})
	if err != nil {
		t.Fatalf("FindCandidates: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate after file filter, got %d", len(cands))
	}
	if !strings.Contains(cands[0].Symbol.FilePath, "pkg/auth") {
		t.Errorf("filter leaked: %q", cands[0].Symbol.FilePath)
	}
}

// TestFindCandidates_LanguageFilter — Go vs Python vs Ruby login()
// functions; --language picks one.
func TestFindCandidates_LanguageFilter(t *testing.T) {
	root := t.TempDir()
	writeAt(t, root, "src/login.go", goLoginSrc)
	writeAt(t, root, "src/login.py", pyLoginSrc)
	writeAt(t, root, "src/login.rb", rubyLoginSrc)

	tracer := NewTracer(root, nil)

	for _, lang := range []string{"go", "python", "ruby"} {
		t.Run(lang, func(t *testing.T) {
			cands, err := tracer.FindCandidates(context.Background(), "login",
				SelectOptions{
					Language: lang,
					Kinds:    []SymbolKind{KindFunction, KindMethod},
				})
			if err != nil {
				t.Fatalf("FindCandidates(%s): %v", lang, err)
			}
			if len(cands) != 1 {
				t.Fatalf("expected 1 %s candidate, got %d", lang, len(cands))
			}
			if cands[0].Language != lang {
				t.Errorf("got language %q, want %q", cands[0].Language, lang)
			}
		})
	}
}

// TestSelectOne_AmbiguousProducesTypedError — three definitions, no
// filter: SelectOne returns *AmbiguousSymbolError carrying every
// candidate and the supplied subject.
func TestSelectOne_AmbiguousProducesTypedError(t *testing.T) {
	root := t.TempDir()
	writeAt(t, root, "src/login.go", goLoginSrc)
	writeAt(t, root, "src/login.py", pyLoginSrc)
	writeAt(t, root, "src/login.rb", rubyLoginSrc)

	tracer := NewTracer(root, nil)
	cands, err := tracer.FindCandidates(context.Background(), "login",
		SelectOptions{Kinds: []SymbolKind{KindFunction, KindMethod}})
	if err != nil {
		t.Fatalf("FindCandidates: %v", err)
	}

	_, err = SelectOne("symbol", "login", cands, SelectOptions{})
	if err == nil {
		t.Fatalf("expected ambiguity error, got nil")
	}
	var amb *AmbiguousSymbolError
	if !errors.As(err, &amb) {
		t.Fatalf("expected *AmbiguousSymbolError, got %T", err)
	}
	if amb.Subject != "symbol" {
		t.Errorf("Subject = %q, want %q", amb.Subject, "symbol")
	}
	if got := len(amb.Candidates); got != 3 {
		t.Errorf("expected 3 candidates, got %d", got)
	}
	msg := amb.Error()
	if !strings.Contains(msg, "symbol \"login\" is ambiguous") {
		t.Errorf("error message subject not propagated:\n%s", msg)
	}
}

// TestSelectOne_NoMatchHintsAtFilters — the not-found message
// mentions the supplied filters so users know which to broaden.
func TestSelectOne_NoMatchHintsAtFilters(t *testing.T) {
	root := t.TempDir()
	writeAt(t, root, "src/login.go", goLoginSrc)

	tracer := NewTracer(root, nil)
	cands, err := tracer.FindCandidates(context.Background(), "login",
		SelectOptions{
			File:  "doesnotexist",
			Kinds: []SymbolKind{KindFunction},
		})
	if err != nil {
		t.Fatalf("FindCandidates: %v", err)
	}
	_, err = SelectOne("function", "login", cands,
		SelectOptions{File: "doesnotexist"})
	if err == nil {
		t.Fatalf("expected not-found error")
	}
	if !strings.Contains(err.Error(), "with the supplied filters") {
		t.Errorf("error should mention applied filters: %v", err)
	}
}

// TestSelectOne_DefaultSubject — when subject is empty the error
// defaults to "symbol".
func TestSelectOne_DefaultSubject(t *testing.T) {
	cands := []Candidate{
		{Symbol: Symbol{Name: "x", FilePath: "a.go", Line: 1, EndLine: 2, Kind: KindFunction}},
		{Symbol: Symbol{Name: "x", FilePath: "b.go", Line: 1, EndLine: 2, Kind: KindFunction}},
	}
	_, err := SelectOne("", "x", cands, SelectOptions{})
	var amb *AmbiguousSymbolError
	if !errors.As(err, &amb) {
		t.Fatalf("expected *AmbiguousSymbolError")
	}
	if !strings.Contains(amb.Error(), "symbol \"x\" is ambiguous") {
		t.Errorf("default subject should be 'symbol', got %s", amb.Error())
	}
}

// TestSelectOptions_IsZero — small contract check used by the CLI.
func TestSelectOptions_IsZero(t *testing.T) {
	if !(SelectOptions{}).IsZero() {
		t.Error("empty SelectOptions should be zero")
	}
	if (SelectOptions{File: "x"}).IsZero() {
		t.Error("File set is not zero")
	}
	if (SelectOptions{Language: "go"}).IsZero() {
		t.Error("Language set is not zero")
	}
	if (SelectOptions{Parent: "Foo"}).IsZero() {
		t.Error("Parent set is not zero")
	}
	if (SelectOptions{Kinds: []SymbolKind{KindFunction}}).IsZero() {
		t.Error("Kinds set is not zero")
	}
}

// TestCandidate_StringFormat — disambiguation messages embed
// Candidate.String(); pin the format so future changes are intentional.
func TestCandidate_StringFormat(t *testing.T) {
	c := Candidate{
		Symbol: Symbol{
			Name:     "Login",
			Kind:     KindMethod,
			FilePath: "pkg/auth/login.go",
			Line:     42,
			EndLine:  60,
			Parent:   "UserService",
		},
		Language: "go",
	}
	got := c.String()
	for _, want := range []string{
		"pkg/auth/login.go:42-60",
		"(go)",
		"[UserService.Login method]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Candidate.String() missing %q: %s", want, got)
		}
	}

	// No parent and no end-line: format degrades gracefully.
	c2 := Candidate{
		Symbol:   Symbol{Name: "main", Kind: KindFunction, FilePath: "main.go", Line: 1, EndLine: 1},
		Language: "go",
	}
	got2 := c2.String()
	if !strings.Contains(got2, "main.go:1") || !strings.Contains(got2, "[main function]") {
		t.Errorf("unexpected format: %s", got2)
	}
}
