/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/ask"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/cfg"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/tui"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(cfgCmd)
	cfgCmd.AddCommand(cfgPathsCmd)
	cfgCmd.AddCommand(cfgExplainCmd)
}

var cfgCmd = &cobra.Command{
	Use:   "cfg [function | path:function]",
	Short: "Generate a Control Flow Graph (CFG) for a function",
	Long: `Generate the Control Flow Graph (CFG) for a single function. The CFG
captures every branch, loop, and termination point in the function and
enumerates the full set of execution paths through it.

The primary use case is water-tight test design: write one test per
enumerated path to achieve branch coverage with confidence.

Argument forms:
  authenticate                       Bare function name. Errors out with a
                                     candidate list when more than one
                                     match exists.
  pkg/auth/login.go:authenticate     File-prefixed name. Use a relative
                                     project path or an absolute path. The
                                     file part is matched as a substring,
                                     so ` + "`pkg/auth:authenticate`" + ` works too.

Disambiguation flags (combine freely with the path form):
  --file <substring>     Match only files whose path contains the value
  --language <name>      Restrict to a specific source language
                         (go, python, ruby, java, javascript, ...)
  --parent <name>        Restrict to a specific receiver / class / module

Output formats:
  mermaid (default) — Mermaid flowchart, pasteable into GitHub markdown
  text              — Human-readable summary of blocks, edges, and paths
  json              — Machine-readable CFG + paths
  paths             — Just the enumerated paths with the source lines hit

Examples:
  saras cfg authenticate                              # Mermaid diagram
  saras cfg pkg/auth/login.go:authenticate            # Same, disambiguated by path
  saras cfg authenticate --language go                # Same, disambiguated by language
  saras cfg authenticate --parent UserService         # Same, disambiguated by parent
  saras cfg authenticate --format text                # Block/edge/path summary
  saras cfg authenticate --format json                # JSON (CFG + paths)
  saras cfg authenticate -o cfg.md                    # Write to file

Subcommands:
  saras cfg <fn> paths    # Path enumeration only
  saras cfg <fn> explain  # LLM walkthrough of each path

The heuristic builder supports brace languages (Go, JS/TS, Java, C/C++,
C#, Rust, Kotlin, Scala, PHP, Dart, Swift, Objective-C, Groovy, Perl,
Zig), indentation-based Python (2 & 3), end-keyword Ruby, and POSIX
shells. For unsupported markup/config files (JSON, YAML, Markdown, ...)
use the LLM-backed ` + "`explain`" + ` subcommand which is
language-agnostic.`,
	Args: cobra.ExactArgs(1),
	RunE: runCFG,
}

var cfgPathsCmd = &cobra.Command{
	Use:   "paths [function | path:function]",
	Short: "Enumerate every execution path through a function",
	Long: `List every simple acyclic path through the function from entry to exit.
Each path corresponds to one branch combination — i.e. one test case
required for branch coverage.

Loop back-edges are excluded; each loop contributes two path categories
(skipped and entered).

Use --file/--language/--parent (or the path:function shorthand) to
disambiguate when multiple functions share a name.

Examples:
  saras cfg paths authenticate
  saras cfg paths pkg/auth/login.go:authenticate
  saras cfg paths authenticate --language python`,
	Args: cobra.ExactArgs(1),
	RunE: runCFGPaths,
}

var cfgExplainCmd = &cobra.Command{
	Use:   "explain [function | path:function]",
	Short: "Use an LLM to walk through each CFG path in natural language",
	Long: `Generate the CFG, enumerate every execution path, and ask your configured
LLM (OpenAI, Ollama, GitHub Copilot, ...) to walk through each path in
plain English. Useful for code review, onboarding, and understanding
legacy functions.

Use --file/--language/--parent (or the path:function shorthand) to
disambiguate when multiple functions share a name.

Examples:
  saras cfg explain authenticate
  saras cfg explain pkg/auth/login.go:authenticate
  saras cfg explain authenticate --no-tui -o EXPLAIN.md`,
	Args: cobra.ExactArgs(1),
	RunE: runCFGExplain,
}

// CFG-specific flag-name and help-text constants. The disambiguation
// flag constants (--file/--language/--parent) live in select_args.go
// because they're shared with `saras trace`.
const (
	flagOutput      = "output"
	flagMaxPaths    = "max-paths"
	flagNoTUI       = "no-tui"
	flagMaxTokens   = "max-tokens"
	flagTemp        = "temperature"
	flagModel       = "model"
	flagWithContext = "with-context"
	helpOutput      = "Write output to file instead of stdout"
	helpMaxPaths    = "Maximum execution paths to enumerate"
	helpNoTUI       = "Print response to stdout (no interactive TUI)"
	helpMaxTokens   = "Maximum response tokens"
	helpTemp        = "LLM temperature"
	helpModelOver   = "Override LLM model"
	helpWithContext = "Append surrounding project context (file header, " +
		"receiver type, referenced types, callee signatures). " +
		"text/paths formats append a markdown appendix; json embeds a " +
		"`context` object; mermaid is unchanged (warning emitted)."

	flagInlineCallees  = "inline-callees"
	flagMaxInlineDepth = "max-inline-depth"
	helpInlineCallees  = "Inline project-internal callees into the CFG so a " +
		"path through outer() walks every branch inside helper() it calls. " +
		"Recursive calls and ambiguous/external callees are left as call " +
		"sites with a Note. Bounded by --max-inline-depth."
	helpMaxInlineDepth = "Maximum inline recursion depth (0 = use default). " +
		"Higher values produce richer CFGs but explode block count and " +
		"Mermaid render time on deep call graphs."
)

// addLLMFlags registers the flags used by `cfg explain` (the only
// LLM-backed cfg subcommand). Test-generation was deliberately
// removed because the model can't reliably produce compile-correct
// tests without deep semantic understanding of the project's mocking
// boundaries — `cfg paths` + `cfg explain` give reviewers what they
// need to write tests by hand.
func addLLMFlags(cmd *cobra.Command, defaultMaxTokens int) {
	cmd.Flags().StringP(flagOutput, "o", "", helpOutput)
	cmd.Flags().Bool(flagNoTUI, false, helpNoTUI)
	cmd.Flags().Int(flagMaxTokens, defaultMaxTokens, helpMaxTokens)
	cmd.Flags().Float32(flagTemp, 0.2, helpTemp)
	cmd.Flags().String(flagModel, "", helpModelOver)
	cmd.Flags().Int(flagMaxPaths, cfg.DefaultMaxPaths, helpMaxPaths)
}

// addInlineFlags registers --inline-callees / --max-inline-depth on
// every cfg subcommand that produces a CFG (i.e. all of them today).
// Keeping this in one helper means future cfg subcommands can opt in
// with a single line.
func addInlineFlags(cmd *cobra.Command) {
	cmd.Flags().Bool(flagInlineCallees, false, helpInlineCallees)
	cmd.Flags().Int(flagMaxInlineDepth, cfg.DefaultMaxInlineDepth, helpMaxInlineDepth)
}

// inlineOptionsFromCmd reads the shared --inline-callees /
// --max-inline-depth flags off cmd and returns the cfg.InlineOptions
// to pass into BuildFromFunctionInlined. Centralising this means a
// future flag rename / default change touches one place.
func inlineOptionsFromCmd(cmd *cobra.Command) cfg.InlineOptions {
	enabled, _ := cmd.Flags().GetBool(flagInlineCallees)
	depth, _ := cmd.Flags().GetInt(flagMaxInlineDepth)
	return cfg.InlineOptions{Enabled: enabled, MaxDepth: depth}
}

func init() {
	cfgCmd.Flags().StringP("format", "f", "mermaid", "Output format: mermaid, text, json, paths")
	cfgCmd.Flags().StringP(flagOutput, "o", "", helpOutput)
	cfgCmd.Flags().Int(flagMaxPaths, cfg.DefaultMaxPaths, helpMaxPaths)
	cfgCmd.Flags().Bool(flagWithContext, false, helpWithContext)
	addSelectFlags(cfgCmd)
	addInlineFlags(cfgCmd)

	cfgPathsCmd.Flags().StringP(flagOutput, "o", "", helpOutput)
	cfgPathsCmd.Flags().Int(flagMaxPaths, cfg.DefaultMaxPaths, helpMaxPaths)
	cfgPathsCmd.Flags().Bool(flagWithContext, false, helpWithContext)
	addSelectFlags(cfgPathsCmd)
	addInlineFlags(cfgPathsCmd)

	addLLMFlags(cfgExplainCmd, 4096)
	cfgExplainCmd.Flags().Bool(flagWithContext, false,
		"Include surrounding project context (file header, receiver "+
			"type, referenced types, callee signatures) in the LLM prompt "+
			"so the explanation references real types and helpers.")
	addSelectFlags(cfgExplainCmd)
	addInlineFlags(cfgExplainCmd)
}

// ---------------------------------------------------------------------------
// runCFG: render the CFG in the requested format
// ---------------------------------------------------------------------------

func runCFG(cmd *cobra.Command, args []string) error {
	format, _ := cmd.Flags().GetString("format")
	outputFile, _ := cmd.Flags().GetString(flagOutput)
	maxPaths, _ := cmd.Flags().GetInt(flagMaxPaths)
	withContext, _ := cmd.Flags().GetBool(flagWithContext)

	c, err := buildCFG(cmd, args[0])
	if err != nil {
		return err
	}

	var pc *ProjectContext
	if withContext {
		// Mermaid is a diagram — appending markdown around it would
		// break renderers (mermaid.live, GitHub) that expect a single
		// flowchart block. Print a stderr note so the user knows the
		// flag was a no-op for this format and can switch.
		if isMermaidFormat(format) {
			fmt.Fprintln(cmd.ErrOrStderr(),
				"warning: --with-context is ignored for mermaid output "+
					"(use --format text|paths|json to see the context)")
		} else {
			pc = gatherCFGContext(context.Background(), c)
		}
	}

	out, err := renderCFG(c, format, maxPaths, pc)
	if err != nil {
		return err
	}

	if outputFile != "" {
		if err := os.WriteFile(outputFile, []byte(out), 0o644); err != nil {
			return fmt.Errorf("write file: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Written to %s\n", outputFile)
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), out)
	return nil
}

// isMermaidFormat reports whether the supplied format string resolves
// to the Mermaid renderer (the default when the user passes "" or
// "mermaid"). Centralising the check keeps the spelling consistent
// across the renderer and the --with-context warning.
func isMermaidFormat(format string) bool {
	f := strings.ToLower(strings.TrimSpace(format))
	return f == "" || f == "mermaid"
}

// renderCFG dispatches to the right format-specific renderer. When
// `pc` is non-nil and useful (Empty()==false), text/paths formats
// receive a markdown appendix and json receives an embedded
// `context` field. Mermaid is unchanged regardless of pc — callers
// in runCFG already short-circuit before getting here.
func renderCFG(c *cfg.CFG, format string, maxPaths int, pc *ProjectContext) (string, error) {
	// Trigger enumeration so the maxPaths cap is honored in all formats.
	_ = c.EnumeratePaths(maxPaths)

	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "mermaid":
		return wrapMermaid(c, maxPaths), nil
	case "text", "plain":
		return appendContextText(c.ToText(), pc), nil
	case "json":
		return cfgJSONWithContext(c, maxPaths, pc)
	case "paths":
		return appendContextText(c.ToPathsText(), pc), nil
	default:
		return "", fmt.Errorf("unknown format %q (expected mermaid, text, json, or paths)", format)
	}
}

// appendContextText concatenates the text rendering of pc to base
// with a blank-line separator, or returns base unchanged when pc is
// empty / nil. Centralising this keeps the text and paths formats
// consistent.
func appendContextText(base string, pc *ProjectContext) string {
	if pc.Empty() {
		return base
	}
	return base + "\n\n" + pc.Text()
}

// cfgJSONWithContext serialises the CFG plus an optional context
// object. When pc is empty the output matches `cfg.CFG.ToJSON()`
// byte-for-byte (modulo whitespace), so existing JSON consumers see
// no schema drift unless --with-context is requested.
func cfgJSONWithContext(c *cfg.CFG, maxPaths int, pc *ProjectContext) (string, error) {
	type out struct {
		*cfg.CFG
		Paths   []cfg.Path      `json:"paths,omitempty"`
		Context *ProjectContext `json:"context,omitempty"`
	}
	o := out{CFG: c}
	if len(c.Blocks) > 0 {
		o.Paths = c.EnumeratePaths(maxPaths)
	}
	if !pc.Empty() {
		o.Context = pc
	}
	data, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// wrapMermaid prefixes the diagram with a fenced markdown block and a short
// header so the output is pasteable into a README.
func wrapMermaid(c *cfg.CFG, maxPaths int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# CFG: %s\n\n", c.Function)
	fmt.Fprintf(&b, "File: `%s:%d-%d`\n", c.File, c.StartLine, c.EndLine)
	if c.Language != "" {
		fmt.Fprintf(&b, "Language: %s\n", c.Language)
	}
	paths := c.EnumeratePaths(maxPaths)
	fmt.Fprintf(&b, "Execution paths: **%d**\n\n", len(paths))
	b.WriteString("```mermaid\n")
	b.WriteString(c.ToMermaid())
	b.WriteString("```\n")
	if len(c.Notes) > 0 {
		b.WriteString("\nNotes:\n")
		for _, n := range c.Notes {
			fmt.Fprintf(&b, "- %s\n", n)
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// runCFGPaths: just the path list
// ---------------------------------------------------------------------------

func runCFGPaths(cmd *cobra.Command, args []string) error {
	outputFile, _ := cmd.Flags().GetString(flagOutput)
	maxPaths, _ := cmd.Flags().GetInt(flagMaxPaths)
	withContext, _ := cmd.Flags().GetBool(flagWithContext)

	c, err := buildCFG(cmd, args[0])
	if err != nil {
		return err
	}
	_ = c.EnumeratePaths(maxPaths)
	out := c.ToPathsText()
	if withContext {
		out = appendContextText(out, gatherCFGContext(context.Background(), c))
	}

	if outputFile != "" {
		if err := os.WriteFile(outputFile, []byte(out), 0o644); err != nil {
			return fmt.Errorf("write file: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Written to %s\n", outputFile)
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), out)
	return nil
}

// ---------------------------------------------------------------------------
// runCFGExplain: LLM walkthrough of each path
// ---------------------------------------------------------------------------

const cfgExplainSystemPrompt = `You are SARAS, a code-flow analyst. You receive
a function's source code and its Control Flow Graph (CFG) with every
execution path enumerated. Walk through each path in clear, concise English.

Instructions:
- For each path, explain:
  - What input/state triggers this path (which conditions are true/false)
  - What the function does along this path step by step
  - The return value or side effects produced
- Be factual — do not invent behavior not visible in the code.
- Reference line numbers from the path summary when useful.
- Highlight any path that looks like an edge case, error path, or unintended
  behavior worth a second look.
- Use markdown with a ` + "`## Path N`" + ` heading per path.`

func runCFGExplain(cmd *cobra.Command, args []string) error {
	outputFile, _ := cmd.Flags().GetString(flagOutput)
	noTUI, _ := cmd.Flags().GetBool(flagNoTUI)
	maxTokens, _ := cmd.Flags().GetInt(flagMaxTokens)
	temperature, _ := cmd.Flags().GetFloat32(flagTemp)
	model, _ := cmd.Flags().GetString(flagModel)
	maxPaths, _ := cmd.Flags().GetInt(flagMaxPaths)
	withContext, _ := cmd.Flags().GetBool(flagWithContext)

	c, err := buildCFG(cmd, args[0])
	if err != nil {
		return err
	}
	_ = c.EnumeratePaths(maxPaths)

	source, err := readFunctionSource(c)
	if err != nil {
		return fmt.Errorf("read function source: %w", err)
	}

	// When --with-context is set we prepend the structured project
	// context to the LLM prompt. Without it the explanation can drift
	// (the model invents type names or guesses helper return values);
	// with it the explanation references real types and helpers from
	// the project.
	prefix := ""
	if withContext {
		pc := gatherCFGContext(context.Background(), c)
		if !pc.Empty() {
			prefix = "Project context:\n" + pc.Text() + "\n"
		}
	}

	contextStr := fmt.Sprintf(
		"%sFunction source:\n```%s\n%s\n```\n\nControl-flow graph:\n```\n%s\n```",
		prefix, c.Language, source, c.ToText())
	question := fmt.Sprintf("Walk through every execution path of `%s` and explain what happens along each.", c.Function)

	return runLLMStream(cmd, llmStreamOpts{
		systemPrompt: cfgExplainSystemPrompt,
		contextStr:   contextStr,
		question:     question,
		maxTokens:    maxTokens,
		temperature:  temperature,
		model:        model,
		outputFile:   outputFile,
		noTUI:        noTUI,
		tuiTitle:     "cfg explain: " + c.Function,
	})
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// buildCFG resolves the supplied positional argument (which may be
// `funcName`, `relative/path:funcName`, or `/abs/path:funcName`), combines
// it with any explicit --file/--language/--parent flags, and returns the
// CFG of the resulting unique function.
//
// On ambiguity the wrapped *cfg.AmbiguousFunctionError is converted to a
// silent cobra error so users get the candidate list without seeing a
// double "Error:" prefix in the terminal.
func buildCFG(cmd *cobra.Command, arg string) (*cfg.CFG, error) {
	projectRoot, err := config.FindProjectRoot()
	if err != nil {
		return nil, fmt.Errorf("not a saras project (run 'saras init' first): %w", err)
	}
	cfgRoot, err := config.Load(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	pathHint, funcName := parseSymbolRef(arg, projectRoot)
	opts := selectOptionsFromCmd(cmd, pathHint)
	inline := inlineOptionsFromCmd(cmd)

	// BuildFromFunctionInlined is a strict superset of
	// BuildFromFunctionWith — when inline.Enabled is false it
	// returns the same CFG as before, so existing tests and
	// callers see no behavioural drift.
	c, err := cfg.BuildFromFunctionInlined(
		context.Background(), projectRoot, funcName, cfgRoot.Ignore, opts, inline)
	if err != nil {
		var amb *cfg.AmbiguousFunctionError
		if errors.As(err, &amb) {
			return nil, printAmbiguous(cmd, amb)
		}
		return nil, err
	}
	return c, nil
}

// readFunctionSource reads the literal source code of the function described
// by the CFG (start line through end line, inclusive). Used to seed LLM
// prompts.
func readFunctionSource(c *cfg.CFG) (string, error) {
	projectRoot, err := config.FindProjectRoot()
	if err != nil {
		return "", err
	}
	absPath := projectRoot + string(os.PathSeparator) + c.File
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	if c.StartLine <= 0 || c.EndLine > len(lines) {
		return "", fmt.Errorf("invalid line range %d-%d for %s", c.StartLine, c.EndLine, c.File)
	}
	return strings.Join(lines[c.StartLine-1:c.EndLine], "\n"), nil
}

// llmStreamOpts bundles the parameters used by runLLMStream so the call
// surface stays manageable.
type llmStreamOpts struct {
	systemPrompt string
	contextStr   string
	question     string
	maxTokens    int
	temperature  float32
	model        string // LLM model override; empty = use config default
	outputFile   string
	noTUI        bool
	tuiTitle     string
}

// runLLMStream powers `cfg explain`. It sets up the pipeline using
// the project's configured LLM (Copilot, OpenAI, Ollama, LMStudio,
// ...) and streams the response either to stdout, a file, or the
// interactive TUI.
func runLLMStream(cmd *cobra.Command, o llmStreamOpts) error {
	projectRoot, err := config.FindProjectRoot()
	if err != nil {
		return fmt.Errorf("not a saras project (run 'saras init' first): %w", err)
	}
	conf, err := config.Load(projectRoot)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	chatEndpoint := buildChatEndpoint(conf)
	pipelineOpts := llmPipelineOptions(conf)

	chatModel := conf.LLM.Model
	if o.model != "" {
		chatModel = o.model
	}

	// We don't need RAG search for CFG explain/tests — the function source
	// and CFG already provide all the context. Pass a nil searcher; ask
	// pipelines handle a nil searcher gracefully for AskWithContext.
	pipeline := ask.NewPipeline(nil, chatEndpoint, chatModel, pipelineOpts...)
	askOpts := ask.AskOptions{
		MaxTokens:   o.maxTokens,
		Temperature: o.temperature,
	}

	ch, err := pipeline.AskWithContext(context.Background(), o.systemPrompt, o.contextStr, o.question, askOpts)
	if err != nil {
		return fmt.Errorf("send to llm: %w", err)
	}

	if o.outputFile != "" {
		return flowExplainToFile(ch, o.outputFile, cmd)
	}
	if o.noTUI {
		return flowExplainPlain(ch, cmd)
	}
	stylize, _ := cmd.Flags().GetBool("stylize-output")
	return cfgExplainTUI(ch, o.tuiTitle, stylize)
}

// cfgExplainTUI mirrors flowExplainTUI but with a custom title.
func cfgExplainTUI(ch <-chan ask.StreamChunk, title string, stylize bool) error {
	model := tui.NewAskModelWithStyle(title, stylize)
	p := tea.NewProgram(model, tea.WithAltScreen())

	go func() {
		for chunk := range ch {
			p.Send(tui.AskStreamChunkMsg{
				Content: chunk.Content,
				Done:    chunk.Done,
				Err:     chunk.Err,
			})
		}
	}()

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}
