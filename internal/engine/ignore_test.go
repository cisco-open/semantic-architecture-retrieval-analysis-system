/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIgnoreMatcherIgnoreList(t *testing.T) {
	root := t.TempDir()
	m := NewIgnoreMatcher(root, []string{"node_modules", "vendor", ".git"})

	if !m.IsNameIgnored("node_modules") {
		t.Error("expected node_modules to be ignored")
	}
	if !m.IsNameIgnored("vendor") {
		t.Error("expected vendor to be ignored")
	}
	if m.IsNameIgnored("src") {
		t.Error("src should not be ignored")
	}
}

func TestIgnoreMatcherPathComponents(t *testing.T) {
	root := t.TempDir()
	m := NewIgnoreMatcher(root, []string{"node_modules", "vendor"})

	// A file nested inside node_modules should be ignored even when
	// checking the full relative path.
	if !m.IsIgnored(filepath.Join("node_modules", "pkg", "index.js"), false) {
		t.Error("expected node_modules/pkg/index.js to be ignored")
	}
	if !m.IsIgnored(filepath.Join("vendor", "dep", "dep.go"), false) {
		t.Error("expected vendor/dep/dep.go to be ignored")
	}
	if m.IsIgnored("src/main.go", false) {
		t.Error("src/main.go should not be ignored")
	}
}

func TestIgnoreMatcherGitignore(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".gitignore", "*.log\nbuild/\n")

	m := NewIgnoreMatcher(root, nil)

	if !m.IsIgnored("debug.log", false) {
		t.Error("expected debug.log to match *.log gitignore pattern")
	}
	if !m.IsIgnored("build", true) {
		t.Error("expected build/ directory to be ignored")
	}
	if m.IsIgnored("main.go", false) {
		t.Error("main.go should not be ignored")
	}
}

func TestIgnoreMatcherSarasignore(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".sarasignore", "*.generated.go\nfixtures/\n")

	m := NewIgnoreMatcher(root, nil)

	if !m.IsIgnored("foo.generated.go", false) {
		t.Error("expected foo.generated.go to match .sarasignore pattern")
	}
	if !m.IsIgnored("fixtures", true) {
		t.Error("expected fixtures/ directory to be ignored via .sarasignore")
	}
	if m.IsIgnored("main.go", false) {
		t.Error("main.go should not be ignored")
	}
}

func TestIgnoreMatcherCombined(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".gitignore", "*.log\n")
	writeFile(t, root, ".sarasignore", "*.generated.go\n")

	m := NewIgnoreMatcher(root, []string{"node_modules"})

	// gitignore pattern
	if !m.IsIgnored("debug.log", false) {
		t.Error("expected debug.log to be ignored via .gitignore")
	}
	// sarasignore pattern
	if !m.IsIgnored("foo.generated.go", false) {
		t.Error("expected foo.generated.go to be ignored via .sarasignore")
	}
	// ignore list
	if !m.IsIgnored("node_modules", true) {
		t.Error("expected node_modules to be ignored via ignore list")
	}
	// should pass
	if m.IsIgnored("main.go", false) {
		t.Error("main.go should not be ignored")
	}
}

func TestScanAllWithSarasignore(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".sarasignore", "*.generated.go\nsecrets/\n")
	writeFile(t, root, "app.go", "package main\n")
	writeFile(t, root, "types.generated.go", "package main\n")
	writeFile(t, root, "secrets/key.go", "package secrets\n")

	s := NewScanner(root, nil)
	files, err := s.ScanAll()
	if err != nil {
		t.Fatal(err)
	}

	paths := make(map[string]bool)
	for _, f := range files {
		paths[f.Path] = true
	}

	if !paths["app.go"] {
		t.Error("should include app.go")
	}
	if paths["types.generated.go"] {
		t.Error("should exclude types.generated.go (sarasignored)")
	}
	if paths[filepath.Join("secrets", "key.go")] {
		t.Error("should exclude secrets/ directory (sarasignored)")
	}
}

func TestScanAllIncludesSarasignoreFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".sarasignore", "*.log\n")
	writeFile(t, root, "app.go", "package main\n")

	s := NewScanner(root, nil)
	files, _ := s.ScanAll()

	paths := make(map[string]bool)
	for _, f := range files {
		paths[f.Path] = true
	}

	// .sarasignore itself should be discoverable (not hidden-skipped)
	if !paths[".sarasignore"] {
		t.Error("should include .sarasignore file in scan results")
	}
}

func TestWatcherIgnoresSarasignorePatterns(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".sarasignore", "generated/\n")
	genDir := filepath.Join(root, "generated")
	os.MkdirAll(genDir, 0755)

	w, err := NewWatcher(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.fsWatcher.Close()

	// The IgnoreMatcher should recognize the generated/ directory
	if !w.ignorer.IsIgnored("generated", true) {
		t.Error("expected generated/ to be ignored via .sarasignore in watcher")
	}
}
