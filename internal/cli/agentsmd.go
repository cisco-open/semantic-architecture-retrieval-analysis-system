/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/architect"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/ask"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/trace"
	"github.com/spf13/cobra"
)

var thinkTagRe = regexp.MustCompile(`(?s)<think>.*?</think>`)

func init() {
	installCmd.AddCommand(installAgentsMDCmd)
	installAgentsMDCmd.Flags().Bool("with-claudemd", false, "Also create CLAUDE.md that imports AGENTS.md")
	installAgentsMDCmd.Flags().Int("min-files", 2, "Minimum files in a package to generate a per-directory AGENTS.md")
}

var installAgentsMDCmd = &cobra.Command{
	Use:   "agentsmd",
	Short: "Generate nested AGENTS.md files using LLM analysis of the codebase",
	Long: `Generate AGENTS.md files by analyzing your codebase architecture with saras map
and using the configured LLM to produce optimized, agent-friendly project guides.

Creates a thin root AGENTS.md (project overview, setup, conventions) and a focused
AGENTS.md inside each package directory (files, symbols, patterns). This follows the
nested AGENTS.md convention used by large projects (e.g. OpenAI Codex repo uses 88+).

AGENTS.md is a standard format read by Codex, Devin, Cursor, Copilot, Gemini CLI,
Claude Code, and many other AI coding agents.

Use --with-claudemd to also create a CLAUDE.md that imports AGENTS.md.

Examples:
  saras install agentsmd
  saras install agentsmd --with-claudemd
  saras install agentsmd --min-files 3`,
	RunE: runInstallAgentsMD,
}

// ---------------------------------------------------------------------------
// System prompts
// ---------------------------------------------------------------------------

const rootAgentsMDPrompt = `You are a technical documentation generator. Output ONLY a markdown document.
No conversation. No questions. No preamble. No "here is". Just the document.

Generate a ROOT-LEVEL AGENTS.md for an entire project. This file gives AI coding agents
the high-level context they need. Per-package details go in subdirectory AGENTS.md files,
so keep this one SHORT and focused on project-wide information.

OUTPUT FORMAT (fill in from the input, skip sections with no evidence):

# AGENTS.md

## Project Overview
(1-2 sentences: what it does, primary language, key frameworks)

## Setup Commands
(exact shell commands: install, build, test, run)

## Testing
(test command, framework, how to run a single test)

## Code Style
(formatting, imports, naming conventions)

## Project Structure
(one-line description of each top-level directory)

## Architecture
(key patterns, how the main components connect)

RULES:
1. Output ONLY markdown starting with "# AGENTS.md".
2. Use exact commands, paths, and names from the input.
3. Maximum 80 lines — keep it concise.
4. Do NOT duplicate per-package details; those go in subdirectory AGENTS.md files.`

const pkgAgentsMDPrompt = `You are a technical documentation generator. Output ONLY a markdown document.
No conversation. No questions. No preamble. Just the document.

Generate a PACKAGE-LEVEL AGENTS.md for a single directory/package in a codebase.
This file tells AI coding agents how to work with THIS specific package.

OUTPUT FORMAT (fill in from the input, skip sections with no evidence):

# <package_name>

## Purpose
(1-2 sentences: what this package does)

## Key Files
(list important files with a one-line description of each)

## Key Symbols
(list the most important exported functions, types, interfaces with one-line descriptions)

## Internal Dependencies
(which other packages this one imports and why)

## Conventions
(any patterns specific to this package)

RULES:
1. Output ONLY markdown starting with "# <package_name>".
2. Be specific: use exact function names, type names, file names from the input.
3. Maximum 60 lines.
4. Focus on what an agent needs to know to EDIT code in this package.`

// ---------------------------------------------------------------------------
// Main command
// ---------------------------------------------------------------------------

func runInstallAgentsMD(cmd *cobra.Command, args []string) error {
	withClaudeMD, _ := cmd.Flags().GetBool("with-claudemd")
	minFiles, _ := cmd.Flags().GetInt("min-files")

	projectRoot, err := config.FindProjectRoot()
	if err != nil {
		return fmt.Errorf("not a saras project (run 'saras init' first): %w", err)
	}

	cfg, err := config.Load(projectRoot)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx := context.Background()
	provider := cfg.LLM.Provider
	baseEndpoint := strings.TrimRight(buildChatEndpoint(cfg), "/")

	// Step 1: Generate architecture map
	fmt.Fprintln(cmd.OutOrStdout(), "Analyzing codebase architecture...")
	mapper := architect.NewMapper(projectRoot, cfg.Ignore)
	cmap, err := mapper.GenerateMap(ctx)
	if err != nil {
		return fmt.Errorf("generate map: %w", err)
	}

	// Step 2: Generate root AGENTS.md
	fmt.Fprintln(cmd.OutOrStdout(), "Generating root AGENTS.md...")
	rootContent, err := generateRootAgentsMD(ctx, cmd, projectRoot, cfg, provider, baseEndpoint, cmap)
	if err != nil {
		return fmt.Errorf("root AGENTS.md: %w", err)
	}

	// AGENTS.md is project documentation, not saras documentation, so
	// we don't paste a full tool reference here. Instead append a
	// small idempotent pointer that tells AI coding agents the
	// project ships a `saras` skill they should use for codebase
	// queries (search, trace, map, flow, cfg, …). The pointer is
	// shared with `saras install skill` so a project that has both
	// commands run end up with exactly one pointer block.
	rootContent = ensureSarasSkillPointer(rootContent)

	agentsPath := filepath.Join(projectRoot, "AGENTS.md")
	if err := writeAgentsMD(cmd, agentsPath, rootContent); err != nil {
		return err
	}

	// Step 3: Generate per-package AGENTS.md files
	created := 1
	for _, pkg := range cmap.Packages {
		if len(pkg.Files) < minFiles {
			continue
		}
		if pkg.Path == "." {
			continue // root package handled above
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Generating AGENTS.md for %s...\n", pkg.Path)
		pkgContent, err := generatePkgAgentsMD(ctx, cfg, provider, baseEndpoint, cmap, pkg)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: skipping %s: %v\n", pkg.Path, err)
			continue
		}

		pkgAgentsPath := filepath.Join(projectRoot, pkg.Path, "AGENTS.md")
		if err := writeAgentsMD(cmd, pkgAgentsPath, pkgContent); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: skipping %s: %v\n", pkg.Path, err)
			continue
		}
		created++
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Created %d AGENTS.md files\n", created)

	// Step 4: Optionally create CLAUDE.md
	if withClaudeMD {
		claudePath := filepath.Join(projectRoot, "CLAUDE.md")
		claudeContent := "@AGENTS.md\n"
		if _, err := os.Stat(claudePath); err == nil {
			fmt.Fprintln(cmd.OutOrStdout(), "Overwriting existing CLAUDE.md")
		}
		if err := os.WriteFile(claudePath, []byte(claudeContent), 0644); err != nil {
			return fmt.Errorf("write CLAUDE.md: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Created %s (imports AGENTS.md)\n", claudePath)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Root AGENTS.md generation
// ---------------------------------------------------------------------------

func generateRootAgentsMD(ctx context.Context, cmd *cobra.Command, projectRoot string, cfg *config.Config, provider, baseEndpoint string, cmap *architect.CodebaseMap) (string, error) {
	// Build project-level context
	var input strings.Builder

	input.WriteString(fmt.Sprintf("PROJECT: %s\n", filepath.Base(projectRoot)))
	input.WriteString(fmt.Sprintf("FILES: %d  LINES: %d  PACKAGES: %d\n\n", cmap.TotalFiles, cmap.TotalLines, len(cmap.Packages)))

	// Package listing
	input.WriteString("PACKAGES:\n")
	for _, pkg := range cmap.Packages {
		input.WriteString(fmt.Sprintf("- %s (%s): %d files, %d funcs, %d types\n",
			pkg.Path, pkg.Name, len(pkg.Files), pkg.Functions, pkg.Types))
	}

	// Dependencies
	if len(cmap.Dependencies) > 0 {
		input.WriteString("\nINTERNAL DEPENDENCIES:\n")
		for _, d := range cmap.Dependencies {
			input.WriteString(fmt.Sprintf("- %s → %s\n", d.From, d.To))
		}
	}

	// Gather extra context from project files
	input.WriteString(gatherProjectContext(projectRoot))

	messages := []ask.Message{
		{Role: "system", Content: rootAgentsMDPrompt},
		{Role: "user", Content: input.String() + "\n\nGenerate the root AGENTS.md now. Start with '# AGENTS.md'."},
	}

	httpClient := llmChatHTTPClient(cfg)
	response, err := ask.ChatCompletion(ctx, provider, baseEndpoint, cfg.LLM.Model, cfg.LLM.APIKey, messages, 4096, 0.3, httpClient)
	if err != nil {
		return "", err
	}

	return cleanLLMResponse(response, "# AGENTS.md"), nil
}

// ---------------------------------------------------------------------------
// Per-package AGENTS.md generation
// ---------------------------------------------------------------------------

func generatePkgAgentsMD(ctx context.Context, cfg *config.Config, provider, baseEndpoint string, cmap *architect.CodebaseMap, pkg architect.PackageInfo) (string, error) {
	var input strings.Builder

	input.WriteString(fmt.Sprintf("PACKAGE: %s (import path: %s)\n", pkg.Name, pkg.Path))
	input.WriteString(fmt.Sprintf("FILES: %d  FUNCTIONS: %d  TYPES: %d  INTERFACES: %d  LINES: %d\n\n",
		len(pkg.Files), pkg.Functions, pkg.Types, pkg.Interfaces, pkg.Lines))

	// Files
	input.WriteString("FILES:\n")
	for _, f := range pkg.Files {
		input.WriteString(fmt.Sprintf("- %s\n", f))
	}

	// Symbols in this package
	pkgSymbols := filterSymbolsByDir(cmap.Symbols, pkg.Path)
	if len(pkgSymbols) > 0 {
		input.WriteString("\nSYMBOLS:\n")
		for _, s := range pkgSymbols {
			if s.Kind == trace.KindImport || s.Kind == trace.KindPackage {
				continue
			}
			sig := s.Signature
			if len(sig) > 100 {
				sig = sig[:100] + "..."
			}
			input.WriteString(fmt.Sprintf("- [%s] %s (%s:%d)", s.Kind, s.Name, filepath.Base(s.FilePath), s.Line))
			if sig != "" {
				input.WriteString(fmt.Sprintf(" — %s", sig))
			}
			input.WriteString("\n")
		}
	}

	// Dependencies involving this package
	var deps []string
	for _, d := range cmap.Dependencies {
		if d.From == pkg.Path {
			deps = append(deps, fmt.Sprintf("imports %s", d.To))
		} else if d.To == pkg.Path || strings.HasSuffix(d.To, "/"+pkg.Name) {
			deps = append(deps, fmt.Sprintf("imported by %s", d.From))
		}
	}
	if len(deps) > 0 {
		input.WriteString("\nDEPENDENCIES:\n")
		for _, d := range deps {
			input.WriteString(fmt.Sprintf("- %s\n", d))
		}
	}

	messages := []ask.Message{
		{Role: "system", Content: pkgAgentsMDPrompt},
		{Role: "user", Content: input.String() + "\n\nGenerate the package AGENTS.md now."},
	}

	httpClient := llmChatHTTPClient(cfg)
	response, err := ask.ChatCompletion(ctx, provider, baseEndpoint, cfg.LLM.Model, cfg.LLM.APIKey, messages, 2048, 0.3, httpClient)
	if err != nil {
		return "", err
	}

	return cleanLLMResponse(response, "# "+pkg.Name), nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func filterSymbolsByDir(symbols []trace.Symbol, dir string) []trace.Symbol {
	var result []trace.Symbol
	for _, s := range symbols {
		if filepath.Dir(s.FilePath) == dir {
			result = append(result, s)
		}
	}
	return result
}

func gatherProjectContext(projectRoot string) string {
	var ctx strings.Builder

	// README
	for _, name := range []string{"README.md", "readme.md", "README"} {
		if data, err := os.ReadFile(filepath.Join(projectRoot, name)); err == nil {
			content := string(data)
			if len(content) > 3000 {
				content = content[:3000] + "\n...(truncated)"
			}
			ctx.WriteString("\nREADME:\n" + content + "\n")
			break
		}
	}

	// Makefile
	for _, name := range []string{"Makefile", "makefile"} {
		if data, err := os.ReadFile(filepath.Join(projectRoot, name)); err == nil {
			content := string(data)
			if len(content) > 1500 {
				content = content[:1500] + "\n...(truncated)"
			}
			ctx.WriteString("\nMAKEFILE:\n" + content + "\n")
			break
		}
	}

	// Dependency file
	for _, name := range []string{"go.mod", "package.json", "requirements.txt", "Cargo.toml", "pom.xml", "pyproject.toml"} {
		if data, err := os.ReadFile(filepath.Join(projectRoot, name)); err == nil {
			content := string(data)
			if len(content) > 1500 {
				content = content[:1500] + "\n...(truncated)"
			}
			ctx.WriteString(fmt.Sprintf("\n%s:\n%s\n", strings.ToUpper(name), content))
			break
		}
	}

	return ctx.String()
}

func cleanLLMResponse(response, expectedHeader string) string {
	// Strip <think>...</think> blocks (chain-of-thought fallback)
	cleaned := thinkTagRe.ReplaceAllString(response, "")
	cleaned = strings.TrimSpace(cleaned)

	// If stripping think tags left us with nothing, keep original content
	if cleaned == "" {
		cleaned = strings.TrimSpace(response)
	}
	response = cleaned

	// Remove markdown code fences wrapping the output
	response = strings.TrimPrefix(response, "```markdown")
	response = strings.TrimPrefix(response, "```md")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	// Ensure it starts with the expected header
	if !strings.HasPrefix(response, "# ") {
		response = expectedHeader + "\n\n" + response
	}

	return response + "\n"
}

func writeAgentsMD(cmd *cobra.Command, path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(cmd.OutOrStdout(), "  Overwriting %s\n", path)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "  Created %s\n", path)
	return nil
}
