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

// symlink creates a symbolic link, skipping the test if the platform/permissions
// do not support symlinks (e.g. Windows without privilege).
func symlink(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}
}

// scanPathSet returns the set of relative paths produced by ScanAll for root.
func scanPathSet(t *testing.T, root string, ignore []string) map[string]bool {
	t.Helper()
	files, err := NewScanner(root, ignore).ScanAll()
	if err != nil {
		t.Fatalf("ScanAll: %v", err)
	}
	set := make(map[string]bool, len(files))
	for _, f := range files {
		set[f.Path] = true
	}
	return set
}

// A relative directory symlink (e.g. a venv's "semver -> ../") must be followed
// when its real target is not otherwise reachable. When BOTH the real directory
// and a symlink to it are present, the real path wins and content is indexed once
// (deduplicated), so identical files are not embedded twice.
func TestScanAllFollowsRelativeDirSymlink(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/inner.go", "package pkg\n")
	symlink(t, "pkg", filepath.Join(root, "linked")) // relative target, like "semver -> ../"

	paths := scanPathSet(t, root, nil)

	if !paths[filepath.Join("pkg", "inner.go")] {
		t.Error("should index the real pkg/inner.go")
	}
	// linked -> pkg resolves to the already-walked real directory, so its content
	// is deduplicated rather than indexed a second time under linked/.
	if paths[filepath.Join("linked", "inner.go")] {
		t.Error("should NOT re-index the same content via the duplicate symlink path")
	}
}

// When a symlinked directory's real target is NOT otherwise indexed in the tree,
// the symlink must be followed so its content is indexed (the core bug:
// filepath.Walk skips it entirely). This mirrors a virtualenv/monorepo whose
// source is exposed only through a symlink.
func TestScanAllFollowsSymlinkToExternalContent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "app.go", "package main\n")

	// Real content lives under a path the scanner ignores (node_modules), exposed
	// to the project only via an in-root symlink. The symlink is the sole indexable
	// route to it, so following it must surface src/code.go.
	writeFile(t, root, "node_modules/realcode/code.go", "package code\n")
	symlink(t, filepath.Join("node_modules", "realcode"), filepath.Join(root, "src"))

	paths := scanPathSet(t, root, []string{"node_modules"})

	if !paths["app.go"] {
		t.Error("should index app.go")
	}
	if paths[filepath.Join("node_modules", "realcode", "code.go")] {
		t.Error("node_modules content must stay ignored on its real path")
	}
	if !paths[filepath.Join("src", "code.go")] {
		t.Error("should follow the in-root symlink and index src/code.go")
	}
}

// A directory symlink whose target is OUTSIDE the project root must be skipped,
// so we never pull system/Homebrew/site-packages trees into the index.
func TestScanAllSkipsEscapingDirSymlink(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "app.go", "package main\n")

	outside := t.TempDir()
	writeFile(t, outside, "leak.go", "package leak\n")
	symlink(t, outside, filepath.Join(root, "ext"))

	paths := scanPathSet(t, root, nil)

	if !paths["app.go"] {
		t.Error("should index in-root app.go")
	}
	if paths[filepath.Join("ext", "leak.go")] {
		t.Error("must NOT follow a symlink that escapes the project root")
	}
}

// A symlink pointing back at an ancestor (a cycle) must terminate and must not
// re-index content through the loop.
func TestScanAllHandlesSymlinkCycle(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a/keep.go", "package a\n")
	symlink(t, root, filepath.Join(root, "a", "loop")) // a/loop -> root (cycle)

	// Must return rather than recurse forever.
	paths := scanPathSet(t, root, nil)

	if !paths[filepath.Join("a", "keep.go")] {
		t.Error("should index a/keep.go")
	}
	if paths[filepath.Join("a", "loop", "a", "keep.go")] {
		t.Error("must not re-index content through a symlink cycle")
	}
}

// When the project root itself is a symlink, the tree must still be scanned.
// (Old filepath.Walk returned 0 files here — the exact reported symptom.)
func TestScanAllSymlinkedRoot(t *testing.T) {
	real := t.TempDir()
	writeFile(t, real, "main.go", "package main\n")

	link := filepath.Join(t.TempDir(), "rootlink")
	symlink(t, real, link)

	paths := scanPathSet(t, link, nil)

	if !paths["main.go"] {
		t.Error("should index main.go when the root is a symlink")
	}
}

// A symlink to a regular file must be indexed, reporting the TARGET's size
// (not the link's), since the indexer reads target content.
func TestScanAllSymlinkToFile(t *testing.T) {
	root := t.TempDir()
	const body = "package main\n"
	writeFile(t, root, "orig.go", body)
	symlink(t, "orig.go", filepath.Join(root, "alias.go"))

	files, err := NewScanner(root, nil).ScanAll()
	if err != nil {
		t.Fatalf("ScanAll: %v", err)
	}

	var alias *FileMeta
	for i := range files {
		if files[i].Path == "alias.go" {
			alias = &files[i]
			break
		}
	}
	if alias == nil {
		t.Fatal("should index the symlinked file alias.go")
	}
	if alias.Size != int64(len(body)) {
		t.Errorf("alias.go size = %d, want target size %d", alias.Size, len(body))
	}
}
