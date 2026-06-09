/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(installCmd)
	installCmd.AddCommand(installSkillCmd)
	installSkillCmd.Flags().Bool("cursor", false, "Install skill and rule for Cursor")
	installSkillCmd.Flags().Bool("devin", false, "Install skill for Devin")
	installSkillCmd.Flags().Bool("claude", false, "Install skill for Claude Code")
	installSkillCmd.Flags().Bool("codex", false, "Install skill for OpenAI Codex")
	installSkillCmd.Flags().Bool("copilot", false, "Install skill for GitHub Copilot")
	installSkillCmd.Flags().Bool("global", false, "Install skill globally to ~/.ide/ instead of the project directory")
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install saras integrations",
}

var installSkillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Install a saras skill for an AI coding agent",
	Long: `Install a skill file that teaches an AI coding agent how to use saras
for codebase search, Q&A, symbol tracing, architecture mapping,
execution-flow visualization, and Control Flow Graph (CFG)
generation with execution-path enumeration.

The skill name and folder are derived from the current directory name so that
the skill matches the project (e.g. directory "myapp" → skill name "myapp").

Pass one or more editor flags to specify which agents to install for:

  --cursor     .cursor/skills/<project>/SKILL.md + .cursor/rules/<project>.mdc
  --devin      .devin/skills/<project>/SKILL.md
  --claude     .claude/skills/<project>/SKILL.md
  --codex      .codex/skills/<project>/SKILL.md
  --copilot    .github/skills/<project>/SKILL.md + .github/copilot-instructions.md

By default, skills are installed in the current project directory. Use --global
to install to the user's home directory (~/.cursor/, ~/.devin/, etc.) so the
skill is available across all projects.

Examples:
  saras install skill --claude
  saras install skill --cursor --global
  saras install skill --devin --codex`,
	RunE: runInstallSkill,
}

type editorSkill struct {
	name    string
	path    string
	content string
}

func runInstallSkill(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	projectName := filepath.Base(cwd)

	global, _ := cmd.Flags().GetBool("global")

	// Determine base directory: project-local (cwd) or global (~/)
	baseDir := cwd
	if global {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get home directory: %w", err)
		}
		baseDir = home
	}

	editors := []editorSkill{
		{"claude", filepath.Join(baseDir, ".claude", "skills", projectName, "SKILL.md"), skillContentAgentSkills(projectName)},
		{"codex", filepath.Join(baseDir, ".codex", "skills", projectName, "SKILL.md"), skillContentAgentSkills(projectName)},
		{"devin", filepath.Join(baseDir, ".devin", "skills", projectName, "SKILL.md"), skillContentAgentSkills(projectName)},
		{"cursor", filepath.Join(baseDir, ".cursor", "skills", projectName, "SKILL.md"), skillContentAgentSkills(projectName)},
		{"copilot", filepath.Join(baseDir, ".github", "skills", projectName, "SKILL.md"), skillContentAgentSkills(projectName)},
	}

	// Cursor also gets a rule file (.mdc) in addition to the skill
	cursorRule := editorSkill{
		"cursor",
		filepath.Join(baseDir, ".cursor", "rules", projectName+".mdc"),
		skillContentCursor(),
	}

	// Copilot also gets a copilot-instructions.md in addition to the skill
	copilotInstructions := editorSkill{
		"copilot",
		filepath.Join(baseDir, ".github", "copilot-instructions.md"),
		"",
	}

	installed := 0
	for _, ed := range editors {
		flag, _ := cmd.Flags().GetBool(ed.name)
		if !flag {
			continue
		}

		if err := installSkillFile(cmd, ed.name, ed.path, ed.content); err != nil {
			return err
		}

		// Install the additional Cursor rule file
		if ed.name == "cursor" {
			if err := installSkillFile(cmd, "cursor (rule)", cursorRule.path, cursorRule.content); err != nil {
				return err
			}
		}

		// Install the additional Copilot instructions file
		if ed.name == "copilot" {
			content := skillContentCopilot(copilotInstructions.path)
			if err := installSkillFile(cmd, "copilot (instructions)", copilotInstructions.path, content); err != nil {
				return err
			}
		}

		installed++
	}

	if installed == 0 {
		return fmt.Errorf("specify at least one editor: --cursor, --devin, --claude, --codex, --copilot")
	}

	// Auto-install workflows for editors that support them
	if err := installWorkflows(cmd, baseDir); err != nil {
		return err
	}

	// Pointer-only update to the project's AGENTS.md (if any).
	// The skill files themselves carry the full saras tool docs;
	// AGENTS.md gets a small stable block telling agents to look at
	// the skill. We always target the project (cwd), even when
	// --global was used, because AGENTS.md is a per-project file.
	if err := patchProjectAgentsMD(cmd, cwd); err != nil {
		// Non-fatal: skill install succeeded, AGENTS.md just won't
		// advertise it. Print and continue.
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not update AGENTS.md: %v\n", err)
	}

	return nil
}

// patchProjectAgentsMD appends sarasSkillPointer to <projectRoot>/AGENTS.md
// if and only if the file exists and does not already carry the
// idempotency marker. Missing AGENTS.md is not an error — users who
// haven't run `saras install agentsmd` simply don't get the pointer
// (and can run that command later, which writes the pointer inline).
func patchProjectAgentsMD(cmd *cobra.Command, projectRoot string) error {
	agentsPath := filepath.Join(projectRoot, "AGENTS.md")
	existing, err := os.ReadFile(agentsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", agentsPath, err)
	}

	updated := ensureSarasSkillPointer(string(existing))
	if updated == string(existing) {
		fmt.Fprintf(cmd.OutOrStdout(), "AGENTS.md already references saras skill (no changes)\n")
		return nil
	}

	if err := os.WriteFile(agentsPath, []byte(updated), 0644); err != nil {
		return fmt.Errorf("write %s: %w", agentsPath, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Updated %s with saras skill pointer\n", agentsPath)
	return nil
}

// editorsWithWorkflows lists editors that support workflows/commands.
// Codex only uses AGENTS.md and has no custom command directory.
var editorsWithWorkflows = []string{"devin", "cursor", "claude", "copilot"}

// installWorkflows installs SARAS workflow/command files for all enabled editors.
func installWorkflows(cmd *cobra.Command, baseDir string) error {
	for _, editor := range editorsWithWorkflows {
		enabled, _ := cmd.Flags().GetBool(editor)
		if !enabled {
			continue
		}

		dir := workflowDir(editor, baseDir)
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}

		workflows := workflowsForEditor(editor)
		for _, wf := range workflows {
			target := filepath.Join(dir, wf.filename)
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("create directory for %s: %w", wf.filename, err)
			}
			if _, err := os.Stat(target); err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Overwriting %s command at %s\n", editor, target)
			}
			if err := os.WriteFile(target, []byte(wf.content), 0644); err != nil {
				return fmt.Errorf("write %s: %w", wf.filename, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Installed %s command: %s\n", editor, target)
		}
	}
	return nil
}

func installSkillFile(cmd *cobra.Command, agent, targetPath, content string) error {
	// Create parent directories
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	// For copilot, append if file already exists
	if agent == "copilot" {
		if existing, err := os.ReadFile(targetPath); err == nil {
			if strings.Contains(string(existing), "# SARAS Codebase Intelligence") {
				fmt.Fprintf(cmd.OutOrStdout(), "SARAS skill already exists in %s\n", targetPath)
				return nil
			}
			content = string(existing) + "\n\n" + content
		}
	} else {
		if _, err := os.Stat(targetPath); err == nil {
			fmt.Fprintf(cmd.OutOrStdout(), "Overwriting existing skill at %s\n", targetPath)
		}
	}

	if err := os.WriteFile(targetPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write skill file: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Installed saras skill for %s at %s\n", agent, targetPath)
	return nil
}

// sarasSkillPointerMarker is an HTML comment used as a stable
// idempotency token. We grep for it to decide whether
// ensureSarasSkillPointer needs to append the block — that way
// running `saras install skill` and `saras install agentsmd`
// repeatedly never produces duplicate pointer sections, and users
// can rename the heading without breaking detection.
const sarasSkillPointerMarker = "<!-- saras-skill-pointer -->"

// sarasSkillPointer is the small markdown block appended to a
// project's root AGENTS.md (or any AI-agent docs file) telling
// agents the repo ships a saras skill to use for codebase queries.
// It is intentionally short — the full tool docs live in the
// per-editor SKILL.md / *.mdc / copilot-instructions.md files
// produced by `saras install skill`. Keeping AGENTS.md project-
// focused (and the saras docs in the skill files) is what the user
// asked for, so this pointer is the single source of truth bridge.
const sarasSkillPointer = `
` + sarasSkillPointerMarker + `
## Codebase Tooling — ` + "`saras`" + `

This repository ships with a ` + "`saras`" + ` skill installed for AI coding agents.
Prefer ` + "`saras`" + ` over ad-hoc ` + "`grep`" + ` / file reads when answering questions
about the codebase or designing tests:

- **Search & Q&A** — ` + "`saras search`" + `, ` + "`saras ask --no-tui`" + `
- **Symbol tracing** — ` + "`saras trace <symbol> [--callers|--callees|--refs]`" + `
- **Architecture & flow** — ` + "`saras map`" + `, ` + "`saras flow`" + `
- **Control Flow Graphs** — ` + "`saras cfg <fn> [--with-context]`" + ` for
  branch / path enumeration (test design, edge cases). Add
  ` + "`--inline-callees`" + ` to splice helper CFGs into the caller so
  paths walk every branch the helper takes. Use
  ` + "`saras cfg paths <fn> --with-context`" + ` to also surface the
  receiver type, referenced types, and callee signatures alongside
  the paths.

The full per-editor SKILL.md (or rule / instructions) lives under
` + "`.cursor/skills/`" + `, ` + "`.claude/skills/`" + `, ` + "`.devin/skills/`" + `,
` + "`.codex/skills/`" + `, or ` + "`.github/copilot-instructions.md`" + ` depending on
your editor. Run ` + "`saras --help`" + ` for the complete CLI surface.
`

// ensureSarasSkillPointer appends sarasSkillPointer to content iff
// the marker is not already present. Returns content unchanged when
// the pointer already exists (so the function is safe to call from
// both ` + "`saras install agentsmd`" + ` and ` + "`saras install skill`" + ` without
// risk of duplicate sections).
//
// We keep the input intact when no change is needed — including
// trailing whitespace — so test fixtures and round-trip writes stay
// byte-identical.
func ensureSarasSkillPointer(content string) string {
	if strings.Contains(content, sarasSkillPointerMarker) {
		return content
	}
	// Normalize: ensure exactly one trailing newline before the
	// pointer block so the appended section reads cleanly even if
	// the LLM output ended with multiple newlines or none at all.
	trimmed := strings.TrimRight(content, "\n")
	return trimmed + "\n" + sarasSkillPointer
}

// cfgSkillSection is the shared Control Flow Graph docs included in
// all SKILL.md / *.mdc / copilot-instructions.md templates. Putting
// it in one constant keeps the four `## Control Flow Graphs`
// sections (Agent Skills, Cursor, Copilot, …) byte-for-byte
// consistent, so a fix in one place lands everywhere.
//
// We deliberately surface --with-context as the recommended pattern
// for AI agents: when an IDE assistant drives `saras cfg paths
// --with-context`, the appended file header / receiver type /
// referenced-type defs / callee signatures give the assistant the
// real shape of the codebase, which is exactly what it needs to
// design tests by hand without inventing parallel mock types.
const cfgSkillSection = `
## Control Flow Graphs (CFG)

Generate a Control Flow Graph for a single function — every branch,
loop, and termination point, plus the full set of execution paths
(one path per branch combination). Use this when the user asks to
"design tests for X", "what are the edge cases of X", "explain the
control flow of X", or "list the paths through X".

` + "```bash" + `
saras cfg authenticate                              # Mermaid diagram (default)
saras cfg authenticate --format text                # block / edge / path summary
saras cfg authenticate --format json                # CFG + paths in JSON
saras cfg authenticate --format paths               # just the paths and lines
saras cfg paths authenticate                        # alias for --format paths
saras cfg explain authenticate --no-tui             # LLM walkthrough of every path

# Surrounding code context — file header, receiver type, referenced
# types, callee signatures. Use when the user wants to write tests
# by hand or you want to reason about types without re-reading files.
saras cfg paths authenticate --with-context         # text appendix
saras cfg authenticate --format json --with-context # JSON "context" field

# Inlining callees — splice each project-internal helper's CFG into
# the caller so paths walk every branch inside the helper too. Uses
# the saras call graph to resolve callees; recursion and external /
# ambiguous callees are left as call sites with a Note. Bounded by
# --max-inline-depth (default 2).
saras cfg paths authenticate --inline-callees                   # 1-step deep
saras cfg paths authenticate --inline-callees --max-inline-depth 3
saras cfg explain authenticate --inline-callees --no-tui        # LLM sees inlined CFG

# Disambiguation — when the same function name exists in multiple
# files / languages / classes, pin the one you want:
saras cfg pkg/auth/login.go:authenticate            # path:fn shorthand
saras cfg authenticate --file pkg/auth              # file substring
saras cfg authenticate --language python            # restrict by language
saras cfg authenticate --parent UserService         # restrict by class/receiver
` + "```" + `

- One CFG per call. The function name is required; if it's
  ambiguous, the CLI prints a candidate list with disambiguation
  flags. Re-run with --file / --language / --parent or path:fn.
- Mermaid output is for diagrams; --with-context is ignored for it
  (a stderr warning is emitted). Use text/paths/json when piping to
  another tool or to your own reasoning.
- For test design, prefer ` + "`saras cfg paths <fn> --with-context`" + `:
  the path enumeration tells you which test cases to write; the
  context appendix tells you which real types to construct.
- Use ` + "`--inline-callees`" + ` when "design tests for X" requires
  understanding how X's helpers branch internally — every spliced
  helper's branches show up on each enumerated path. Inlined blocks
  are prefixed ` + "`[helperName]`" + ` so you can see the boundary in
  text/JSON output, and edges are labelled ` + "`call helperName`" + `.
`

// skillDepSection is the shared cross-repo dependency docs appended to all skill templates.
const skillDepSection = `
## Cross-Repository Dependencies

Query code across linked repositories. First check available deps:

` + "```bash" + `
saras dep list                                            # list available deps
` + "```" + `

Then use dep flags on any command:

` + "```bash" + `
saras search --from-dep legacy-auth "token validation"    # search one dep only
saras search --all-deps "database pool"                   # search all deps (no current repo)
saras search --with-deps "auth flow"                      # current repo + all deps
saras ask --from-dep legacy-auth --no-tui "how does session management work?"
saras trace --from-dep shared-lib HandleRequest
saras flow --from-dep legacy-auth
saras map --from-dep legacy-auth --format summary
` + "```" + `

- --from-dep, --all-deps, --with-deps are mutually exclusive
- Results from deps are labeled [role: name]
- Roles: legacy, shared-lib, reference, service
`

// skillContentAgentSkills returns the SKILL.md content following the Agent Skills spec
// (used by Claude Code, OpenAI Codex, and Devin).
func skillContentAgentSkills(projectName string) string {
	return `---
name: ` + projectName + `
description: Uses saras CLI to search, ask questions about, trace symbols in, map the
  architecture of, visualize execution flow in, and generate Control Flow Graphs (CFGs)
  with execution paths for any function in a codebase. Use when user asks to
  "search the code", "find where this is defined", "explain how this works", "trace this
  function", "show me the architecture", "what calls this", "understand this codebase",
  "how does this feature work", "generate an architecture map", "show the execution flow",
  "what does main call", "design tests for X", "what are the edge cases of X", "list the
  paths through X", or "explain the control flow of X". Requires saras to be initialized
  in the project.
license: Apache-2.0
compatibility: Requires saras CLI.
metadata:
  author: saras
  version: "1.0"
---
## Searching Code

Use semantic search to find code.

` + "```bash" + `
saras search "authentication middleware" --limit 10 --json
` + "```" + `

- Always use --json for machine-readable output you can parse
- Use --limit to control how many results (default 5)
- Phrase queries as natural language for best results

## Asking Questions

For questions that need explanation.
**Keep every ask atomic: one focused question per call.** Do not combine
multiple topics into a single question. If the user's request spans several
areas, break it into separate saras ask calls and synthesize the answers
yourself. Smaller, targeted questions yield faster and more accurate answers.

` + "```bash" + `
# Good: one topic per call
saras ask --no-tui "how does the payment flow work?"
saras ask --no-tui "what validation does processOrder perform?"

# Bad: multiple topics crammed into one call
# saras ask --no-tui "how does the payment flow work and what validation does processOrder perform and how are errors handled?"

saras ask --no-tui --with-arch "how does auth work?"            # include call-flow tree in context
saras ask --no-tui --with-arch=handleAuth "explain error paths" # call-flow from specific function
` + "```" + `

## Tracing Symbols

Find where a symbol is defined, who calls it, and what it calls:

` + "```bash" + `
saras trace HandleRequest
saras trace HandleRequest --callers
saras trace HandleRequest --callees
` + "```" + `

- Use exact symbol names (case-sensitive)

## Architecture Maps

Generate a structural overview of the project:

` + "```bash" + `
saras map --format summary
saras map --format markdown
saras map --format tree
` + "```" + `

## Execution Flow

Visualize call trees from entry points (main, CLI handlers, HTTP handlers):

` + "```bash" + `
saras flow                                     # all entry points
saras flow full                                # same as above
saras flow HandleRequest                       # from a specific function
saras flow --depth 3                           # limit depth (default: 8)
saras flow -o FLOW.md                          # write to file
saras flow explain --no-tui                    # concise LLM summary
saras flow explain full --no-tui               # exhaustive deep-dive analysis
saras flow explain full --no-tui -o EXPLAIN.md # deep-dive to file
saras flow explain runSearch --no-tui          # explain a specific function's flow
` + "```" + `

- Works across all supported languages
- Aliases: saras architecture, saras arch
- Markers: (cycle), (↩) already expanded, (...) depth limit
` + cfgSkillSection + `
## Important
- **Keep free-text requests atomic.** Every saras ask, flow explain, and
  cfg explain call should contain a single focused question or topic.
  Break multi-part user requests into separate calls and combine results.
- Always pass --no-tui for ask, flow explain, and cfg explain (TUI will block)
- Do not run saras watch (blocking)
- No results: run saras init or saras reindex
- Outputs include paths, lines, relevance
- Ask is stateless
- Runs locally
` + skillDepSection
}

// skillContentCursor returns the .mdc content for Cursor rules.
func skillContentCursor() string {
	return `---
description: Uses saras CLI for codebase search, Q&A, symbol tracing, architecture
  mapping, execution flow visualization, and Control Flow Graphs (CFGs) with full
  execution-path enumeration for any function. Use when user asks to search code,
  explain how something works, trace a function, show architecture, visualize
  execution flow, design tests for a function, list edge cases / branches, or
  understand the codebase. Requires saras to be initialized.
alwaysApply: false
---
## Searching Code

Use semantic search to find code.

` + "```bash" + `
saras search "authentication middleware" --limit 10 --json
` + "```" + `

- Always use --json for machine-readable output you can parse
- Use --limit to control how many results (default 5)
- Phrase queries as natural language for best results

## Asking Questions

For questions that need explanation.
**Keep every ask atomic: one focused question per call.** Do not combine
multiple topics into a single question. If the user's request spans several
areas, break it into separate saras ask calls and synthesize the answers
yourself. Smaller, targeted questions yield faster and more accurate answers.

` + "```bash" + `
# Good: one topic per call
saras ask --no-tui "how does the payment flow work?"
saras ask --no-tui "what validation does processOrder perform?"

# Bad: multiple topics crammed into one call
# saras ask --no-tui "how does the payment flow work and what validation does processOrder perform and how are errors handled?"

saras ask --no-tui --with-arch "how does auth work?"            # include call-flow tree in context
saras ask --no-tui --with-arch=handleAuth "explain error paths" # call-flow from specific function
` + "```" + `

## Tracing Symbols

Find where a symbol is defined, who calls it, and what it calls:

` + "```bash" + `
saras trace HandleRequest
saras trace HandleRequest --callers
saras trace HandleRequest --callees
` + "```" + `

- Use exact symbol names (case-sensitive)

## Architecture Maps

Generate a structural overview of the project:

` + "```bash" + `
saras map --format summary
saras map --format markdown
saras map --format tree
` + "```" + `

## Execution Flow

Visualize call trees from entry points (main, CLI handlers, HTTP handlers):

` + "```bash" + `
saras flow                                     # all entry points
saras flow full                                # same as above
saras flow HandleRequest                       # from a specific function
saras flow --depth 3                           # limit depth (default: 8)
saras flow -o FLOW.md                          # write to file
saras flow explain --no-tui                    # concise LLM summary
saras flow explain full --no-tui               # exhaustive deep-dive analysis
saras flow explain full --no-tui -o EXPLAIN.md # deep-dive to file
saras flow explain runSearch --no-tui          # explain a specific function's flow
` + "```" + `

- Works across all supported languages
- Aliases: saras architecture, saras arch
- Markers: (cycle), (↩) already expanded, (...) depth limit
` + cfgSkillSection + `
## Important
- **Keep free-text requests atomic.** Every saras ask, flow explain, and
  cfg explain call should contain a single focused question or topic.
  Break multi-part user requests into separate calls and combine results.
- Always pass --no-tui for ask, flow explain, and cfg explain (TUI will block)
- Do not run saras watch (blocking)
- No results: run saras init or saras reindex
- Outputs include paths, lines, relevance
- Ask is stateless
- Runs locally
` + skillDepSection
}

// skillContentCopilot returns plain markdown for GitHub Copilot instructions.
func skillContentCopilot(path string) string {
	return `# SARAS Codebase Intelligence

## Searching Code

Use semantic search to find code.

` + "```bash" + `
saras search "authentication middleware" --limit 10 --json
` + "```" + `

- Always use --json for machine-readable output you can parse
- Use --limit to control how many results (default 5)
- Phrase queries as natural language for best results

## Asking Questions

For questions that need explanation.
**Keep every ask atomic: one focused question per call.** Do not combine
multiple topics into a single question. If the user's request spans several
areas, break it into separate saras ask calls and synthesize the answers
yourself. Smaller, targeted questions yield faster and more accurate answers.

` + "```bash" + `
# Good: one topic per call
saras ask --no-tui "how does the payment flow work?"
saras ask --no-tui "what validation does processOrder perform?"

# Bad: multiple topics crammed into one call
# saras ask --no-tui "how does the payment flow work and what validation does processOrder perform and how are errors handled?"

saras ask --no-tui --with-arch "how does auth work?"            # include call-flow tree in context
saras ask --no-tui --with-arch=handleAuth "explain error paths" # call-flow from specific function
` + "```" + `

## Tracing Symbols

Find where a symbol is defined, who calls it, and what it calls:

` + "```bash" + `
saras trace HandleRequest
saras trace HandleRequest --callers
saras trace HandleRequest --callees
` + "```" + `

- Use exact symbol names (case-sensitive)

## Architecture Maps

Generate a structural overview of the project:

` + "```bash" + `
saras map --format summary
saras map --format markdown
saras map --format tree
` + "```" + `

## Execution Flow

Visualize call trees from entry points (main, CLI handlers, HTTP handlers):

` + "```bash" + `
saras flow                                     # all entry points
saras flow full                                # same as above
saras flow HandleRequest                       # from a specific function
saras flow --depth 3                           # limit depth (default: 8)
saras flow -o FLOW.md                          # write to file
saras flow explain --no-tui                    # concise LLM summary
saras flow explain full --no-tui               # exhaustive deep-dive analysis
saras flow explain full --no-tui -o EXPLAIN.md # deep-dive to file
saras flow explain runSearch --no-tui          # explain a specific function's flow
` + "```" + `

- Works across all supported languages
- Aliases: saras architecture, saras arch
- Markers: (cycle), (↩) already expanded, (...) depth limit
` + cfgSkillSection + `
## Important
- **Keep free-text requests atomic.** Every saras ask, flow explain, and
  cfg explain call should contain a single focused question or topic.
  Break multi-part user requests into separate calls and combine results.
- Always pass --no-tui for ask, flow explain, and cfg explain (TUI will block)
- Do not run saras watch (blocking)
- No results: run saras init or saras reindex
- Outputs include paths, lines, relevance
- Ask is stateless
- Runs locally
` + skillDepSection
}
