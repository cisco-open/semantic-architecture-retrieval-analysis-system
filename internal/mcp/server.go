/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/architect"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/ask"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/embedder"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/search"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/store"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/trace"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// Server is the MCP server exposing saras tools via the standard MCP protocol.
type Server struct {
	searcher    *search.Searcher
	pipeline    *ask.Pipeline
	tracer      *trace.Tracer
	mapper      *architect.Mapper
	cfg         *config.Config
	projectRoot string
	emb         embedder.Embedder

	mcpServer *mcpserver.MCPServer
	sseServer *mcpserver.SSEServer
	addr      string
	name      string
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithAddr sets the listen address.
func WithAddr(addr string) ServerOption {
	return func(s *Server) { s.addr = addr }
}

// WithName sets the server name (advertised to MCP clients).
func WithName(name string) ServerOption {
	return func(s *Server) { s.name = name }
}

// WithProjectRoot sets the project root for dependency resolution.
func WithProjectRoot(root string) ServerOption {
	return func(s *Server) { s.projectRoot = root }
}

// WithEmbedder sets the embedder for dependency search.
func WithEmbedder(emb embedder.Embedder) ServerOption {
	return func(s *Server) { s.emb = emb }
}

// NewServer creates a new MCP server with SSE transport.
func NewServer(
	searcher *search.Searcher,
	pipeline *ask.Pipeline,
	tracer *trace.Tracer,
	mapper *architect.Mapper,
	cfg *config.Config,
	opts ...ServerOption,
) *Server {
	s := &Server{
		searcher: searcher,
		pipeline: pipeline,
		tracer:   tracer,
		mapper:   mapper,
		cfg:      cfg,
		addr:     "127.0.0.1:9420",
		name:     "saras",
	}

	for _, opt := range opts {
		opt(s)
	}

	// Create the mcp-go MCPServer
	s.mcpServer = mcpserver.NewMCPServer(
		s.name,
		"1.0.0",
		mcpserver.WithToolCapabilities(false),
	)

	s.registerTools()

	// Create SSE server with base URL for proper endpoint advertisement
	s.sseServer = mcpserver.NewSSEServer(s.mcpServer,
		mcpserver.WithBaseURL(fmt.Sprintf("http://%s", s.addr)),
	)

	return s
}

func (s *Server) registerTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("search",
			mcp.WithDescription("Semantic search across the indexed codebase"),
			mcp.WithString("query", mcp.Required(), mcp.Description("Natural language search query")),
			mcp.WithNumber("limit", mcp.Description("Max results (default 10)")),
			mcp.WithString("from_dep", mcp.Description("Search ONLY this dependency (by name)")),
			mcp.WithBoolean("all_deps", mcp.Description("Search all dependencies (excludes current repo)")),
			mcp.WithBoolean("with_deps", mcp.Description("Search current repo AND all dependencies")),
		),
		s.handleSearch,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("ask",
			mcp.WithDescription("Ask a question about the codebase using RAG (search + LLM)"),
			mcp.WithString("question", mcp.Required(), mcp.Description("Question about the codebase")),
			mcp.WithNumber("limit", mcp.Description("Number of code snippets for context")),
			mcp.WithNumber("maxTokens", mcp.Description("Maximum response tokens")),
			mcp.WithString("from_dep", mcp.Description("Search ONLY this dependency for context (by name)")),
			mcp.WithBoolean("all_deps", mcp.Description("Search all dependencies for context (excludes current repo)")),
			mcp.WithBoolean("with_deps", mcp.Description("Search current repo AND all dependencies for context")),
		),
		s.handleAsk,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("trace",
			mcp.WithDescription("Trace a code symbol: definition, references, callers, callees"),
			mcp.WithString("symbol", mcp.Required(), mcp.Description("Symbol name to trace")),
			mcp.WithString("from_dep", mcp.Description("Trace ONLY in this dependency (by name)")),
			mcp.WithBoolean("all_deps", mcp.Description("Trace in all dependencies (excludes current repo)")),
			mcp.WithBoolean("with_deps", mcp.Description("Trace in current repo AND all dependencies")),
		),
		s.handleTrace,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("map",
			mcp.WithDescription("Generate a codebase architecture map"),
			mcp.WithString("format", mcp.Description("Output format: tree, markdown, summary"), mcp.Enum("tree", "markdown", "summary")),
			mcp.WithString("from_dep", mcp.Description("Map ONLY this dependency (by name)")),
			mcp.WithBoolean("all_deps", mcp.Description("Map all dependencies (excludes current repo)")),
			mcp.WithBoolean("with_deps", mcp.Description("Map current repo AND all dependencies")),
		),
		s.handleMap,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("symbols",
			mcp.WithDescription("List all symbols in the codebase"),
			mcp.WithString("kind", mcp.Description("Filter by kind: function, type, interface, method")),
			mcp.WithString("name", mcp.Description("Filter by name (substring match)")),
		),
		s.handleSymbols,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("dep_list",
			mcp.WithDescription("List configured cross-repository dependencies with their names, roles, and paths"),
		),
		s.handleDepList,
	)
}

// Serve starts the MCP server with SSE transport.
func (s *Server) Serve() error {
	return s.sseServer.Start(s.addr)
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.sseServer.Shutdown(ctx)
}

// GetMCPServer returns the underlying mcp-go MCPServer (for testing).
func (s *Server) GetMCPServer() *mcpserver.MCPServer {
	return s.mcpServer
}

// ---------------------------------------------------------------------------
// Helper to build tool results
// ---------------------------------------------------------------------------

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{Type: "text", Text: text},
		},
	}
}

func errorResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{Type: "text", Text: text},
		},
		IsError: true,
	}
}

// ---------------------------------------------------------------------------
// Tool handlers
// ---------------------------------------------------------------------------

// resolveDepFlags extracts dep flags from an MCP request.
func (s *Server) resolveDepFlags(req mcp.CallToolRequest) (string, bool, bool) {
	fromDep := req.GetString("from_dep", "")
	allDeps, _ := req.GetArguments()["all_deps"].(bool)
	withDeps, _ := req.GetArguments()["with_deps"].(bool)
	return fromDep, allDeps, withDeps
}

// mcpSearchDeps searches dependencies and returns labeled results.
func (s *Server) mcpSearchDeps(ctx context.Context, deps []config.Dependency, includeCurrent bool, query string, limit int) ([]search.Result, error) {
	var allResults []search.Result

	if includeCurrent {
		currentResults, err := s.searcher.Search(ctx, query, limit)
		if err == nil {
			allResults = append(allResults, currentResults...)
		}
	}

	if s.emb == nil {
		return allResults, nil
	}

	for _, dep := range deps {
		depStorePath := filepath.Join(config.GetConfigDir(dep.Path), "index.gob")
		depSt := store.NewGobStore(depStorePath)
		if err := depSt.Load(ctx); err != nil {
			continue
		}
		depSearcher := search.NewSearcher(depSt, s.emb, s.cfg.Search)
		depResults, err := depSearcher.Search(ctx, query, limit)
		depSt.Close()
		if err != nil {
			continue
		}
		for i := range depResults {
			depResults[i].DepName = dep.Name
			depResults[i].DepRole = dep.Role
		}
		allResults = append(allResults, depResults...)
	}

	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Score > allResults[j].Score
	})
	if len(allResults) > limit {
		allResults = allResults[:limit]
	}
	return allResults, nil
}

func (s *Server) handleSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query := req.GetString("query", "")
	if query == "" {
		return errorResult("query is required"), nil
	}
	limit := req.GetInt("limit", 10)
	if limit <= 0 {
		limit = 10
	}

	fromDep, allDeps, withDeps := s.resolveDepFlags(req)
	deps, includeCurrent, err := config.ResolveDeps(s.cfg, fromDep, allDeps, withDeps)
	if err != nil {
		return errorResult(err.Error()), nil
	}

	var results []search.Result
	if deps != nil {
		results, err = s.mcpSearchDeps(ctx, deps, includeCurrent, query, limit)
	} else {
		results, err = s.searcher.Search(ctx, query, limit)
	}
	if err != nil {
		return errorResult("search error: " + err.Error()), nil
	}

	if len(results) == 0 {
		return textResult("No results found for: " + query), nil
	}

	var b strings.Builder
	for i, r := range results {
		label := ""
		if r.DepName != "" {
			label = fmt.Sprintf("[%s: %s] ", r.DepRole, r.DepName)
		}
		b.WriteString(fmt.Sprintf("%d. [%.2f] %s%s:%d-%d\n", i+1, r.Score, label, r.FilePath, r.StartLine, r.EndLine))
		lines := strings.SplitN(strings.TrimSpace(r.Content), "\n", 4)
		maxLines := 3
		if len(lines) < maxLines {
			maxLines = len(lines)
		}
		for _, l := range lines[:maxLines] {
			b.WriteString("   " + l + "\n")
		}
		b.WriteString("\n")
	}

	return textResult(b.String()), nil
}

func (s *Server) handleAsk(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	question := req.GetString("question", "")
	if question == "" {
		return errorResult("question is required"), nil
	}

	if s.pipeline == nil {
		return errorResult("ask pipeline not configured (no LLM endpoint)"), nil
	}

	response, err := s.pipeline.AskSync(ctx, ask.AskOptions{
		Query:     question,
		Limit:     req.GetInt("limit", 0),
		MaxTokens: req.GetInt("maxTokens", 0),
	})
	if err != nil {
		return errorResult("ask error: " + err.Error()), nil
	}

	return textResult(response), nil
}

func (s *Server) handleTrace(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	symbol := req.GetString("symbol", "")
	if symbol == "" {
		return errorResult("symbol is required"), nil
	}

	result, err := s.tracer.Trace(ctx, symbol)
	if err != nil {
		return errorResult("trace error: " + err.Error()), nil
	}

	var b strings.Builder
	if result.Symbol != nil {
		b.WriteString(fmt.Sprintf("Symbol: %s (%s)\n", result.Symbol.Name, result.Symbol.Kind))
		b.WriteString(fmt.Sprintf("  File: %s:%d-%d\n", result.Symbol.FilePath, result.Symbol.Line, result.Symbol.EndLine))
		if result.Symbol.Signature != "" {
			b.WriteString(fmt.Sprintf("  Sig:  %s\n", result.Symbol.Signature))
		}
		b.WriteString("\n")
	}

	if len(result.References) > 0 {
		b.WriteString(fmt.Sprintf("References (%d):\n", len(result.References)))
		max := 20
		if len(result.References) < max {
			max = len(result.References)
		}
		for _, r := range result.References[:max] {
			b.WriteString(fmt.Sprintf("  %s:%d  %s\n", r.FilePath, r.Line, r.Context))
		}
		b.WriteString("\n")
	}

	if len(result.Callers) > 0 {
		b.WriteString(fmt.Sprintf("Callers (%d):\n", len(result.Callers)))
		for _, c := range result.Callers {
			b.WriteString(fmt.Sprintf("  %s (%s:%d)\n", c.Caller, c.CallerFile, c.CallerLine))
		}
		b.WriteString("\n")
	}

	if len(result.Callees) > 0 {
		b.WriteString(fmt.Sprintf("Callees (%d):\n", len(result.Callees)))
		for _, c := range result.Callees {
			b.WriteString(fmt.Sprintf("  %s\n", c.Callee))
		}
	}

	if b.Len() == 0 {
		return textResult(fmt.Sprintf("No information found for symbol: %s", symbol)), nil
	}

	return textResult(b.String()), nil
}

func (s *Server) handleMap(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	format := req.GetString("format", "markdown")

	switch format {
	case "tree":
		tree, err := s.mapper.GenerateTree(ctx)
		if err != nil {
			return errorResult("error: " + err.Error()), nil
		}
		return textResult(tree), nil

	case "markdown", "md":
		md, err := s.mapper.GenerateMarkdown(ctx)
		if err != nil {
			return errorResult("error: " + err.Error()), nil
		}
		return textResult(md), nil

	case "summary":
		cmap, err := s.mapper.GenerateMap(ctx)
		if err != nil {
			return errorResult("error: " + err.Error()), nil
		}
		data, _ := json.MarshalIndent(cmap, "", "  ")
		return textResult(string(data)), nil

	default:
		return errorResult(fmt.Sprintf("unknown format: %s", format)), nil
	}
}

func (s *Server) handleSymbols(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	kind := req.GetString("kind", "")
	name := req.GetString("name", "")

	symbols, err := s.tracer.ExtractSymbols(ctx)
	if err != nil {
		return errorResult("error: " + err.Error()), nil
	}

	// Filter
	var filtered []trace.Symbol
	for _, sym := range symbols {
		if kind != "" && sym.Kind.String() != kind {
			continue
		}
		if name != "" && !strings.Contains(strings.ToLower(sym.Name), strings.ToLower(name)) {
			continue
		}
		filtered = append(filtered, sym)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Symbols (%d):\n", len(filtered)))
	max := 50
	if len(filtered) < max {
		max = len(filtered)
	}
	for _, sym := range filtered[:max] {
		b.WriteString(fmt.Sprintf("  %-12s %-30s %s:%d\n", sym.Kind, sym.Name, sym.FilePath, sym.Line))
	}
	if len(filtered) > 50 {
		b.WriteString(fmt.Sprintf("  ... and %d more\n", len(filtered)-50))
	}

	return textResult(b.String()), nil
}

func (s *Server) handleDepList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if len(s.cfg.Dependencies) == 0 {
		return textResult("No dependencies configured. Use 'saras dep add' to add one."), nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Dependencies (%d):\n", len(s.cfg.Dependencies)))
	for _, dep := range s.cfg.Dependencies {
		status := "ok"
		if !config.Exists(dep.Path) {
			status = "missing"
		}
		b.WriteString(fmt.Sprintf("  %-20s %-12s %-8s %s\n", dep.Name, dep.Role, status, dep.Path))
	}
	return textResult(b.String()), nil
}

// GetAddr returns the server address.
func (s *Server) GetAddr() string { return s.addr }
