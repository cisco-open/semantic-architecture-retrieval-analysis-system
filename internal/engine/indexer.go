/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/embedder"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/lang"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/store"
)

// Indexer orchestrates scanning, chunking, embedding, and storing code.
type Indexer struct {
	root     string
	store    store.VectorStore
	embedder embedder.Embedder
	chunker  *Chunker
	scanner  *Scanner
}

// IndexStats contains statistics from an indexing run.
type IndexStats struct {
	FilesIndexed  int
	FilesSkipped  int
	ChunksCreated int
	FilesRemoved  int
	Duration      time.Duration
}

// ProgressInfo reports progress during indexing.
type ProgressInfo struct {
	Current     int
	Total       int
	CurrentFile string
}

// ProgressCallback is called for each file during indexing.
type ProgressCallback func(info ProgressInfo)

// NewIndexer creates a new indexer with all required components.
func NewIndexer(root string, st store.VectorStore, emb embedder.Embedder, chunker *Chunker, scanner *Scanner) *Indexer {
	return &Indexer{
		root:     root,
		store:    st,
		embedder: emb,
		chunker:  chunker,
		scanner:  scanner,
	}
}

// IndexAll performs a full index of the project.
func (idx *Indexer) IndexAll(ctx context.Context) (*IndexStats, error) {
	return idx.IndexAllWithProgress(ctx, nil)
}

// IndexAllWithProgress performs a full index with progress reporting.
func (idx *Indexer) IndexAllWithProgress(ctx context.Context, onProgress ProgressCallback) (*IndexStats, error) {
	start := time.Now()
	stats := &IndexStats{}

	files, err := idx.scanner.ScanAll()
	if err != nil {
		return nil, fmt.Errorf("scan files: %w", err)
	}

	// Build set of current files for removal detection
	currentFiles := make(map[string]bool)
	for _, f := range files {
		currentFiles[f.Path] = true
	}

	// Remove documents that no longer exist on disk
	existingDocs, err := idx.store.ListDocuments(ctx)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	for _, docPath := range existingDocs {
		if !currentFiles[docPath] {
			if err := idx.store.DeleteByFile(ctx, docPath); err != nil {
				return nil, fmt.Errorf("delete chunks for %s: %w", docPath, err)
			}
			if err := idx.store.DeleteDocument(ctx, docPath); err != nil {
				return nil, fmt.Errorf("delete document %s: %w", docPath, err)
			}
			stats.FilesRemoved++
		}
	}

	// Index each file
	for i, f := range files {
		if ctx.Err() != nil {
			return stats, ctx.Err()
		}

		if onProgress != nil {
			onProgress(ProgressInfo{
				Current:     i + 1,
				Total:       len(files),
				CurrentFile: f.Path,
			})
		}

		indexed, err := idx.indexFile(ctx, f)
		if err != nil {
			// Log but continue — don't fail the whole index for one file
			stats.FilesSkipped++
			continue
		}
		if indexed {
			stats.FilesIndexed++
		} else {
			stats.FilesSkipped++
		}
	}

	stats.Duration = time.Since(start)

	// Count total chunks
	allChunks, err := idx.store.GetAllChunks(ctx)
	if err == nil {
		stats.ChunksCreated = len(allChunks)
	}

	return stats, nil
}

// IndexFile indexes a single file. Returns true if the file was (re)indexed.
func (idx *Indexer) IndexFile(ctx context.Context, filePath string) (bool, error) {
	absPath := filePath
	if !isAbsPath(filePath) {
		absPath = fmt.Sprintf("%s/%s", idx.root, filePath)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return false, fmt.Errorf("stat file: %w", err)
	}

	// Skip directories and non-text files (e.g. images, SVGs)
	if info.IsDir() {
		return false, nil
	}
	if !isTextFile(info.Name()) {
		return false, nil
	}

	fm := FileMeta{
		Path:    filePath,
		AbsPath: absPath,
		ModTime: info.ModTime(),
		Size:    info.Size(),
	}

	return idx.indexFile(ctx, fm)
}

// RemoveFile removes a file from the index.
func (idx *Indexer) RemoveFile(ctx context.Context, filePath string) error {
	if err := idx.store.DeleteByFile(ctx, filePath); err != nil {
		return fmt.Errorf("delete chunks: %w", err)
	}
	if err := idx.store.DeleteDocument(ctx, filePath); err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	return nil
}

func (idx *Indexer) indexFile(ctx context.Context, f FileMeta) (bool, error) {
	// Check if file has changed since last index
	existing, err := idx.store.GetDocument(ctx, f.Path)
	if err != nil {
		return false, err
	}

	fileHash := computeFileHash(f.AbsPath)
	if existing != nil && existing.Hash == fileHash {
		return false, nil // unchanged
	}

	// Read file content
	content, err := os.ReadFile(f.AbsPath)
	if err != nil {
		return false, fmt.Errorf("read file: %w", err)
	}

	// Chunk the content — use symbol-aware chunking when a language parser is available
	var chunks []ChunkInfo
	if parser := lang.ParserForFile(f.Path); parser != nil {
		langSymbols := parser.ExtractSymbols(string(content))
		if len(langSymbols) > 0 {
			boundaries := make([]SymbolBoundary, 0, len(langSymbols))
			for _, s := range langSymbols {
				boundaries = append(boundaries, SymbolBoundary{
					Name:      s.Name,
					Kind:      s.Kind.String(),
					StartLine: s.StartLine,
					EndLine:   s.EndLine,
				})
			}
			chunks = idx.chunker.ChunkBySymbols(f.Path, string(content), boundaries)
		}
	}
	if len(chunks) == 0 {
		chunks = idx.chunker.Chunk(f.Path, string(content))
	}
	if len(chunks) == 0 {
		return false, nil
	}

	// Embed each chunk
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.EmbedContent
	}

	// Try content-hash cache first
	vectors := make([][]float32, len(chunks))
	var uncachedIndices []int
	var uncachedTexts []string

	if cache, ok := idx.store.(store.EmbeddingCache); ok {
		for i, c := range chunks {
			if c.ContentHash != "" {
				vec, found, err := cache.LookupByContentHash(ctx, c.ContentHash)
				if err == nil && found {
					vectors[i] = vec
					continue
				}
			}
			uncachedIndices = append(uncachedIndices, i)
			uncachedTexts = append(uncachedTexts, texts[i])
		}
	} else {
		for i := range chunks {
			uncachedIndices = append(uncachedIndices, i)
			uncachedTexts = append(uncachedTexts, texts[i])
		}
	}

	// Embed uncached chunks in batches to stay within provider API limits.
	if len(uncachedTexts) > 0 {
		const maxBatchSize = 200
		allEmbeddings := make([][]float32, len(uncachedTexts))
		for start := 0; start < len(uncachedTexts); start += maxBatchSize {
			end := start + maxBatchSize
			if end > len(uncachedTexts) {
				end = len(uncachedTexts)
			}
			batch, err := idx.embedder.EmbedBatch(ctx, uncachedTexts[start:end])
			if err != nil {
				return false, fmt.Errorf("embed chunks: %w", err)
			}
			copy(allEmbeddings[start:end], batch)
		}
		for j, idx := range uncachedIndices {
			vectors[idx] = allEmbeddings[j]
		}
	}

	// Delete old chunks for this file
	if err := idx.store.DeleteByFile(ctx, f.Path); err != nil {
		return false, fmt.Errorf("delete old chunks: %w", err)
	}

	// Build store chunks
	storeChunks := make([]store.Chunk, len(chunks))
	chunkIDs := make([]string, len(chunks))
	for i, c := range chunks {
		storeChunks[i] = store.Chunk{
			ID:          c.ID,
			FilePath:    c.FilePath,
			StartLine:   c.StartLine,
			EndLine:     c.EndLine,
			Content:     c.Content,
			Vector:      vectors[i],
			Hash:        c.Hash,
			ContentHash: c.ContentHash,
			UpdatedAt:   time.Now(),
		}
		chunkIDs[i] = c.ID
	}

	if err := idx.store.SaveChunks(ctx, storeChunks); err != nil {
		return false, fmt.Errorf("save chunks: %w", err)
	}

	// Save document metadata
	doc := store.Document{
		Path:     f.Path,
		Hash:     fileHash,
		ModTime:  f.ModTime,
		ChunkIDs: chunkIDs,
	}
	if err := idx.store.SaveDocument(ctx, doc); err != nil {
		return false, fmt.Errorf("save document: %w", err)
	}

	return true, nil
}

func computeFileHash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func isAbsPath(p string) bool {
	return len(p) > 0 && p[0] == '/'
}
