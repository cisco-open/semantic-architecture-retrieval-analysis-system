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

	// Side-effect: register bundled language parsers (python, ruby, shell, ...).
	_ "github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/lang"
)

// writeTempFile writes content to a fresh temp file with the given name and
// returns the temp directory.
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return dir
}

// ---------------------------------------------------------------------------
// Python (indent style)
// ---------------------------------------------------------------------------

func TestPython_IfElifElse(t *testing.T) {
	src := `def sign(x):
    if x > 0:
        return "pos"
    elif x < 0:
        return "neg"
    else:
        return "zero"
`
	dir := writeTempFile(t, "sign.py", src)
	c, err := BuildFromFunction(context.Background(), dir, "sign", nil)
	if err != nil {
		t.Fatalf("BuildFromFunction: %v", err)
	}
	if c.Language != "python" {
		t.Errorf("Language = %q, want python", c.Language)
	}
	paths := c.EnumeratePaths(0)
	if len(paths) != 3 {
		t.Errorf("paths = %d, want 3 (pos/neg/zero)\n%s", len(paths), c.ToText())
	}
	// Branch labels.
	var hasTrue, hasFalse bool
	for _, e := range c.Edges {
		blk := c.blockByID(e.From)
		if blk != nil && blk.Kind == KindBranch {
			if e.Label == "true" {
				hasTrue = true
			}
			if e.Label == "false" {
				hasFalse = true
			}
		}
	}
	if !hasTrue || !hasFalse {
		t.Errorf("branch labels missing: true=%v false=%v\n%s", hasTrue, hasFalse, c.ToText())
	}
}

func TestPython_ForLoop(t *testing.T) {
	src := `def total(items):
    s = 0
    for x in items:
        s += x
    return s
`
	dir := writeTempFile(t, "total.py", src)
	c, err := BuildFromFunction(context.Background(), dir, "total", nil)
	if err != nil {
		t.Fatalf("BuildFromFunction: %v", err)
	}
	var hasLoop, hasBack bool
	for _, b := range c.Blocks {
		if b.Kind == KindLoopHead {
			hasLoop = true
		}
	}
	for _, e := range c.Edges {
		if e.Back {
			hasBack = true
		}
	}
	if !hasLoop {
		t.Errorf("expected a loop-head block\n%s", c.ToText())
	}
	if !hasBack {
		t.Errorf("expected a back-edge\n%s", c.ToText())
	}
}

func TestPython_EarlyReturn(t *testing.T) {
	src := `def find(items, target):
    if items is None:
        return -1
    for i, v in enumerate(items):
        if v == target:
            return i
    return -1
`
	dir := writeTempFile(t, "find.py", src)
	c, err := BuildFromFunction(context.Background(), dir, "find", nil)
	if err != nil {
		t.Fatalf("BuildFromFunction: %v", err)
	}
	paths := c.EnumeratePaths(0)
	if len(paths) < 3 {
		t.Errorf("paths = %d, want >=3 (None / found / not-found)\n%s",
			len(paths), c.ToText())
	}
}

func TestPython_StringsAndCommentsDoNotConfuseParser(t *testing.T) {
	// A docstring and a comment both containing the word `if` must not
	// trigger a spurious branch.
	src := `def f():
    """if this looks like control flow it shouldn't be parsed"""
    # also this: if True
    return 1
`
	dir := writeTempFile(t, "f.py", src)
	c, err := BuildFromFunction(context.Background(), dir, "f", nil)
	if err != nil {
		t.Fatalf("BuildFromFunction: %v", err)
	}
	branches := 0
	for _, b := range c.Blocks {
		if b.Kind == KindBranch {
			branches++
		}
	}
	if branches != 0 {
		t.Errorf("expected 0 branches, got %d:\n%s", branches, c.ToText())
	}
}

// ---------------------------------------------------------------------------
// Ruby (end-keyword style)
// ---------------------------------------------------------------------------

func TestRuby_IfElsifElse(t *testing.T) {
	src := `def sign(x)
  if x > 0
    return "pos"
  elsif x < 0
    return "neg"
  else
    return "zero"
  end
end
`
	dir := writeTempFile(t, "sign.rb", src)
	c, err := BuildFromFunction(context.Background(), dir, "sign", nil)
	if err != nil {
		t.Fatalf("BuildFromFunction: %v", err)
	}
	if c.Language != "ruby" {
		t.Errorf("Language = %q, want ruby", c.Language)
	}
	paths := c.EnumeratePaths(0)
	if len(paths) != 3 {
		t.Errorf("paths = %d, want 3\n%s", len(paths), c.ToText())
	}
}

func TestRuby_CaseWhen(t *testing.T) {
	src := `def label(x)
  case x
  when 1
    return "one"
  when 2
    return "two"
  else
    return "other"
  end
end
`
	dir := writeTempFile(t, "label.rb", src)
	c, err := BuildFromFunction(context.Background(), dir, "label", nil)
	if err != nil {
		t.Fatalf("BuildFromFunction: %v", err)
	}
	var hasSwitch bool
	for _, b := range c.Blocks {
		if b.Kind == KindSwitch {
			hasSwitch = true
		}
	}
	if !hasSwitch {
		t.Errorf("expected a switch block\n%s", c.ToText())
	}
	paths := c.EnumeratePaths(0)
	if len(paths) < 3 {
		t.Errorf("paths = %d, want >=3\n%s", len(paths), c.ToText())
	}
}

func TestRuby_WhileLoop(t *testing.T) {
	src := `def countdown(n)
  while n > 0
    n -= 1
  end
  return n
end
`
	dir := writeTempFile(t, "countdown.rb", src)
	c, err := BuildFromFunction(context.Background(), dir, "countdown", nil)
	if err != nil {
		t.Fatalf("BuildFromFunction: %v", err)
	}
	var hasLoop, hasBack bool
	for _, b := range c.Blocks {
		if b.Kind == KindLoopHead {
			hasLoop = true
		}
	}
	for _, e := range c.Edges {
		if e.Back {
			hasBack = true
		}
	}
	if !hasLoop {
		t.Errorf("expected loop head\n%s", c.ToText())
	}
	if !hasBack {
		t.Errorf("expected back-edge\n%s", c.ToText())
	}
}

// ---------------------------------------------------------------------------
// Shell
// ---------------------------------------------------------------------------

func TestShell_IfElifElse(t *testing.T) {
	src := `#!/bin/bash
sign() {
  if [ "$1" -gt 0 ]; then
    return 1
  elif [ "$1" -lt 0 ]; then
    return 2
  else
    return 3
  fi
}
`
	dir := writeTempFile(t, "sign.sh", src)
	c, err := BuildFromFunction(context.Background(), dir, "sign", nil)
	if err != nil {
		t.Fatalf("BuildFromFunction: %v", err)
	}
	if c.Language != "shell" {
		t.Errorf("Language = %q, want shell", c.Language)
	}
	paths := c.EnumeratePaths(0)
	if len(paths) != 3 {
		t.Errorf("paths = %d, want 3\n%s", len(paths), c.ToText())
	}
}

func TestShell_ForLoop(t *testing.T) {
	src := `#!/bin/bash
sum() {
  total=0
  for x in 1 2 3; do
    total=$((total + x))
  done
  echo $total
}
`
	dir := writeTempFile(t, "sum.sh", src)
	c, err := BuildFromFunction(context.Background(), dir, "sum", nil)
	if err != nil {
		t.Fatalf("BuildFromFunction: %v", err)
	}
	var hasLoop bool
	for _, b := range c.Blocks {
		if b.Kind == KindLoopHead {
			hasLoop = true
		}
	}
	if !hasLoop {
		t.Errorf("expected loop head\n%s", c.ToText())
	}
}

func TestShell_Case(t *testing.T) {
	src := `#!/bin/bash
classify() {
  case "$1" in
    foo)
      return 1
      ;;
    bar)
      return 2
      ;;
    *)
      return 3
      ;;
  esac
}
`
	dir := writeTempFile(t, "classify.sh", src)
	c, err := BuildFromFunction(context.Background(), dir, "classify", nil)
	if err != nil {
		t.Fatalf("BuildFromFunction: %v", err)
	}
	var hasSwitch bool
	for _, b := range c.Blocks {
		if b.Kind == KindSwitch {
			hasSwitch = true
		}
	}
	if !hasSwitch {
		t.Errorf("expected a switch block\n%s", c.ToText())
	}
}

// ---------------------------------------------------------------------------
// Registry sanity checks
// ---------------------------------------------------------------------------

func TestStyleForLanguage(t *testing.T) {
	cases := []struct {
		name string
		want Style
	}{
		{"go", StyleBrace},
		{"javascript", StyleBrace},
		{"rust", StyleBrace},
		{"php", StyleBrace},
		{"perl", StyleBrace},
		{"zig", StyleBrace},
		{"python", StyleIndent},
		{"python2", StyleIndent},
		{"ruby", StyleEnd},
		{"shell", StyleShell},
		{"json", StyleUnsupported},
		{"yaml", StyleUnsupported},
		{"markdown", StyleUnsupported},
		{"makefile", StyleUnsupported},
		{"madeup", StyleUnsupported},
	}
	for _, c := range cases {
		if got := StyleForLanguage(c.name); got != c.want {
			t.Errorf("StyleForLanguage(%q) = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestSupportedLanguagesIncludesNewFamilies(t *testing.T) {
	langs := SupportedLanguages()
	wantSome := []string{"python", "ruby", "shell", "go", "javascript"}
	for _, w := range wantSome {
		found := false
		for _, l := range langs {
			if l == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in SupportedLanguages(), got %v", w, langs)
		}
	}
	// And explicitly-unsupported markup must NOT be there.
	for _, l := range langs {
		if l == "json" || l == "yaml" || l == "markdown" {
			t.Errorf("did not expect %q in SupportedLanguages(): %v", l, langs)
		}
	}
}

func TestStyleStrings(t *testing.T) {
	if Style(99).String() != "unsupported" {
		t.Error("unknown style should stringify as unsupported")
	}
	pairs := map[Style]string{
		StyleBrace: "brace", StyleIndent: "indent",
		StyleEnd: "end", StyleShell: "shell",
		StyleUnsupported: "unsupported",
	}
	for s, want := range pairs {
		if got := s.String(); got != want {
			t.Errorf("%v.String() = %q, want %q", s, got, want)
		}
	}
}

// Sanity: the brace parser must still work for Go (regression guard).
func TestBraceStillWorksForGo(t *testing.T) {
	src := `package p

func g(x int) int {
	if x > 0 {
		return 1
	}
	return 0
}
`
	dir := writeTempFile(t, "g.go", src)
	c, err := BuildFromFunction(context.Background(), dir, "g", nil)
	if err != nil {
		t.Fatalf("BuildFromFunction: %v", err)
	}
	if c.Language != "go" {
		t.Errorf("Language = %q, want go", c.Language)
	}
	if !strings.Contains(c.ToText(), "branch") {
		t.Errorf("expected a branch in:\n%s", c.ToText())
	}
}
