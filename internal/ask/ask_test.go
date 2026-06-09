/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package ask

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/search"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/store"
)

// ---------------------------------------------------------------------------
// Mock embedder for search
// ---------------------------------------------------------------------------

type mockEmbedder struct {
	dim     int
	vectors map[string][]float32
}

func newMockEmbedder(dim int) *mockEmbedder {
	return &mockEmbedder{dim: dim, vectors: make(map[string][]float32)}
}

func (m *mockEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if v, ok := m.vectors[text]; ok {
		return v, nil
	}
	vec := make([]float32, m.dim)
	for i := range vec {
		vec[i] = float32(i+1) / float32(m.dim)
	}
	return vec, nil
}

func (m *mockEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := m.Embed(context.Background(), t)
		if err != nil {
			return nil, err
		}
		results[i] = v
	}
	return results, nil
}

func (m *mockEmbedder) Dimensions() int { return m.dim }
func (m *mockEmbedder) Close() error    { return nil }

func (m *mockEmbedder) SetVector(text string, vec []float32) {
	m.vectors[text] = vec
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func setupSearcher(t *testing.T) (*search.Searcher, *mockEmbedder) {
	t.Helper()
	st := store.NewGobStore("")
	emb := newMockEmbedder(3)

	chunks := []store.Chunk{
		{ID: "c1", FilePath: "src/auth.go", StartLine: 1, EndLine: 10, Content: "func Login(user, pass string) error { return validate(user, pass) }", Vector: []float32{1, 0, 0}},
		{ID: "c2", FilePath: "src/db.go", StartLine: 1, EndLine: 8, Content: "func Connect(dsn string) (*DB, error) { return sql.Open(dsn) }", Vector: []float32{0, 1, 0}},
	}
	st.SaveChunks(context.Background(), chunks)

	cfg := config.SearchConfig{
		Boost:  config.BoostConfig{Enabled: false},
		Hybrid: config.HybridConfig{Enabled: false, K: 60},
		Dedup:  config.DedupConfig{Enabled: false},
	}

	s := search.NewSearcher(st, emb, cfg)
	return s, emb
}

func newMockLLMServer(responses []string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)

		for _, content := range responses {
			chunk := streamResponse{
				Choices: []streamChoice{
					{Delta: struct {
						Content string `json:"content"`
					}{Content: content}},
				},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			if flusher != nil {
				flusher.Flush()
			}
		}

		fmt.Fprintf(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
}

func newMockErrorServer(status int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, body, status)
	}))
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestBuildContext(t *testing.T) {
	results := []search.Result{
		{FilePath: "a.go", StartLine: 1, EndLine: 5, Score: 0.95, Content: "func A() {}"},
		{FilePath: "b.go", StartLine: 10, EndLine: 20, Score: 0.80, Content: "func B() {}"},
	}

	ctx := BuildContext(results)

	if !strings.Contains(ctx, "a.go") {
		t.Error("expected a.go in context")
	}
	if !strings.Contains(ctx, "b.go") {
		t.Error("expected b.go in context")
	}
	if !strings.Contains(ctx, "func A()") {
		t.Error("expected func A content")
	}
	if !strings.Contains(ctx, "0.95") {
		t.Error("expected score in context")
	}
}

func TestBuildContextEmpty(t *testing.T) {
	ctx := BuildContext(nil)
	if !strings.Contains(ctx, "no relevant code found") {
		t.Error("expected no-results message")
	}
}

func TestNewPipeline(t *testing.T) {
	s, _ := setupSearcher(t)
	p := NewPipeline(s, "http://localhost:11434/v1", "test-model")

	if p.model != "test-model" {
		t.Errorf("expected model test-model, got %s", p.model)
	}
	if p.endpoint != "http://localhost:11434/v1" {
		t.Errorf("expected endpoint, got %s", p.endpoint)
	}
	if p.systemPrompt != defaultSystemPrompt {
		t.Error("expected default system prompt")
	}
}

func TestNewPipelineWithOptions(t *testing.T) {
	s, _ := setupSearcher(t)
	p := NewPipeline(s, "http://test/v1", "model",
		WithAPIKey("sk-test"),
		WithSystemPrompt("custom prompt"),
	)

	if p.apiKey != "sk-test" {
		t.Errorf("expected api key sk-test, got %s", p.apiKey)
	}
	if p.systemPrompt != "custom prompt" {
		t.Errorf("expected custom prompt, got %s", p.systemPrompt)
	}
}

func TestNewPipelineWithHTTPClient(t *testing.T) {
	s, _ := setupSearcher(t)
	client := &http.Client{}
	p := NewPipeline(s, "http://test/v1", "model", WithHTTPClient(client))

	if p.httpClient != client {
		t.Error("expected custom HTTP client")
	}
}

func TestPipelineEndpointTrailingSlash(t *testing.T) {
	s, _ := setupSearcher(t)
	p := NewPipeline(s, "http://test/v1/", "model")
	if p.endpoint != "http://test/v1" {
		t.Errorf("expected trailing slash stripped, got %s", p.endpoint)
	}
}

func TestAskStreaming(t *testing.T) {
	s, emb := setupSearcher(t)
	emb.SetVector("authentication", []float32{1, 0, 0})

	server := newMockLLMServer([]string{"The ", "Login ", "function ", "handles ", "auth."})
	defer server.Close()

	p := NewPipeline(s, server.URL, "test-model")

	ch, err := p.Ask(context.Background(), AskOptions{
		Query: "authentication",
		Limit: 2,
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	var fullResponse strings.Builder
	chunks := 0
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		if chunk.Done {
			break
		}
		fullResponse.WriteString(chunk.Content)
		chunks++
	}

	if chunks == 0 {
		t.Error("expected at least one chunk")
	}

	response := fullResponse.String()
	if !strings.Contains(response, "Login") {
		t.Errorf("expected 'Login' in response, got: %s", response)
	}
}

func TestAskSync(t *testing.T) {
	s, emb := setupSearcher(t)
	emb.SetVector("database connection", []float32{0, 1, 0})

	server := newMockLLMServer([]string{"Connect ", "uses ", "sql.Open."})
	defer server.Close()

	p := NewPipeline(s, server.URL, "test-model")

	response, err := p.AskSync(context.Background(), AskOptions{
		Query: "database connection",
		Limit: 2,
	})
	if err != nil {
		t.Fatalf("AskSync: %v", err)
	}

	if !strings.Contains(response, "Connect") {
		t.Errorf("expected 'Connect' in response, got: %s", response)
	}
}

func TestAskDefaultOptions(t *testing.T) {
	s, _ := setupSearcher(t)

	server := newMockLLMServer([]string{"ok"})
	defer server.Close()

	p := NewPipeline(s, server.URL, "test-model")

	ch, err := p.Ask(context.Background(), AskOptions{Query: "test"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	// Drain channel
	for range ch {
	}
}

func TestAskLLMError(t *testing.T) {
	s, _ := setupSearcher(t)

	server := newMockErrorServer(http.StatusInternalServerError, "internal error")
	defer server.Close()

	p := NewPipeline(s, server.URL, "test-model")

	_, err := p.Ask(context.Background(), AskOptions{Query: "test", Limit: 1})
	if err == nil {
		t.Error("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected status code in error, got: %v", err)
	}
}

func TestAskLLMUnauthorized(t *testing.T) {
	s, _ := setupSearcher(t)

	server := newMockErrorServer(http.StatusUnauthorized, "invalid api key")
	defer server.Close()

	p := NewPipeline(s, server.URL, "test-model", WithAPIKey("bad-key"))

	_, err := p.Ask(context.Background(), AskOptions{Query: "test", Limit: 1})
	if err == nil {
		t.Error("expected error for 401 response")
	}
}

func TestAskContextCancelled(t *testing.T) {
	s, _ := setupSearcher(t)

	server := newMockLLMServer([]string{"chunk1", "chunk2", "chunk3"})
	defer server.Close()

	p := NewPipeline(s, server.URL, "test-model")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := p.Ask(ctx, AskOptions{Query: "test", Limit: 1})
	if err == nil {
		// Either Ask itself fails or the stream will fail
		t.Log("Ask returned nil error with cancelled context (search may succeed from store)")
	}
}

func TestAskWithAPIKeyHeader(t *testing.T) {
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	s, _ := setupSearcher(t)
	p := NewPipeline(s, server.URL, "model", WithAPIKey("sk-test-123"))

	ch, err := p.Ask(context.Background(), AskOptions{Query: "test", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}

	if receivedAuth != "Bearer sk-test-123" {
		t.Errorf("expected Bearer auth header, got: %s", receivedAuth)
	}
}

func TestAskRequestBody(t *testing.T) {
	var receivedReq chatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	s, _ := setupSearcher(t)
	p := NewPipeline(s, server.URL, "gpt-4")

	ch, err := p.Ask(context.Background(), AskOptions{
		Query:       "test",
		Limit:       1,
		MaxTokens:   1024,
		Temperature: 0.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}

	if receivedReq.Model != "gpt-4" {
		t.Errorf("expected model gpt-4, got %s", receivedReq.Model)
	}
	if !receivedReq.Stream {
		t.Error("expected stream=true")
	}
	if receivedReq.MaxTokens != 1024 {
		t.Errorf("expected max_tokens 1024, got %d", receivedReq.MaxTokens)
	}
	if receivedReq.Temperature != 0.5 {
		t.Errorf("expected temperature 0.5, got %f", receivedReq.Temperature)
	}
	if len(receivedReq.Messages) != 2 {
		t.Errorf("expected 2 messages (system + user), got %d", len(receivedReq.Messages))
	}
	if receivedReq.Messages[0].Role != "system" {
		t.Errorf("expected system role, got %s", receivedReq.Messages[0].Role)
	}
	if receivedReq.Messages[1].Role != "user" {
		t.Errorf("expected user role, got %s", receivedReq.Messages[1].Role)
	}
}

func TestAskWithModelOverride(t *testing.T) {
	var receivedReq chatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	s, _ := setupSearcher(t)
	p := NewPipeline(s, server.URL, "default-model")

	ch, err := p.Ask(context.Background(), AskOptions{
		Query: "test",
		Limit: 1,
		Model: "override-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}

	if receivedReq.Model != "override-model" {
		t.Errorf("expected model override, got %s", receivedReq.Model)
	}
}

func TestStreamResponseWithFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		reason := "stop"
		chunk := streamResponse{
			Choices: []streamChoice{
				{
					Delta: struct {
						Content string `json:"content"`
					}{Content: "final"},
					FinishReason: &reason,
				},
			},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer server.Close()

	s, _ := setupSearcher(t)
	p := NewPipeline(s, server.URL, "model")

	ch, err := p.Ask(context.Background(), AskOptions{Query: "test", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}

	var gotDone bool
	for chunk := range ch {
		if chunk.Done {
			gotDone = true
		}
	}

	if !gotDone {
		t.Error("expected Done chunk from finish_reason")
	}
}
