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
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/embedder"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/engine"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/store"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/tui"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(watchCmd)
}

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Watch for file changes and re-index automatically",
	Long: `Start a file system watcher that monitors your project for changes
and automatically re-indexes modified files. Displays a live dashboard
showing watcher activity and indexing statistics.

Press 'q' to stop watching.`,
	RunE: runWatch,
}

func init() {
	watchCmd.Flags().Bool("no-tui", false, "Log events to stdout (no interactive TUI)")
	watchCmd.Flags().Bool("index-first", true, "Run a full index before starting the watcher")
	watchCmd.Flags().Bool("index-only", false, "Run a full index and exit (no watcher)")
}

// watchTracker tracks cumulative stats and sends them to a Bubble Tea program.
type watchTracker struct {
	mu           sync.Mutex
	filesIndexed int
	chunksTotal  int
	eventsCount  int
	dirsWatched  int
	errors       int
	prog         *tea.Program
}

func (t *watchTracker) sendStats() {
	t.mu.Lock()
	msg := tui.WatchStatsMsg{
		DirsWatched:    t.dirsWatched,
		EventsReceived: t.eventsCount,
		FilesIndexed:   t.filesIndexed,
		ChunksTotal:    t.chunksTotal,
		Errors:         t.errors,
	}
	t.mu.Unlock()
	if t.prog != nil {
		t.prog.Send(msg)
	}
}

func runWatch(cmd *cobra.Command, args []string) error {
	noTUI, _ := cmd.Flags().GetBool("no-tui")
	indexFirst, _ := cmd.Flags().GetBool("index-first")
	indexOnly, _ := cmd.Flags().GetBool("index-only")

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

	// Index-only mode: run full index then exit
	if indexOnly {
		fmt.Fprintln(cmd.OutOrStdout(), "Running full index...")
		start := time.Now()
		stats, err := indexer.IndexAllWithProgress(ctx, func(p engine.ProgressInfo) {
			fmt.Fprintf(cmd.OutOrStdout(), "\r  [%d/%d] %s", p.Current, p.Total, p.CurrentFile)
		})
		if err != nil {
			return fmt.Errorf("index: %w", err)
		}
		st.Persist(ctx)
		fmt.Fprintf(cmd.OutOrStdout(), "\n✓ Indexed %d files, %d chunks in %s\n", stats.FilesIndexed, stats.ChunksCreated, time.Since(start).Round(time.Millisecond))
		return nil
	}

	tracker := &watchTracker{}

	if noTUI {
		// Non-TUI mode
		watcher, err := engine.NewWatcher(projectRoot, cfg.Ignore,
			engine.WithDebounce(cfg.Watch.DebounceMs),
			engine.WithOnEvent(func(e engine.WatchEvent) {
				handleWatchEvent(ctx, e, indexer, st, cmd)
			}),
			engine.WithOnError(func(err error) {
				fmt.Fprintf(cmd.ErrOrStderr(), "watcher error: %v\n", err)
			}),
		)
		if err != nil {
			return fmt.Errorf("create watcher: %w", err)
		}

		// Start watcher first so file changes are detected immediately
		watcherErrCh := make(chan error, 1)
		go func() {
			watcherErrCh <- watcher.Start(ctx)
		}()
		<-watcher.Ready()
		fmt.Fprintln(cmd.OutOrStdout(), "Watching for changes... (Ctrl+C to stop)")

		if indexFirst {
			fmt.Fprintln(cmd.OutOrStdout(), "Running initial index...")
			stats, err := indexer.IndexAll(ctx)
			if err != nil {
				return fmt.Errorf("initial index: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Indexed %d files, %d chunks\n", stats.FilesIndexed, stats.ChunksCreated)
			st.Persist(ctx)
		}

		return <-watcherErrCh
	}

	// TUI mode: create the program first so callbacks can send messages
	model := tui.NewWatchModel()
	p := tea.NewProgram(model, tea.WithAltScreen())
	tracker.prog = p

	// Create watcher with TUI-aware callbacks
	watcher, err := engine.NewWatcher(projectRoot, cfg.Ignore,
		engine.WithDebounce(cfg.Watch.DebounceMs),
		engine.WithOnEvent(func(e engine.WatchEvent) {
			handleWatchEvent(ctx, e, indexer, st, cmd)

			// Send event to TUI
			p.Send(tui.WatchEventMsg{
				Path:      e.Path,
				Op:        e.Op.String(),
				Timestamp: e.Timestamp,
			})

			// Update and send stats
			tracker.mu.Lock()
			tracker.eventsCount++
			tracker.mu.Unlock()

			// Recount from store for accuracy
			if allChunks, err := st.GetAllChunks(ctx); err == nil {
				tracker.mu.Lock()
				tracker.chunksTotal = len(allChunks)
				tracker.mu.Unlock()
			}
			if docs, err := st.ListDocuments(ctx); err == nil {
				tracker.mu.Lock()
				tracker.filesIndexed = len(docs)
				tracker.mu.Unlock()
			}
			tracker.sendStats()
		}),
		engine.WithOnError(func(err error) {
			tracker.mu.Lock()
			tracker.errors++
			tracker.mu.Unlock()
			tracker.sendStats()
		}),
	)
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}

	// Start watcher first so file changes are detected immediately
	go func() {
		watcher.Start(ctx)
	}()

	// Wait for directory registration, then send initial stats and index
	go func() {
		<-watcher.Ready()

		ws := watcher.Stats()
		tracker.mu.Lock()
		tracker.dirsWatched = ws.DirsWatched
		tracker.mu.Unlock()
		tracker.sendStats()

		if indexFirst {
			stats, err := indexer.IndexAll(ctx)
			if err == nil {
				tracker.mu.Lock()
				tracker.filesIndexed = stats.FilesIndexed
				tracker.chunksTotal = stats.ChunksCreated
				tracker.mu.Unlock()
				st.Persist(ctx)
				tracker.sendStats()
			}
		}
	}()

	if _, err := p.Run(); err != nil {
		cancel()
		return fmt.Errorf("TUI error: %w", err)
	}

	cancel()
	return nil
}

func handleWatchEvent(ctx context.Context, e engine.WatchEvent, indexer *engine.Indexer, st *store.GobStore, cmd *cobra.Command) {
	switch e.Op {
	case engine.OpCreate, engine.OpWrite:
		if _, err := indexer.IndexFile(ctx, e.Path); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "index error for %s: %v\n", e.Path, err)
		}
	case engine.OpRemove, engine.OpRename:
		if err := indexer.RemoveFile(ctx, e.Path); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "remove error for %s: %v\n", e.Path, err)
		}
	}

	// Persist after each event batch
	st.Persist(ctx)
}
