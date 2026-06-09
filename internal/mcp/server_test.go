/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/architect"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/search"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/store"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/trace"
	gomcp "github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// Mock embedder
// ---------------------------------------------------------------------------

type mockEmbedder struct {
	dim int
}

func (m *mockEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	vec := make([]float32, m.dim)
	for i := range vec {
		vec[i] = float32(i+1) / float32(m.dim)
	}
	return vec, nil
}

func (m *mockEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, t := range texts {
		v, _ := m.Embed(context.Background(), t)
		results[i] = v
	}
	return results, nil
}

func (m *mockEmbedder) Dimensions() int { return m.dim }
func (m *mockEmbedder) Close() error    { return nil }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func setupTestServer(t *testing.T) *Server {
	t.Helper()

	st := store.NewGobStore("")
	chunks := []store.Chunk{
		{ID: "c1", FilePath: "src/auth.go", StartLine: 1, EndLine: 10, Content: "func Login(user, pass string) error { return validate(user, pass) }", Vector: []float32{1, 0, 0}},
		{ID: "c2", FilePath: "src/db.go", StartLine: 1, EndLine: 8, Content: "func Connect(dsn string) (*DB, error) { return sql.Open(dsn) }", Vector: []float32{0, 1, 0}},
	}
	st.SaveChunks(context.Background(), chunks)

	emb := &mockEmbedder{dim: 3}

	cfg := &config.Config{
		Search: config.SearchConfig{
			Boost:  config.BoostConfig{Enabled: false},
			Hybrid: config.HybridConfig{Enabled: false, K: 60},
			Dedup:  config.DedupConfig{Enabled: false},
		},
	}

	searcher := search.NewSearcher(st, emb, cfg.Search)

	// Create tracer and mapper with temp dir
	root := t.TempDir()
	tracer := trace.NewTracer(root, nil)
	mapper := architect.NewMapper(root, nil)

	return NewServer(searcher, nil, tracer, mapper, cfg)
}

// callToolReq builds a mcp.CallToolRequest with given arguments.
func callToolReq(args map[string]any) gomcp.CallToolRequest {
	return gomcp.CallToolRequest{
		Params: gomcp.CallToolParams{
			Arguments: args,
		},
	}
}

// resultText extracts the text from the first TextContent in a CallToolResult.
func resultText(r *gomcp.CallToolResult) string {
	if len(r.Content) == 0 {
		return ""
	}
	if tc, ok := r.Content[0].(gomcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

// ---------------------------------------------------------------------------
// Server construction tests
// ---------------------------------------------------------------------------

func TestNewServer(t *testing.T) {
	s := setupTestServer(t)
	if s == nil {
		t.Fatal("expected non-nil server")
	}
	if s.addr != "127.0.0.1:9420" {
		t.Errorf("expected default addr, got %s", s.addr)
	}
	if s.mcpServer == nil {
		t.Error("expected non-nil mcpServer")
	}
	if s.sseServer == nil {
		t.Error("expected non-nil sseServer")
	}
}

func TestNewServerWithAddr(t *testing.T) {
	st := store.NewGobStore("")
	emb := &mockEmbedder{dim: 3}
	cfg := &config.Config{}
	searcher := search.NewSearcher(st, emb, cfg.Search)
	root := t.TempDir()

	s := NewServer(searcher, nil, trace.NewTracer(root, nil), architect.NewMapper(root, nil), cfg, WithAddr("0.0.0.0:8080"))
	if s.addr != "0.0.0.0:8080" {
		t.Errorf("expected custom addr, got %s", s.addr)
	}
}

func TestGetAddr(t *testing.T) {
	s := setupTestServer(t)
	if s.GetAddr() != "127.0.0.1:9420" {
		t.Errorf("expected addr, got %s", s.GetAddr())
	}
}

// ---------------------------------------------------------------------------
// Tool handler tests (call handlers directly)
// ---------------------------------------------------------------------------

func TestHandleSearch(t *testing.T) {
	s := setupTestServer(t)

	result, err := s.handleSearch(context.Background(), callToolReq(map[string]any{"query": "login", "limit": 5}))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", resultText(result))
	}
	if !strings.Contains(resultText(result), "auth.go") {
		t.Error("expected auth.go in search results")
	}
}

func TestHandleSearchEmpty(t *testing.T) {
	s := setupTestServer(t)

	result, err := s.handleSearch(context.Background(), callToolReq(map[string]any{"query": "", "limit": 5}))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for empty query")
	}
}

func TestHandleSearchDefaultLimit(t *testing.T) {
	s := setupTestServer(t)

	result, err := s.handleSearch(context.Background(), callToolReq(map[string]any{"query": "test"}))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", resultText(result))
	}
}

func TestHandleAskNoPipeline(t *testing.T) {
	s := setupTestServer(t)

	result, err := s.handleAsk(context.Background(), callToolReq(map[string]any{"question": "how does auth work?"}))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error when pipeline is nil")
	}
	if !strings.Contains(resultText(result), "not configured") {
		t.Errorf("expected 'not configured' message, got: %s", resultText(result))
	}
}

func TestHandleAskEmptyQuestion(t *testing.T) {
	s := setupTestServer(t)

	result, err := s.handleAsk(context.Background(), callToolReq(map[string]any{"question": ""}))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for empty question")
	}
}

func TestHandleTrace(t *testing.T) {
	s := setupTestServer(t)

	result, err := s.handleTrace(context.Background(), callToolReq(map[string]any{"symbol": "Login"}))
	if err != nil {
		t.Fatal(err)
	}
	// With an empty temp dir, trace won't find anything, but shouldn't error
	if result.IsError {
		t.Errorf("unexpected error: %s", resultText(result))
	}
}

func TestHandleTraceEmptySymbol(t *testing.T) {
	s := setupTestServer(t)

	result, err := s.handleTrace(context.Background(), callToolReq(map[string]any{"symbol": ""}))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for empty symbol")
	}
}

func TestHandleMapTree(t *testing.T) {
	s := setupTestServer(t)

	result, err := s.handleMap(context.Background(), callToolReq(map[string]any{"format": "tree"}))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", resultText(result))
	}
}

func TestHandleMapMarkdown(t *testing.T) {
	s := setupTestServer(t)

	result, err := s.handleMap(context.Background(), callToolReq(map[string]any{"format": "markdown"}))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", resultText(result))
	}
	if !strings.Contains(resultText(result), "Architecture") {
		t.Error("expected Architecture heading in markdown")
	}
}

func TestHandleMapDefaultFormat(t *testing.T) {
	s := setupTestServer(t)

	result, err := s.handleMap(context.Background(), callToolReq(map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", resultText(result))
	}
}

func TestHandleMapUnknownFormat(t *testing.T) {
	s := setupTestServer(t)

	result, err := s.handleMap(context.Background(), callToolReq(map[string]any{"format": "invalid"}))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for unknown format")
	}
}

func TestHandleSymbols(t *testing.T) {
	s := setupTestServer(t)

	result, err := s.handleSymbols(context.Background(), callToolReq(map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", resultText(result))
	}
	if !strings.Contains(resultText(result), "Symbols") {
		t.Error("expected Symbols header")
	}
}

func TestGetMCPServer(t *testing.T) {
	s := setupTestServer(t)
	if s.GetMCPServer() == nil {
		t.Error("expected non-nil MCPServer")
	}
}

func TestShutdown(t *testing.T) {
	s := setupTestServer(t)
	// Shutdown without Start should not panic
	err := s.Shutdown(context.Background())
	if err != nil {
		t.Errorf("unexpected shutdown error: %v", err)
	}
}
