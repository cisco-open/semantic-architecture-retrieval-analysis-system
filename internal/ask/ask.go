/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package ask

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/search"
)

// Message represents a single chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// StreamChunk is one piece of a streaming LLM response.
type StreamChunk struct {
	Content string
	Done    bool
	Err     error
}

// AskOptions configures an ask request.
type AskOptions struct {
	Query        string
	Limit        int
	MaxTokens    int
	Temperature  float32
	Model        string
	ExtraContext string // optional additional context prepended to RAG results
}

// Pipeline orchestrates search → context building → LLM chat completion.
type Pipeline struct {
	searcher     *search.Searcher
	endpoint     string
	model        string
	apiKey       string
	httpClient   *http.Client
	systemPrompt string
}

// PipelineOption configures a Pipeline.
type PipelineOption func(*Pipeline)

// WithAPIKey sets the API key for the LLM endpoint.
func WithAPIKey(key string) PipelineOption {
	return func(p *Pipeline) { p.apiKey = key }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(c *http.Client) PipelineOption {
	return func(p *Pipeline) { p.httpClient = c }
}

// WithSystemPrompt overrides the default system prompt.
func WithSystemPrompt(prompt string) PipelineOption {
	return func(p *Pipeline) { p.systemPrompt = prompt }
}

// NewPipeline creates a new RAG pipeline.
func NewPipeline(searcher *search.Searcher, endpoint, model string, opts ...PipelineOption) *Pipeline {
	p := &Pipeline{
		searcher: searcher,
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
		systemPrompt: defaultSystemPrompt,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

const defaultSystemPrompt = `You are SARAS, a code assistant. Use provided snippets as the primary source of truth.

For each question/sub-question:
- Use relevant snippets only.

Decision rule (MANDATORY):
1. If ANY relevant symbol, function, type, or pattern exists:
   - You MUST answer using those signals.
   - Infer the mechanism at a high level from naming and structure.
2. Only if absolutely nothing relevant exists:
   - Output: Not enough information in provided snippets.

Answer rules:
- Prefer incomplete but useful answers over empty ones.
- Infer structure/mechanism from code patterns.
- Do NOT invent new identifiers or constants.
- Do NOT include unrelated content.
- Stay within question scope.

Deduplication rules:
- Never repeat the same file path + line range more than once.
- Merge duplicate references into one.
- Do not repeat identical code blocks.

Cite file paths + line numbers. Use fenced code blocks.
Be concise and factual.

IMPORTANT:
- One-shot: no follow-ups.
- Answer ALL parts.
- No extra commentary.`

// Ask performs a RAG query: searches for context, builds a prompt, and streams the LLM response.
func (p *Pipeline) Ask(ctx context.Context, opts AskOptions) (<-chan StreamChunk, error) {
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = 2048
	}
	if opts.Temperature <= 0 {
		opts.Temperature = 0.1
	}
	if opts.Model != "" {
		p.model = opts.Model
	}

	// Step 1: Search for relevant code
	results, err := p.searcher.Search(ctx, opts.Query, opts.Limit)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	// Step 2: Build context from search results
	codeContext := buildContext(results)

	// Step 3: Build messages
	var userContent string
	if opts.ExtraContext != "" {
		userContent = fmt.Sprintf("%s\n\nCode context:\n%s\n\nQuestion: %s", opts.ExtraContext, codeContext, opts.Query)
	} else {
		userContent = fmt.Sprintf("Code context:\n%s\n\nQuestion: %s", codeContext, opts.Query)
	}
	messages := []Message{
		{Role: "system", Content: p.systemPrompt},
		{Role: "user", Content: userContent},
	}

	// Step 4: Stream LLM response
	ch, err := p.streamChat(ctx, messages, opts)
	if err != nil {
		return nil, fmt.Errorf("chat: %w", err)
	}

	return ch, nil
}

// AskWithContext streams an LLM response using the provided context string
// instead of performing a search. Useful for explaining pre-built content
// like flow trees or architecture maps.
func (p *Pipeline) AskWithContext(ctx context.Context, systemPrompt, contextStr, question string, opts AskOptions) (<-chan StreamChunk, error) {
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = 2048
	}
	if opts.Temperature <= 0 {
		opts.Temperature = 0.1
	}
	if opts.Model != "" {
		p.model = opts.Model
	}

	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: fmt.Sprintf("%s\n\nQuestion: %s", contextStr, question)},
	}

	ch, err := p.streamChat(ctx, messages, opts)
	if err != nil {
		return nil, fmt.Errorf("chat: %w", err)
	}

	return ch, nil
}

// AskSync performs a non-streaming RAG query and returns the full response.
func (p *Pipeline) AskSync(ctx context.Context, opts AskOptions) (string, error) {
	ch, err := p.Ask(ctx, opts)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			return buf.String(), chunk.Err
		}
		buf.WriteString(chunk.Content)
	}
	return buf.String(), nil
}

func buildContext(results []search.Result) string {
	if len(results) == 0 {
		return "(no relevant code found)"
	}

	var b strings.Builder
	for i, r := range results {
		b.WriteString(fmt.Sprintf("--- File: %s (lines %d-%d, score: %.2f) ---\n",
			r.FilePath, r.StartLine, r.EndLine, r.Score))
		b.WriteString(r.Content)
		if i < len(results)-1 {
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

// chatRequest is the OpenAI-compatible chat completion request body.
type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float32   `json:"temperature,omitempty"`
	Stream      bool      `json:"stream"`
}

// streamChoice represents a single choice in a streaming response.
type streamChoice struct {
	Delta struct {
		Content string `json:"content"`
	} `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

// streamResponse is one SSE chunk from the LLM.
type streamResponse struct {
	Choices []streamChoice `json:"choices"`
}

func (p *Pipeline) streamChat(ctx context.Context, messages []Message, opts AskOptions) (<-chan StreamChunk, error) {
	reqBody := chatRequest{
		Model:       p.model,
		Messages:    messages,
		MaxTokens:   opts.MaxTokens,
		Temperature: opts.Temperature,
		Stream:      true,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := p.endpoint + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("LLM returned status %d: %s", resp.StatusCode, string(body))
	}

	ch := make(chan StreamChunk, 64)

	go func() {
		defer close(ch)
		defer resp.Body.Close()
		p.readSSEStream(ctx, resp.Body, ch)
	}()

	return ch, nil
}

func (p *Pipeline) readSSEStream(ctx context.Context, body io.Reader, ch chan<- StreamChunk) {
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			ch <- StreamChunk{Err: ctx.Err()}
			return
		default:
		}

		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			ch <- StreamChunk{Done: true}
			return
		}

		var sr streamResponse
		if err := json.Unmarshal([]byte(data), &sr); err != nil {
			continue // skip malformed chunks
		}

		for _, choice := range sr.Choices {
			if choice.Delta.Content != "" {
				ch <- StreamChunk{Content: choice.Delta.Content}
			}
			if choice.FinishReason != nil {
				ch <- StreamChunk{Done: true}
				return
			}
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- StreamChunk{Err: fmt.Errorf("read stream: %w", err)}
	}
}

// BuildContext is exported for testing.
func BuildContext(results []search.Result) string {
	return buildContext(results)
}
