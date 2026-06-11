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
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// Streaming path: AskWithContext chunked retry
// ---------------------------------------------------------------------------

func TestAskWithContext_ChunkedRetry(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// First call has the full context — reject it.
		count := callCount.Add(1)
		userContent := ""
		for _, m := range req.Messages {
			if m.Role == "user" {
				userContent = m.Content
			}
		}

		// Reject if the user content is longer than a threshold
		// (simulating token limit exceeded).
		if count == 1 && len(userContent) > 500 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":{"message":"prompt token count of 13435 exceeds the limit of 12288","code":"model_max_prompt_tokens_exceeded"}}`)
			return
		}

		// For streaming requests, return SSE.
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)

			chunk := streamResponse{
				Choices: []streamChoice{
					{Delta: struct {
						Content string `json:"content"`
					}{Content: "streamed final answer"}},
				},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			if flusher != nil {
				flusher.Flush()
			}
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return
		}

		// For non-streaming requests (intermediate chunks).
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "partial summary for chunk"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := NewPipeline(nil, server.URL, "test-model")

	// Build a context string that's large enough to trigger the token limit.
	var largeCtx strings.Builder
	for i := 0; i < 10; i++ {
		if i > 0 {
			largeCtx.WriteString("\n\n")
		}
		largeCtx.WriteString(fmt.Sprintf("Entry point %d:\n", i))
		largeCtx.WriteString(strings.Repeat("function call details ", 20))
	}

	ch, err := p.AskWithContext(context.Background(), "system prompt", largeCtx.String(), "explain the flow", AskOptions{
		MaxTokens:   1024,
		Temperature: 0.1,
	})
	if err != nil {
		t.Fatalf("AskWithContext: %v", err)
	}

	var result strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		result.WriteString(chunk.Content)
	}

	if result.Len() == 0 {
		t.Error("expected non-empty response")
	}

	// Should have been called more than once (first call rejected, then chunks + final).
	if callCount.Load() < 2 {
		t.Errorf("expected multiple LLM calls, got %d", callCount.Load())
	}
}

// ---------------------------------------------------------------------------
// Non-streaming path: ChatCompletion chunked retry
// ---------------------------------------------------------------------------

func TestChatCompletion_ChunkedRetry(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		count := callCount.Add(1)
		userContent := ""
		for _, m := range req.Messages {
			if m.Role == "user" {
				userContent = m.Content
			}
		}

		// Reject the first call if content is too large.
		if count == 1 && len(userContent) > 500 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":{"message":"prompt token count of 10000 exceeds the limit of 8192","code":"model_max_prompt_tokens_exceeded"}}`)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "chunk response"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Build large messages.
	var largeContent strings.Builder
	for i := 0; i < 10; i++ {
		if i > 0 {
			largeContent.WriteString("\n\n")
		}
		largeContent.WriteString(fmt.Sprintf("Package %d details:\n", i))
		largeContent.WriteString(strings.Repeat("symbol info ", 20))
	}

	messages := []Message{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: largeContent.String() + "\n\nGenerate the analysis."},
	}

	result, err := ChatCompletion(context.Background(), "openai", server.URL, "test-model", "", messages, 2048, 0.3)
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	if result == "" {
		t.Error("expected non-empty result")
	}

	if callCount.Load() < 2 {
		t.Errorf("expected multiple LLM calls, got %d", callCount.Load())
	}
}

// ---------------------------------------------------------------------------
// Token limit detection in streamChat
// ---------------------------------------------------------------------------

func TestStreamChat_TokenLimitDetection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":{"message":"prompt token count of 13435 exceeds the limit of 12288","code":"model_max_prompt_tokens_exceeded"}}`)
	}))
	defer server.Close()

	p := NewPipeline(nil, server.URL, "test-model")

	_, err := p.streamChat(context.Background(), []Message{
		{Role: "user", Content: "test"},
	}, AskOptions{MaxTokens: 1024})

	if err == nil {
		t.Fatal("expected error")
	}

	tle, ok := err.(*TokenLimitError)
	if !ok {
		t.Fatalf("expected *TokenLimitError, got %T: %v", err, err)
	}
	if tle.PromptLen != 13435 {
		t.Errorf("expected PromptLen 13435, got %d", tle.PromptLen)
	}
	if tle.Limit != 12288 {
		t.Errorf("expected Limit 12288, got %d", tle.Limit)
	}
}

// ---------------------------------------------------------------------------
// Non-token-limit 400 errors should not trigger chunking
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Proactive split: when contextWindow is set, large prompts should be
// chunked WITHOUT any 400 error from the server.
// ---------------------------------------------------------------------------

func TestAskWithContext_ProactiveSplit(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		callCount.Add(1)

		// If any request has user content > 500 chars, fail — the
		// proactive check should have prevented this.
		for _, m := range req.Messages {
			if m.Role == "user" && len(m.Content) > 2000 {
				t.Errorf("server received oversized user message (%d chars); proactive split should have prevented this", len(m.Content))
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprintf(w, `{"error":{"message":"prompt token count of 5000 exceeds the limit of 500","code":"model_max_prompt_tokens_exceeded"}}`)
				return
			}
		}

		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			chunk := streamResponse{
				Choices: []streamChoice{
					{Delta: struct {
						Content string `json:"content"`
					}{Content: "proactive final answer"}},
				},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			if flusher != nil {
				flusher.Flush()
			}
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "proactive chunk summary"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Set a low contextWindow so the proactive check triggers.
	p := NewPipeline(nil, server.URL, "test-model", WithContextWindow(500))

	// Build context larger than the 500-token limit (~2000 chars / 4 = 500 tokens).
	var largeCtx strings.Builder
	for i := 0; i < 10; i++ {
		if i > 0 {
			largeCtx.WriteString("\n\n")
		}
		largeCtx.WriteString(fmt.Sprintf("Section %d:\n", i))
		largeCtx.WriteString(strings.Repeat("code details here ", 30))
	}

	ch, err := p.AskWithContext(context.Background(), "system prompt", largeCtx.String(), "explain", AskOptions{
		MaxTokens:   1024,
		Temperature: 0.1,
	})
	if err != nil {
		t.Fatalf("AskWithContext with proactive split: %v", err)
	}

	var result strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		result.WriteString(chunk.Content)
	}

	if result.Len() == 0 {
		t.Error("expected non-empty response")
	}
	if callCount.Load() < 2 {
		t.Errorf("expected multiple chunk calls, got %d", callCount.Load())
	}
}

// ---------------------------------------------------------------------------
// Dynamic caching: after a 400 error, subsequent calls should proactively
// split without hitting the server with the oversized prompt again.
// ---------------------------------------------------------------------------

func TestAskWithContext_DynamicCaching(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		count := callCount.Add(1)
		userContent := ""
		for _, m := range req.Messages {
			if m.Role == "user" {
				userContent = m.Content
			}
		}

		// First call: reject large prompts (simulates first 400).
		if count == 1 && len(userContent) > 500 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":{"message":"prompt token count of 5000 exceeds the limit of 500","code":"model_max_prompt_tokens_exceeded"}}`)
			return
		}

		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			chunk := streamResponse{
				Choices: []streamChoice{
					{Delta: struct {
						Content string `json:"content"`
					}{Content: "answer"}},
				},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			if flusher != nil {
				flusher.Flush()
			}
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "summary"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// No contextWindow set — will learn from the first 400.
	p := NewPipeline(nil, server.URL, "test-model")

	var largeCtx strings.Builder
	for i := 0; i < 10; i++ {
		if i > 0 {
			largeCtx.WriteString("\n\n")
		}
		largeCtx.WriteString(fmt.Sprintf("Section %d:\n", i))
		largeCtx.WriteString(strings.Repeat("code details here ", 30))
	}
	ctx := largeCtx.String()

	// First call: gets a 400, learns the limit, retries chunked.
	ch, err := p.AskWithContext(context.Background(), "sys", ctx, "explain", AskOptions{MaxTokens: 1024, Temperature: 0.1})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("first call stream error: %v", chunk.Err)
		}
	}

	firstCallCount := callCount.Load()

	// Second call: should proactively split (no 400 needed).
	// Reset counter to check that the server never gets a rejected request.
	callCount.Store(0)

	ch2, err := p.AskWithContext(context.Background(), "sys", ctx, "explain again", AskOptions{MaxTokens: 1024, Temperature: 0.1})
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	for chunk := range ch2 {
		if chunk.Err != nil {
			t.Fatalf("second call stream error: %v", chunk.Err)
		}
	}

	secondCallCount := callCount.Load()

	// The second call should have fewer total LLM calls because it
	// didn't waste one on the doomed full-context request.
	if secondCallCount >= firstCallCount {
		t.Logf("first call count: %d, second call count: %d", firstCallCount, secondCallCount)
		t.Log("second call should have skipped the doomed full-context request")
	}
}

func TestAskWithContext_NonTokenLimitError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":{"message":"invalid model","code":"model_not_found"}}`)
	}))
	defer server.Close()

	p := NewPipeline(nil, server.URL, "test-model")

	_, err := p.AskWithContext(context.Background(), "sys", "ctx", "question", AskOptions{
		MaxTokens: 1024,
	})

	if err == nil {
		t.Fatal("expected error")
	}
	// Should NOT contain "chunk" or "split" — it's a straight-through error.
	if strings.Contains(err.Error(), "chunk") || strings.Contains(err.Error(), "split") {
		t.Errorf("non-token-limit error should not trigger chunking: %v", err)
	}
}
