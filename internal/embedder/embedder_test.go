/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package embedder

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
)

// ---------------------------------------------------------------------------
// Error tests
// ---------------------------------------------------------------------------

func TestContextLengthError(t *testing.T) {
	err := NewContextLengthError(8192, 10000, 3, "too long")
	if err.MaxTokens != 8192 {
		t.Errorf("expected MaxTokens 8192, got %d", err.MaxTokens)
	}
	if err.RequestTokens != 10000 {
		t.Errorf("expected RequestTokens 10000, got %d", err.RequestTokens)
	}
	if err.ChunkIndex != 3 {
		t.Errorf("expected ChunkIndex 3, got %d", err.ChunkIndex)
	}
	msg := err.Error()
	if !strings.Contains(msg, "10000 tokens requested") {
		t.Errorf("expected token count in message, got: %s", msg)
	}
	if !strings.Contains(msg, "max 8192") {
		t.Errorf("expected max in message, got: %s", msg)
	}
}

func TestContextLengthErrorZeroMax(t *testing.T) {
	err := NewContextLengthError(0, 5000, 0, "context exceeded")
	msg := err.Error()
	if strings.Contains(msg, "max 0") {
		t.Errorf("should not show max when 0, got: %s", msg)
	}
	if !strings.Contains(msg, "context exceeded") {
		t.Errorf("expected detail in message, got: %s", msg)
	}
}

func TestAsContextLengthError(t *testing.T) {
	orig := NewContextLengthError(100, 200, 0, "test")
	wrapped := fmt.Errorf("wrapped: %w", orig)

	extracted := AsContextLengthError(wrapped)
	if extracted == nil {
		t.Fatal("expected to extract ContextLengthError from wrapped error")
	}
	if extracted.MaxTokens != 100 {
		t.Errorf("expected MaxTokens 100, got %d", extracted.MaxTokens)
	}
}

func TestAsContextLengthErrorNil(t *testing.T) {
	if AsContextLengthError(fmt.Errorf("unrelated error")) != nil {
		t.Error("expected nil for non-ContextLengthError")
	}
}

// ---------------------------------------------------------------------------
// Ollama tests
// ---------------------------------------------------------------------------

func newOllamaTestServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

func TestOllamaEmbed(t *testing.T) {
	srv := newOllamaTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			http.NotFound(w, r)
			return
		}
		var req ollamaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if req.Model != "test-model" {
			t.Errorf("expected model test-model, got %s", req.Model)
		}
		json.NewEncoder(w).Encode(ollamaResponse{
			Embedding: []float32{0.1, 0.2, 0.3},
		})
	})
	defer srv.Close()

	e := NewOllamaEmbedder(
		WithOllamaEndpoint(srv.URL),
		WithOllamaModel("test-model"),
		WithOllamaDimensions(3),
	)

	vec, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(vec) != 3 {
		t.Errorf("expected 3 dimensions, got %d", len(vec))
	}
	if vec[0] != 0.1 || vec[1] != 0.2 || vec[2] != 0.3 {
		t.Errorf("unexpected vector values: %v", vec)
	}
}

func TestOllamaEmbedBatch(t *testing.T) {
	callCount := 0
	srv := newOllamaTestServer(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(ollamaResponse{
			Embedding: []float32{float32(callCount), 0, 0},
		})
	})
	defer srv.Close()

	e := NewOllamaEmbedder(WithOllamaEndpoint(srv.URL))
	vecs, err := e.EmbedBatch(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("expected 3 results, got %d", len(vecs))
	}
	if callCount != 3 {
		t.Errorf("expected 3 API calls (sequential), got %d", callCount)
	}
	// Each call increments, so vecs[0][0]=1, vecs[1][0]=2, vecs[2][0]=3
	for i := 0; i < 3; i++ {
		if vecs[i][0] != float32(i+1) {
			t.Errorf("vecs[%d][0] = %f, expected %f", i, vecs[i][0], float32(i+1))
		}
	}
}

func TestOllamaEmbedServerError(t *testing.T) {
	srv := newOllamaTestServer(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server broke", 500)
	})
	defer srv.Close()

	e := NewOllamaEmbedder(WithOllamaEndpoint(srv.URL))
	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for server error response")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("expected status 500 in error, got: %v", err)
	}
}

func TestOllamaEmbedContextLengthError(t *testing.T) {
	srv := newOllamaTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("input exceeds the context length of the model"))
	})
	defer srv.Close()

	e := NewOllamaEmbedder(WithOllamaEndpoint(srv.URL))
	_, err := e.Embed(context.Background(), "very long text")
	if err == nil {
		t.Fatal("expected error")
	}
	ctxErr := AsContextLengthError(err)
	if ctxErr == nil {
		t.Fatalf("expected ContextLengthError, got: %v", err)
	}
}

func TestOllamaEmbedEmptyResponse(t *testing.T) {
	srv := newOllamaTestServer(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ollamaResponse{Embedding: nil})
	})
	defer srv.Close()

	e := NewOllamaEmbedder(WithOllamaEndpoint(srv.URL))
	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for empty embedding")
	}
	if !strings.Contains(err.Error(), "empty embedding") {
		t.Errorf("expected empty embedding error, got: %v", err)
	}
}

func TestOllamaEmbedBatchContextLengthPropagation(t *testing.T) {
	call := 0
	srv := newOllamaTestServer(func(w http.ResponseWriter, r *http.Request) {
		call++
		if call == 2 {
			w.WriteHeader(500)
			w.Write([]byte("exceeds the context length"))
			return
		}
		json.NewEncoder(w).Encode(ollamaResponse{Embedding: []float32{1}})
	})
	defer srv.Close()

	e := NewOllamaEmbedder(WithOllamaEndpoint(srv.URL))
	_, err := e.EmbedBatch(context.Background(), []string{"ok", "too long", "ok"})
	if err == nil {
		t.Fatal("expected error")
	}
	ctxErr := AsContextLengthError(err)
	if ctxErr == nil {
		t.Fatalf("expected ContextLengthError, got: %v", err)
	}
	if ctxErr.ChunkIndex != 1 {
		t.Errorf("expected ChunkIndex 1, got %d", ctxErr.ChunkIndex)
	}
}

func TestOllamaDimensions(t *testing.T) {
	e := NewOllamaEmbedder(WithOllamaDimensions(256))
	if e.Dimensions() != 256 {
		t.Errorf("expected 256, got %d", e.Dimensions())
	}
}

func TestOllamaClose(t *testing.T) {
	e := NewOllamaEmbedder()
	if err := e.Close(); err != nil {
		t.Errorf("expected nil error from Close, got: %v", err)
	}
}

func TestOllamaPing(t *testing.T) {
	srv := newOllamaTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.WriteHeader(200)
			w.Write([]byte(`{"models":[]}`))
			return
		}
		http.NotFound(w, r)
	})
	defer srv.Close()

	e := NewOllamaEmbedder(WithOllamaEndpoint(srv.URL))
	if err := e.Ping(context.Background()); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

func TestOllamaPingFail(t *testing.T) {
	e := NewOllamaEmbedder(WithOllamaEndpoint("http://127.0.0.1:1"))
	if err := e.Ping(context.Background()); err == nil {
		t.Fatal("expected Ping to fail for unreachable server")
	}
}

func TestOllamaDefaults(t *testing.T) {
	e := NewOllamaEmbedder()
	if e.endpoint != "http://localhost:11434" {
		t.Errorf("unexpected default endpoint: %s", e.endpoint)
	}
	if e.model != "nomic-embed-text" {
		t.Errorf("unexpected default model: %s", e.model)
	}
	if e.dimensions != 768 {
		t.Errorf("unexpected default dimensions: %d", e.dimensions)
	}
}

// ---------------------------------------------------------------------------
// LM Studio tests
// ---------------------------------------------------------------------------

func TestLMStudioEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			http.NotFound(w, r)
			return
		}
		var req lmStudioRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "test-lm-model" {
			t.Errorf("expected model test-lm-model, got %s", req.Model)
		}
		resp := lmStudioResponse{}
		for i := range req.Input {
			resp.Data = append(resp.Data, struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				Embedding: []float32{0.5, 0.6},
				Index:     i,
			})
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := NewLMStudioEmbedder(
		WithLMStudioEndpoint(srv.URL),
		WithLMStudioModel("test-lm-model"),
		WithLMStudioDimensions(2),
	)

	vec, err := e.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(vec) != 2 {
		t.Errorf("expected 2 dimensions, got %d", len(vec))
	}
}

func TestLMStudioEmbedBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req lmStudioRequest
		json.NewDecoder(r.Body).Decode(&req)
		resp := lmStudioResponse{}
		for i := range req.Input {
			resp.Data = append(resp.Data, struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				Embedding: []float32{float32(i + 1)},
				Index:     i,
			})
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := NewLMStudioEmbedder(WithLMStudioEndpoint(srv.URL))
	vecs, err := e.EmbedBatch(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 results, got %d", len(vecs))
	}
	if vecs[0][0] != 1.0 || vecs[1][0] != 2.0 {
		t.Errorf("unexpected values: %v", vecs)
	}
}

func TestLMStudioEmbedBatchEmpty(t *testing.T) {
	e := NewLMStudioEmbedder()
	vecs, err := e.EmbedBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vecs != nil {
		t.Errorf("expected nil for empty input, got %v", vecs)
	}
}

func TestLMStudioEmbedServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(lmStudioErrorResponse{Error: struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		}{Message: "internal error", Type: "server_error"}})
	}))
	defer srv.Close()

	e := NewLMStudioEmbedder(WithLMStudioEndpoint(srv.URL))
	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "internal error") {
		t.Errorf("expected error message, got: %v", err)
	}
}

func TestLMStudioEmbedContextLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":{"message":"context length exceeded"}}`))
	}))
	defer srv.Close()

	e := NewLMStudioEmbedder(WithLMStudioEndpoint(srv.URL))
	_, err := e.Embed(context.Background(), "long text")
	if err == nil {
		t.Fatal("expected error")
	}
	if AsContextLengthError(err) == nil {
		t.Errorf("expected ContextLengthError, got: %v", err)
	}
}

func TestLMStudioEmbedCountMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 1 embedding for 2 inputs
		json.NewEncoder(w).Encode(lmStudioResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{{Embedding: []float32{1}, Index: 0}},
		})
	}))
	defer srv.Close()

	e := NewLMStudioEmbedder(WithLMStudioEndpoint(srv.URL))
	_, err := e.EmbedBatch(context.Background(), []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error for count mismatch")
	}
}

func TestLMStudioDimensions(t *testing.T) {
	e := NewLMStudioEmbedder(WithLMStudioDimensions(512))
	if e.Dimensions() != 512 {
		t.Errorf("expected 512, got %d", e.Dimensions())
	}
}

func TestLMStudioClose(t *testing.T) {
	e := NewLMStudioEmbedder()
	if err := e.Close(); err != nil {
		t.Errorf("expected nil from Close, got: %v", err)
	}
}

func TestLMStudioPing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.WriteHeader(200)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	e := NewLMStudioEmbedder(WithLMStudioEndpoint(srv.URL))
	if err := e.Ping(context.Background()); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

func TestLMStudioDefaults(t *testing.T) {
	e := NewLMStudioEmbedder()
	if e.endpoint != "http://127.0.0.1:1234" {
		t.Errorf("unexpected default endpoint: %s", e.endpoint)
	}
	if e.model != "text-embedding-nomic-embed-text-v1.5" {
		t.Errorf("unexpected default model: %s", e.model)
	}
	if e.dimensions != 768 {
		t.Errorf("unexpected default dimensions: %d", e.dimensions)
	}
}

// ---------------------------------------------------------------------------
// OpenAI tests
// ---------------------------------------------------------------------------

func TestOpenAIEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			http.NotFound(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", auth)
		}
		var req openAIEmbedRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "text-embedding-3-small" {
			t.Errorf("unexpected model: %s", req.Model)
		}
		resp := openAIEmbedResponse{}
		for i := range req.Input {
			resp.Data = append(resp.Data, struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				Embedding: []float32{0.1, 0.2, 0.3, 0.4},
				Index:     i,
			})
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e, err := NewOpenAIEmbedder(
		WithOpenAIEndpoint(srv.URL),
		WithOpenAIKey("test-key"),
		WithOpenAIDimensions(4),
	)
	if err != nil {
		t.Fatal(err)
	}

	vec, err := e.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(vec) != 4 {
		t.Errorf("expected 4 dimensions, got %d", len(vec))
	}
}

func TestOpenAIEmbedBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openAIEmbedRequest
		json.NewDecoder(r.Body).Decode(&req)
		resp := openAIEmbedResponse{}
		for i := range req.Input {
			resp.Data = append(resp.Data, struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				Embedding: []float32{float32(i)},
				Index:     i,
			})
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e, _ := NewOpenAIEmbedder(WithOpenAIEndpoint(srv.URL))
	vecs, err := e.EmbedBatch(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("expected 3 results, got %d", len(vecs))
	}
}

func TestOpenAIEmbedBatchSplitsLargeBatches(t *testing.T) {
	var requestSizes []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openAIEmbedRequest
		json.NewDecoder(r.Body).Decode(&req)
		requestSizes = append(requestSizes, len(req.Input))

		resp := openAIEmbedResponse{}
		for i := range req.Input {
			resp.Data = append(resp.Data, struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				Embedding: []float32{float32(len(requestSizes)), float32(i)},
				Index:     i,
			})
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	texts := make([]string, 358)
	for i := range texts {
		texts[i] = fmt.Sprintf("text-%d", i)
	}

	e, _ := NewOpenAIEmbedder(WithOpenAIEndpoint(srv.URL))
	vecs, err := e.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}
	if len(vecs) != len(texts) {
		t.Fatalf("expected %d results, got %d", len(texts), len(vecs))
	}
	if len(requestSizes) != 2 {
		t.Fatalf("expected 2 requests, got %d with sizes %v", len(requestSizes), requestSizes)
	}
	if requestSizes[0] != openAICompatibleMaxBatchSize || requestSizes[1] != 158 {
		t.Errorf("unexpected request sizes: %v", requestSizes)
	}
	if vecs[199][0] != 1 || vecs[199][1] != 199 {
		t.Errorf("unexpected final vector from first batch: %v", vecs[199])
	}
	if vecs[200][0] != 2 || vecs[200][1] != 0 {
		t.Errorf("unexpected first vector from second batch: %v", vecs[200])
	}
}

func TestOpenAIEmbedBatchEmpty(t *testing.T) {
	e, _ := NewOpenAIEmbedder()
	vecs, err := e.EmbedBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vecs != nil {
		t.Errorf("expected nil for empty input")
	}
}

func TestOpenAIEmbedUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":{"message":"invalid key"}}`))
	}))
	defer srv.Close()

	e, _ := NewOpenAIEmbedder(WithOpenAIEndpoint(srv.URL))
	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("expected auth error, got: %v", err)
	}
}

func TestOpenAIEmbedRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"error":{"message":"rate limit exceeded"}}`))
	}))
	defer srv.Close()

	e, _ := NewOpenAIEmbedder(WithOpenAIEndpoint(srv.URL))
	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("expected rate limit error, got: %v", err)
	}
}

func TestOpenAIEmbedContextLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":{"message":"maximum context length exceeded","code":"context_length_exceeded"}}`))
	}))
	defer srv.Close()

	e, _ := NewOpenAIEmbedder(WithOpenAIEndpoint(srv.URL))
	_, err := e.Embed(context.Background(), "long text")
	if err == nil {
		t.Fatal("expected error")
	}
	if AsContextLengthError(err) == nil {
		t.Errorf("expected ContextLengthError, got: %v", err)
	}
}

func TestOpenAIEmbedCountMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(openAIEmbedResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{{Embedding: []float32{1}, Index: 0}},
		})
	}))
	defer srv.Close()

	e, _ := NewOpenAIEmbedder(WithOpenAIEndpoint(srv.URL))
	_, err := e.EmbedBatch(context.Background(), []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error for count mismatch")
	}
}

func TestOpenAIDimensions(t *testing.T) {
	e, _ := NewOpenAIEmbedder(WithOpenAIDimensions(3072))
	if e.Dimensions() != 3072 {
		t.Errorf("expected 3072, got %d", e.Dimensions())
	}
}

func TestOpenAIClose(t *testing.T) {
	e, _ := NewOpenAIEmbedder()
	if err := e.Close(); err != nil {
		t.Errorf("expected nil from Close, got: %v", err)
	}
}

func TestOpenAIDefaults(t *testing.T) {
	e, _ := NewOpenAIEmbedder()
	if e.endpoint != "https://api.openai.com/v1" {
		t.Errorf("unexpected default endpoint: %s", e.endpoint)
	}
	if e.model != "text-embedding-3-small" {
		t.Errorf("unexpected default model: %s", e.model)
	}
	if e.dimensions != 1536 {
		t.Errorf("unexpected default dimensions: %d", e.dimensions)
	}
}

func TestOpenAINoAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "" {
			t.Errorf("expected no auth header, got: %s", auth)
		}
		json.NewEncoder(w).Encode(openAIEmbedResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{{Embedding: []float32{1}, Index: 0}},
		})
	}))
	defer srv.Close()

	e, _ := NewOpenAIEmbedder(WithOpenAIEndpoint(srv.URL))
	_, err := e.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenAIEndpointTrailingSlash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(openAIEmbedResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{{Embedding: []float32{1}, Index: 0}},
		})
	}))
	defer srv.Close()

	// Endpoint with trailing slash should not produce double slash
	e, _ := NewOpenAIEmbedder(WithOpenAIEndpoint(srv.URL + "/"))
	_, err := e.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Factory tests
// ---------------------------------------------------------------------------

func TestFactoryOllama(t *testing.T) {
	dims := 768
	cfg := &config.Config{
		Embedder: config.EmbedderConfig{
			Provider:   "ollama",
			Model:      "nomic-embed-text",
			Endpoint:   "http://localhost:11434",
			Dimensions: &dims,
		},
	}

	emb, err := NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}
	if emb.Dimensions() != 768 {
		t.Errorf("expected 768, got %d", emb.Dimensions())
	}
	if _, ok := emb.(*OllamaEmbedder); !ok {
		t.Error("expected *OllamaEmbedder")
	}
}

func TestFactoryLMStudio(t *testing.T) {
	dims := 768
	cfg := &config.Config{
		Embedder: config.EmbedderConfig{
			Provider:   "lmstudio",
			Model:      "test-model",
			Endpoint:   "http://127.0.0.1:1234",
			Dimensions: &dims,
		},
	}

	emb, err := NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}
	if _, ok := emb.(*LMStudioEmbedder); !ok {
		t.Error("expected *LMStudioEmbedder")
	}
}

func TestFactoryOpenAI(t *testing.T) {
	dims := 1536
	cfg := &config.Config{
		Embedder: config.EmbedderConfig{
			Provider:   "openai",
			Model:      "text-embedding-3-small",
			Endpoint:   "https://api.openai.com/v1",
			APIKey:     "sk-test",
			Dimensions: &dims,
		},
	}

	emb, err := NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}
	if _, ok := emb.(*OpenAIEmbedder); !ok {
		t.Error("expected *OpenAIEmbedder")
	}
}

func TestFactoryCopilot(t *testing.T) {
	dims := 1536
	cfg := &config.Config{
		Embedder: config.EmbedderConfig{
			Provider:   "copilot",
			Model:      "text-embedding-3-small",
			Endpoint:   "https://api.githubcopilot.com",
			Dimensions: &dims,
		},
	}

	emb, err := NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}
	openai, ok := emb.(*OpenAIEmbedder)
	if !ok {
		t.Fatalf("expected *OpenAIEmbedder for copilot provider, got %T", emb)
	}
	// The copilot embedder reuses the OpenAI implementation but must NOT
	// carry a static API key — the http.Client transport handles auth.
	if openai.apiKey != "" {
		t.Errorf("copilot embedder should not have a static apiKey; got %q", openai.apiKey)
	}
	if openai.client == nil {
		t.Error("copilot embedder should have a non-default HTTP client")
	}
	if !strings.HasPrefix(openai.endpoint, "https://api.githubcopilot.com") {
		t.Errorf("unexpected copilot endpoint: %s", openai.endpoint)
	}
}

func TestFactoryCopilotDefaultsEndpointWhenEmpty(t *testing.T) {
	cfg := &config.Config{
		Embedder: config.EmbedderConfig{
			Provider: "copilot",
			Model:    "text-embedding-3-small",
		},
	}
	emb, err := NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}
	openai := emb.(*OpenAIEmbedder)
	if openai.endpoint != "https://api.githubcopilot.com" {
		t.Errorf("expected default copilot endpoint, got %s", openai.endpoint)
	}
}

func TestFactoryUnknownProvider(t *testing.T) {
	cfg := &config.Config{
		Embedder: config.EmbedderConfig{
			Provider: "unknown-provider",
		},
	}

	_, err := NewFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "unknown embedding provider") {
		t.Errorf("expected unknown provider error, got: %v", err)
	}
}

func TestFactoryNilDimensions(t *testing.T) {
	cfg := &config.Config{
		Embedder: config.EmbedderConfig{
			Provider: "ollama",
			Model:    "nomic-embed-text",
			Endpoint: "http://localhost:11434",
		},
	}

	emb, err := NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}
	// Should use OllamaEmbedder default (768)
	if emb.Dimensions() != 768 {
		t.Errorf("expected default 768, got %d", emb.Dimensions())
	}
}

// ---------------------------------------------------------------------------
// ProbeDimensions tests
// ---------------------------------------------------------------------------

func TestProbeDimensions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ollamaResponse{
			Embedding: make([]float32, 1024),
		})
	}))
	defer srv.Close()

	dims, err := ProbeDimensions(context.Background(), "ollama", "test-model", srv.URL, "")
	if err != nil {
		t.Fatalf("ProbeDimensions failed: %v", err)
	}
	if dims != 1024 {
		t.Errorf("expected 1024, got %d", dims)
	}
}

func TestProbeDimensionsError(t *testing.T) {
	_, err := ProbeDimensions(context.Background(), "ollama", "test-model", "http://127.0.0.1:1", "")
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

func TestProbeDimensionsUnknownProvider(t *testing.T) {
	_, err := ProbeDimensions(context.Background(), "unknown", "model", "http://localhost", "")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

var (
	_ Embedder = (*OllamaEmbedder)(nil)
	_ Embedder = (*LMStudioEmbedder)(nil)
	_ Embedder = (*OpenAIEmbedder)(nil)
)
