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
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/architect"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/ask"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/embedder"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/search"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/store"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/tui"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(askCmd)
}

var askCmd = &cobra.Command{
	Use:   "ask [question]",
	Short: "Ask a question about your codebase using RAG",
	Long: `Ask a natural language question about your codebase. SARAS will search
for relevant code, build context, and stream an AI-generated answer.

The response is powered by your configured LLM provider.

Examples:
  saras ask "how does the authentication flow work?"
  saras ask "what database connections are used?" --limit 10
  saras ask "explain the error handling strategy" --no-tui`,
	Args: cobra.MinimumNArgs(1),
	RunE: runAsk,
}

func init() {
	askCmd.Flags().IntP("limit", "n", 5, "Number of code snippets for context")
	askCmd.Flags().Int("max-tokens", 2048, "Maximum response tokens")
	askCmd.Flags().Float32("temperature", 0.1, "LLM temperature")
	askCmd.Flags().String("model", "", "Override LLM model")
	askCmd.Flags().Bool("no-tui", false, "Print response to stdout (no interactive TUI)")
	askCmd.Flags().StringP("output", "o", "", "Write response to file instead of stdout")
	askCmd.Flags().String("with-arch", "", "Include execution call-flow tree in context (optional: function name). For deeper analysis use 'saras flow explain full'")
	askCmd.Flags().Lookup("with-arch").NoOptDefVal = "__all__"
	addDepFlags(askCmd)
}

func runAsk(cmd *cobra.Command, args []string) error {
	question := strings.Join(args, " ")
	limit, _ := cmd.Flags().GetInt("limit")
	maxTokens, _ := cmd.Flags().GetInt("max-tokens")
	temperature, _ := cmd.Flags().GetFloat32("temperature")
	model, _ := cmd.Flags().GetString("model")
	noTUI, _ := cmd.Flags().GetBool("no-tui")
	outputFile, _ := cmd.Flags().GetString("output")
	withFlow, _ := cmd.Flags().GetString("with-arch")
	fromDep, allDeps, withDeps := parseDepFlags(cmd)

	projectRoot, err := config.FindProjectRoot()
	if err != nil {
		return fmt.Errorf("not a saras project (run 'saras init' first): %w", err)
	}

	cfg, err := config.Load(projectRoot)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Resolve dependency flags
	deps, includeCurrent, err := config.ResolveDeps(cfg, fromDep, allDeps, withDeps)
	if err != nil {
		return err
	}

	// Open store
	storePath := filepath.Join(config.GetConfigDir(projectRoot), "index.gob")
	st := store.NewGobStore(storePath)
	if err := st.Load(context.Background()); err != nil {
		_ = err
	}
	defer st.Close()

	// Create embedder
	emb, err := embedder.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("create embedder: %w", err)
	}
	defer emb.Close()

	// Create searcher
	searcher := search.NewSearcher(st, emb, cfg.Search)

	// Build chat endpoint from LLM config
	chatEndpoint := buildChatEndpoint(cfg)

	// Create pipeline
	pipelineOpts := llmPipelineOptions(cfg)

	chatModel := cfg.LLM.Model
	if model != "" {
		chatModel = model
	}

	pipeline := ask.NewPipeline(searcher, chatEndpoint, chatModel, pipelineOpts...)

	// Build optional flow context
	var extraContext string
	if withFlow != "" {
		flowCtx, err := buildFlowContext(projectRoot, cfg, withFlow)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not generate flow context: %v\n", err)
		} else {
			extraContext = flowCtx
			// Reduce RAG limit to leave room for flow data in context window
			if !cmd.Flags().Changed("limit") && limit > 3 {
				limit = limit - 2
			}
		}
	}

	// Build dep context if dep flags are set
	if deps != nil {
		ctx := context.Background()
		var currentResults []search.Result
		if includeCurrent {
			currentResults, _ = searcher.Search(ctx, question, limit)
		}
		depResults, depErr := searchWithDeps(ctx, cfg, emb, deps, includeCurrent, currentResults, question, limit)
		if depErr == nil && len(depResults) > 0 {
			var depCtx strings.Builder
			depCtx.WriteString("Cross-repository context:\n")
			for _, r := range depResults {
				label := formatResultLabel(r)
				depCtx.WriteString(fmt.Sprintf("--- %s%s (lines %d-%d, score: %.2f) ---\n",
					label, r.FilePath, r.StartLine, r.EndLine, r.Score))
				depCtx.WriteString(r.Content)
				depCtx.WriteString("\n\n")
			}
			if extraContext != "" {
				extraContext = extraContext + "\n\n" + depCtx.String()
			} else {
				extraContext = depCtx.String()
			}
		}
	}

	opts := ask.AskOptions{
		Query:        question,
		Limit:        limit,
		MaxTokens:    maxTokens,
		Temperature:  temperature,
		ExtraContext: extraContext,
	}

	if outputFile != "" {
		return runAskToFile(cmd, pipeline, opts, outputFile)
	}

	if noTUI {
		return runAskPlain(cmd, pipeline, opts)
	}

	return runAskTUI(cmd, pipeline, opts)
}

func runAskToFile(cmd *cobra.Command, pipeline *ask.Pipeline, opts ask.AskOptions, path string) error {
	ch, err := pipeline.Ask(context.Background(), opts)
	if err != nil {
		return fmt.Errorf("ask: %w", err)
	}

	var buf strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			return fmt.Errorf("ask: %w", chunk.Err)
		}
		buf.WriteString(chunk.Content)
	}
	if err := os.WriteFile(path, []byte(buf.String()), 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Written to %s\n", path)
	return nil
}

func runAskPlain(cmd *cobra.Command, pipeline *ask.Pipeline, opts ask.AskOptions) error {
	ch, err := pipeline.Ask(context.Background(), opts)
	if err != nil {
		return fmt.Errorf("ask: %w", err)
	}

	var buf strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			return fmt.Errorf("ask: %w", chunk.Err)
		}
		buf.WriteString(chunk.Content)
	}

	stylize, _ := cmd.Flags().GetBool("stylize-output")
	if stylize {
		width := terminalWidth()
		rendered := tui.RenderMarkdown(buf.String(), width)
		fmt.Fprint(cmd.OutOrStdout(), rendered)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), buf.String())
	}
	return nil
}

// terminalWidth returns the current terminal width, or 80 as fallback.
func terminalWidth() int {
	w, _, err := term.GetSize(os.Stdout.Fd())
	if err != nil || w <= 0 {
		return 80
	}
	return w
}

func runAskTUI(cmd *cobra.Command, pipeline *ask.Pipeline, opts ask.AskOptions) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := pipeline.Ask(ctx, opts)
	if err != nil {
		return fmt.Errorf("ask: %w", err)
	}

	stylize, _ := cmd.Flags().GetBool("stylize-output")
	model := tui.NewAskModelWithStyle(opts.Query, stylize)
	p := tea.NewProgram(model, tea.WithAltScreen())

	// Feed stream chunks to TUI in background
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
		cancel()
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}

const flowHintFooter = "\n\n---\nFor deeper flow analysis, use: `saras flow` (call tree) or `saras flow explain full` (exhaustive LLM walkthrough)."

// buildFlowContext generates a compact call-flow tree (depth 3) for use as
// additional context in ask queries. If funcName is "__all__" it generates
// trees from all entry points; otherwise it generates the tree for the named function.
func buildFlowContext(projectRoot string, cfg *config.Config, funcName string) (string, error) {
	const compactDepth = 3
	fm := architect.NewFlowMapper(projectRoot, cfg.Ignore, compactDepth)
	ctx := context.Background()

	var flowOutput string
	if funcName == "__all__" {
		trees, err := fm.GenerateFullFlow(ctx)
		if err != nil {
			return "", err
		}
		if len(trees) == 0 {
			return "", fmt.Errorf("no entry points detected")
		}
		flowOutput = architect.FormatFlowTrees(trees)
	} else {
		tree, err := fm.GenerateFunctionFlow(ctx, funcName)
		if err != nil {
			return "", err
		}
		flowOutput = architect.FormatFlowTree(tree)
	}

	return fmt.Sprintf("Call-flow tree (depth %d):\n```\n%s```%s", compactDepth, flowOutput, flowHintFooter), nil
}

func buildChatEndpoint(cfg *config.Config) string {
	endpoint := cfg.LLM.Endpoint

	switch cfg.LLM.Provider {
	case "ollama":
		endpoint = strings.TrimRight(endpoint, "/")
		if !strings.HasSuffix(endpoint, "/v1") {
			endpoint += "/v1"
		}
	case "lmstudio":
		endpoint = strings.TrimRight(endpoint, "/")
		if !strings.HasSuffix(endpoint, "/v1") {
			endpoint += "/v1"
		}
	case "openai":
		endpoint = strings.TrimRight(endpoint, "/")
	case "copilot":
		// GitHub Copilot's OpenAI-compatible surface lives at
		// /chat/completions directly under the root host (no /v1 prefix).
		if endpoint == "" {
			endpoint = config.DefaultCopilotEndpoint
		}
		endpoint = strings.TrimRight(endpoint, "/")
	}

	return endpoint
}
