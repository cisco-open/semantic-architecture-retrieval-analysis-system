/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/embedder"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/search"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/store"
	"github.com/spf13/cobra"
)

// addDepFlags registers --from-dep, --all-deps, and --with-deps on a command.
func addDepFlags(cmd *cobra.Command) {
	cmd.Flags().String("from-dep", "", "Query ONLY this dependency (by name)")
	cmd.Flags().Bool("all-deps", false, "Query all dependencies (excludes current repo)")
	cmd.Flags().Bool("with-deps", false, "Query current repo AND all dependencies")
}

// parseDepFlags reads the three dep flags from a command.
func parseDepFlags(cmd *cobra.Command) (fromDep string, allDeps, withDeps bool) {
	fromDep, _ = cmd.Flags().GetString("from-dep")
	allDeps, _ = cmd.Flags().GetBool("all-deps")
	withDeps, _ = cmd.Flags().GetBool("with-deps")
	return
}

// searchWithDeps performs search across dependencies, returning labeled results.
// If includeCurrent is true, currentResults are included with empty dep labels.
func searchWithDeps(ctx context.Context, cfg *config.Config, emb embedder.Embedder,
	deps []config.Dependency, includeCurrent bool, currentResults []search.Result,
	query string, limit int) ([]search.Result, error) {

	var allResults []search.Result

	// Include current repo results if requested
	if includeCurrent {
		for i := range currentResults {
			currentResults[i].DepName = ""
			currentResults[i].DepRole = ""
		}
		allResults = append(allResults, currentResults...)
	}

	// Search each dependency
	for _, dep := range deps {
		depStorePath := filepath.Join(config.GetConfigDir(dep.Path), "index.gob")
		depSt := store.NewGobStore(depStorePath)
		if err := depSt.Load(ctx); err != nil {
			// Skip deps that can't be loaded
			continue
		}

		depSearcher := search.NewSearcher(depSt, emb, cfg.Search)
		depResults, err := depSearcher.Search(ctx, query, limit)
		depSt.Close()
		if err != nil {
			continue
		}

		// Label results with dep info
		for i := range depResults {
			depResults[i].DepName = dep.Name
			depResults[i].DepRole = dep.Role
		}
		allResults = append(allResults, depResults...)
	}

	// Sort by score descending and cap at limit
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Score > allResults[j].Score
	})
	if len(allResults) > limit {
		allResults = allResults[:limit]
	}

	return allResults, nil
}

// formatResultLabel returns a label prefix for a search result.
// Empty string for current repo results, "[role: name]" for dep results.
func formatResultLabel(r search.Result) string {
	if r.DepName == "" {
		return ""
	}
	return fmt.Sprintf("[%s: %s] ", r.DepRole, r.DepName)
}
