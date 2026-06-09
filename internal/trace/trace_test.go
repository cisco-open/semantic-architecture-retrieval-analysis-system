/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package trace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func setupTraceProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Create a multi-file Go project
	srcDir := filepath.Join(root, "src")
	os.MkdirAll(srcDir, 0755)

	writeFile(t, filepath.Join(root, "main.go"), `package main

import "fmt"

func main() {
	result := Login("admin", "secret")
	fmt.Println(result)
	handleRequest()
}

func handleRequest() {
	user := getUser()
	if user != "" {
		Login(user, "")
	}
}
`)

	writeFile(t, filepath.Join(srcDir, "auth.go"), `package src

func Login(user, pass string) error {
	if err := validate(user, pass); err != nil {
		return err
	}
	return createSession(user)
}

func validate(user, pass string) error {
	if user == "" {
		return fmt.Errorf("empty user")
	}
	return nil
}

func createSession(user string) error {
	return nil
}
`)

	writeFile(t, filepath.Join(srcDir, "user.go"), `package src

type User struct {
	Name  string
	Email string
}

var DefaultUser = User{Name: "admin"}

const MaxUsers = 100

func getUser() string {
	return DefaultUser.Name
}

func (u User) FullName() string {
	return u.Name
}
`)

	writeFile(t, filepath.Join(srcDir, "db.go"), `package src

type Database interface {
	Connect(dsn string) error
	Close() error
}

func NewDB(dsn string) error {
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
// SymbolKind tests
// ---------------------------------------------------------------------------

func TestSymbolKindString(t *testing.T) {
	tests := []struct {
		kind SymbolKind
		want string
	}{
		{KindFunction, "function"},
		{KindMethod, "method"},
		{KindType, "type"},
		{KindInterface, "interface"},
		{KindVariable, "variable"},
		{KindConstant, "constant"},
		{KindImport, "import"},
		{KindPackage, "package"},
		{SymbolKind(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("SymbolKind(%d).String() = %s, want %s", tt.kind, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// ExtractSymbols tests
// ---------------------------------------------------------------------------

func TestExtractSymbols(t *testing.T) {
	root := setupTraceProject(t)
	tracer := NewTracer(root, nil)

	symbols, err := tracer.ExtractSymbols(context.Background())
	if err != nil {
		t.Fatalf("ExtractSymbols: %v", err)
	}

	if len(symbols) == 0 {
		t.Fatal("expected symbols")
	}

	// Check we found expected symbol kinds
	kinds := make(map[SymbolKind]int)
	for _, s := range symbols {
		kinds[s.Kind]++
	}

	if kinds[KindFunction] == 0 {
		t.Error("expected at least one function")
	}
	if kinds[KindType] == 0 {
		t.Error("expected at least one type")
	}
	if kinds[KindInterface] == 0 {
		t.Error("expected at least one interface")
	}
	if kinds[KindPackage] == 0 {
		t.Error("expected at least one package")
	}
}

func TestExtractSymbolsFindsSpecificFunctions(t *testing.T) {
	root := setupTraceProject(t)
	tracer := NewTracer(root, nil)

	symbols, err := tracer.ExtractSymbols(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	names := make(map[string]bool)
	for _, s := range symbols {
		names[s.Name] = true
	}

	expected := []string{"main", "Login", "validate", "createSession", "getUser", "NewDB", "FullName"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("expected symbol %s not found", name)
		}
	}
}

func TestExtractSymbolsTypes(t *testing.T) {
	root := setupTraceProject(t)
	tracer := NewTracer(root, nil)

	symbols, _ := tracer.ExtractSymbols(context.Background())

	found := false
	for _, s := range symbols {
		if s.Name == "User" && s.Kind == KindType {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected User type")
	}
}

func TestExtractSymbolsInterface(t *testing.T) {
	root := setupTraceProject(t)
	tracer := NewTracer(root, nil)

	symbols, _ := tracer.ExtractSymbols(context.Background())

	found := false
	for _, s := range symbols {
		if s.Name == "Database" && s.Kind == KindInterface {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected Database interface")
	}
}

func TestExtractSymbolsMethod(t *testing.T) {
	root := setupTraceProject(t)
	tracer := NewTracer(root, nil)

	symbols, _ := tracer.ExtractSymbols(context.Background())

	for _, s := range symbols {
		if s.Name == "FullName" && s.Kind == KindMethod {
			if s.Parent != "User" {
				t.Errorf("expected parent User, got %s", s.Parent)
			}
			return
		}
	}
	t.Error("expected FullName method")
}

func TestExtractSymbolsVariable(t *testing.T) {
	root := setupTraceProject(t)
	tracer := NewTracer(root, nil)

	symbols, _ := tracer.ExtractSymbols(context.Background())

	found := false
	for _, s := range symbols {
		if s.Name == "DefaultUser" && s.Kind == KindVariable {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected DefaultUser variable")
	}
}

func TestExtractSymbolsConstant(t *testing.T) {
	root := setupTraceProject(t)
	tracer := NewTracer(root, nil)

	symbols, _ := tracer.ExtractSymbols(context.Background())

	found := false
	for _, s := range symbols {
		if s.Name == "MaxUsers" && s.Kind == KindConstant {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected MaxUsers constant")
	}
}

func TestExtractSymbolsIgnoresHiddenDirs(t *testing.T) {
	root := setupTraceProject(t)

	hiddenDir := filepath.Join(root, ".hidden")
	os.MkdirAll(hiddenDir, 0755)
	writeFile(t, filepath.Join(hiddenDir, "secret.go"), `package hidden
func Secret() {}`)

	tracer := NewTracer(root, nil)
	symbols, _ := tracer.ExtractSymbols(context.Background())

	for _, s := range symbols {
		if s.Name == "Secret" {
			t.Error("should not extract symbols from hidden dirs")
		}
	}
}

func TestExtractSymbolsIgnoreList(t *testing.T) {
	root := setupTraceProject(t)

	vendorDir := filepath.Join(root, "vendor")
	os.MkdirAll(vendorDir, 0755)
	writeFile(t, filepath.Join(vendorDir, "lib.go"), `package vendor
func VendorFunc() {}`)

	tracer := NewTracer(root, []string{"vendor"})
	symbols, _ := tracer.ExtractSymbols(context.Background())

	for _, s := range symbols {
		if s.Name == "VendorFunc" {
			t.Error("should not extract symbols from ignored dirs")
		}
	}
}

func TestExtractSymbolsIgnoresTestFiles(t *testing.T) {
	root := setupTraceProject(t)
	writeFile(t, filepath.Join(root, "main_test.go"), `package main
func TestMain() {}`)

	tracer := NewTracer(root, nil)
	symbols, _ := tracer.ExtractSymbols(context.Background())

	for _, s := range symbols {
		if s.Name == "TestMain" {
			t.Error("should not extract symbols from _test.go files")
		}
	}
}

// ---------------------------------------------------------------------------
// FindSymbol tests
// ---------------------------------------------------------------------------

func TestFindSymbol(t *testing.T) {
	root := setupTraceProject(t)
	tracer := NewTracer(root, nil)

	matches, err := tracer.FindSymbol(context.Background(), "Login")
	if err != nil {
		t.Fatal(err)
	}

	if len(matches) == 0 {
		t.Error("expected to find Login symbol")
	}
	if matches[0].Kind != KindFunction {
		t.Errorf("expected function kind, got %s", matches[0].Kind)
	}
}

func TestFindSymbolNotFound(t *testing.T) {
	root := setupTraceProject(t)
	tracer := NewTracer(root, nil)

	matches, err := tracer.FindSymbol(context.Background(), "NonExistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("expected no matches, got %d", len(matches))
	}
}

// ---------------------------------------------------------------------------
// FindReferences tests
// ---------------------------------------------------------------------------

func TestFindReferences(t *testing.T) {
	root := setupTraceProject(t)
	tracer := NewTracer(root, nil)

	refs, err := tracer.FindReferences(context.Background(), "Login")
	if err != nil {
		t.Fatal(err)
	}

	if len(refs) < 2 {
		t.Errorf("expected at least 2 references to Login, got %d", len(refs))
	}

	// Should find references in both main.go and auth.go
	files := make(map[string]bool)
	for _, r := range refs {
		files[r.FilePath] = true
	}

	if !files["main.go"] {
		t.Error("expected reference in main.go")
	}
}

func TestFindReferencesNotFound(t *testing.T) {
	root := setupTraceProject(t)
	tracer := NewTracer(root, nil)

	refs, err := tracer.FindReferences(context.Background(), "NonExistentXYZ123")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Errorf("expected no refs, got %d", len(refs))
	}
}

// ---------------------------------------------------------------------------
// FindCallers tests
// ---------------------------------------------------------------------------

func TestFindCallers(t *testing.T) {
	root := setupTraceProject(t)
	tracer := NewTracer(root, nil)

	callers, err := tracer.FindCallers(context.Background(), "Login")
	if err != nil {
		t.Fatal(err)
	}

	if len(callers) == 0 {
		t.Error("expected at least one caller of Login")
	}

	callerNames := make(map[string]bool)
	for _, c := range callers {
		callerNames[c.Caller] = true
	}

	if !callerNames["main"] {
		t.Error("expected main to be a caller of Login")
	}
}

// ---------------------------------------------------------------------------
// FindCallees tests
// ---------------------------------------------------------------------------

func TestFindCallees(t *testing.T) {
	root := setupTraceProject(t)
	tracer := NewTracer(root, nil)

	callees, err := tracer.FindCallees(context.Background(), "Login")
	if err != nil {
		t.Fatal(err)
	}

	if len(callees) == 0 {
		t.Error("expected at least one callee of Login")
	}

	calleeNames := make(map[string]bool)
	for _, c := range callees {
		calleeNames[c.Callee] = true
	}

	if !calleeNames["validate"] {
		t.Error("expected validate to be a callee of Login")
	}
	if !calleeNames["createSession"] {
		t.Error("expected createSession to be a callee of Login")
	}
}

func TestFindCalleesNotFound(t *testing.T) {
	root := setupTraceProject(t)
	tracer := NewTracer(root, nil)

	_, err := tracer.FindCallees(context.Background(), "NonExistent")
	if err == nil {
		t.Error("expected error for non-existent function")
	}
}

// ---------------------------------------------------------------------------
// Trace (full) tests
// ---------------------------------------------------------------------------

func TestTrace(t *testing.T) {
	root := setupTraceProject(t)
	tracer := NewTracer(root, nil)

	result, err := tracer.Trace(context.Background(), "Login")
	if err != nil {
		t.Fatal(err)
	}

	if result.Symbol == nil {
		t.Fatal("expected symbol")
	}
	if result.Symbol.Name != "Login" {
		t.Errorf("expected Login, got %s", result.Symbol.Name)
	}
	if len(result.References) == 0 {
		t.Error("expected references")
	}
	if len(result.Callers) == 0 {
		t.Error("expected callers")
	}
	if len(result.Callees) == 0 {
		t.Error("expected callees")
	}
}

func TestTraceType(t *testing.T) {
	root := setupTraceProject(t)
	tracer := NewTracer(root, nil)

	result, err := tracer.Trace(context.Background(), "User")
	if err != nil {
		t.Fatal(err)
	}

	if result.Symbol == nil {
		t.Fatal("expected symbol")
	}
	if result.Symbol.Kind != KindType {
		t.Errorf("expected type kind, got %s", result.Symbol.Kind)
	}
	// Types shouldn't have callers/callees
	if len(result.Callers) != 0 {
		t.Error("expected no callers for type")
	}
}

// ---------------------------------------------------------------------------
// Context cancellation
// ---------------------------------------------------------------------------

func TestExtractSymbolsCancelled(t *testing.T) {
	root := setupTraceProject(t)
	tracer := NewTracer(root, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tracer.ExtractSymbols(ctx)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func TestIsSupportedFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"main.go", true},
		{"src/auth.go", true},
		{"main_test.go", false},
		{"README.md", true},
		{"config.yaml", true},
		{"app.py", true},
		{"test_app.py", false},
		{"index.js", true},
		{"app.test.js", false},
		{"Main.java", true},
		{"main.rs", true},
		{"main.c", true},
		{"main.cpp", true},
		{"App.kt", true},
		{"service.ts", true},
	}

	for _, tt := range tests {
		if got := isSupportedFile(tt.path); got != tt.want {
			t.Errorf("isSupportedFile(%s) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestDeduplicateEdges(t *testing.T) {
	edges := []CallEdge{
		{Caller: "A", Callee: "B"},
		{Caller: "A", Callee: "B"},
		{Caller: "A", Callee: "C"},
	}

	result := deduplicateEdges(edges)
	if len(result) != 2 {
		t.Errorf("expected 2 unique edges, got %d", len(result))
	}
}
