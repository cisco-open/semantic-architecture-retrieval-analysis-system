/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"net/http"
	"time"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/ask"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/copilot"
)

// llmPipelineOptions returns the ask.PipelineOption list appropriate for the
// configured LLM provider. For GitHub Copilot it wires in an http.Client
// that uses the per-user OAuth token to obtain short-lived Copilot API
// tokens; for all other providers it falls back to the optional API key
// stored in config.
func llmPipelineOptions(cfg *config.Config) []ask.PipelineOption {
	opts := []ask.PipelineOption{}
	switch cfg.LLM.Provider {
	case "copilot":
		opts = append(opts, ask.WithHTTPClient(newCopilotChatClient()))
	default:
		if cfg.LLM.APIKey != "" {
			opts = append(opts, ask.WithAPIKey(cfg.LLM.APIKey))
		}
	}
	if cw := cfg.LLM.GetContextWindow(); cw > 0 {
		opts = append(opts, ask.WithContextWindow(cw))
		ask.SetTokenLimit(cw)
	}
	return opts
}

// llmChatHTTPClient returns an *http.Client for non-streaming ChatCompletion
// calls when the configured provider is "copilot". Returns nil for other
// providers (which use the default http.Client baked into chat.go).
func llmChatHTTPClient(cfg *config.Config) *http.Client {
	if cfg.LLM.Provider == "copilot" {
		return newCopilotChatClient()
	}
	return nil
}

// newCopilotChatClient builds an HTTP client suitable for streaming and
// non-streaming Copilot chat completions. Timeouts are generous because
// long-form answers can stream for several minutes.
func newCopilotChatClient() *http.Client {
	return copilot.NewHTTPClient(nil, 300*time.Second)
}
