/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OllamaEmbedder implements Embedder using the Ollama REST API.
type OllamaEmbedder struct {
	endpoint   string
	model      string
	dimensions int
	client     *http.Client
}

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaResponse struct {
	Embedding []float32 `json:"embedding"`
}

// OllamaOption configures an OllamaEmbedder.
type OllamaOption func(*OllamaEmbedder)

// WithOllamaEndpoint sets the Ollama API base URL.
func WithOllamaEndpoint(endpoint string) OllamaOption {
	return func(e *OllamaEmbedder) { e.endpoint = endpoint }
}

// WithOllamaModel sets the embedding model name.
func WithOllamaModel(model string) OllamaOption {
	return func(e *OllamaEmbedder) { e.model = model }
}

// WithOllamaDimensions sets the expected vector dimensionality.
func WithOllamaDimensions(d int) OllamaOption {
	return func(e *OllamaEmbedder) { e.dimensions = d }
}

// WithOllamaHTTPClient sets a custom HTTP client (useful for testing).
func WithOllamaHTTPClient(c *http.Client) OllamaOption {
	return func(e *OllamaEmbedder) { e.client = c }
}

// NewOllamaEmbedder creates an Ollama-backed embedder.
func NewOllamaEmbedder(opts ...OllamaOption) *OllamaEmbedder {
	e := &OllamaEmbedder{
		endpoint:   "http://localhost:11434",
		model:      "nomic-embed-text",
		dimensions: 768,
		client:     &http.Client{Timeout: 60 * time.Second},
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(ollamaRequest{Model: e.model, Prompt: text})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/embeddings", e.endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request to Ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		msg := string(respBody)
		if resp.StatusCode == http.StatusInternalServerError &&
			strings.Contains(msg, "exceeds the context length") {
			estimatedTokens := len(text) / 4
			return nil, NewContextLengthError(0, estimatedTokens, 0, msg)
		}
		return nil, fmt.Errorf("Ollama returned status %d: %s", resp.StatusCode, msg)
	}

	var result ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("Ollama returned empty embedding")
	}
	return result.Embedding, nil
}

func (e *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		vec, err := e.Embed(ctx, text)
		if err != nil {
			if ctxErr := AsContextLengthError(err); ctxErr != nil {
				ctxErr.ChunkIndex = i
				return nil, ctxErr
			}
			return nil, fmt.Errorf("embed text %d: %w", i, err)
		}
		results[i] = vec
	}
	return results, nil
}

func (e *OllamaEmbedder) Dimensions() int { return e.dimensions }
func (e *OllamaEmbedder) Close() error    { return nil }

// Ping checks whether the Ollama server is reachable.
func (e *OllamaEmbedder) Ping(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/tags", e.endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("reach Ollama at %s: %w", e.endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Ollama returned status %d", resp.StatusCode)
	}
	return nil
}
