/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and SARAS Contributors
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Template content tests
// ---------------------------------------------------------------------------

func TestSkillContentAgentUsesFlowPrimary(t *testing.T) {
	content := skillContentAgentSkills("testproject")

	// Should reference saras flow as the primary command
	if !strings.Contains(content, "saras flow") {
		t.Error("agent skill template should reference 'saras flow'")
	}
}

func TestSkillContentAgentNoStaleArchitectureCommands(t *testing.T) {
	content := skillContentAgentSkills("testproject")

	// "saras architecture" should only appear as an alias reference, not as a command example
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "saras architecture") {
			t.Errorf("skill template has stale 'saras architecture' command: %q", trimmed)
		}
	}
}

func TestSkillContentAgentSections(t *testing.T) {
	content := skillContentAgentSkills("testproject")

	required := []string{
		"## Searching Code",
		"## Asking Questions",
		"## Tracing Symbols",
		"## Architecture Maps",
		"## Execution Flow",
		"## Important",
	}
	for _, section := range required {
		if !strings.Contains(content, section) {
			t.Errorf("agent skill template missing section: %s", section)
		}
	}
}

func TestSkillContentAgentProjectName(t *testing.T) {
	content := skillContentAgentSkills("my-cool-project")
	if !strings.Contains(content, "name: my-cool-project") {
		t.Error("agent skill template should include the project name in frontmatter")
	}
}

func TestSkillContentAgentFrontmatterFields(t *testing.T) {
	content := skillContentAgentSkills("testproject")
	for _, want := range []string{
		"license: Apache-2.0",
		"compatibility: Requires saras CLI.",
		"metadata:",
		"  author: saras",
		"  version:",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("agent skill template should contain %q", want)
		}
	}
}

func TestSkillContentAgentImportantSection(t *testing.T) {
	content := skillContentAgentSkills("testproject")

	if !strings.Contains(content, "flow explain") {
		t.Error("Important section should reference 'flow explain', not 'architecture explain'")
	}
}

func TestSkillContentCursorNoStaleArchitectureCommands(t *testing.T) {
	content := skillContentCursor()

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "saras architecture") {
			t.Errorf("cursor skill template has stale 'saras architecture' command: %q", trimmed)
		}
	}
}

func TestSkillContentCursorHasMDCFrontmatter(t *testing.T) {
	content := skillContentCursor()
	if !strings.HasPrefix(content, "---\n") {
		t.Error("cursor skill should start with YAML frontmatter")
	}
	if !strings.Contains(content, "alwaysApply: false") {
		t.Error("cursor skill should have alwaysApply: false")
	}
}

func TestSkillContentCopilotUsesFlowPrimary(t *testing.T) {
	content := skillContentCopilot("/some/path")

	if !strings.Contains(content, "saras flow") {
		t.Error("copilot skill template should reference 'saras flow'")
	}
}

func TestSkillContentCopilotNoStaleArchitectureCommands(t *testing.T) {
	content := skillContentCopilot("/some/path")

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "saras architecture") {
			t.Errorf("copilot skill template has stale 'saras architecture' command: %q", trimmed)
		}
	}
}

func TestSkillContentCopilotTitle(t *testing.T) {
	content := skillContentCopilot("/some/path")
	if !strings.Contains(content, "# SARAS Codebase Intelligence") {
		t.Error("copilot skill should use SARAS (uppercase) in title")
	}
}

// ---------------------------------------------------------------------------
// Skill installation tests
// ---------------------------------------------------------------------------

func TestInstallSkillFileCreatesDirectories(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "deep", "nested", "SKILL.md")

	var buf bytes.Buffer
	cmd := installSkillCmd
	cmd.SetOut(&buf)

	err := installSkillFile(cmd, "test-agent", target, "test content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if string(data) != "test content" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestInstallSkillFileCopilotAppends(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "copilot-instructions.md")

	// Write existing content
	os.WriteFile(target, []byte("existing content"), 0644)

	var buf bytes.Buffer
	cmd := installSkillCmd
	cmd.SetOut(&buf)

	err := installSkillFile(cmd, "copilot", target, "new content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, "existing content") {
		t.Error("should preserve existing content")
	}
	if !strings.Contains(content, "new content") {
		t.Error("should append new content")
	}
}

func TestInstallSkillFileCopilotSkipsDuplicate(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "copilot-instructions.md")

	// Write existing content that already has the marker
	os.WriteFile(target, []byte("# SARAS Codebase Intelligence\nexisting"), 0644)

	var buf bytes.Buffer
	cmd := installSkillCmd
	cmd.SetOut(&buf)

	err := installSkillFile(cmd, "copilot", target, "new content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should not have been modified
	data, _ := os.ReadFile(target)
	if strings.Contains(string(data), "new content") {
		t.Error("should not duplicate SARAS skill")
	}

	if !strings.Contains(buf.String(), "already exists") {
		t.Error("should print already-exists message")
	}
}

func TestSkillContentAgentHasDepSection(t *testing.T) {
	content := skillContentAgentSkills("testproject")
	if !strings.Contains(content, "## Cross-Repository Dependencies") {
		t.Error("agent skill template should include dep section")
	}
	if !strings.Contains(content, "--from-dep") {
		t.Error("agent skill template should reference --from-dep flag")
	}
	if !strings.Contains(content, "saras dep list") {
		t.Error("agent skill template should reference 'saras dep list'")
	}
}

func TestSkillContentCursorHasDepSection(t *testing.T) {
	content := skillContentCursor()
	if !strings.Contains(content, "## Cross-Repository Dependencies") {
		t.Error("cursor skill template should include dep section")
	}
}

func TestSkillContentCopilotHasDepSection(t *testing.T) {
	content := skillContentCopilot("/some/path")
	if !strings.Contains(content, "## Cross-Repository Dependencies") {
		t.Error("copilot skill template should include dep section")
	}
}

func TestInstallSkillCmdRequiresEditorFlag(t *testing.T) {
	rootCmd.SetArgs([]string{"install", "skill"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error when no editor flag specified")
	}
	if err != nil && !strings.Contains(err.Error(), "specify at least one editor") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CFG section tests
// ---------------------------------------------------------------------------
//
// All three skill content templates (Agent Skills, Cursor, Copilot)
// share the cfgSkillSection constant, so we assert each renders the
// new "## Control Flow Graphs (CFG)" heading and the headline
// commands. This catches accidental decoupling — e.g. someone
// editing one template in place and forgetting the others.

func TestSkillContentAgentHasCFGSection(t *testing.T) {
	content := skillContentAgentSkills("testproject")
	for _, want := range []string{
		"## Control Flow Graphs (CFG)",
		"saras cfg authenticate",
		"saras cfg paths authenticate",
		"--with-context",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("agent skill template missing CFG content: %q", want)
		}
	}
}

func TestSkillContentCursorHasCFGSection(t *testing.T) {
	content := skillContentCursor()
	for _, want := range []string{
		"## Control Flow Graphs (CFG)",
		"saras cfg",
		"--with-context",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("cursor skill template missing CFG content: %q", want)
		}
	}
}

func TestSkillContentCopilotHasCFGSection(t *testing.T) {
	content := skillContentCopilot("/some/path")
	for _, want := range []string{
		"## Control Flow Graphs (CFG)",
		"saras cfg",
		"--with-context",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("copilot skill template missing CFG content: %q", want)
		}
	}
}

// ---------------------------------------------------------------------------
// AGENTS.md skill-pointer tests
// ---------------------------------------------------------------------------
//
// AGENTS.md stays project-focused. The saras-skill pointer is a
// short markdown block that links agents to the per-editor SKILL.md.
// We test ensureSarasSkillPointer (the idempotent injector) directly
// and patchProjectAgentsMD (the file-level wrapper used by `saras
// install skill`) end-to-end.

func TestEnsureSarasSkillPointerAppendsWhenMissing(t *testing.T) {
	in := "# AGENTS.md\n\n## Project Overview\nA test project.\n"
	out := ensureSarasSkillPointer(in)
	if out == in {
		t.Fatal("ensureSarasSkillPointer should have appended to AGENTS.md without marker")
	}
	if !strings.Contains(out, sarasSkillPointerMarker) {
		t.Error("output missing idempotency marker")
	}
	if !strings.Contains(out, "## Codebase Tooling") {
		t.Error("output missing pointer heading")
	}
	if !strings.Contains(out, "saras cfg") {
		t.Error("pointer should mention saras cfg")
	}
	if !strings.HasPrefix(out, "# AGENTS.md") {
		t.Error("original content should be preserved at the top")
	}
}

func TestEnsureSarasSkillPointerIsIdempotent(t *testing.T) {
	in := "# AGENTS.md\nbody\n"
	once := ensureSarasSkillPointer(in)
	twice := ensureSarasSkillPointer(once)
	if once != twice {
		t.Error("ensureSarasSkillPointer should be idempotent — running twice changed the output")
	}
}

func TestEnsureSarasSkillPointerLeavesUnrelatedContent(t *testing.T) {
	// File already has the marker hidden inside an unrelated section.
	in := "# AGENTS.md\nbody\n" + sarasSkillPointerMarker + "\n## Codebase Tooling — `saras`\nold pointer\n"
	out := ensureSarasSkillPointer(in)
	if out != in {
		t.Error("ensureSarasSkillPointer must not modify content that already contains the marker")
	}
}

func TestPatchProjectAgentsMDAppendsToExistingFile(t *testing.T) {
	tmp := t.TempDir()
	agents := filepath.Join(tmp, "AGENTS.md")
	if err := os.WriteFile(agents, []byte("# AGENTS.md\nproject body\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	cmd := installSkillCmd
	cmd.SetOut(&buf)

	if err := patchProjectAgentsMD(cmd, tmp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(agents)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, sarasSkillPointerMarker) {
		t.Error("AGENTS.md should now contain the pointer marker")
	}
	if !strings.Contains(got, "## Codebase Tooling") {
		t.Error("AGENTS.md should now contain the pointer heading")
	}
	if !strings.HasPrefix(got, "# AGENTS.md\nproject body") {
		t.Error("original content should be preserved at the top")
	}
	if !strings.Contains(buf.String(), "Updated") {
		t.Error("should print an Updated message on first append")
	}
}

func TestPatchProjectAgentsMDIsIdempotent(t *testing.T) {
	tmp := t.TempDir()
	agents := filepath.Join(tmp, "AGENTS.md")
	if err := os.WriteFile(agents, []byte("# AGENTS.md\nproject body\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	cmd := installSkillCmd
	cmd.SetOut(&buf)

	if err := patchProjectAgentsMD(cmd, tmp); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(agents)
	buf.Reset()

	if err := patchProjectAgentsMD(cmd, tmp); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(agents)

	if string(first) != string(second) {
		t.Error("second patchProjectAgentsMD call modified AGENTS.md — should be a no-op")
	}
	if !strings.Contains(buf.String(), "already references saras skill") {
		t.Error("should print an already-references message on second call")
	}
}

func TestPatchProjectAgentsMDMissingFileIsNoOp(t *testing.T) {
	tmp := t.TempDir()
	var buf bytes.Buffer
	cmd := installSkillCmd
	cmd.SetOut(&buf)

	if err := patchProjectAgentsMD(cmd, tmp); err != nil {
		t.Fatalf("missing AGENTS.md should not error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "AGENTS.md")); !os.IsNotExist(err) {
		t.Error("patchProjectAgentsMD should not create AGENTS.md when none exists")
	}
}
