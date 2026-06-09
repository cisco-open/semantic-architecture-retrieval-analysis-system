/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and SARAS Contributors
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"strings"
	"testing"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/architect"
)

func TestFormatSummaryBasic(t *testing.T) {
	cmap := &architect.CodebaseMap{
		ProjectRoot: "/tmp/myproject",
		TotalFiles:  42,
		TotalLines:  1500,
		Packages: []architect.PackageInfo{
			{Path: "internal/cli", Files: []string{"root.go", "ask.go"}, Functions: 10, Types: 2, Interfaces: 1},
			{Path: "internal/search", Files: []string{"search.go"}, Functions: 5, Types: 1, Interfaces: 0},
		},
	}

	out := formatSummary(cmap)

	if !strings.Contains(out, "Project: /tmp/myproject") {
		t.Error("expected project root in output")
	}
	if !strings.Contains(out, "Files:   42") {
		t.Error("expected file count in output")
	}
	if !strings.Contains(out, "Lines:   1500") {
		t.Error("expected line count in output")
	}
	if !strings.Contains(out, "Packages: 2") {
		t.Error("expected package count in output")
	}
	if !strings.Contains(out, "internal/cli") {
		t.Error("expected package path in output")
	}
	if !strings.Contains(out, "10 funcs") {
		t.Error("expected function count in package listing")
	}
}

func TestFormatSummaryWithDependencies(t *testing.T) {
	cmap := &architect.CodebaseMap{
		ProjectRoot: "/tmp/proj",
		TotalFiles:  10,
		TotalLines:  200,
		Packages:    []architect.PackageInfo{},
		Dependencies: []architect.Dependency{
			{From: "cli", To: "search"},
			{From: "cli", To: "config"},
		},
	}

	out := formatSummary(cmap)

	if !strings.Contains(out, "Dependencies: 2") {
		t.Error("expected dependency count in output")
	}
	if !strings.Contains(out, "cli → search") {
		t.Error("expected dependency relationship in output")
	}
	if !strings.Contains(out, "cli → config") {
		t.Error("expected second dependency in output")
	}
}

func TestFormatSummaryNoDependencies(t *testing.T) {
	cmap := &architect.CodebaseMap{
		ProjectRoot:  "/tmp/proj",
		TotalFiles:   5,
		TotalLines:   100,
		Packages:     []architect.PackageInfo{},
		Dependencies: []architect.Dependency{},
	}

	out := formatSummary(cmap)

	if strings.Contains(out, "Dependencies:") {
		t.Error("should not show dependencies section when empty")
	}
}

func TestFormatSummaryEmptyProject(t *testing.T) {
	cmap := &architect.CodebaseMap{
		ProjectRoot: "/tmp/empty",
		TotalFiles:  0,
		TotalLines:  0,
		Packages:    []architect.PackageInfo{},
	}

	out := formatSummary(cmap)

	if !strings.Contains(out, "Files:   0") {
		t.Error("expected zero files")
	}
	if !strings.Contains(out, "Packages: 0") {
		t.Error("expected zero packages")
	}
}

func TestMapCommandRegisteredOnRoot(t *testing.T) {
	found := false
	for _, sub := range rootCmd.Commands() {
		if sub == mapCmd {
			found = true
			break
		}
	}
	if !found {
		t.Error("map command not registered on root command")
	}
}

func TestMapCommandHasExpectedFlags(t *testing.T) {
	flags := []string{"format", "output"}
	for _, name := range flags {
		if mapCmd.Flags().Lookup(name) == nil {
			t.Errorf("expected flag --%s on map command", name)
		}
	}
}

func TestMapFormatFlagDefault(t *testing.T) {
	f := mapCmd.Flags().Lookup("format")
	if f == nil {
		t.Fatal("format flag not found")
	}
	if f.DefValue != "tree" {
		t.Errorf("expected format default 'tree', got %s", f.DefValue)
	}
}
