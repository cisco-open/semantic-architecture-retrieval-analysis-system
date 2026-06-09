/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package config

import (
	"testing"
)

func TestValidateDependency(t *testing.T) {
	tests := []struct {
		name    string
		dep     Dependency
		wantErr bool
	}{
		{"valid legacy", Dependency{Name: "auth", Path: "/tmp", Role: "legacy"}, false},
		{"valid shared-lib", Dependency{Name: "lib", Path: "/tmp", Role: "shared-lib"}, false},
		{"valid reference", Dependency{Name: "ref", Path: "/tmp", Role: "reference"}, false},
		{"valid service", Dependency{Name: "svc", Path: "/tmp", Role: "service"}, false},
		{"empty name", Dependency{Name: "", Path: "/tmp", Role: "legacy"}, true},
		{"empty path", Dependency{Name: "x", Path: "", Role: "legacy"}, true},
		{"empty role", Dependency{Name: "x", Path: "/tmp", Role: ""}, true},
		{"invalid role", Dependency{Name: "x", Path: "/tmp", Role: "unknown"}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDependency(tc.dep)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateDependency() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestAddDependency(t *testing.T) {
	cfg := DefaultConfig()

	dep := Dependency{Name: "auth", Path: "/tmp/auth", Role: "legacy"}
	if err := cfg.AddDependency(dep); err != nil {
		t.Fatalf("AddDependency() error = %v", err)
	}

	if len(cfg.Dependencies) != 1 {
		t.Fatalf("expected 1 dep, got %d", len(cfg.Dependencies))
	}
	if cfg.Dependencies[0].Name != "auth" {
		t.Errorf("expected name auth, got %s", cfg.Dependencies[0].Name)
	}

	// Duplicate name
	dup := Dependency{Name: "auth", Path: "/tmp/other", Role: "service"}
	if err := cfg.AddDependency(dup); err == nil {
		t.Error("expected error for duplicate name")
	}

	// Duplicate path
	dup2 := Dependency{Name: "auth2", Path: "/tmp/auth", Role: "service"}
	if err := cfg.AddDependency(dup2); err == nil {
		t.Error("expected error for duplicate path")
	}
}

func TestRemoveDependency(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Dependencies = []Dependency{
		{Name: "auth", Path: "/tmp/auth", Role: "legacy"},
		{Name: "lib", Path: "/tmp/lib", Role: "shared-lib"},
	}

	// Remove by name
	if err := cfg.RemoveDependency("auth"); err != nil {
		t.Fatalf("RemoveDependency(auth) error = %v", err)
	}
	if len(cfg.Dependencies) != 1 {
		t.Fatalf("expected 1 dep, got %d", len(cfg.Dependencies))
	}

	// Remove by path
	if err := cfg.RemoveDependency("/tmp/lib"); err != nil {
		t.Fatalf("RemoveDependency(path) error = %v", err)
	}
	if len(cfg.Dependencies) != 0 {
		t.Fatalf("expected 0 deps, got %d", len(cfg.Dependencies))
	}

	// Remove non-existent
	if err := cfg.RemoveDependency("none"); err == nil {
		t.Error("expected error for non-existent dep")
	}
}

func TestFindDependency(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Dependencies = []Dependency{
		{Name: "auth", Path: "/tmp/auth", Role: "legacy"},
	}

	found := cfg.FindDependency("auth")
	if found == nil {
		t.Fatal("expected to find dep auth")
	}
	if found.Role != "legacy" {
		t.Errorf("expected role legacy, got %s", found.Role)
	}

	notFound := cfg.FindDependency("nonexistent")
	if notFound != nil {
		t.Error("expected nil for non-existent dep")
	}
}

func TestCheckEmbeddingCompatibility(t *testing.T) {
	dims384 := 384
	dims768 := 768

	main := &Config{
		Embedder: EmbedderConfig{
			Provider:   "ollama",
			Model:      "nomic-embed-text",
			Dimensions: &dims384,
		},
	}

	compatible := &Config{
		Embedder: EmbedderConfig{
			Provider:   "ollama",
			Model:      "nomic-embed-text",
			Dimensions: &dims384,
		},
	}

	incompatible := &Config{
		Embedder: EmbedderConfig{
			Provider:   "openai",
			Model:      "text-embedding-3-small",
			Dimensions: &dims768,
		},
	}

	if err := main.CheckEmbeddingCompatibility(compatible); err != nil {
		t.Errorf("expected compatible, got error: %v", err)
	}

	if err := main.CheckEmbeddingCompatibility(incompatible); err == nil {
		t.Error("expected incompatible error")
	}
}

func TestResolveDeps(t *testing.T) {
	cfg := &Config{
		Dependencies: []Dependency{
			{Name: "auth", Path: "/tmp/auth", Role: "legacy"},
			{Name: "lib", Path: "/tmp/lib", Role: "shared-lib"},
		},
	}

	// No flags — returns nil deps, false includeCurrent
	deps, inc, err := ResolveDeps(cfg, "", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deps != nil {
		t.Error("expected nil deps when no flags set")
	}
	if inc {
		t.Error("expected includeCurrent=false")
	}

	// --from-dep auth
	deps, inc, err = ResolveDeps(cfg, "auth", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deps) != 1 || deps[0].Name != "auth" {
		t.Errorf("expected [auth], got %v", deps)
	}
	if inc {
		t.Error("expected includeCurrent=false for --from-dep")
	}

	// --all-deps
	deps, inc, err = ResolveDeps(cfg, "", true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deps) != 2 {
		t.Errorf("expected 2 deps, got %d", len(deps))
	}
	if inc {
		t.Error("expected includeCurrent=false for --all-deps")
	}

	// --with-deps
	deps, inc, err = ResolveDeps(cfg, "", false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deps) != 2 {
		t.Errorf("expected 2 deps, got %d", len(deps))
	}
	if !inc {
		t.Error("expected includeCurrent=true for --with-deps")
	}

	// Mutual exclusivity: --from-dep + --all-deps
	_, _, err = ResolveDeps(cfg, "auth", true, false)
	if err == nil {
		t.Error("expected error for mutually exclusive flags")
	}

	// Mutual exclusivity: --from-dep + --with-deps
	_, _, err = ResolveDeps(cfg, "auth", false, true)
	if err == nil {
		t.Error("expected error for mutually exclusive flags")
	}

	// Mutual exclusivity: --all-deps + --with-deps
	_, _, err = ResolveDeps(cfg, "", true, true)
	if err == nil {
		t.Error("expected error for mutually exclusive flags")
	}

	// --from-dep with non-existent dep
	_, _, err = ResolveDeps(cfg, "nonexistent", false, false)
	if err == nil {
		t.Error("expected error for non-existent dep")
	}

	// --all-deps with no deps configured
	emptyCfg := &Config{}
	_, _, err = ResolveDeps(emptyCfg, "", true, false)
	if err == nil {
		t.Error("expected error for --all-deps with no deps")
	}
}

func TestSaveAndLoadWithDependencies(t *testing.T) {
	tmp := t.TempDir()

	cfg := DefaultConfig()
	cfg.Dependencies = []Dependency{
		{Name: "auth", Path: "/tmp/auth", Role: "legacy"},
		{Name: "lib", Path: "/tmp/lib", Role: "shared-lib"},
	}

	if err := cfg.Save(tmp); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := Load(tmp)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if len(loaded.Dependencies) != 2 {
		t.Fatalf("expected 2 deps, got %d", len(loaded.Dependencies))
	}
	if loaded.Dependencies[0].Name != "auth" {
		t.Errorf("expected first dep name auth, got %s", loaded.Dependencies[0].Name)
	}
	if loaded.Dependencies[1].Role != "shared-lib" {
		t.Errorf("expected second dep role shared-lib, got %s", loaded.Dependencies[1].Role)
	}
}
