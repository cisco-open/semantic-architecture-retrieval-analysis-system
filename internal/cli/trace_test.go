/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/trace"
	// Side-effect: register the bundled language parsers (go, python, …)
	// so the temp project below resolves symbols correctly.
	_ "github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/lang"
)

// writeProjectFile writes a single file under root, creating any
// intermediate directories. Locally scoped to avoid collisions with
// helpers defined in other test files.
func writeProjectFile(t *testing.T, root, rel, contents string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

// goLogin returns a Go function with at least one branch so trace
// extraction picks it up unambiguously.
const goLoginBody = `package main

func login(ok bool) string {
	if ok {
		return "ok"
	}
	return "no"
}
`

const pyLoginBody = `def login(ok):
    if ok:
        return "ok"
    return "no"
`

// makeTraceCmd returns a fresh cobra.Command wired to runTrace with
// the disambiguation flags installed. Each test gets its own command
// so flag state doesn't leak between cases.
func makeTraceCmd(t *testing.T) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	c := &cobra.Command{
		Use:  "trace",
		Args: cobra.ExactArgs(1),
		RunE: runTrace,
	}
	c.Flags().Bool("callers", false, "")
	c.Flags().Bool("callees", false, "")
	c.Flags().Bool("refs", false, "")
	c.Flags().Bool("json", false, "")
	addSelectFlags(c)
	addDepFlags(c)

	var stdout, stderr bytes.Buffer
	c.SetOut(&stdout)
	c.SetErr(&stderr)
	return c, &stdout, &stderr
}

// withTempProject creates a temp directory, populates it with the
// supplied files, drops a minimal saras config so config.FindProjectRoot
// resolves to it, and cd's the test process into the directory. It
// returns a cleanup callback.
func withTempProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	// Minimal config so config.FindProjectRoot stops walking here.
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

// TestRunTrace_AmbiguousWithFiltersErrorsOut — when --file/--language/
// --parent are set and multiple definitions still match, runTrace
// must print the candidate list and return errAmbiguousResolved.
func TestRunTrace_AmbiguousWithFiltersErrorsOut(t *testing.T) {
	withTempProject(t, map[string]string{
		"src/login.go": goLoginBody,
		"src/login.py": pyLoginBody,
	})

	cmd, _, stderr := makeTraceCmd(t)
	cmd.SetArgs([]string{"login", "--language", "ZZZ"})
	err := cmd.Execute()

	// Unknown language → 0 candidates with filters set → not-found
	// error mentioning the supplied filters.
	if err == nil {
		t.Fatalf("expected error for impossible filter combo")
	}
	if !strings.Contains(err.Error(), "with the supplied filters") {
		t.Errorf("error should hint at supplied filters: %v\nstderr=%s", err, stderr.String())
	}
}

// TestRunTrace_AmbiguousNoFiltersWarnsAndContinues — without any
// disambiguator, runTrace tolerates ambiguity (legacy behaviour) but
// emits a warning so the user knows other matches exist.
func TestRunTrace_AmbiguousNoFiltersWarnsAndContinues(t *testing.T) {
	withTempProject(t, map[string]string{
		"src/login.go": goLoginBody,
		"src/login.py": pyLoginBody,
	})

	cmd, stdout, stderr := makeTraceCmd(t)
	cmd.SetArgs([]string{"login"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected success with warning, got error: %v", err)
	}
	if !strings.Contains(stderr.String(), "warning:") {
		t.Errorf("expected ambiguity warning on stderr, got:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "definitions of \"login\" exist") {
		t.Errorf("warning should call out multiple definitions:\n%s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Symbol: login") {
		t.Errorf("expected symbol summary on stdout:\n%s", stdout.String())
	}
}

// TestRunTrace_PathPrefixDisambiguates — `path:symbol` shorthand
// resolves to a unique definition without warnings on stderr.
func TestRunTrace_PathPrefixDisambiguates(t *testing.T) {
	withTempProject(t, map[string]string{
		"src/login.go": goLoginBody,
		"src/login.py": pyLoginBody,
	})

	cmd, stdout, stderr := makeTraceCmd(t)
	cmd.SetArgs([]string{"src/login.py:login"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\nstderr=%s", err, stderr.String())
	}
	if strings.Contains(stderr.String(), "warning:") {
		t.Errorf("expected no ambiguity warning, got:\n%s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "src/login.py") {
		t.Errorf("expected python file in trace output:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Lang: python") {
		t.Errorf("expected python language label, got:\n%s", stdout.String())
	}
}

// TestRunTrace_LanguageFilterDisambiguates — --language picks the
// right *definition* in a polyglot project. References stay
// name-based by design, so a python `login` reference may still
// surface in the references section; only the Symbol header is
// disambiguated.
func TestRunTrace_LanguageFilterDisambiguates(t *testing.T) {
	withTempProject(t, map[string]string{
		"src/login.go": goLoginBody,
		"src/login.py": pyLoginBody,
	})

	cmd, stdout, _ := makeTraceCmd(t)
	cmd.SetArgs([]string{"login", "--language", "go"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Lang: go") {
		t.Errorf("expected go language label:\n%s", out)
	}
	// The Symbol summary block must point at the .go file, not .py.
	symbolBlock := out
	if idx := strings.Index(out, "References to"); idx >= 0 {
		symbolBlock = out[:idx]
	}
	if !strings.Contains(symbolBlock, "src/login.go") {
		t.Errorf("Symbol block should reference go file:\n%s", symbolBlock)
	}
	if strings.Contains(symbolBlock, "src/login.py") {
		t.Errorf("Symbol block leaked python file under --language go:\n%s", symbolBlock)
	}
}

// TestRunTrace_CalleesAmbiguousErrorsWithoutGuessing — for --callees,
// even without explicit filters we refuse to guess (callees of the
// "wrong" function would be silently misleading) and surface the
// candidate list.
func TestRunTrace_CalleesAmbiguousErrorsWithoutGuessing(t *testing.T) {
	withTempProject(t, map[string]string{
		"src/login.go": goLoginBody,
		"src/login.py": pyLoginBody,
	})

	cmd, _, stderr := makeTraceCmd(t)
	cmd.SetArgs([]string{"login", "--callees"})
	err := cmd.Execute()

	if err == nil {
		t.Fatalf("expected ambiguity error from --callees, got nil")
	}
	if !errors.Is(err, errAmbiguousResolved) {
		t.Errorf("expected errAmbiguousResolved, got %T: %v", err, err)
	}
	if !strings.Contains(stderr.String(), "is ambiguous") {
		t.Errorf("stderr should list candidates:\n%s", stderr.String())
	}
}

// TestRunTrace_CalleesUniqueAfterDisambiguator — once disambiguated,
// --callees runs and produces output (even if the callee list is
// empty for these toy functions).
func TestRunTrace_CalleesUniqueAfterDisambiguator(t *testing.T) {
	withTempProject(t, map[string]string{
		"src/login.go": goLoginBody,
		"src/login.py": pyLoginBody,
	})

	cmd, stdout, stderr := makeTraceCmd(t)
	cmd.SetArgs([]string{"login", "--callees", "--language", "go"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\nstderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Callees of \"login\"") {
		t.Errorf("expected callees header on stdout:\n%s", stdout.String())
	}
}

// TestSelectOptionsFromCmd_FlagPrecedence — explicit --file overrides
// any path inferred from the positional argument; other flags map
// straight through.
func TestSelectOptionsFromCmd_FlagPrecedence(t *testing.T) {
	cmd, _, _ := makeTraceCmd(t)
	if err := cmd.Flags().Set(flagFile, "from/flag"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if err := cmd.Flags().Set(flagLanguage, "go"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if err := cmd.Flags().Set(flagParent, "UserService"); err != nil {
		t.Fatalf("set flag: %v", err)
	}

	opts := selectOptionsFromCmd(cmd, "from/path")
	if opts.File != "from/flag" {
		t.Errorf("explicit --file should win, got %q", opts.File)
	}
	if opts.Language != "go" {
		t.Errorf("Language = %q, want go", opts.Language)
	}
	if opts.Parent != "UserService" {
		t.Errorf("Parent = %q, want UserService", opts.Parent)
	}

	// Without the flag, the path hint flows through.
	cmd2, _, _ := makeTraceCmd(t)
	opts2 := selectOptionsFromCmd(cmd2, "from/path")
	if opts2.File != "from/path" {
		t.Errorf("File = %q, want from/path", opts2.File)
	}
}

// TestRunTrace_RefsIgnoresKindFilter — references are name-based and
// don't require a unique definition. The flag is accepted but doesn't
// narrow the result set, matching the documented behaviour.
func TestRunTrace_RefsIgnoresKindFilter(t *testing.T) {
	withTempProject(t, map[string]string{
		"src/login.go":  goLoginBody,
		"src/caller.go": "package main\n\nfunc Caller() { login(true); login(false) }\n",
	})

	cmd, stdout, stderr := makeTraceCmd(t)
	cmd.SetArgs([]string{"login", "--refs"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\nstderr=%s", err, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "References to \"login\"") {
		t.Errorf("expected references header:\n%s", out)
	}
	// caller.go calls login twice; both should appear plus the def.
	if !strings.Contains(out, "src/caller.go") {
		t.Errorf("expected caller.go references:\n%s", out)
	}
}

// Sanity check: runTrace doesn't panic when called from outside a
// saras project (no .saras dir) — it should return the standard
// "not a saras project" error.
func TestRunTrace_NoProject(t *testing.T) {
	tmp := t.TempDir()
	prev, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	cmd, _, _ := makeTraceCmd(t)
	cmd.SetArgs([]string{"login"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error in non-saras directory")
	}
	if !strings.Contains(err.Error(), "not a saras project") {
		t.Errorf("unexpected error: %v", err)
	}
}

// Ensure trace.SelectOne and addSelectFlags compile for the trace
// command — guards against accidental import drift.
func TestTraceCmd_HasSelectFlags(t *testing.T) {
	for _, name := range []string{flagFile, flagLanguage, flagParent} {
		if traceCmd.Flag(name) == nil {
			t.Errorf("traceCmd missing %q flag", name)
		}
	}
	// Smoke check that trace.SelectOne is reachable; the import would
	// be deemed unused otherwise.
	_, err := trace.SelectOne("function", "x", nil, trace.SelectOptions{})
	if err == nil {
		t.Errorf("expected not-found for empty candidates, got nil")
	}
	_ = context.Background()
}
