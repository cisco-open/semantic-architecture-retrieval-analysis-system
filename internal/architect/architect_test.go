/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package architect

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupArchitectProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// go.mod
	writeFile(t, filepath.Join(root, "go.mod"), `module github.com/test/myapp

go 1.21
`)

	// main.go
	writeFile(t, filepath.Join(root, "main.go"), `package main

import "fmt"

func main() {
	fmt.Println("hello")
	serve()
}

func serve() {
	fmt.Println("serving")
}
`)

	// internal/auth
	authDir := filepath.Join(root, "internal", "auth")
	os.MkdirAll(authDir, 0755)

	writeFile(t, filepath.Join(authDir, "auth.go"), `package auth

func Login(user, pass string) error {
	return validate(user, pass)
}

func validate(user, pass string) error {
	return nil
}
`)

	writeFile(t, filepath.Join(authDir, "session.go"), `package auth

type Session struct {
	UserID string
	Token  string
}

func NewSession(userID string) *Session {
	return &Session{UserID: userID}
}
`)

	// internal/db
	dbDir := filepath.Join(root, "internal", "db")
	os.MkdirAll(dbDir, 0755)

	writeFile(t, filepath.Join(dbDir, "db.go"), `package db

type Database interface {
	Connect(dsn string) error
	Close() error
}

type PgStore struct {
	DSN string
}

func NewPgStore(dsn string) *PgStore {
	return &PgStore{DSN: dsn}
}

func (p *PgStore) Connect(dsn string) error {
	return nil
}

func (p *PgStore) Close() error {
	return nil
}
`)

	return root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// GenerateMap tests
// ---------------------------------------------------------------------------

func TestGenerateMap(t *testing.T) {
	root := setupArchitectProject(t)
	mapper := NewMapper(root, nil)

	cmap, err := mapper.GenerateMap(context.Background())
	if err != nil {
		t.Fatalf("GenerateMap: %v", err)
	}

	if cmap.TotalFiles == 0 {
		t.Error("expected files")
	}
	if cmap.TotalLines == 0 {
		t.Error("expected lines")
	}
	if len(cmap.Packages) == 0 {
		t.Error("expected packages")
	}
	if len(cmap.Symbols) == 0 {
		t.Error("expected symbols")
	}
}

func TestGenerateMapPackages(t *testing.T) {
	root := setupArchitectProject(t)
	mapper := NewMapper(root, nil)

	cmap, err := mapper.GenerateMap(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	pkgPaths := make(map[string]bool)
	for _, p := range cmap.Packages {
		pkgPaths[p.Path] = true
	}

	if !pkgPaths["."] {
		t.Error("expected root package")
	}
	if !pkgPaths["internal/auth"] {
		t.Error("expected internal/auth package")
	}
	if !pkgPaths["internal/db"] {
		t.Error("expected internal/db package")
	}
}

func TestGenerateMapPackageNames(t *testing.T) {
	root := setupArchitectProject(t)
	mapper := NewMapper(root, nil)

	cmap, err := mapper.GenerateMap(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range cmap.Packages {
		if p.Name == "" {
			t.Errorf("package %s has no name", p.Path)
		}
	}
}

func TestGenerateMapPackageSymbolCounts(t *testing.T) {
	root := setupArchitectProject(t)
	mapper := NewMapper(root, nil)

	cmap, err := mapper.GenerateMap(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range cmap.Packages {
		if p.Path == "internal/auth" {
			if p.Functions == 0 {
				t.Error("expected functions in auth package")
			}
			if p.Types == 0 {
				t.Error("expected types in auth package")
			}
		}
		if p.Path == "internal/db" {
			if p.Interfaces == 0 {
				t.Error("expected interfaces in db package")
			}
		}
	}
}

func TestGenerateMapIgnoresDirs(t *testing.T) {
	root := setupArchitectProject(t)

	vendorDir := filepath.Join(root, "vendor")
	os.MkdirAll(vendorDir, 0755)
	writeFile(t, filepath.Join(vendorDir, "lib.go"), `package vendor
func Lib() {}`)

	mapper := NewMapper(root, []string{"vendor"})
	cmap, err := mapper.GenerateMap(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range cmap.Packages {
		if p.Path == "vendor" {
			t.Error("vendor should be ignored")
		}
	}
}

func TestGenerateMapIgnoresHiddenDirs(t *testing.T) {
	root := setupArchitectProject(t)

	hiddenDir := filepath.Join(root, ".hidden")
	os.MkdirAll(hiddenDir, 0755)
	writeFile(t, filepath.Join(hiddenDir, "secret.go"), `package hidden
func Secret() {}`)

	mapper := NewMapper(root, nil)
	cmap, err := mapper.GenerateMap(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range cmap.Packages {
		if strings.HasPrefix(p.Path, ".hidden") {
			t.Error("hidden dir should be ignored")
		}
	}
}

// ---------------------------------------------------------------------------
// GenerateTree tests
// ---------------------------------------------------------------------------

func TestGenerateTree(t *testing.T) {
	root := setupArchitectProject(t)
	mapper := NewMapper(root, nil)

	tree, err := mapper.GenerateTree(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if tree == "" {
		t.Error("expected non-empty tree")
	}
	if !strings.Contains(tree, "main.go") {
		t.Error("expected main.go in tree")
	}
	if !strings.Contains(tree, "internal/") || !strings.Contains(tree, "auth") {
		t.Error("expected internal/auth in tree")
	}
}

func TestGenerateTreeIgnoresHidden(t *testing.T) {
	root := setupArchitectProject(t)

	os.MkdirAll(filepath.Join(root, ".git"), 0755)
	writeFile(t, filepath.Join(root, ".git", "config"), "gitconfig")

	mapper := NewMapper(root, nil)
	tree, err := mapper.GenerateTree(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(tree, ".git") {
		t.Error("tree should not contain .git")
	}
}

// ---------------------------------------------------------------------------
// GenerateMarkdown tests
// ---------------------------------------------------------------------------

func TestGenerateMarkdown(t *testing.T) {
	root := setupArchitectProject(t)
	mapper := NewMapper(root, nil)

	md, err := mapper.GenerateMarkdown(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(md, "# Codebase Architecture") {
		t.Error("expected markdown header")
	}
	if !strings.Contains(md, "## Packages") {
		t.Error("expected packages section")
	}
	if !strings.Contains(md, "auth") {
		t.Error("expected auth package in markdown")
	}
	if !strings.Contains(md, "Functions") {
		t.Error("expected function count")
	}
}

func TestGenerateMarkdownTypes(t *testing.T) {
	root := setupArchitectProject(t)
	mapper := NewMapper(root, nil)

	md, err := mapper.GenerateMarkdown(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(md, "Types & Interfaces") {
		t.Error("expected Types & Interfaces section")
	}
}

func TestGenerateMarkdownFunctions(t *testing.T) {
	root := setupArchitectProject(t)
	mapper := NewMapper(root, nil)

	md, err := mapper.GenerateMarkdown(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(md, "Key Functions") {
		t.Error("expected Key Functions section")
	}
	if !strings.Contains(md, "Login") {
		t.Error("expected Login function in markdown")
	}
}

// ---------------------------------------------------------------------------
// Context cancellation
// ---------------------------------------------------------------------------

func TestGenerateMapCancelled(t *testing.T) {
	root := setupArchitectProject(t)
	mapper := NewMapper(root, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := mapper.GenerateMap(ctx)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

// ---------------------------------------------------------------------------
// Helper tests
// ---------------------------------------------------------------------------

func TestCountLines(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.go")
	writeFile(t, path, "line1\nline2\nline3\n")

	lines, err := countLines(path)
	if err != nil {
		t.Fatal(err)
	}
	if lines != 4 { // 3 newlines + trailing
		t.Errorf("expected 4 lines, got %d", lines)
	}
}

func TestCountLinesEmpty(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "empty.go")
	writeFile(t, path, "")

	lines, err := countLines(path)
	if err != nil {
		t.Fatal(err)
	}
	if lines != 0 {
		t.Errorf("expected 0 lines for empty file, got %d", lines)
	}
}

func TestExtractPackageName(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.go")
	writeFile(t, path, "package mypackage\n\nfunc Test() {}\n")

	name := extractPackageName(path)
	if name != "mypackage" {
		t.Errorf("expected mypackage, got %s", name)
	}
}

func TestExtractImports(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.go")
	writeFile(t, path, `package main

import (
	"fmt"
	"os"
	"github.com/test/myapp/internal/auth"
)

func main() {}
`)

	imports := extractImports(path)
	if len(imports) != 3 {
		t.Errorf("expected 3 imports, got %d: %v", len(imports), imports)
	}
}

func TestExtractImportsSingle(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.go")
	writeFile(t, path, `package main

import "fmt"

func main() {}
`)

	imports := extractImports(path)
	if len(imports) != 1 {
		t.Errorf("expected 1 import, got %d", len(imports))
	}
}

func TestLastPathSegments(t *testing.T) {
	tests := []struct {
		path string
		n    int
		want string
	}{
		{"github.com/test/myapp/internal/auth", 2, "internal/auth"},
		{"fmt", 2, "fmt"},
		{"a/b/c", 1, "c"},
	}
	for _, tt := range tests {
		got := lastPathSegments(tt.path, tt.n)
		if got != tt.want {
			t.Errorf("lastPathSegments(%s, %d) = %s, want %s", tt.path, tt.n, got, tt.want)
		}
	}
}

func TestFilterSymbols(t *testing.T) {
	root := setupArchitectProject(t)
	mapper := NewMapper(root, nil)

	cmap, err := mapper.GenerateMap(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	funcs := filterSymbols(cmap.Symbols, 0) // KindFunction
	if len(funcs) == 0 {
		t.Error("expected at least one function")
	}
}
