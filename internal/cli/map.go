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

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/architect"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(mapCmd)
}

var mapCmd = &cobra.Command{
	Use:   "map",
	Short: "Generate a codebase architecture map",
	Long: `Generate a high-level map of your codebase showing packages, types,
functions, dependencies, and project structure.

Output formats:
  - tree: directory tree (default)
  - markdown: detailed markdown report
  - summary: compact overview

Examples:
  saras map
  saras map --format markdown
  saras map --format markdown --output ARCHITECTURE.md`,
	RunE: runMap,
}

func init() {
	mapCmd.Flags().StringP("format", "f", "tree", "Output format: tree, markdown, summary")
	mapCmd.Flags().StringP("output", "o", "", "Write output to file instead of stdout")
	addDepFlags(mapCmd)
}

func runMap(cmd *cobra.Command, args []string) error {
	format, _ := cmd.Flags().GetString("format")
	outputFile, _ := cmd.Flags().GetString("output")
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

	// Build list of (label, mapper) pairs
	type labeledMapper struct {
		label  string
		mapper *architect.Mapper
	}
	var mappers []labeledMapper

	if deps != nil {
		if includeCurrent {
			mappers = append(mappers, labeledMapper{"", architect.NewMapper(projectRoot, cfg.Ignore)})
		}
		for _, dep := range deps {
			depCfg, err := config.Load(dep.Path)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: skipping dep %s: %v\n", dep.Name, err)
				continue
			}
			label := fmt.Sprintf("[%s: %s]", dep.Role, dep.Name)
			mappers = append(mappers, labeledMapper{label, architect.NewMapper(dep.Path, depCfg.Ignore)})
		}
	} else {
		mappers = append(mappers, labeledMapper{"", architect.NewMapper(projectRoot, cfg.Ignore)})
	}

	ctx := context.Background()
	var combined string

	for _, lm := range mappers {
		var output string

		switch format {
		case "tree":
			output, err = lm.mapper.GenerateTree(ctx)
			if err != nil {
				if lm.label != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "  %s generate tree: %v\n", lm.label, err)
					continue
				}
				return fmt.Errorf("generate tree: %w", err)
			}

		case "markdown", "md":
			output, err = lm.mapper.GenerateMarkdown(ctx)
			if err != nil {
				if lm.label != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "  %s generate markdown: %v\n", lm.label, err)
					continue
				}
				return fmt.Errorf("generate markdown: %w", err)
			}

		case "summary":
			cmap, mapErr := lm.mapper.GenerateMap(ctx)
			if mapErr != nil {
				if lm.label != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "  %s generate map: %v\n", lm.label, mapErr)
					continue
				}
				return fmt.Errorf("generate map: %w", mapErr)
			}
			output = formatSummary(cmap)

		default:
			return fmt.Errorf("unknown format %q (use: tree, markdown, summary)", format)
		}

		if lm.label != "" {
			combined += fmt.Sprintf("\n## %s\n\n%s", lm.label, output)
		} else {
			combined += output
		}
	}

	if outputFile != "" {
		if err := os.WriteFile(outputFile, []byte(combined), 0644); err != nil {
			return fmt.Errorf("write file: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Written to %s\n", outputFile)
		return nil
	}

	fmt.Fprint(cmd.OutOrStdout(), combined)
	return nil
}

func formatSummary(cmap *architect.CodebaseMap) string {
	var s string
	s += fmt.Sprintf("Project: %s\n", cmap.ProjectRoot)
	s += fmt.Sprintf("Files:   %d\n", cmap.TotalFiles)
	s += fmt.Sprintf("Lines:   %d\n", cmap.TotalLines)
	s += fmt.Sprintf("Packages: %d\n\n", len(cmap.Packages))

	for _, pkg := range cmap.Packages {
		s += fmt.Sprintf("  %-30s %d files, %d funcs, %d types, %d ifaces\n",
			pkg.Path, len(pkg.Files), pkg.Functions, pkg.Types, pkg.Interfaces)
	}

	if len(cmap.Dependencies) > 0 {
		s += fmt.Sprintf("\nDependencies: %d\n", len(cmap.Dependencies))
		for _, d := range cmap.Dependencies {
			s += fmt.Sprintf("  %s → %s\n", d.From, d.To)
		}
	}

	return s
}
