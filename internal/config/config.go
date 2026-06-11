/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	ConfigDir      = ".saras"
	ConfigFileName = "config.yaml"
	IndexFileName  = "index.gob"
	SymbolFileName = "symbols.gob"

	DefaultEmbedderProvider    = "ollama"
	DefaultOllamaModel         = "nomic-embed-text"
	DefaultOllamaEndpoint      = "http://localhost:11434"
	DefaultLMStudioModel       = "text-embedding-nomic-embed-text-v1.5"
	DefaultLMStudioEndpoint    = "http://127.0.0.1:1234"
	DefaultOpenAIModel         = "text-embedding-3-small"
	DefaultOpenAIEndpoint      = "https://api.openai.com/v1"
	DefaultLocalEmbeddingDims  = 768
	DefaultOpenAIEmbeddingDims = 1536

	DefaultChunkSize    = 512
	DefaultChunkOverlap = 50
	DefaultDebounceMs   = 500

	DefaultLLMProvider         = "ollama"
	DefaultLLMModel            = "llama3.2"
	DefaultLLMEndpoint         = "http://localhost:11434"
	DefaultLMStudioLLMModel    = "llama3.2"
	DefaultLMStudioLLMEndpoint = "http://127.0.0.1:1234"
	DefaultOpenAILLMModel      = "gpt-4o-mini"
	DefaultOpenAILLMEndpoint   = "https://api.openai.com/v1"

	// GitHub Copilot defaults. Authentication is handled via the device
	// OAuth flow ('saras copilot login'); no API key is stored in config.
	DefaultCopilotEndpoint       = "https://api.githubcopilot.com"
	DefaultCopilotEmbeddingModel = "text-embedding-3-small"
	DefaultCopilotEmbeddingDims  = 1536
	DefaultCopilotLLMModel       = "gpt-4o-mini"
)

// ValidDependencyRoles lists the allowed values for Dependency.Role.
var ValidDependencyRoles = []string{"legacy", "shared-lib", "reference", "service"}

// Dependency represents a reference to another saras-initialized repository.
type Dependency struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
	Role string `yaml:"role"`
}

// Config is the top-level configuration for a saras project.
type Config struct {
	Version      int            `yaml:"version"`
	Embedder     EmbedderConfig `yaml:"embedder"`
	LLM          LLMConfig      `yaml:"llm"`
	Store        StoreConfig    `yaml:"store"`
	Chunking     ChunkingConfig `yaml:"chunking"`
	Watch        WatchConfig    `yaml:"watch"`
	Search       SearchConfig   `yaml:"search"`
	Trace        TraceConfig    `yaml:"trace"`
	Ignore       []string       `yaml:"ignore"`
	Dependencies []Dependency   `yaml:"dependencies,omitempty"`
}

// EmbedderConfig controls the embedding provider used for vector search.
type EmbedderConfig struct {
	Provider   string `yaml:"provider"`
	Model      string `yaml:"model"`
	Endpoint   string `yaml:"endpoint,omitempty"`
	APIKey     string `yaml:"api_key,omitempty"`
	Dimensions *int   `yaml:"dimensions,omitempty"`
}

// GetDimensions returns the configured dimensions or a provider-appropriate default.
func (e *EmbedderConfig) GetDimensions() int {
	if e.Dimensions != nil {
		return *e.Dimensions
	}
	switch e.Provider {
	case "openai":
		return DefaultOpenAIEmbeddingDims
	case "copilot":
		return DefaultCopilotEmbeddingDims
	default:
		return DefaultLocalEmbeddingDims
	}
}

// LLMConfig controls the chat/completion LLM used for ask/explain and architecture map.
type LLMConfig struct {
	Provider      string `yaml:"provider"`
	Model         string `yaml:"model"`
	Endpoint      string `yaml:"endpoint,omitempty"`
	APIKey        string `yaml:"api_key,omitempty"`
	ContextWindow *int   `yaml:"context_window,omitempty"`
}

// GetContextWindow returns the configured context window (max prompt tokens)
// or 0 if not set. When 0, the system relies on reactive detection from
// LLM 400 errors. Once a limit is learned from an error it is cached
// automatically, but setting this avoids the first failed request.
func (l *LLMConfig) GetContextWindow() int {
	if l.ContextWindow != nil {
		return *l.ContextWindow
	}
	return 0
}

// StoreConfig controls the vector store backend.
type StoreConfig struct {
	Backend string `yaml:"backend"`
}

// ChunkingConfig controls how source files are split into chunks.
type ChunkingConfig struct {
	Size    int `yaml:"size"`
	Overlap int `yaml:"overlap"`
}

// WatchConfig controls the file watcher daemon behaviour.
type WatchConfig struct {
	DebounceMs int `yaml:"debounce_ms"`
}

// SearchConfig controls search scoring and ranking.
type SearchConfig struct {
	Boost  BoostConfig  `yaml:"boost"`
	Hybrid HybridConfig `yaml:"hybrid"`
	Dedup  DedupConfig  `yaml:"dedup"`
}

// BoostConfig adjusts scores based on file path patterns.
type BoostConfig struct {
	Enabled   bool        `yaml:"enabled"`
	Penalties []BoostRule `yaml:"penalties"`
	Bonuses   []BoostRule `yaml:"bonuses"`
}

// BoostRule maps a path pattern to a scoring factor.
type BoostRule struct {
	Pattern string  `yaml:"pattern"`
	Factor  float32 `yaml:"factor"`
}

// HybridConfig controls reciprocal-rank fusion of vector and text search.
type HybridConfig struct {
	Enabled bool    `yaml:"enabled"`
	K       float32 `yaml:"k"`
}

// DedupConfig controls file-level deduplication of search results.
type DedupConfig struct {
	Enabled bool `yaml:"enabled"`
}

// TraceConfig controls symbol extraction and call graph analysis.
type TraceConfig struct {
	Mode             string   `yaml:"mode"`
	EnabledLanguages []string `yaml:"enabled_languages"`
	ExcludePatterns  []string `yaml:"exclude_patterns"`
}

// DefaultConfig returns a fully populated Config with sensible defaults.
func DefaultConfig() *Config {
	dims := DefaultLocalEmbeddingDims
	return &Config{
		Version: 1,
		Embedder: EmbedderConfig{
			Provider:   DefaultEmbedderProvider,
			Model:      DefaultOllamaModel,
			Endpoint:   DefaultOllamaEndpoint,
			Dimensions: &dims,
		},
		LLM: LLMConfig{
			Provider: DefaultLLMProvider,
			Model:    DefaultLLMModel,
			Endpoint: DefaultLLMEndpoint,
		},
		Store: StoreConfig{
			Backend: "gob",
		},
		Chunking: ChunkingConfig{
			Size:    DefaultChunkSize,
			Overlap: DefaultChunkOverlap,
		},
		Watch: WatchConfig{
			DebounceMs: DefaultDebounceMs,
		},
		Search: SearchConfig{
			Dedup: DedupConfig{Enabled: true},
			Hybrid: HybridConfig{
				Enabled: false,
				K:       60,
			},
			Boost: BoostConfig{
				Enabled: true,
				Penalties: []BoostRule{
					{Pattern: "/tests/", Factor: 0.5},
					{Pattern: "/test/", Factor: 0.5},
					{Pattern: "__tests__", Factor: 0.5},
					{Pattern: "_test.", Factor: 0.5},
					{Pattern: ".test.", Factor: 0.5},
					{Pattern: ".spec.", Factor: 0.5},
					{Pattern: "/mocks/", Factor: 0.4},
					{Pattern: "/fixtures/", Factor: 0.4},
					{Pattern: "/generated/", Factor: 0.4},
					{Pattern: ".md", Factor: 0.6},
					{Pattern: "/docs/", Factor: 0.6},
				},
				Bonuses: []BoostRule{
					{Pattern: "/src/", Factor: 1.1},
					{Pattern: "/lib/", Factor: 1.1},
					{Pattern: "/app/", Factor: 1.1},
				},
			},
		},
		Trace: TraceConfig{
			Mode: "fast",
			EnabledLanguages: []string{
				".go", ".js", ".ts", ".jsx", ".tsx", ".py", ".rs",
				".java", ".c", ".h", ".cpp", ".hpp", ".cs", ".rb", ".php",
			},
			ExcludePatterns: []string{
				"*_test.go", "*.spec.ts", "*.spec.js",
				"*.test.ts", "*.test.js", "__tests__/*",
			},
		},
		Ignore: []string{
			".git", ".saras", "node_modules", "vendor", "bin", "dist",
			"__pycache__", ".venv", "venv", ".idea", ".vscode",
			"target", ".zig-cache", "zig-out",
		},
	}
}

// DefaultEmbedderForProvider returns default EmbedderConfig for the given provider name.
func DefaultEmbedderForProvider(provider string) EmbedderConfig {
	switch strings.ToLower(provider) {
	case "lmstudio":
		dims := DefaultLocalEmbeddingDims
		return EmbedderConfig{
			Provider:   "lmstudio",
			Model:      DefaultLMStudioModel,
			Endpoint:   DefaultLMStudioEndpoint,
			Dimensions: &dims,
		}
	case "openai":
		return EmbedderConfig{
			Provider: "openai",
			Model:    DefaultOpenAIModel,
			Endpoint: DefaultOpenAIEndpoint,
		}
	case "copilot":
		dims := DefaultCopilotEmbeddingDims
		return EmbedderConfig{
			Provider:   "copilot",
			Model:      DefaultCopilotEmbeddingModel,
			Endpoint:   DefaultCopilotEndpoint,
			Dimensions: &dims,
		}
	default:
		dims := DefaultLocalEmbeddingDims
		return EmbedderConfig{
			Provider:   "ollama",
			Model:      DefaultOllamaModel,
			Endpoint:   DefaultOllamaEndpoint,
			Dimensions: &dims,
		}
	}
}

// DefaultLLMForProvider returns default LLMConfig for the given provider name.
func DefaultLLMForProvider(provider string) LLMConfig {
	switch strings.ToLower(provider) {
	case "lmstudio":
		return LLMConfig{
			Provider: "lmstudio",
			Model:    DefaultLMStudioLLMModel,
			Endpoint: DefaultLMStudioLLMEndpoint,
		}
	case "openai":
		return LLMConfig{
			Provider: "openai",
			Model:    DefaultOpenAILLMModel,
			Endpoint: DefaultOpenAILLMEndpoint,
		}
	case "copilot":
		return LLMConfig{
			Provider: "copilot",
			Model:    DefaultCopilotLLMModel,
			Endpoint: DefaultCopilotEndpoint,
		}
	default:
		return LLMConfig{
			Provider: "ollama",
			Model:    DefaultLLMModel,
			Endpoint: DefaultLLMEndpoint,
		}
	}
}

// Paths

// GetConfigDir returns the .saras directory path for a project root.
func GetConfigDir(projectRoot string) string {
	return filepath.Join(projectRoot, ConfigDir)
}

// GetConfigPath returns the full path to config.yaml.
func GetConfigPath(projectRoot string) string {
	return filepath.Join(GetConfigDir(projectRoot), ConfigFileName)
}

// GetIndexPath returns the full path to the vector index file.
func GetIndexPath(projectRoot string) string {
	return filepath.Join(GetConfigDir(projectRoot), IndexFileName)
}

// GetSymbolPath returns the full path to the symbol index file.
func GetSymbolPath(projectRoot string) string {
	return filepath.Join(GetConfigDir(projectRoot), SymbolFileName)
}

// Persistence

// Save writes the config to .saras/config.yaml under the given project root.
func (c *Config) Save(projectRoot string) error {
	configDir := GetConfigDir(projectRoot)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(GetConfigPath(projectRoot), data, 0600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	return nil
}

// Load reads and parses .saras/config.yaml from the given project root.
func Load(projectRoot string) (*Config, error) {
	data, err := os.ReadFile(GetConfigPath(projectRoot))
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}
	cfg.applyDefaults()
	return &cfg, nil
}

// UpdateContextWindow persists a learned context_window value to the
// project config. It loads the current config, sets the field only if it
// is not already set (or the new value is smaller), and saves it back.
// Errors are silently ignored — this is a best-effort optimisation.
func UpdateContextWindow(limit int) {
	if limit <= 0 {
		return
	}
	root, err := FindProjectRoot()
	if err != nil {
		return
	}
	cfg, err := Load(root)
	if err != nil {
		return
	}
	// Only write if the config doesn't already have a value, or the
	// new limit is smaller (more restrictive) than what's stored.
	if cfg.LLM.ContextWindow != nil && *cfg.LLM.ContextWindow > 0 && *cfg.LLM.ContextWindow <= limit {
		return
	}
	cfg.LLM.ContextWindow = &limit
	_ = cfg.Save(root)
}

// Exists reports whether a .saras/config.yaml exists under projectRoot.
func Exists(projectRoot string) bool {
	_, err := os.Stat(GetConfigPath(projectRoot))
	return err == nil
}

// FindProjectRoot walks up from the current directory looking for .saras/.
func FindProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks: %w", err)
	}
	dir := cwd
	for {
		if Exists(dir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("no saras project found (run 'saras init' first)")
}

// ValidateDependency checks that a dependency has all required fields and a valid role.
func ValidateDependency(dep Dependency) error {
	if dep.Name == "" {
		return fmt.Errorf("dependency name is required")
	}
	if dep.Path == "" {
		return fmt.Errorf("dependency path is required")
	}
	if dep.Role == "" {
		return fmt.Errorf("dependency role is required (valid: %s)", strings.Join(ValidDependencyRoles, ", "))
	}
	valid := false
	for _, r := range ValidDependencyRoles {
		if dep.Role == r {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("invalid dependency role %q (valid: %s)", dep.Role, strings.Join(ValidDependencyRoles, ", "))
	}
	return nil
}

// FindDependency returns the dependency with the given name, or nil.
func (c *Config) FindDependency(name string) *Dependency {
	for i := range c.Dependencies {
		if c.Dependencies[i].Name == name {
			return &c.Dependencies[i]
		}
	}
	return nil
}

// AddDependency validates and appends a dependency, rejecting duplicates.
func (c *Config) AddDependency(dep Dependency) error {
	if err := ValidateDependency(dep); err != nil {
		return err
	}
	for _, existing := range c.Dependencies {
		if existing.Name == dep.Name {
			return fmt.Errorf("dependency %q already exists", dep.Name)
		}
		if existing.Path == dep.Path {
			return fmt.Errorf("dependency path %q already registered as %q", dep.Path, existing.Name)
		}
	}
	c.Dependencies = append(c.Dependencies, dep)
	return nil
}

// RemoveDependency removes a dependency by name or path. Returns an error if not found.
func (c *Config) RemoveDependency(nameOrPath string) error {
	for i, dep := range c.Dependencies {
		if dep.Name == nameOrPath || dep.Path == nameOrPath {
			c.Dependencies = append(c.Dependencies[:i], c.Dependencies[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("dependency %q not found", nameOrPath)
}

// CheckEmbeddingCompatibility checks if the other config uses the same embedding setup.
// Returns a non-nil error describing the mismatch if incompatible.
func (c *Config) CheckEmbeddingCompatibility(other *Config) error {
	var mismatches []string
	if c.Embedder.Provider != other.Embedder.Provider {
		mismatches = append(mismatches, fmt.Sprintf("provider: %s vs %s", c.Embedder.Provider, other.Embedder.Provider))
	}
	if c.Embedder.Model != other.Embedder.Model {
		mismatches = append(mismatches, fmt.Sprintf("model: %s vs %s", c.Embedder.Model, other.Embedder.Model))
	}
	if c.Embedder.GetDimensions() != other.Embedder.GetDimensions() {
		mismatches = append(mismatches, fmt.Sprintf("dimensions: %d vs %d", c.Embedder.GetDimensions(), other.Embedder.GetDimensions()))
	}
	if len(mismatches) > 0 {
		return fmt.Errorf("embedding mismatch: %s", strings.Join(mismatches, "; "))
	}
	return nil
}

// applyDefaults fills missing fields with sensible values for backward compatibility.
func (c *Config) applyDefaults() {
	defaults := DefaultConfig()

	if c.Embedder.Provider == "" {
		c.Embedder.Provider = defaults.Embedder.Provider
	}
	if c.Embedder.Model == "" {
		c.Embedder.Model = DefaultEmbedderForProvider(c.Embedder.Provider).Model
	}
	if c.Embedder.Endpoint == "" {
		c.Embedder.Endpoint = DefaultEmbedderForProvider(c.Embedder.Provider).Endpoint
	}
	if c.Embedder.Dimensions == nil {
		providerDefaults := DefaultEmbedderForProvider(c.Embedder.Provider)
		if providerDefaults.Dimensions != nil {
			dim := *providerDefaults.Dimensions
			c.Embedder.Dimensions = &dim
		}
	}

	if c.LLM.Provider == "" {
		c.LLM.Provider = defaults.LLM.Provider
	}
	if c.LLM.Model == "" {
		c.LLM.Model = defaults.LLM.Model
	}
	if c.LLM.Endpoint == "" {
		c.LLM.Endpoint = defaults.LLM.Endpoint
	}

	if c.Store.Backend == "" {
		c.Store.Backend = defaults.Store.Backend
	}
	if c.Chunking.Size == 0 {
		c.Chunking.Size = defaults.Chunking.Size
	}
	if c.Chunking.Overlap == 0 {
		c.Chunking.Overlap = defaults.Chunking.Overlap
	}
	if c.Watch.DebounceMs == 0 {
		c.Watch.DebounceMs = defaults.Watch.DebounceMs
	}
	if c.Trace.Mode == "" {
		c.Trace.Mode = defaults.Trace.Mode
	}
	if len(c.Trace.EnabledLanguages) == 0 {
		c.Trace.EnabledLanguages = defaults.Trace.EnabledLanguages
	}
	if len(c.Ignore) == 0 {
		c.Ignore = defaults.Ignore
	}
}
