/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSarasWorkflowsNotEmpty(t *testing.T) {
	wfs := sarasWorkflows()
	if len(wfs) == 0 {
		t.Fatal("expected at least one workflow")
	}
}

func TestSarasWorkflowsHaveRequiredFields(t *testing.T) {
	for _, wf := range sarasWorkflows() {
		t.Run(wf.name, func(t *testing.T) {
			if wf.name == "" {
				t.Error("workflow name must not be empty")
			}
			if wf.description == "" {
				t.Error("workflow description must not be empty")
			}
			if wf.body == "" {
				t.Error("workflow body must not be empty")
			}
			if !strings.Contains(wf.body, "saras ") {
				t.Error("workflow body should contain saras commands")
			}
		})
	}
}

func TestSarasWorkflowNames(t *testing.T) {
	wfs := sarasWorkflows()
	seen := make(map[string]bool)
	for _, wf := range wfs {
		if seen[wf.name] {
			t.Errorf("duplicate workflow name: %s", wf.name)
		}
		seen[wf.name] = true

		if !strings.HasPrefix(wf.name, "saras-") {
			t.Errorf("workflow name %s should start with 'saras-'", wf.name)
		}
	}
}

func TestSarasWorkflowsCoverKeyFeatures(t *testing.T) {
	wfs := sarasWorkflows()
	names := make(map[string]bool)
	for _, wf := range wfs {
		names[wf.name] = true
	}

	required := []string{
		"saras-search",
		"saras-ask",
		"saras-trace",
		"saras-flow",
		"saras-map",
		"saras-cfg",
		"saras-cross-repo",
	}
	for _, name := range required {
		if !names[name] {
			t.Errorf("missing required workflow: %s", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Per-editor format tests
// ---------------------------------------------------------------------------

func TestFormatDevin(t *testing.T) {
	wfs := workflowsForEditor("devin")
	if len(wfs) == 0 {
		t.Fatal("expected devin workflows")
	}
	for _, wf := range wfs {
		if !strings.HasPrefix(wf.content, "---\n") {
			t.Errorf("%s: should have YAML frontmatter", wf.filename)
		}
		if !strings.Contains(wf.content, "name:") {
			t.Errorf("%s: should have name in frontmatter", wf.filename)
		}
		if !strings.Contains(wf.content, "description:") {
			t.Errorf("%s: should have description in frontmatter", wf.filename)
		}
		if !strings.Contains(wf.content, "category: Workflow") {
			t.Errorf("%s: should have category in frontmatter", wf.filename)
		}
		if !strings.Contains(wf.content, "tags:") {
			t.Errorf("%s: should have tags in frontmatter", wf.filename)
		}
		if !strings.HasSuffix(wf.filename, ".md") {
			t.Errorf("%s: should end with .md", wf.filename)
		}
	}
}

func TestFormatCursor(t *testing.T) {
	wfs := workflowsForEditor("cursor")
	if len(wfs) == 0 {
		t.Fatal("expected cursor commands")
	}
	for _, wf := range wfs {
		if !strings.HasPrefix(wf.content, "---\n") {
			t.Errorf("%s: should have YAML frontmatter", wf.filename)
		}
		if !strings.Contains(wf.content, "name: /saras-") {
			t.Errorf("%s: should have name with / prefix in frontmatter", wf.filename)
		}
		if !strings.Contains(wf.content, "id: saras-") {
			t.Errorf("%s: should have id in frontmatter", wf.filename)
		}
		if !strings.Contains(wf.content, "category: Workflow") {
			t.Errorf("%s: should have category in frontmatter", wf.filename)
		}
		if strings.Contains(wf.content, "// turbo") {
			t.Errorf("%s: should not contain // turbo", wf.filename)
		}
		if !strings.HasSuffix(wf.filename, ".md") {
			t.Errorf("%s: should end with .md", wf.filename)
		}
	}
}

func TestFormatClaude(t *testing.T) {
	wfs := workflowsForEditor("claude")
	if len(wfs) == 0 {
		t.Fatal("expected claude commands")
	}
	for _, wf := range wfs {
		if !strings.HasPrefix(wf.content, "---\n") {
			t.Errorf("%s: should have YAML frontmatter", wf.filename)
		}
		if !strings.Contains(wf.content, "name:") {
			t.Errorf("%s: should have name in frontmatter", wf.filename)
		}
		if !strings.Contains(wf.content, "category: Workflow") {
			t.Errorf("%s: should have category in frontmatter", wf.filename)
		}
		if !strings.HasPrefix(wf.filename, "saras/") {
			t.Errorf("%s: should use saras/ subfolder", wf.filename)
		}
		if strings.Contains(wf.content, "// turbo") {
			t.Errorf("%s: should not contain // turbo", wf.filename)
		}
	}
}

func TestFormatCopilot(t *testing.T) {
	wfs := workflowsForEditor("copilot")
	if len(wfs) == 0 {
		t.Fatal("expected copilot prompts")
	}
	for _, wf := range wfs {
		if !strings.HasPrefix(wf.content, "---\n") {
			t.Errorf("%s: should have YAML frontmatter", wf.filename)
		}
		if !strings.Contains(wf.content, "description:") {
			t.Errorf("%s: should have description in frontmatter", wf.filename)
		}
		if strings.Contains(wf.content, "agent:") {
			t.Errorf("%s: should not have agent field", wf.filename)
		}
		if !strings.HasSuffix(wf.filename, ".prompt.md") {
			t.Errorf("%s: should end with .prompt.md", wf.filename)
		}
		if strings.Contains(wf.content, "// turbo") {
			t.Errorf("%s: should not contain // turbo", wf.filename)
		}
	}
}

func TestFormatCodexReturnsNil(t *testing.T) {
	wfs := workflowsForEditor("codex")
	if len(wfs) != 0 {
		t.Errorf("codex should return no workflows, got %d", len(wfs))
	}
}

func TestWorkflowDisplayName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"saras-search", "SARAS: Search"},
		{"saras-ask", "SARAS: Ask"},
		{"saras-understand-codebase", "SARAS: Understand Codebase"},
		{"saras-add-dependency", "SARAS: Add Dependency"},
		{"saras-cross-repo", "SARAS: Cross Repo"},
	}
	for _, tc := range tests {
		got := workflowDisplayName(tc.input)
		if got != tc.want {
			t.Errorf("workflowDisplayName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestStripTurbo(t *testing.T) {
	input := "step 1\n// turbo\n```bash\ncommand\n```\nstep 2"
	got := stripTurbo(input)
	if strings.Contains(got, "// turbo") {
		t.Error("should have stripped // turbo")
	}
	if !strings.Contains(got, "step 1") || !strings.Contains(got, "step 2") {
		t.Error("should preserve other content")
	}
}

func TestWorkflowDir(t *testing.T) {
	tests := []struct {
		editor   string
		expected string
	}{
		{"devin", "/base/.devin/workflows"},
		{"cursor", "/base/.cursor/commands"},
		{"claude", "/base/.claude/commands"},
		{"copilot", "/base/.github/prompts"},
		{"codex", ""},
		{"unknown", ""},
	}
	for _, tc := range tests {
		got := workflowDir(tc.editor, "/base")
		if got != tc.expected {
			t.Errorf("workflowDir(%q) = %q, want %q", tc.editor, got, tc.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Installation tests
// ---------------------------------------------------------------------------

func TestInstallWorkflowsDevin(t *testing.T) {
	tmp := t.TempDir()

	cmd := installSkillCmd
	cmd.Flags().Set("devin", "true")
	cmd.Flags().Set("cursor", "false")
	cmd.Flags().Set("claude", "false")
	cmd.Flags().Set("copilot", "false")

	var buf strings.Builder
	cmd.SetOut(&buf)

	if err := installWorkflows(cmd, tmp); err != nil {
		t.Fatalf("installWorkflows error: %v", err)
	}

	dir := filepath.Join(tmp, ".devin", "workflows")
	wfs := workflowsForEditor("devin")
	for _, wf := range wfs {
		target := filepath.Join(dir, wf.filename)
		data, err := os.ReadFile(target)
		if err != nil {
			t.Errorf("workflow %s not created: %v", wf.filename, err)
			continue
		}
		if string(data) != wf.content {
			t.Errorf("workflow %s content mismatch", wf.filename)
		}
	}
}

func TestInstallWorkflowsCursor(t *testing.T) {
	tmp := t.TempDir()

	cmd := installSkillCmd
	cmd.Flags().Set("devin", "false")
	cmd.Flags().Set("cursor", "true")
	cmd.Flags().Set("claude", "false")
	cmd.Flags().Set("copilot", "false")

	var buf strings.Builder
	cmd.SetOut(&buf)

	if err := installWorkflows(cmd, tmp); err != nil {
		t.Fatalf("installWorkflows error: %v", err)
	}

	dir := filepath.Join(tmp, ".cursor", "commands")
	entries, _ := os.ReadDir(dir)
	if len(entries) == 0 {
		t.Fatal("expected cursor command files")
	}
}

func TestInstallWorkflowsClaude(t *testing.T) {
	tmp := t.TempDir()

	cmd := installSkillCmd
	cmd.Flags().Set("devin", "false")
	cmd.Flags().Set("cursor", "false")
	cmd.Flags().Set("claude", "true")
	cmd.Flags().Set("copilot", "false")

	var buf strings.Builder
	cmd.SetOut(&buf)

	if err := installWorkflows(cmd, tmp); err != nil {
		t.Fatalf("installWorkflows error: %v", err)
	}

	dir := filepath.Join(tmp, ".claude", "commands")
	entries, _ := os.ReadDir(dir)
	if len(entries) == 0 {
		t.Fatal("expected claude command files")
	}
}

func TestInstallWorkflowsCopilot(t *testing.T) {
	tmp := t.TempDir()

	cmd := installSkillCmd
	cmd.Flags().Set("devin", "false")
	cmd.Flags().Set("cursor", "false")
	cmd.Flags().Set("claude", "false")
	cmd.Flags().Set("copilot", "true")

	var buf strings.Builder
	cmd.SetOut(&buf)

	if err := installWorkflows(cmd, tmp); err != nil {
		t.Fatalf("installWorkflows error: %v", err)
	}

	dir := filepath.Join(tmp, ".github", "prompts")
	entries, _ := os.ReadDir(dir)
	if len(entries) == 0 {
		t.Fatal("expected copilot prompt files")
	}
	// Verify .prompt.md extension
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".prompt.md") {
			t.Errorf("copilot file %s should have .prompt.md extension", e.Name())
		}
	}
}

func TestInstallWorkflowsSkipsDisabledEditors(t *testing.T) {
	tmp := t.TempDir()

	cmd := installSkillCmd
	cmd.Flags().Set("devin", "false")
	cmd.Flags().Set("cursor", "false")
	cmd.Flags().Set("claude", "false")
	cmd.Flags().Set("copilot", "false")

	if err := installWorkflows(cmd, tmp); err != nil {
		t.Fatalf("installWorkflows error: %v", err)
	}

	// No directories should be created
	for _, sub := range []string{".devin/workflows", ".cursor/commands", ".claude/commands", ".github/prompts"} {
		if _, err := os.Stat(filepath.Join(tmp, sub)); err == nil {
			t.Errorf("should not create %s when editor disabled", sub)
		}
	}
}

func TestInstallWorkflowsOverwrites(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".devin", "workflows")
	os.MkdirAll(dir, 0755)

	wfs := workflowsForEditor("devin")
	existing := filepath.Join(dir, wfs[0].filename)
	os.WriteFile(existing, []byte("old content"), 0644)

	cmd := installSkillCmd
	cmd.Flags().Set("devin", "true")
	cmd.Flags().Set("cursor", "false")
	cmd.Flags().Set("claude", "false")
	cmd.Flags().Set("copilot", "false")

	var buf strings.Builder
	cmd.SetOut(&buf)

	if err := installWorkflows(cmd, tmp); err != nil {
		t.Fatalf("installWorkflows error: %v", err)
	}

	data, _ := os.ReadFile(existing)
	if string(data) == "old content" {
		t.Error("should have overwritten existing workflow")
	}

	if !strings.Contains(buf.String(), "Overwriting") {
		t.Error("should print overwrite message")
	}
}
