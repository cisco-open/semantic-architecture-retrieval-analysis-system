/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package engine

import (
	"fmt"
	"strings"
	"testing"
)

func TestNewChunkerDefaults(t *testing.T) {
	c := NewChunker(0, -1)
	if c.ChunkSize() != DefaultChunkSize {
		t.Errorf("expected default chunk size %d, got %d", DefaultChunkSize, c.ChunkSize())
	}
	if c.Overlap() != DefaultChunkOverlap {
		t.Errorf("expected default overlap %d, got %d", DefaultChunkOverlap, c.Overlap())
	}
}

func TestNewChunkerOverlapClamped(t *testing.T) {
	// overlap >= chunkSize should be clamped to chunkSize/10
	c := NewChunker(100, 200)
	if c.Overlap() != 10 {
		t.Errorf("expected clamped overlap 10, got %d", c.Overlap())
	}
}

func TestChunkEmptyContent(t *testing.T) {
	c := NewChunker(100, 10)
	chunks := c.Chunk("empty.go", "")
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty content, got %d", len(chunks))
	}

	chunks = c.Chunk("whitespace.go", "   \n  \n  ")
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for whitespace content, got %d", len(chunks))
	}
}

func TestChunkSmallFile(t *testing.T) {
	c := NewChunker(512, 50)
	content := "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
	chunks := c.Chunk("main.go", content)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for small file, got %d", len(chunks))
	}

	ch := chunks[0]
	if ch.FilePath != "main.go" {
		t.Errorf("expected FilePath main.go, got %s", ch.FilePath)
	}
	if ch.StartLine != 1 {
		t.Errorf("expected StartLine 1, got %d", ch.StartLine)
	}
	if ch.ID != "main.go_0" {
		t.Errorf("expected ID main.go_0, got %s", ch.ID)
	}
	if ch.Content != content {
		t.Error("content should match original")
	}
	if !strings.HasPrefix(ch.EmbedContent, "File: main.go") {
		t.Error("EmbedContent should start with File: header")
	}
	if ch.Hash == "" {
		t.Error("expected non-empty hash")
	}
	if ch.ContentHash == "" {
		t.Error("expected non-empty content hash")
	}
}

func TestChunkLargeFileProducesMultipleChunks(t *testing.T) {
	c := NewChunker(50, 5) // small chunk size for testing (50 tokens ≈ 200 chars)

	// Generate content that requires multiple chunks
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("func handler%d() { return nil }", i))
	}
	content := strings.Join(lines, "\n")

	chunks := c.Chunk("handlers.go", content)
	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks for large file, got %d", len(chunks))
	}

	// Verify each chunk has correct metadata
	for i, ch := range chunks {
		if ch.FilePath != "handlers.go" {
			t.Errorf("chunk %d: wrong FilePath %s", i, ch.FilePath)
		}
		if ch.ID != fmt.Sprintf("handlers.go_%d", i) {
			t.Errorf("chunk %d: expected ID handlers.go_%d, got %s", i, i, ch.ID)
		}
		if ch.StartLine < 1 {
			t.Errorf("chunk %d: invalid StartLine %d", i, ch.StartLine)
		}
		if ch.EndLine < ch.StartLine {
			t.Errorf("chunk %d: EndLine %d < StartLine %d", i, ch.EndLine, ch.StartLine)
		}
		if len(strings.TrimSpace(ch.Content)) == 0 {
			t.Errorf("chunk %d: empty content", i)
		}
	}
}

func TestChunkUniqueHashes(t *testing.T) {
	c := NewChunker(50, 5)

	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("line %d: some code here that does stuff", i))
	}
	content := strings.Join(lines, "\n")

	chunks := c.Chunk("file.go", content)
	if len(chunks) < 2 {
		t.Skip("need multiple chunks for hash uniqueness test")
	}

	hashes := make(map[string]bool)
	for _, ch := range chunks {
		if hashes[ch.Hash] {
			t.Errorf("duplicate hash: %s", ch.Hash)
		}
		hashes[ch.Hash] = true
	}
}

func TestChunkContentHashDeterministic(t *testing.T) {
	c := NewChunker(512, 50)
	content := "package main\nfunc main() {}\n"

	chunks1 := c.Chunk("a.go", content)
	chunks2 := c.Chunk("b.go", content)

	if len(chunks1) != 1 || len(chunks2) != 1 {
		t.Fatal("expected 1 chunk each")
	}

	// Same content should produce same content hash (path-independent)
	if chunks1[0].ContentHash != chunks2[0].ContentHash {
		t.Error("same content in different files should have same ContentHash")
	}

	// But different overall Hash (path-dependent)
	if chunks1[0].Hash == chunks2[0].Hash {
		t.Error("different files should have different Hash")
	}
}

func TestChunkVeryLongLine(t *testing.T) {
	c := NewChunker(50, 5)
	// One very long line with no newlines
	longLine := strings.Repeat("x", 5000)
	chunks := c.Chunk("minified.js", longLine)

	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk for long line")
	}

	// All content should be represented
	totalLen := 0
	for _, ch := range chunks {
		totalLen += len(ch.Content)
	}
	// Due to overlap, total may be larger than original
	if totalLen < len(longLine) {
		t.Errorf("chunks don't cover all content: %d < %d", totalLen, len(longLine))
	}
}

// ---------------------------------------------------------------------------
// Symbol-based chunking tests
// ---------------------------------------------------------------------------

func TestChunkBySymbolsEmpty(t *testing.T) {
	c := NewChunker(512, 50)
	content := "package main\nfunc main() {}\n"

	// No symbols — should fall back to regular chunking
	chunks := c.ChunkBySymbols("main.go", content, nil)
	if len(chunks) == 0 {
		t.Error("expected fallback to regular chunking")
	}
}

func TestChunkBySymbolsSingleFunction(t *testing.T) {
	c := NewChunker(512, 50)
	content := `package main

import "fmt"

func hello() {
	fmt.Println("hello world")
}
`
	symbols := []SymbolBoundary{
		{Name: "hello", Kind: "function", StartLine: 5, EndLine: 7},
	}

	chunks := c.ChunkBySymbols("main.go", content, symbols)
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}

	// Find the hello chunk
	found := false
	for _, ch := range chunks {
		if ch.SymbolName == "hello" {
			found = true
			if ch.StartLine != 5 {
				t.Errorf("expected StartLine 5, got %d", ch.StartLine)
			}
			if ch.EndLine != 7 {
				t.Errorf("expected EndLine 7, got %d", ch.EndLine)
			}
			if !strings.Contains(ch.EmbedContent, "Symbol: hello") {
				t.Error("EmbedContent should contain Symbol: header")
			}
			break
		}
	}
	if !found {
		t.Error("expected a chunk with SymbolName=hello")
	}
}

func TestChunkBySymbolsMultipleFunctions(t *testing.T) {
	c := NewChunker(512, 50)
	content := `package auth

func Login(user, pass string) error {
	// validate credentials
	return nil
}

func Logout(token string) error {
	// invalidate session
	return nil
}

func Refresh(token string) (string, error) {
	// refresh token
	return "", nil
}
`
	symbols := []SymbolBoundary{
		{Name: "Login", Kind: "function", StartLine: 3, EndLine: 6},
		{Name: "Logout", Kind: "function", StartLine: 8, EndLine: 11},
		{Name: "Refresh", Kind: "function", StartLine: 13, EndLine: 16},
	}

	chunks := c.ChunkBySymbols("auth.go", content, symbols)

	symbolNames := make(map[string]bool)
	for _, ch := range chunks {
		if ch.SymbolName != "" {
			symbolNames[ch.SymbolName] = true
		}
	}

	for _, name := range []string{"Login", "Logout", "Refresh"} {
		if !symbolNames[name] {
			t.Errorf("expected chunk for symbol %s", name)
		}
	}
}

func TestChunkBySymbolsLargeFunction(t *testing.T) {
	c := NewChunker(20, 2) // very small chunk size to force sub-splitting

	var lines []string
	lines = append(lines, "package main")
	lines = append(lines, "")
	lines = append(lines, "func bigFunction() {")
	for i := 0; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("\tline%d := %d", i, i))
	}
	lines = append(lines, "}")
	content := strings.Join(lines, "\n")

	symbols := []SymbolBoundary{
		{Name: "bigFunction", Kind: "function", StartLine: 3, EndLine: len(lines)},
	}

	chunks := c.ChunkBySymbols("big.go", content, symbols)
	if len(chunks) < 2 {
		t.Errorf("expected large function to be sub-chunked, got %d chunks", len(chunks))
	}

	// All sub-chunks should reference the symbol
	for _, ch := range chunks {
		if ch.SymbolName != "" && ch.SymbolName != "bigFunction" {
			t.Errorf("unexpected symbol name: %s", ch.SymbolName)
		}
	}
}

func TestChunkBySymbolsInterstitialContent(t *testing.T) {
	c := NewChunker(512, 50)
	content := `package main

import (
	"fmt"
	"os"
)

// Constants for the application
const (
	AppName    = "saras"
	AppVersion = "1.0"
)

func main() {
	fmt.Println(AppName)
	os.Exit(0)
}
`
	// Only the function is a symbol — the imports/consts are interstitial
	symbols := []SymbolBoundary{
		{Name: "main", Kind: "function", StartLine: 14, EndLine: 18},
	}

	chunks := c.ChunkBySymbols("main.go", content, symbols)

	// Should have both interstitial content and the main function
	hasMain := false
	hasInterstitial := false
	for _, ch := range chunks {
		if ch.SymbolName == "main" {
			hasMain = true
		}
		if ch.SymbolName == "" && strings.Contains(ch.Content, "import") {
			hasInterstitial = true
		}
	}

	if !hasMain {
		t.Error("expected chunk for main symbol")
	}
	if !hasInterstitial {
		t.Error("expected interstitial chunk for imports/constants")
	}
}

// ---------------------------------------------------------------------------
// Helper function tests
// ---------------------------------------------------------------------------

func TestBuildLineStarts(t *testing.T) {
	content := "line1\nline2\nline3\n"
	starts := buildLineStarts(content)

	if len(starts) != 3 {
		t.Fatalf("expected 3 line starts, got %d", len(starts))
	}
	if starts[0] != 0 {
		t.Errorf("first line should start at 0, got %d", starts[0])
	}
	if starts[1] != 6 {
		t.Errorf("second line should start at 6, got %d", starts[1])
	}
	if starts[2] != 12 {
		t.Errorf("third line should start at 12, got %d", starts[2])
	}
}

func TestBuildLineStartsSingleLine(t *testing.T) {
	starts := buildLineStarts("no newline")
	if len(starts) != 1 {
		t.Errorf("expected 1 line start for no-newline content, got %d", len(starts))
	}
}

func TestAlignRuneBoundary(t *testing.T) {
	// ASCII: every byte is a rune start
	s := "hello"
	if alignRuneBoundary(s, 2) != 2 {
		t.Error("ASCII should not change position")
	}

	// Multi-byte UTF-8: é is 2 bytes (0xC3 0xA9)
	s = "café"
	// Position 4 is the second byte of é, should advance to 5
	pos := alignRuneBoundary(s, 4)
	if pos != 5 {
		t.Errorf("expected 5, got %d", pos)
	}
}

func TestSymbolBoundary(t *testing.T) {
	sym := SymbolBoundary{
		Name:      "MyFunc",
		Kind:      "function",
		StartLine: 10,
		EndLine:   25,
	}

	if sym.Name != "MyFunc" {
		t.Errorf("unexpected name: %s", sym.Name)
	}
	if sym.Kind != "function" {
		t.Errorf("unexpected kind: %s", sym.Kind)
	}
}
