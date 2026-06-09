/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and SARAS Contributors
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"strings"
	"testing"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
)

func TestBuildChatEndpointOllama(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Provider: "ollama",
			Endpoint: "http://localhost:11434",
		},
	}

	got := buildChatEndpoint(cfg)
	if got != "http://localhost:11434/v1" {
		t.Errorf("expected http://localhost:11434/v1, got %s", got)
	}
}

func TestBuildChatEndpointOllamaAlreadyV1(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Provider: "ollama",
			Endpoint: "http://localhost:11434/v1",
		},
	}

	got := buildChatEndpoint(cfg)
	if got != "http://localhost:11434/v1" {
		t.Errorf("expected http://localhost:11434/v1, got %s", got)
	}
}

func TestBuildChatEndpointOllamaTrailingSlash(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Provider: "ollama",
			Endpoint: "http://localhost:11434/",
		},
	}

	got := buildChatEndpoint(cfg)
	if got != "http://localhost:11434/v1" {
		t.Errorf("expected http://localhost:11434/v1, got %s", got)
	}
}

func TestBuildChatEndpointLMStudio(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Provider: "lmstudio",
			Endpoint: "http://localhost:1234",
		},
	}

	got := buildChatEndpoint(cfg)
	if got != "http://localhost:1234/v1" {
		t.Errorf("expected http://localhost:1234/v1, got %s", got)
	}
}

func TestBuildChatEndpointOpenAI(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Provider: "openai",
			Endpoint: "https://api.openai.com/v1",
		},
	}

	got := buildChatEndpoint(cfg)
	if got != "https://api.openai.com/v1" {
		t.Errorf("expected https://api.openai.com/v1, got %s", got)
	}
}

func TestBuildChatEndpointOpenAITrailingSlash(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Provider: "openai",
			Endpoint: "https://api.openai.com/v1/",
		},
	}

	got := buildChatEndpoint(cfg)
	if got != "https://api.openai.com/v1" {
		t.Errorf("expected no trailing slash, got %s", got)
	}
}

func TestBuildChatEndpointCopilot(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Provider: "copilot",
			Endpoint: "https://api.githubcopilot.com",
		},
	}

	got := buildChatEndpoint(cfg)
	if got != "https://api.githubcopilot.com" {
		t.Errorf("expected https://api.githubcopilot.com (no /v1), got %s", got)
	}
}

func TestBuildChatEndpointCopilotEmptyDefaults(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Provider: "copilot",
			Endpoint: "",
		},
	}

	got := buildChatEndpoint(cfg)
	if got != config.DefaultCopilotEndpoint {
		t.Errorf("expected %s, got %s", config.DefaultCopilotEndpoint, got)
	}
}

func TestBuildChatEndpointCopilotTrailingSlash(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Provider: "copilot",
			Endpoint: "https://api.githubcopilot.com/",
		},
	}

	got := buildChatEndpoint(cfg)
	if got != "https://api.githubcopilot.com" {
		t.Errorf("expected no trailing slash, got %s", got)
	}
}

func TestLLMPipelineOptionsCopilotUsesHTTPClient(t *testing.T) {
	// For the copilot provider we want an HTTP client option (not an
	// API-key option) so the per-request Authorization header is owned
	// by the copilot transport, not a static key from config.
	cfg := &config.Config{LLM: config.LLMConfig{Provider: "copilot"}}
	opts := llmPipelineOptions(cfg)
	if len(opts) != 1 {
		t.Fatalf("expected 1 pipeline option for copilot, got %d", len(opts))
	}
	if c := llmChatHTTPClient(cfg); c == nil {
		t.Error("llmChatHTTPClient should return a non-nil client for copilot")
	}
}

func TestLLMPipelineOptionsOpenAIUsesAPIKey(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{Provider: "openai", APIKey: "sk-test"}}
	opts := llmPipelineOptions(cfg)
	if len(opts) != 1 {
		t.Fatalf("expected 1 pipeline option for openai with key, got %d", len(opts))
	}
	if c := llmChatHTTPClient(cfg); c != nil {
		t.Error("llmChatHTTPClient should return nil for non-copilot providers")
	}
}

func TestLLMPipelineOptionsOpenAINoKeyHasNoOptions(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{Provider: "openai"}}
	opts := llmPipelineOptions(cfg)
	if len(opts) != 0 {
		t.Errorf("expected 0 pipeline options for openai without key, got %d", len(opts))
	}
}

func TestFlowHintFooterContent(t *testing.T) {
	if !strings.Contains(flowHintFooter, "saras flow") {
		t.Error("flowHintFooter should reference 'saras flow'")
	}
	if !strings.Contains(flowHintFooter, "saras flow explain full") {
		t.Error("flowHintFooter should reference 'saras flow explain full'")
	}
	if strings.Contains(flowHintFooter, "saras architecture") {
		t.Error("flowHintFooter should not reference 'saras architecture'")
	}
}

func TestAskCommandWithArchFlag(t *testing.T) {
	f := askCmd.Flags().Lookup("with-arch")
	if f == nil {
		t.Fatal("expected --with-arch flag on ask command")
	}

	if f.NoOptDefVal != "__all__" {
		t.Errorf("expected NoOptDefVal='__all__', got %q", f.NoOptDefVal)
	}

	if !strings.Contains(f.Usage, "saras flow") {
		t.Error("--with-arch usage should reference 'saras flow'")
	}
}

func TestAskCommandHasExpectedFlags(t *testing.T) {
	flags := []string{"limit", "max-tokens", "temperature", "model", "no-tui", "output", "with-arch"}
	for _, name := range flags {
		if askCmd.Flags().Lookup(name) == nil {
			t.Errorf("expected flag --%s on ask command", name)
		}
	}
}

func TestAskCommandRegisteredOnRoot(t *testing.T) {
	found := false
	for _, sub := range rootCmd.Commands() {
		if sub == askCmd {
			found = true
			break
		}
	}
	if !found {
		t.Error("ask command not registered on root command")
	}
}
