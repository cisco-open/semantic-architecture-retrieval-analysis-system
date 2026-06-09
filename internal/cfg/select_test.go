/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cfg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	// Ensure language parsers are registered.
	_ "github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/lang"
)

// writeTempFileAt writes contents at root/rel, creating intermediate
// directories. Distinct from the single-file writeTempFile helper in
// languages_test.go which only writes one flat file.
func writeTempFileAt(t *testing.T, root, rel, contents string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

// newTempProject creates a fresh project root with the given file map.
// Files are written under the temp dir so each test gets isolation.
func newTempProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		writeTempFileAt(t, root, rel, body)
	}
	return root
}

// goLogin returns a tiny Go function that has at least one branch so
// the heuristic builder produces > 1 path. Used as the "ambiguous"
// payload across files/packages.
func goLogin() string {
	return `package main

func login(ok bool) string {
	if ok {
		return "ok"
	}
	return "no"
}
`
}

// pyLogin returns the equivalent Python definition. Importing it via
// the indent style verifies the language disambiguator.
func pyLogin() string {
	return `def login(ok):
    if ok:
        return "ok"
    return "no"
`
}

// rubyLogin uses end-keyword style to exercise StyleEnd.
func rubyLogin() string {
	return `def login(ok)
  if ok
    return "ok"
  end
  return "no"
end
`
}

// TestBuildFromFunction_AmbiguousReturnsCandidateList — two Go files
// each defining `login`. The bare lookup should return an
// AmbiguousFunctionError that lists both candidates with file:line and
// language.
func TestBuildFromFunction_AmbiguousReturnsCandidateList(t *testing.T) {
	root := newTempProject(t, map[string]string{
		"pkg/auth/a.go":  goLogin(),
		"pkg/admin/b.go": goLogin(),
	})

	_, err := BuildFromFunction(context.Background(), root, "login", nil)
	if err == nil {
		t.Fatalf("expected AmbiguousFunctionError, got nil")
	}
	var amb *AmbiguousFunctionError
	if !errors.As(err, &amb) {
		t.Fatalf("expected *AmbiguousFunctionError, got %T: %v", err, err)
	}
	if got := len(amb.Candidates); got != 2 {
		t.Errorf("expected 2 candidates, got %d", got)
	}
	msg := amb.Error()
	for _, want := range []string{"pkg/auth/a.go", "pkg/admin/b.go", "(go)"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q\n%s", want, msg)
		}
	}
}

// TestBuildFromFunction_FileFilterDisambiguates — same setup, but a
// file-substring filter narrows it to exactly one match.
func TestBuildFromFunction_FileFilterDisambiguates(t *testing.T) {
	root := newTempProject(t, map[string]string{
		"pkg/auth/a.go":  goLogin(),
		"pkg/admin/b.go": goLogin(),
	})

	c, err := BuildFromFunctionWith(
		context.Background(), root, "login", nil,
		SelectOptions{File: "pkg/auth"},
	)
	if err != nil {
		t.Fatalf("BuildFromFunctionWith: %v", err)
	}
	if !strings.Contains(c.File, "pkg/auth") {
		t.Errorf("expected file under pkg/auth, got %q", c.File)
	}
}

// TestBuildFromFunction_LanguageFilterDisambiguates — the same name
// exists in Go and in Python; --language picks one.
func TestBuildFromFunction_LanguageFilterDisambiguates(t *testing.T) {
	root := newTempProject(t, map[string]string{
		"src/login.go": goLogin(),
		"src/login.py": pyLogin(),
	})

	c, err := BuildFromFunctionWith(
		context.Background(), root, "login", nil,
		SelectOptions{Language: "python"},
	)
	if err != nil {
		t.Fatalf("BuildFromFunctionWith(python): %v", err)
	}
	if c.Language != "python" {
		t.Errorf("expected python CFG, got language=%q", c.Language)
	}
	if !strings.HasSuffix(c.File, ".py") {
		t.Errorf("expected .py file, got %q", c.File)
	}

	c2, err := BuildFromFunctionWith(
		context.Background(), root, "login", nil,
		SelectOptions{Language: "go"},
	)
	if err != nil {
		t.Fatalf("BuildFromFunctionWith(go): %v", err)
	}
	if c2.Language != "go" {
		t.Errorf("expected go CFG, got language=%q", c2.Language)
	}
}

// TestBuildFromFunction_PolyglotAmbiguityListsBoth — without a filter
// we should see candidates from each language (go + ruby + python).
func TestBuildFromFunction_PolyglotAmbiguityListsBoth(t *testing.T) {
	root := newTempProject(t, map[string]string{
		"src/login.go": goLogin(),
		"src/login.py": pyLogin(),
		"src/login.rb": rubyLogin(),
	})

	_, err := BuildFromFunction(context.Background(), root, "login", nil)
	if err == nil {
		t.Fatalf("expected ambiguity error")
	}
	var amb *AmbiguousFunctionError
	if !errors.As(err, &amb) {
		t.Fatalf("expected *AmbiguousFunctionError, got %T", err)
	}
	if got := len(amb.Candidates); got != 3 {
		t.Errorf("expected 3 candidates across languages, got %d:\n%s",
			got, amb.Error())
	}
	langs := make(map[string]bool, 3)
	for _, c := range amb.Candidates {
		langs[c.Language] = true
	}
	for _, want := range []string{"go", "python", "ruby"} {
		if !langs[want] {
			t.Errorf("missing %s candidate; have %v", want, langs)
		}
	}
}

// TestBuildFromFunction_SingleMatchUnchanged — when exactly one
// definition exists the bare lookup keeps working with no flags.
func TestBuildFromFunction_SingleMatchUnchanged(t *testing.T) {
	root := newTempProject(t, map[string]string{
		"src/lonely.go": `package main

func unique(x int) int {
	if x > 0 {
		return x
	}
	return 0
}
`,
	})

	c, err := BuildFromFunction(context.Background(), root, "unique", nil)
	if err != nil {
		t.Fatalf("unexpected error for unique function: %v", err)
	}
	if c.Function != "unique" {
		t.Errorf("got function %q, want unique", c.Function)
	}
}

// TestBuildFromFunction_NotFoundWithFiltersHints — the "not found"
// branch of SelectOne should mention the filters that were applied so
// users know what to broaden.
func TestBuildFromFunction_NotFoundWithFiltersHints(t *testing.T) {
	root := newTempProject(t, map[string]string{
		"src/login.go": goLogin(),
	})

	_, err := BuildFromFunctionWith(
		context.Background(), root, "login", nil,
		SelectOptions{File: "doesnotexist"},
	)
	if err == nil {
		t.Fatalf("expected not-found error")
	}
	if !strings.Contains(err.Error(), "with the supplied filters") {
		t.Errorf("error should mention applied filters: %v", err)
	}
}

// TestSelectOptions_IsZero — small contract check used by the CLI when
// deciding whether the user supplied any disambiguators.
func TestSelectOptions_IsZero(t *testing.T) {
	if !(SelectOptions{}).IsZero() {
		t.Error("empty SelectOptions should be zero")
	}
	if (SelectOptions{File: "x"}).IsZero() {
		t.Error("SelectOptions with File set is not zero")
	}
	if (SelectOptions{Language: "go"}).IsZero() {
		t.Error("SelectOptions with Language set is not zero")
	}
	if (SelectOptions{Parent: "Foo"}).IsZero() {
		t.Error("SelectOptions with Parent set is not zero")
	}
}

// TestCandidate_StringFormat — disambiguation messages embed
// Candidate.String(); pin its format so future changes are intentional.
func TestCandidate_StringFormat(t *testing.T) {
	root := newTempProject(t, map[string]string{
		"src/login.go": goLogin(),
	})
	cands, err := FindFunctionSymbols(
		context.Background(), root, nil, "login", SelectOptions{},
	)
	if err != nil {
		t.Fatalf("FindFunctionSymbols: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}
	got := cands[0].String()
	// New format includes the kind ("function") so reviewers can tell
	// a method apart from a class apart from a constant in candidate
	// lists. The cfg-level helper only ever returns functions/methods,
	// hence the kind-suffix is "function" here.
	for _, want := range []string{"src/login.go:", "(go)", "[login function]"} {
		if !strings.Contains(got, want) {
			t.Errorf("Candidate.String() missing %q: %s", want, got)
		}
	}
}
