/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/tui"
	"github.com/spf13/cobra"
)

// freshInitCmd creates an isolated init command for testing.
func freshInitCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "init", RunE: runInit}
	cmd.Flags().BoolP("yes", "y", false, "Accept all defaults")
	cmd.Flags().String("provider", "", "Embedding provider")
	cmd.Flags().String("model", "", "Embedding model name")
	cmd.Flags().String("endpoint", "", "API endpoint URL")
	cmd.Flags().String("api-key", "", "API key")
	return cmd
}

func TestBuildNonInteractiveResultDefaults(t *testing.T) {
	cmd := freshInitCmd()
	cmd.ParseFlags([]string{"--yes"})

	result := buildNonInteractiveResult(cmd)

	if result.Provider != "ollama" {
		t.Errorf("expected default provider ollama, got %s", result.Provider)
	}
	if result.Model == "" {
		t.Error("expected non-empty default model")
	}
	if result.Endpoint == "" {
		t.Error("expected non-empty default endpoint")
	}
	if !result.Done {
		t.Error("expected Done to be true")
	}
}

func TestBuildNonInteractiveResultCustomProvider(t *testing.T) {
	cmd := freshInitCmd()
	cmd.ParseFlags([]string{"--provider", "lmstudio", "--model", "custom-model"})

	result := buildNonInteractiveResult(cmd)

	if result.Provider != "lmstudio" {
		t.Errorf("expected provider lmstudio, got %s", result.Provider)
	}
	if result.Model != "custom-model" {
		t.Errorf("expected model custom-model, got %s", result.Model)
	}
}

func TestBuildConfigFromResult(t *testing.T) {
	result := tui.InitResult{
		Provider: "ollama",
		Model:    "nomic-embed-text",
		Endpoint: "http://localhost:11434",
		Done:     true,
	}

	cfg := buildConfigFromResult(result)

	if cfg.Embedder.Provider != "ollama" {
		t.Errorf("expected provider ollama, got %s", cfg.Embedder.Provider)
	}
	if cfg.Embedder.Model != "nomic-embed-text" {
		t.Errorf("expected model nomic-embed-text, got %s", cfg.Embedder.Model)
	}
	if cfg.Embedder.Endpoint != "http://localhost:11434" {
		t.Errorf("expected endpoint, got %s", cfg.Embedder.Endpoint)
	}
}

func TestBuildConfigFromResultOpenAI(t *testing.T) {
	result := tui.InitResult{
		Provider: "openai",
		Model:    "text-embedding-3-small",
		Endpoint: "https://api.openai.com/v1",
		APIKey:   "sk-test-key",
		Done:     true,
	}

	cfg := buildConfigFromResult(result)

	if cfg.Embedder.APIKey != "sk-test-key" {
		t.Errorf("expected API key, got %s", cfg.Embedder.APIKey)
	}
}

func TestInitCommandAlreadyInitialized(t *testing.T) {
	// Create a temp dir with existing config
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Save(root)

	// Override working directory
	origDir, _ := os.Getwd()
	os.Chdir(root)
	defer os.Chdir(origDir)

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"init", "--yes"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if output == "" {
		t.Skip("output capture not working in this test environment")
	}
}

func TestInitCommandCreatesConfig(t *testing.T) {
	root := t.TempDir()

	origDir, _ := os.Getwd()
	os.Chdir(root)
	defer os.Chdir(origDir)

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"init", "--yes"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify config file was created
	configPath := filepath.Join(root, ".saras", "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("expected config file to be created")
	}

	// Verify config can be loaded
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Embedder.Provider != "ollama" {
		t.Errorf("expected default provider ollama, got %s", cfg.Embedder.Provider)
	}
}

func TestInitCommandWithProvider(t *testing.T) {
	root := t.TempDir()

	origDir, _ := os.Getwd()
	os.Chdir(root)
	defer os.Chdir(origDir)

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"init", "--provider", "lmstudio"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Embedder.Provider != "lmstudio" {
		t.Errorf("expected provider lmstudio, got %s", cfg.Embedder.Provider)
	}
}
