/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Version != 1 {
		t.Errorf("expected version 1, got %d", cfg.Version)
	}
	if cfg.Embedder.Provider != "ollama" {
		t.Errorf("expected provider ollama, got %s", cfg.Embedder.Provider)
	}
	if cfg.Embedder.Model != DefaultOllamaModel {
		t.Errorf("expected model %s, got %s", DefaultOllamaModel, cfg.Embedder.Model)
	}
	if cfg.Embedder.Endpoint != DefaultOllamaEndpoint {
		t.Errorf("expected endpoint %s, got %s", DefaultOllamaEndpoint, cfg.Embedder.Endpoint)
	}
	if cfg.Embedder.Dimensions == nil || *cfg.Embedder.Dimensions != DefaultLocalEmbeddingDims {
		t.Errorf("expected dimensions %d", DefaultLocalEmbeddingDims)
	}
	if cfg.LLM.Provider != DefaultLLMProvider {
		t.Errorf("expected LLM provider %s, got %s", DefaultLLMProvider, cfg.LLM.Provider)
	}
	if cfg.LLM.Model != DefaultLLMModel {
		t.Errorf("expected LLM model %s, got %s", DefaultLLMModel, cfg.LLM.Model)
	}
	if cfg.Store.Backend != "gob" {
		t.Errorf("expected store backend gob, got %s", cfg.Store.Backend)
	}
	if cfg.Chunking.Size != DefaultChunkSize {
		t.Errorf("expected chunk size %d, got %d", DefaultChunkSize, cfg.Chunking.Size)
	}
	if cfg.Chunking.Overlap != DefaultChunkOverlap {
		t.Errorf("expected chunk overlap %d, got %d", DefaultChunkOverlap, cfg.Chunking.Overlap)
	}
	if cfg.Watch.DebounceMs != DefaultDebounceMs {
		t.Errorf("expected debounce %d, got %d", DefaultDebounceMs, cfg.Watch.DebounceMs)
	}
	if !cfg.Search.Boost.Enabled {
		t.Error("expected boost to be enabled by default")
	}
	if cfg.Search.Hybrid.Enabled {
		t.Error("expected hybrid to be disabled by default")
	}
	if !cfg.Search.Dedup.Enabled {
		t.Error("expected dedup to be enabled by default")
	}
	if len(cfg.Ignore) == 0 {
		t.Error("expected non-empty ignore list")
	}
	if len(cfg.Trace.EnabledLanguages) == 0 {
		t.Error("expected non-empty enabled languages")
	}
}

func TestEmbedderGetDimensions(t *testing.T) {
	tests := []struct {
		name     string
		cfg      EmbedderConfig
		expected int
	}{
		{
			name:     "explicit dimensions",
			cfg:      EmbedderConfig{Provider: "ollama", Dimensions: intPtr(256)},
			expected: 256,
		},
		{
			name:     "ollama default",
			cfg:      EmbedderConfig{Provider: "ollama"},
			expected: DefaultLocalEmbeddingDims,
		},
		{
			name:     "lmstudio default",
			cfg:      EmbedderConfig{Provider: "lmstudio"},
			expected: DefaultLocalEmbeddingDims,
		},
		{
			name:     "openai default",
			cfg:      EmbedderConfig{Provider: "openai"},
			expected: DefaultOpenAIEmbeddingDims,
		},
		{
			name:     "copilot default",
			cfg:      EmbedderConfig{Provider: "copilot"},
			expected: DefaultCopilotEmbeddingDims,
		},
		{
			name:     "unknown provider falls back to local dims",
			cfg:      EmbedderConfig{Provider: "custom"},
			expected: DefaultLocalEmbeddingDims,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cfg.GetDimensions()
			if got != tc.expected {
				t.Errorf("expected %d, got %d", tc.expected, got)
			}
		})
	}
}

func TestDefaultEmbedderForProvider(t *testing.T) {
	tests := []struct {
		provider         string
		expectedModel    string
		expectedEndpoint string
		hasDimensions    bool
	}{
		{"ollama", DefaultOllamaModel, DefaultOllamaEndpoint, true},
		{"lmstudio", DefaultLMStudioModel, DefaultLMStudioEndpoint, true},
		{"openai", DefaultOpenAIModel, DefaultOpenAIEndpoint, false},
		{"copilot", DefaultCopilotEmbeddingModel, DefaultCopilotEndpoint, true},
		{"OLLAMA", DefaultOllamaModel, DefaultOllamaEndpoint, true},
		{"unknown", DefaultOllamaModel, DefaultOllamaEndpoint, true},
		{"", DefaultOllamaModel, DefaultOllamaEndpoint, true},
	}

	for _, tc := range tests {
		t.Run(tc.provider, func(t *testing.T) {
			cfg := DefaultEmbedderForProvider(tc.provider)
			if cfg.Model != tc.expectedModel {
				t.Errorf("expected model %s, got %s", tc.expectedModel, cfg.Model)
			}
			if cfg.Endpoint != tc.expectedEndpoint {
				t.Errorf("expected endpoint %s, got %s", tc.expectedEndpoint, cfg.Endpoint)
			}
			if tc.hasDimensions && cfg.Dimensions == nil {
				t.Error("expected dimensions to be set")
			}
			if !tc.hasDimensions && cfg.Dimensions != nil {
				t.Error("expected dimensions to be nil")
			}
		})
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmp := t.TempDir()

	cfg := DefaultConfig()
	cfg.Embedder.Model = "test-model"
	cfg.LLM.Model = "test-llm"

	if err := cfg.Save(tmp); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	configPath := GetConfigPath(tmp)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatalf("config file was not created at %s", configPath)
	}

	loaded, err := Load(tmp)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if loaded.Embedder.Model != "test-model" {
		t.Errorf("expected model test-model, got %s", loaded.Embedder.Model)
	}
	if loaded.LLM.Model != "test-llm" {
		t.Errorf("expected LLM model test-llm, got %s", loaded.LLM.Model)
	}
	if loaded.Version != 1 {
		t.Errorf("expected version 1, got %d", loaded.Version)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, ConfigDir)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write a minimal config with only version and provider
	minimal := []byte("version: 1\nembedder:\n  provider: ollama\n")
	if err := os.WriteFile(filepath.Join(configDir, ConfigFileName), minimal, 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if cfg.Embedder.Model != DefaultOllamaModel {
		t.Errorf("expected default model %s, got %s", DefaultOllamaModel, cfg.Embedder.Model)
	}
	if cfg.Embedder.Endpoint != DefaultOllamaEndpoint {
		t.Errorf("expected default endpoint %s, got %s", DefaultOllamaEndpoint, cfg.Embedder.Endpoint)
	}
	if cfg.Chunking.Size != DefaultChunkSize {
		t.Errorf("expected default chunk size %d, got %d", DefaultChunkSize, cfg.Chunking.Size)
	}
	if cfg.Watch.DebounceMs != DefaultDebounceMs {
		t.Errorf("expected default debounce %d, got %d", DefaultDebounceMs, cfg.Watch.DebounceMs)
	}
	if cfg.LLM.Provider != DefaultLLMProvider {
		t.Errorf("expected default LLM provider %s, got %s", DefaultLLMProvider, cfg.LLM.Provider)
	}
	if cfg.Store.Backend != "gob" {
		t.Errorf("expected default store backend gob, got %s", cfg.Store.Backend)
	}
}

func TestLoadMissingFile(t *testing.T) {
	tmp := t.TempDir()
	_, err := Load(tmp)
	if err == nil {
		t.Error("expected error when loading non-existent config")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, ConfigDir)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, ConfigFileName), []byte("{{invalid"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(tmp)
	if err == nil {
		t.Error("expected error when loading invalid YAML")
	}
}

func TestExists(t *testing.T) {
	tmp := t.TempDir()

	if Exists(tmp) {
		t.Error("expected Exists to return false for empty directory")
	}

	cfg := DefaultConfig()
	if err := cfg.Save(tmp); err != nil {
		t.Fatal(err)
	}

	if !Exists(tmp) {
		t.Error("expected Exists to return true after save")
	}
}

func TestGetPaths(t *testing.T) {
	root := "/project"

	if got := GetConfigDir(root); got != filepath.Join(root, ".saras") {
		t.Errorf("unexpected config dir: %s", got)
	}
	if got := GetConfigPath(root); got != filepath.Join(root, ".saras", "config.yaml") {
		t.Errorf("unexpected config path: %s", got)
	}
	if got := GetIndexPath(root); got != filepath.Join(root, ".saras", "index.gob") {
		t.Errorf("unexpected index path: %s", got)
	}
	if got := GetSymbolPath(root); got != filepath.Join(root, ".saras", "symbols.gob") {
		t.Errorf("unexpected symbol path: %s", got)
	}
}

func TestFindProjectRoot(t *testing.T) {
	tmp := t.TempDir()
	nested := filepath.Join(tmp, "a", "b", "c")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	if err := cfg.Save(tmp); err != nil {
		t.Fatal(err)
	}

	// Change to nested directory and find root
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	if err := os.Chdir(nested); err != nil {
		t.Fatal(err)
	}

	root, err := FindProjectRoot()
	if err != nil {
		t.Fatalf("FindProjectRoot failed: %v", err)
	}

	// Resolve symlinks on both to compare
	expectedRoot, _ := filepath.EvalSymlinks(tmp)
	if root != expectedRoot {
		t.Errorf("expected root %s, got %s", expectedRoot, root)
	}
}

func TestFindProjectRootNotFound(t *testing.T) {
	tmp := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}

	_, err := FindProjectRoot()
	if err == nil {
		t.Error("expected error when no project root found")
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	tmp := t.TempDir()
	nested := filepath.Join(tmp, "new-project")

	cfg := DefaultConfig()
	if err := cfg.Save(nested); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(nested, ConfigDir)); os.IsNotExist(err) {
		t.Error("expected .saras directory to be created")
	}
}

func TestSaveFilePermissions(t *testing.T) {
	tmp := t.TempDir()

	cfg := DefaultConfig()
	if err := cfg.Save(tmp); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(GetConfigPath(tmp))
	if err != nil {
		t.Fatal(err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("expected file permission 0600, got %o", perm)
	}
}

func TestLLMGetContextWindow(t *testing.T) {
	tests := []struct {
		name     string
		cfg      LLMConfig
		expected int
	}{
		{
			name:     "nil returns 0",
			cfg:      LLMConfig{Provider: "ollama"},
			expected: 0,
		},
		{
			name:     "explicit value",
			cfg:      LLMConfig{Provider: "copilot", ContextWindow: intPtr(12288)},
			expected: 12288,
		},
		{
			name:     "zero value pointer",
			cfg:      LLMConfig{Provider: "openai", ContextWindow: intPtr(0)},
			expected: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cfg.GetContextWindow()
			if got != tc.expected {
				t.Errorf("expected %d, got %d", tc.expected, got)
			}
		})
	}
}

func intPtr(i int) *int {
	return &i
}
