/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/trace"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(traceCmd)
}

var traceCmd = &cobra.Command{
	Use:   "trace [symbol | path:symbol]",
	Short: "Trace a symbol: definition, references, callers, callees",
	Long: `Trace a code symbol across your project. Shows the symbol definition,
all references, caller functions, and callee functions.

Argument forms:
  Login                              Bare symbol name. When more than one
                                     symbol with this name exists, saras
                                     prints a candidate list and exits
                                     non-zero (refs/callers ignore this).
  pkg/auth/login.go:Login            File-prefixed name. Use a relative
                                     project path or an absolute path.
                                     The file part is matched as a
                                     substring, so ` + "`pkg/auth:Login`" + ` works.

Disambiguation flags (combine freely with the path form):
  --file <substring>     Match only files whose path contains the value
  --language <name>      Restrict to a specific source language
                         (go, python, ruby, java, javascript, ...)
  --parent <name>        Restrict to a specific receiver / class / module

Disambiguation is enforced when picking the symbol's *definition* and
when listing its callees (which reads the resolved function body). The
broader scans — references and callers — are name-based and are not
narrowed by these flags.

Examples:
  saras trace Login                                     # full trace
  saras trace pkg/auth/login.go:Login                   # path:symbol shorthand
  saras trace handleRequest --callers
  saras trace NewDB --callees --file internal/store
  saras trace authenticate --parent UserService
  saras trace login --language python --callees`,
	Args: cobra.ExactArgs(1),
	RunE: runTrace,
}

func init() {
	traceCmd.Flags().Bool("callers", false, "Show only callers")
	traceCmd.Flags().Bool("callees", false, "Show only callees")
	traceCmd.Flags().Bool("refs", false, "Show only references")
	traceCmd.Flags().Bool("json", false, "Output as JSON")
	addSelectFlags(traceCmd)
	addDepFlags(traceCmd)
}

func runTrace(cmd *cobra.Command, args []string) error {
	showCallers, _ := cmd.Flags().GetBool("callers")
	showCallees, _ := cmd.Flags().GetBool("callees")
	showRefs, _ := cmd.Flags().GetBool("refs")
	fromDep, allDeps, withDeps := parseDepFlags(cmd)

	projectRoot, err := config.FindProjectRoot()
	if err != nil {
		return fmt.Errorf("not a saras project (run 'saras init' first): %w", err)
	}

	cfg, err := config.Load(projectRoot)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	deps, includeCurrent, err := config.ResolveDeps(cfg, fromDep, allDeps, withDeps)
	if err != nil {
		return err
	}

	// Parse the optional `path:symbolName` shorthand and merge with
	// any explicit --file/--language/--parent flags. The same options
	// are applied to every tracer (current project + deps) so a
	// polyglot dep with a same-named symbol is handled the same way
	// as the current project.
	pathHint, symbolName := parseSymbolRef(args[0], projectRoot)
	opts := selectOptionsFromCmd(cmd, pathHint)

	ctx := context.Background()
	out := cmd.OutOrStdout()

	type labeledTracer struct {
		label  string
		tracer *trace.Tracer
	}
	var tracers []labeledTracer

	if deps != nil {
		if includeCurrent {
			tracers = append(tracers, labeledTracer{"", trace.NewTracer(projectRoot, cfg.Ignore)})
		}
		for _, dep := range deps {
			depCfg, err := config.Load(dep.Path)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: skipping dep %s: %v\n", dep.Name, err)
				continue
			}
			label := fmt.Sprintf("[%s: %s] ", dep.Role, dep.Name)
			tracers = append(tracers, labeledTracer{label, trace.NewTracer(dep.Path, depCfg.Ignore)})
		}
	} else {
		tracers = append(tracers, labeledTracer{"", trace.NewTracer(projectRoot, cfg.Ignore)})
	}

	for _, lt := range tracers {
		if lt.label != "" {
			fmt.Fprintf(out, "\n%s\n", lt.label)
		}

		if showCallers {
			callers, err := lt.tracer.FindCallers(ctx, symbolName)
			if err != nil {
				if lt.label != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "  %sfind callers: %v\n", lt.label, err)
					continue
				}
				return fmt.Errorf("find callers: %w", err)
			}
			printCallers(cmd, symbolName, callers)
			continue
		}

		if showCallees {
			if err := traceCallees(cmd, lt.tracer, lt.label, symbolName, opts); err != nil {
				return err
			}
			continue
		}

		if showRefs {
			refs, err := lt.tracer.FindReferences(ctx, symbolName)
			if err != nil {
				if lt.label != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "  %sfind references: %v\n", lt.label, err)
					continue
				}
				return fmt.Errorf("find references: %w", err)
			}
			printRefs(cmd, symbolName, refs)
			continue
		}

		if err := traceFull(cmd, lt.tracer, lt.label, symbolName, opts); err != nil {
			return err
		}
	}

	return nil
}

// traceFull resolves a unique symbol (or bails with an
// AmbiguousSymbolError list) and emits its definition, references,
// callers, and callees. References and callers are still scanned
// project-wide by name, so cross-language collisions don't drop
// useful results.
func traceFull(
	cmd *cobra.Command,
	tracer *trace.Tracer,
	label, symbolName string,
	opts trace.SelectOptions,
) error {
	ctx := context.Background()
	out := cmd.OutOrStdout()

	pick, err := resolveTraceSymbol(cmd, tracer, label, symbolName, opts)
	if err != nil {
		return err
	}

	if pick != nil {
		s := pick.Symbol
		fmt.Fprintf(out, "Symbol: %s (%s)\n", s.Name, s.Kind)
		fmt.Fprintf(out, "  File: %s:%d-%d\n", s.FilePath, s.Line, s.EndLine)
		if s.Signature != "" {
			fmt.Fprintf(out, "  Sig:  %s\n", s.Signature)
		}
		if s.Parent != "" {
			fmt.Fprintf(out, "  Type: %s\n", s.Parent)
		}
		if pick.Language != "" {
			fmt.Fprintf(out, "  Lang: %s\n", pick.Language)
		}
		fmt.Fprintln(out)
	} else if label == "" {
		fmt.Fprintf(out, "Symbol %q not found as a definition\n\n", symbolName)
	}

	refs, err := tracer.FindReferences(ctx, symbolName)
	if err == nil && len(refs) > 0 {
		printRefs(cmd, symbolName, refs)
		fmt.Fprintln(out)
	}

	if pick != nil && (pick.Symbol.Kind == trace.KindFunction || pick.Symbol.Kind == trace.KindMethod) {
		if callers, err := tracer.FindCallers(ctx, symbolName); err == nil && len(callers) > 0 {
			printCallers(cmd, symbolName, callers)
			fmt.Fprintln(out)
		}
		if callees, err := tracer.FindCallees(ctx, symbolName); err == nil && len(callees) > 0 {
			printCallees(cmd, symbolName, callees)
		}
	}
	return nil
}

// traceCallees resolves the unique target function/method and lists
// its callees. The current trace.FindCallees implementation picks the
// first matching definition; once we resolve a unique candidate up
// front we can guarantee the callee list belongs to the function the
// user actually meant.
func traceCallees(
	cmd *cobra.Command,
	tracer *trace.Tracer,
	label, symbolName string,
	opts trace.SelectOptions,
) error {
	ctx := context.Background()

	// --callees only makes sense on a function or method.
	scoped := opts
	scoped.Kinds = []trace.SymbolKind{trace.KindFunction, trace.KindMethod}

	cands, err := tracer.FindCandidates(ctx, symbolName, scoped)
	if err != nil {
		if label != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "  %sfind candidates: %v\n", label, err)
			return nil
		}
		return fmt.Errorf("find candidates: %w", err)
	}

	pick, err := trace.SelectOne("function", symbolName, cands, scoped)
	if err != nil {
		var amb *trace.AmbiguousSymbolError
		if errors.As(err, &amb) {
			return printAmbiguous(cmd, amb)
		}
		if label != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "  %s%v\n", label, err)
			return nil
		}
		return err
	}

	callees, err := tracer.FindCallees(ctx, pick.Symbol.Name)
	if err != nil {
		if label != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "  %sfind callees: %v\n", label, err)
			return nil
		}
		return fmt.Errorf("find callees: %w", err)
	}
	printCallees(cmd, pick.Symbol.Name, callees)
	return nil
}

// resolveTraceSymbol returns the unique trace candidate matching the
// supplied name and options. When the user supplied no disambiguators
// and the lookup is ambiguous, full-trace mode falls back to the FIRST
// candidate (preserving the pre-disambiguation behaviour) but warns on
// stderr so the user knows other candidates exist. With any explicit
// disambiguator we error out as usual.
func resolveTraceSymbol(
	cmd *cobra.Command,
	tracer *trace.Tracer,
	label, symbolName string,
	opts trace.SelectOptions,
) (*trace.Candidate, error) {
	ctx := context.Background()
	cands, err := tracer.FindCandidates(ctx, symbolName, opts)
	if err != nil {
		if label != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "  %sfind candidates: %v\n", label, err)
			return nil, nil
		}
		return nil, fmt.Errorf("find candidates: %w", err)
	}

	if len(cands) == 0 {
		// With explicit filters this is almost always user error
		// (typo'd language, wrong file path) and silently producing
		// only references would mask it. Surface as a hard error so
		// the user sees what filters were applied.
		if !opts.IsZero() {
			return nil, fmt.Errorf(
				"symbol %q not found with the supplied filters "+
					"(file=%q, language=%q, parent=%q); try broadening the search",
				symbolName, opts.File, opts.Language, opts.Parent)
		}
		// No filters: bare-name lookup that returns no definition.
		// References and callers may still be useful so we let the
		// rest of the pipeline run with pick=nil.
		return nil, nil
	}
	if len(cands) == 1 {
		return &cands[0], nil
	}

	// Multiple matches. With explicit filters we follow the strict
	// rule used by `saras cfg`: print the candidate list and exit
	// non-zero. Without filters we keep the legacy ergonomics — pick
	// the first match and emit a warning so the user can re-run with
	// --file / --language / --parent for precision.
	if !opts.IsZero() {
		amb := &trace.AmbiguousSymbolError{
			Subject:    "symbol",
			Name:       symbolName,
			Candidates: cands,
		}
		return nil, printAmbiguous(cmd, amb)
	}
	fmt.Fprintf(cmd.ErrOrStderr(),
		"warning: %d definitions of %q exist; tracing the first match (%s).\n"+
			"  Use --file / --language / --parent (or path:symbol) to pick a specific one.\n",
		len(cands), symbolName, cands[0].String())
	return &cands[0], nil
}

func printRefs(cmd *cobra.Command, name string, refs []trace.Reference) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "References to %q (%d):\n", name, len(refs))
	for _, r := range refs {
		fmt.Fprintf(out, "  %s:%d  %s\n", r.FilePath, r.Line, truncate(r.Context, 80))
	}
}

func printCallers(cmd *cobra.Command, name string, callers []trace.CallEdge) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Callers of %q (%d):\n", name, len(callers))
	for _, c := range callers {
		fmt.Fprintf(out, "  %s (%s:%d)\n", c.Caller, c.CallerFile, c.CallerLine)
	}
}

func printCallees(cmd *cobra.Command, name string, callees []trace.CallEdge) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Callees of %q (%d):\n", name, len(callees))
	for _, c := range callees {
		fmt.Fprintf(out, "  %s\n", c.Callee)
	}
}

func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
