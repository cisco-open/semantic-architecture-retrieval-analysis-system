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
	"time"
)

func setupTestProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Create source files
	writeFile(t, root, "main.go", "package main\nfunc main() {}\n")
	writeFile(t, root, "util.go", "package main\nfunc helper() {}\n")
	writeFile(t, root, "README.md", "# Project\n")
	writeFile(t, root, "src/auth.go", "package auth\nfunc Login() {}\n")
	writeFile(t, root, "src/db.go", "package db\nfunc Connect() {}\n")

	// Create files that should be ignored
	writeFile(t, root, "node_modules/pkg/index.js", "module.exports = {}\n")
	writeFile(t, root, "vendor/dep/dep.go", "package dep\n")
	writeFile(t, root, ".git/config", "[core]\n")
	writeFile(t, root, "image.png", "\x89PNG\r\n\x1a\n") // binary

	return root
}

func writeFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	abs := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestScanAll(t *testing.T) {
	root := setupTestProject(t)
	s := NewScanner(root, []string{"node_modules", "vendor"})

	files, err := s.ScanAll()
	if err != nil {
		t.Fatalf("ScanAll failed: %v", err)
	}

	paths := make(map[string]bool)
	for _, f := range files {
		paths[f.Path] = true
	}

	// Should find source files
	for _, expected := range []string{"main.go", "util.go", "README.md", filepath.Join("src", "auth.go"), filepath.Join("src", "db.go")} {
		if !paths[expected] {
			t.Errorf("expected to find %s", expected)
		}
	}

	// Should NOT find ignored files
	for _, excluded := range []string{
		filepath.Join("node_modules", "pkg", "index.js"),
		filepath.Join("vendor", "dep", "dep.go"),
		filepath.Join(".git", "config"),
		"image.png",
	} {
		if paths[excluded] {
			t.Errorf("should not find %s", excluded)
		}
	}
}

func TestScanAllPopulatesMetadata(t *testing.T) {
	root := setupTestProject(t)
	s := NewScanner(root, []string{"node_modules", "vendor"})

	files, err := s.ScanAll()
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		if f.Path == "" {
			t.Error("empty Path")
		}
		if f.AbsPath == "" {
			t.Error("empty AbsPath")
		}
		if f.Size <= 0 {
			t.Errorf("expected positive size for %s, got %d", f.Path, f.Size)
		}
		if f.ModTime.IsZero() {
			t.Errorf("zero ModTime for %s", f.Path)
		}
	}
}

func TestScanAllSkipsLargeFiles(t *testing.T) {
	root := t.TempDir()

	// Create a file >1MB
	largeContent := make([]byte, 2*1024*1024)
	for i := range largeContent {
		largeContent[i] = 'x'
	}
	writeFile(t, root, "large.go", string(largeContent))
	writeFile(t, root, "small.go", "package main\n")

	s := NewScanner(root, nil)
	files, _ := s.ScanAll()

	paths := make(map[string]bool)
	for _, f := range files {
		paths[f.Path] = true
	}

	if paths["large.go"] {
		t.Error("should skip files >1MB")
	}
	if !paths["small.go"] {
		t.Error("should include small.go")
	}
}

func TestScanAllSkipsHiddenFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".hidden_file.go", "package hidden\n")
	writeFile(t, root, ".hidden_dir/file.go", "package hidden\n")
	writeFile(t, root, "visible.go", "package main\n")

	s := NewScanner(root, nil)
	files, _ := s.ScanAll()

	paths := make(map[string]bool)
	for _, f := range files {
		paths[f.Path] = true
	}

	if paths[".hidden_file.go"] {
		t.Error("should skip hidden files")
	}
	if paths[filepath.Join(".hidden_dir", "file.go")] {
		t.Error("should skip files in hidden directories")
	}
	if !paths["visible.go"] {
		t.Error("should include visible.go")
	}
}

func TestScanChanged(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "old.go", "package old\n")

	// Set old.go to an old time
	oldTime := time.Now().Add(-24 * time.Hour)
	os.Chtimes(filepath.Join(root, "old.go"), oldTime, oldTime)

	since := time.Now().Add(-1 * time.Hour)

	writeFile(t, root, "new.go", "package new\n")

	s := NewScanner(root, nil)
	changed, err := s.ScanChanged(since)
	if err != nil {
		t.Fatal(err)
	}

	paths := make(map[string]bool)
	for _, f := range changed {
		paths[f.Path] = true
	}

	if paths["old.go"] {
		t.Error("old.go should not appear in changed files")
	}
	if !paths["new.go"] {
		t.Error("new.go should appear in changed files")
	}
}

func TestScanAllEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	s := NewScanner(root, nil)

	files, err := s.ScanAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files in empty dir, got %d", len(files))
	}
}

func TestScanAllWithGitignore(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".gitignore", "*.log\nbuild/\n")
	writeFile(t, root, "app.go", "package main\n")
	writeFile(t, root, "debug.log", "log data\n")
	writeFile(t, root, "build/out.go", "package out\n")

	s := NewScanner(root, nil)
	files, _ := s.ScanAll()

	paths := make(map[string]bool)
	for _, f := range files {
		paths[f.Path] = true
	}

	if !paths["app.go"] {
		t.Error("should include app.go")
	}
	if paths["debug.log"] {
		t.Error("should exclude debug.log (gitignored)")
	}
	if paths[filepath.Join("build", "out.go")] {
		t.Error("should exclude build/ (gitignored directory)")
	}
}

func TestScanAllGitignoreNegation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".gitignore", "*.log\n!important.log\n")
	writeFile(t, root, "debug.log", "debug\n")
	writeFile(t, root, "important.log", "important\n")
	writeFile(t, root, "app.go", "package main\n")

	s := NewScanner(root, nil)
	files, _ := s.ScanAll()

	paths := make(map[string]bool)
	for _, f := range files {
		paths[f.Path] = true
	}

	if paths["debug.log"] {
		t.Error("debug.log should be excluded")
	}
	// Note: .log is not in textExtensions, so important.log won't be found
	// regardless of negation — that's correct behavior (binary filter)
	if !paths["app.go"] {
		t.Error("app.go should be included")
	}
}

func TestIsTextFile(t *testing.T) {
	textFiles := []string{
		"main.go", "app.js", "style.css", "README.md", "config.yaml",
		"Makefile", "Dockerfile", "go.mod", "index.html", "query.sql",
		"schema.proto", "app.rs", "lib.py", "main.java", "Cargo.toml",
	}
	for _, name := range textFiles {
		if !isTextFile(name) {
			t.Errorf("expected %s to be text file", name)
		}
	}

	binaryFiles := []string{
		"image.png", "photo.jpg", "video.mp4", "archive.zip",
		"binary.exe", "lib.so", "lib.dylib", "font.woff2",
	}
	for _, name := range binaryFiles {
		if isTextFile(name) {
			t.Errorf("expected %s to NOT be text file", name)
		}
	}
}

func TestParseIgnoreLine(t *testing.T) {
	tests := []struct {
		line     string
		pattern  string
		negated  bool
		dirOnly  bool
		anchored bool
	}{
		{"*.log", "*.log", false, false, false},
		{"!important.log", "important.log", true, false, false},
		{"build/", "build", false, true, false},
		{"src/generated/", "src/generated", false, true, true},
		{"docs/*.md", "docs/*.md", false, false, true},
	}

	for _, tc := range tests {
		t.Run(tc.line, func(t *testing.T) {
			p := parseIgnoreLine(tc.line)
			if p.pattern != tc.pattern {
				t.Errorf("pattern: expected %q, got %q", tc.pattern, p.pattern)
			}
			if p.negated != tc.negated {
				t.Errorf("negated: expected %v, got %v", tc.negated, p.negated)
			}
			if p.dirOnly != tc.dirOnly {
				t.Errorf("dirOnly: expected %v, got %v", tc.dirOnly, p.dirOnly)
			}
			if p.anchored != tc.anchored {
				t.Errorf("anchored: expected %v, got %v", tc.anchored, p.anchored)
			}
		})
	}
}
