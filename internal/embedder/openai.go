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

// OpenAIEmbedder implements Embedder using the OpenAI embeddings API.
// It also works with any OpenAI-compatible endpoint (e.g. vLLM, together.ai, etc.).
type OpenAIEmbedder struct {
	endpoint   string
	model      string
	apiKey     string
	dimensions int
	client     *http.Client
}

// Keep OpenAI-compatible embedding requests below stricter provider limits
// such as Vertex AI's 251-exclusive batch size cap.
const openAICompatibleMaxBatchSize = 200

type openAIEmbedRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions *int     `json:"dimensions,omitempty"`
}

type openAIEmbedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

type openAIErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// OpenAIOption configures an OpenAIEmbedder.
type OpenAIOption func(*OpenAIEmbedder)

// WithOpenAIEndpoint sets the API base URL.
func WithOpenAIEndpoint(endpoint string) OpenAIOption {
	return func(e *OpenAIEmbedder) { e.endpoint = endpoint }
}

// WithOpenAIModel sets the embedding model name.
func WithOpenAIModel(model string) OpenAIOption {
	return func(e *OpenAIEmbedder) { e.model = model }
}

// WithOpenAIKey sets the API key for authentication.
func WithOpenAIKey(key string) OpenAIOption {
	return func(e *OpenAIEmbedder) { e.apiKey = key }
}

// WithOpenAIDimensions sets the expected vector dimensionality.
func WithOpenAIDimensions(d int) OpenAIOption {
	return func(e *OpenAIEmbedder) { e.dimensions = d }
}

// WithOpenAIHTTPClient sets a custom HTTP client (useful for testing).
func WithOpenAIHTTPClient(c *http.Client) OpenAIOption {
	return func(e *OpenAIEmbedder) { e.client = c }
}

// NewOpenAIEmbedder creates an OpenAI-compatible embedder.
func NewOpenAIEmbedder(opts ...OpenAIOption) (*OpenAIEmbedder, error) {
	e := &OpenAIEmbedder{
		endpoint:   "https://api.openai.com/v1",
		model:      "text-embedding-3-small",
		dimensions: 1536,
		client:     &http.Client{Timeout: 60 * time.Second},
	}
	for _, o := range opts {
		o(e)
	}
	return e, nil
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

func (e *OpenAIEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if len(texts) > openAICompatibleMaxBatchSize {
		embeddings := make([][]float32, len(texts))
		for start := 0; start < len(texts); start += openAICompatibleMaxBatchSize {
			end := start + openAICompatibleMaxBatchSize
			if end > len(texts) {
				end = len(texts)
			}
			batch, err := e.embedBatchRequest(ctx, texts[start:end])
			if err != nil {
				if ctxErr := AsContextLengthError(err); ctxErr != nil {
					ctxErr.ChunkIndex += start
				}
				return nil, err
			}
			copy(embeddings[start:end], batch)
		}
		return embeddings, nil
	}

	return e.embedBatchRequest(ctx, texts)
}

func (e *OpenAIEmbedder) embedBatchRequest(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := openAIEmbedRequest{
		Model: e.model,
		Input: texts,
	}
	if e.dimensions > 0 {
		reqBody.Dimensions = &e.dimensions
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/embeddings", strings.TrimRight(e.endpoint, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request to OpenAI: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp openAIErrorResponse
		msg := string(respBody)
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Message != "" {
			msg = errResp.Error.Message
		}

		if strings.Contains(msg, "context_length_exceeded") ||
			strings.Contains(msg, "maximum context length") ||
			strings.Contains(msg, "too many tokens") {
			totalChars := 0
			for _, t := range texts {
				totalChars += len(t)
			}
			return nil, NewContextLengthError(0, totalChars/4, 0, msg)
		}

		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("authentication failed (status 401): check your API key")
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, fmt.Errorf("rate limited (status 429): %s", msg)
		}

		return nil, fmt.Errorf("OpenAI returned status %d: %s", resp.StatusCode, msg)
	}

	var result openAIEmbedResponse
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

func (e *OpenAIEmbedder) Dimensions() int { return e.dimensions }
func (e *OpenAIEmbedder) Close() error    { return nil }
