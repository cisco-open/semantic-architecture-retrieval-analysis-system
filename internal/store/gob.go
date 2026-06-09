/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package store

import (
	"context"
	"encoding/gob"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// GobStore is a file-backed vector store that serializes to Go's gob format.
// It keeps everything in memory for fast search and persists to disk on demand.
type GobStore struct {
	mu        sync.RWMutex
	chunks    map[string]Chunk    // key: chunk ID
	documents map[string]Document // key: file path
	filePath  string              // path to the .gob file on disk
}

// gobSnapshot is the serializable state of a GobStore.
type gobSnapshot struct {
	Chunks    map[string]Chunk
	Documents map[string]Document
}

// NewGobStore creates a new gob-backed store that will persist to filePath.
func NewGobStore(filePath string) *GobStore {
	return &GobStore{
		chunks:    make(map[string]Chunk),
		documents: make(map[string]Document),
		filePath:  filePath,
	}
}

func (s *GobStore) SaveChunks(ctx context.Context, chunks []Chunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for i := range chunks {
		if chunks[i].UpdatedAt.IsZero() {
			chunks[i].UpdatedAt = now
		}
		s.chunks[chunks[i].ID] = chunks[i]
	}
	return nil
}

func (s *GobStore) DeleteByFile(ctx context.Context, filePath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, c := range s.chunks {
		if c.FilePath == filePath {
			delete(s.chunks, id)
		}
	}
	return nil
}

func (s *GobStore) Search(ctx context.Context, queryVector []float32, limit int, opts SearchOptions) ([]SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type scored struct {
		chunk Chunk
		score float32
	}
	var results []scored

	for _, c := range s.chunks {
		if opts.PathPrefix != "" && !strings.HasPrefix(c.FilePath, opts.PathPrefix) {
			continue
		}
		if len(c.Vector) == 0 {
			continue
		}
		score := cosineSimilarity(queryVector, c.Vector)
		results = append(results, scored{chunk: c, score: score})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if len(results) > limit {
		results = results[:limit]
	}

	out := make([]SearchResult, len(results))
	for i, r := range results {
		out[i] = SearchResult{Chunk: r.chunk, Score: r.score}
	}
	return out, nil
}

func (s *GobStore) GetDocument(ctx context.Context, filePath string) (*Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	doc, ok := s.documents[filePath]
	if !ok {
		return nil, nil
	}
	return &doc, nil
}

func (s *GobStore) SaveDocument(ctx context.Context, doc Document) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.documents[doc.Path] = doc
	return nil
}

func (s *GobStore) DeleteDocument(ctx context.Context, filePath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.documents, filePath)
	return nil
}

func (s *GobStore) ListDocuments(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	paths := make([]string, 0, len(s.documents))
	for p := range s.documents {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}

func (s *GobStore) Load(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // fresh store
		}
		return fmt.Errorf("open store file: %w", err)
	}
	defer f.Close()

	var snap gobSnapshot
	if err := gob.NewDecoder(f).Decode(&snap); err != nil {
		return fmt.Errorf("decode store: %w", err)
	}
	if snap.Chunks != nil {
		s.chunks = snap.Chunks
	}
	if snap.Documents != nil {
		s.documents = snap.Documents
	}
	return nil
}

func (s *GobStore) Persist(ctx context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	f, err := os.Create(s.filePath)
	if err != nil {
		return fmt.Errorf("create store file: %w", err)
	}
	defer f.Close()

	snap := gobSnapshot{
		Chunks:    s.chunks,
		Documents: s.documents,
	}
	if err := gob.NewEncoder(f).Encode(snap); err != nil {
		return fmt.Errorf("encode store: %w", err)
	}
	return nil
}

func (s *GobStore) Close() error {
	return nil
}

func (s *GobStore) GetStats(ctx context.Context) (*IndexStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var lastUpdated time.Time
	for _, c := range s.chunks {
		if c.UpdatedAt.After(lastUpdated) {
			lastUpdated = c.UpdatedAt
		}
	}

	// Estimate index size from gob file
	var indexSize int64
	if info, err := os.Stat(s.filePath); err == nil {
		indexSize = info.Size()
	}

	return &IndexStats{
		TotalFiles:  len(s.documents),
		TotalChunks: len(s.chunks),
		IndexSize:   indexSize,
		LastUpdated: lastUpdated,
	}, nil
}

func (s *GobStore) ListFilesWithStats(ctx context.Context) ([]FileStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Count chunks per file
	chunkCounts := make(map[string]int)
	for _, c := range s.chunks {
		chunkCounts[c.FilePath]++
	}

	var result []FileStats
	for path, doc := range s.documents {
		result = append(result, FileStats{
			Path:       path,
			ChunkCount: chunkCounts[path],
			ModTime:    doc.ModTime,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Path < result[j].Path
	})
	return result, nil
}

func (s *GobStore) GetChunksForFile(ctx context.Context, filePath string) ([]Chunk, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []Chunk
	for _, c := range s.chunks {
		if c.FilePath == filePath {
			result = append(result, c)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].StartLine < result[j].StartLine
	})
	return result, nil
}

func (s *GobStore) GetAllChunks(ctx context.Context) ([]Chunk, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Chunk, 0, len(s.chunks))
	for _, c := range s.chunks {
		result = append(result, c)
	}
	return result, nil
}

// LookupByContentHash implements EmbeddingCache for content-addressed deduplication.
func (s *GobStore) LookupByContentHash(ctx context.Context, contentHash string) ([]float32, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.chunks {
		if c.ContentHash == contentHash && len(c.Vector) > 0 {
			return c.Vector, true, nil
		}
	}
	return nil, false, nil
}

// cosineSimilarity computes the cosine similarity between two vectors.
// Returns 0 if either vector is zero-length or they have mismatched dimensions.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return float32(dot / denom)
}
