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
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/embedder"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/engine"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/store"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(reindexCmd)
}

var reindexCmd = &cobra.Command{
	Use:   "reindex",
	Short: "Re-index the entire project",
	Long: `Perform a full re-index of the project. Scans all supported files,
removes stale entries, and re-embeds every file. Shows progress as it works.

This is equivalent to 'saras watch --index-only' but more convenient.

Examples:
  saras reindex`,
	RunE: runReindex,
}

func runReindex(cmd *cobra.Command, args []string) error {
	projectRoot, err := config.FindProjectRoot()
	if err != nil {
		return fmt.Errorf("not a saras project (run 'saras init' first): %w", err)
	}

	cfg, err := config.Load(projectRoot)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Open store
	storePath := filepath.Join(config.GetConfigDir(projectRoot), "index.gob")
	st := store.NewGobStore(storePath)
	if err := st.Load(context.Background()); err != nil {
		_ = err // fresh store is fine
	}
	defer func() {
		st.Persist(context.Background())
		st.Close()
	}()

	// Create embedder
	emb, err := embedder.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("create embedder: %w", err)
	}
	defer emb.Close()

	// Create indexer
	scanner := engine.NewScanner(projectRoot, cfg.Ignore)
	chunker := engine.NewChunker(cfg.Chunking.Size, cfg.Chunking.Overlap)
	indexer := engine.NewIndexer(projectRoot, st, emb, chunker, scanner)

	// Signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Re-indexing project...")
	start := time.Now()

	stats, err := indexer.IndexAllWithProgress(ctx, func(p engine.ProgressInfo) {
		fmt.Fprintf(out, "\r  [%d/%d] %s", p.Current, p.Total, p.CurrentFile)
	})
	if err != nil {
		return fmt.Errorf("reindex: %w", err)
	}

	st.Persist(ctx)
	fmt.Fprintf(out, "\n✓ Indexed %d files, %d chunks in %s\n",
		stats.FilesIndexed, stats.ChunksCreated, time.Since(start).Round(time.Millisecond))

	return nil
}
