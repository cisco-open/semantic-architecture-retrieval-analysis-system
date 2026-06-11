/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package ask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
)

// cachedTokenLimit stores a token limit learned from a 400 error for
// the non-streaming ChatCompletion path. Once set, subsequent calls
// proactively split instead of sending a doomed request.
var cachedTokenLimit int

// SetTokenLimit allows callers to pre-configure the known token limit
// for the non-streaming ChatCompletion path (e.g. from config).
func SetTokenLimit(n int) {
	if n > 0 {
		cachedTokenLimit = n
	}
}

// ChatCompletion performs a non-streaming chat completion.
// provider should be "ollama", "lmstudio", "openai", "copilot", etc.
// baseEndpoint is the raw endpoint (e.g. http://localhost:11434), NOT the /v1 suffixed one.
// For Ollama, uses native /api/chat with think:false to disable chain-of-thought.
// For others, uses an OpenAI-compatible /chat/completions endpoint.
//
// httpClient is optional; when non-nil it is used for the underlying HTTP
// transport. Callers wiring up the GitHub Copilot provider should pass an
// *http.Client whose Transport handles bearer-token refresh and the
// Copilot-required headers.
func ChatCompletion(ctx context.Context, provider, baseEndpoint, model, apiKey string, messages []Message, maxTokens int, temperature float32, httpClient ...*http.Client) (string, error) {
	baseEndpoint = strings.TrimRight(baseEndpoint, "/")

	var client *http.Client
	if len(httpClient) > 0 && httpClient[0] != nil {
		client = httpClient[0]
	}

	// callFn wraps the provider-specific completion so the chunking
	// retry can re-invoke it with smaller messages.
	callFn := func(msgs []Message) (string, error) {
		if provider == "ollama" {
			return ollamaChatCompletion(ctx, baseEndpoint, model, msgs, maxTokens, temperature, client)
		}
		return openaiChatCompletion(ctx, baseEndpoint, model, apiKey, msgs, maxTokens, temperature, client)
	}

	// Proactively chunk if the token limit is known and would be exceeded.
	if cachedTokenLimit > 0 && EstimateTokens(messages) > cachedTokenLimit {
		return chatCompletionChunked(ctx, callFn, messages, maxTokens, temperature)
	}

	result, err := callFn(messages)
	if err != nil {
		var tle *TokenLimitError
		if errors.As(err, &tle) {
			if tle.Limit > 0 {
				cachedTokenLimit = tle.Limit
				config.UpdateContextWindow(tle.Limit)
			}
			return chatCompletionChunked(ctx, callFn, messages, maxTokens, temperature)
		}
	}
	return result, err
}

// chatCompletionChunked implements the sliding-window chunking protocol
// for non-streaming ChatCompletion calls. It finds the longest user
// message, splits its content, and processes chunks serially with the
// prior summary carried forward.
func chatCompletionChunked(ctx context.Context, callFn func([]Message) (string, error), messages []Message, maxTokens int, temperature float32) (string, error) {
	// Find the longest user message — that's the one with the context data.
	bestIdx := -1
	bestLen := 0
	var systemPrompt, question string
	for i, m := range messages {
		if m.Role == "system" && systemPrompt == "" {
			systemPrompt = m.Content
		}
		if m.Role == "user" && len(m.Content) > bestLen {
			bestIdx = i
			bestLen = len(m.Content)
		}
	}
	if bestIdx < 0 {
		return "", fmt.Errorf("chat: prompt exceeds token limit but no user message found to split")
	}

	// Try to extract the question from the user message (after last "\n\n").
	userContent := messages[bestIdx].Content
	if idx := strings.LastIndex(userContent, "\n\n"); idx > 0 {
		question = strings.TrimSpace(userContent[idx:])
		userContent = userContent[:idx]
	}

	chunks := SplitContext(userContent, 0)
	if len(chunks) <= 1 {
		return "", fmt.Errorf("chat: prompt exceeds token limit and context cannot be split further")
	}

	var priorSummary string

	for i, chunk := range chunks {
		var userMsg string
		if i == len(chunks)-1 {
			userMsg = buildFinalChunkUserMessage(chunk, priorSummary, question, len(chunks))
		} else {
			userMsg = buildChunkUserMessage(i, len(chunks), chunk, priorSummary, question)
		}

		chunkMsgs := []Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMsg},
		}

		partial, err := callFn(chunkMsgs)
		if err != nil {
			var tle *TokenLimitError
			if errors.As(err, &tle) {
				// Sub-split and expand remaining chunks.
				subChunks := SplitContext(chunk, 1)
				if len(subChunks) <= 1 {
					return "", fmt.Errorf("chat: chunk %d still exceeds token limit after sub-splitting", i+1)
				}
				newChunks := make([]string, 0, len(subChunks)+len(chunks)-i-1)
				newChunks = append(newChunks, subChunks...)
				newChunks = append(newChunks, chunks[i+1:]...)
				chunks = append(chunks[:i], newChunks...)
				i-- // retry from the same index
				continue
			}
			return "", fmt.Errorf("chat: chunk %d/%d failed: %w", i+1, len(chunks), err)
		}

		priorSummary = partial
	}

	return priorSummary, nil
}

// ollamaChatCompletion uses Ollama's native /api/chat endpoint where think:false works.
func ollamaChatCompletion(ctx context.Context, baseEndpoint, model string, messages []Message, maxTokens int, temperature float32, client *http.Client) (string, error) {
	type ollamaMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	type ollamaRequest struct {
		Model    string          `json:"model"`
		Messages []ollamaMessage `json:"messages"`
		Stream   bool            `json:"stream"`
		Think    bool            `json:"think"`
		Options  map[string]any  `json:"options,omitempty"`
	}

	msgs := make([]ollamaMessage, len(messages))
	for i, m := range messages {
		msgs[i] = ollamaMessage{Role: m.Role, Content: m.Content}
	}

	opts := map[string]any{}
	if maxTokens > 0 {
		opts["num_predict"] = maxTokens
	}
	if temperature > 0 {
		opts["temperature"] = temperature
	}

	reqBody := ollamaRequest{
		Model:    model,
		Messages: msgs,
		Stream:   false,
		Think:    false,
		Options:  opts,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := baseEndpoint + "/api/chat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if client == nil {
		client = &http.Client{Timeout: 300 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if tle := isTokenLimitError(resp.StatusCode, respBody); tle != nil {
			return "", tle
		}
		return "", fmt.Errorf("Ollama returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var ollamaResp struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Done bool `json:"done"`
	}

	if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
		return "", fmt.Errorf("parse ollama response: %w (body: %.200s)", err, string(respBody))
	}

	return ollamaResp.Message.Content, nil
}

// openaiChatCompletion uses the standard OpenAI /v1/chat/completions endpoint.
func openaiChatCompletion(ctx context.Context, baseEndpoint, model, apiKey string, messages []Message, maxTokens int, temperature float32, client *http.Client) (string, error) {
	reqBody := chatRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: temperature,
		Stream:      false,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	// baseEndpoint is expected to be ready-to-use (callers should run it
	// through buildChatEndpoint which adds /v1 only for providers that
	// need it; GitHub Copilot exposes /chat/completions directly under
	// the API root with no /v1 prefix).
	url := strings.TrimRight(baseEndpoint, "/") + "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	if client == nil {
		client = &http.Client{Timeout: 300 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if tle := isTokenLimitError(resp.StatusCode, respBody); tle != nil {
			return "", tle
		}
		return "", fmt.Errorf("LLM returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("parse response: %w (body: %.200s)", err, string(respBody))
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("LLM returned no choices (body: %.500s)", string(respBody))
	}

	return chatResp.Choices[0].Message.Content, nil
}
