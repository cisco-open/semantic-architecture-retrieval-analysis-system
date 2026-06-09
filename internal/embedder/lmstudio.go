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

// LMStudioEmbedder implements Embedder using the LM Studio OpenAI-compatible API.
type LMStudioEmbedder struct {
	endpoint   string
	model      string
	dimensions int
	client     *http.Client
}

type lmStudioRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type lmStudioResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

type lmStudioErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// LMStudioOption configures a LMStudioEmbedder.
type LMStudioOption func(*LMStudioEmbedder)

// WithLMStudioEndpoint sets the LM Studio API base URL.
func WithLMStudioEndpoint(endpoint string) LMStudioOption {
	return func(e *LMStudioEmbedder) { e.endpoint = endpoint }
}

// WithLMStudioModel sets the embedding model name.
func WithLMStudioModel(model string) LMStudioOption {
	return func(e *LMStudioEmbedder) { e.model = model }
}

// WithLMStudioDimensions sets the expected vector dimensionality.
func WithLMStudioDimensions(d int) LMStudioOption {
	return func(e *LMStudioEmbedder) { e.dimensions = d }
}

// WithLMStudioHTTPClient sets a custom HTTP client (useful for testing).
func WithLMStudioHTTPClient(c *http.Client) LMStudioOption {
	return func(e *LMStudioEmbedder) { e.client = c }
}

// NewLMStudioEmbedder creates an LM Studio-backed embedder.
func NewLMStudioEmbedder(opts ...LMStudioOption) *LMStudioEmbedder {
	e := &LMStudioEmbedder{
		endpoint:   "http://127.0.0.1:1234",
		model:      "text-embedding-nomic-embed-text-v1.5",
		dimensions: 768,
		client:     &http.Client{Timeout: 60 * time.Second},
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *LMStudioEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

func (e *LMStudioEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	body, err := json.Marshal(lmStudioRequest{Model: e.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/embeddings", e.endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request to LM Studio: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp lmStudioErrorResponse
		msg := string(respBody)
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Message != "" {
			msg = errResp.Error.Message
		}
		if strings.Contains(msg, "context length") ||
			strings.Contains(msg, "too many tokens") ||
			strings.Contains(msg, "maximum context") {
			totalChars := 0
			for _, t := range texts {
				totalChars += len(t)
			}
			return nil, NewContextLengthError(0, totalChars/4, 0, msg)
		}
		return nil, fmt.Errorf("LM Studio returned status %d: %s", resp.StatusCode, msg)
	}

	var result lmStudioResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(result.Data) != len(texts) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(texts), len(result.Data))
	}

	embeddings := make([][]float32, len(texts))
	for _, item := range result.Data {
		if item.Index < 0 || item.Index >= len(texts) {
			return nil, fmt.Errorf("invalid embedding index %d for batch size %d", item.Index, len(texts))
		}
		embeddings[item.Index] = item.Embedding
	}
	return embeddings, nil
}

func (e *LMStudioEmbedder) Dimensions() int { return e.dimensions }
func (e *LMStudioEmbedder) Close() error    { return nil }

// Ping checks whether the LM Studio server is reachable.
func (e *LMStudioEmbedder) Ping(ctx context.Context) error {
	url := fmt.Sprintf("%s/v1/models", e.endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("reach LM Studio at %s: %w", e.endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("LM Studio returned status %d", resp.StatusCode)
	}
	return nil
}
