/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package search

import (
	"context"
	"math"
	"sort"
	"strings"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/embedder"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/store"
)

// Result represents a ranked search result.
type Result struct {
	FilePath  string  `json:"file_path"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Content   string  `json:"content"`
	Score     float32 `json:"score"`
	DepName   string  `json:"dep_name,omitempty"`
	DepRole   string  `json:"dep_role,omitempty"`
}

// Searcher performs hybrid semantic + text search over the vector store.
type Searcher struct {
	store    store.VectorStore
	embedder embedder.Embedder
	cfg      config.SearchConfig
}

// NewSearcher creates a searcher with the given dependencies.
func NewSearcher(st store.VectorStore, emb embedder.Embedder, cfg config.SearchConfig) *Searcher {
	return &Searcher{
		store:    st,
		embedder: emb,
		cfg:      cfg,
	}
}

// Search performs a semantic search, optionally combined with text search and boost scoring.
func (s *Searcher) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if limit <= 0 {
		limit = 20
	}

	// Phase 1: Vector search
	queryVec, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}

	// Request more results than needed for post-processing
	fetchLimit := limit * 3
	if fetchLimit < 20 {
		fetchLimit = 20
	}

	vectorResults, err := s.store.Search(ctx, queryVec, fetchLimit, store.SearchOptions{})
	if err != nil {
		return nil, err
	}

	// Phase 2: Text search (if hybrid enabled)
	var textResults []store.SearchResult
	if s.cfg.Hybrid.Enabled {
		textResults, err = s.textSearch(ctx, query, fetchLimit)
		if err != nil {
			return nil, err
		}
	}

	// Phase 3: Fuse results
	var merged []Result
	if s.cfg.Hybrid.Enabled && len(textResults) > 0 {
		merged = s.reciprocalRankFusion(vectorResults, textResults)
	} else {
		merged = storeResultsToResults(vectorResults)
	}

	// Phase 4: Apply boost scoring
	if s.cfg.Boost.Enabled {
		merged = s.applyBoost(merged)
	}

	// Phase 5: Dedup by file (optional)
	if s.cfg.Dedup.Enabled {
		merged = s.dedup(merged)
	}

	// Phase 6: Sort by final score and truncate
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Score > merged[j].Score
	})

	if len(merged) > limit {
		merged = merged[:limit]
	}

	return merged, nil
}

// textSearch performs a simple substring match across all chunks.
func (s *Searcher) textSearch(ctx context.Context, query string, limit int) ([]store.SearchResult, error) {
	allChunks, err := s.store.GetAllChunks(ctx)
	if err != nil {
		return nil, err
	}

	queryLower := strings.ToLower(query)
	queryTerms := strings.Fields(queryLower)

	type scored struct {
		chunk store.Chunk
		score float32
	}
	var results []scored

	for _, c := range allChunks {
		contentLower := strings.ToLower(c.Content)
		pathLower := strings.ToLower(c.FilePath)

		var totalScore float32

		for _, term := range queryTerms {
			// Count occurrences in content
			contentCount := strings.Count(contentLower, term)
			// Count occurrences in path
			pathCount := strings.Count(pathLower, term)

			if contentCount > 0 || pathCount > 0 {
				// TF-like score: more occurrences = higher relevance, with diminishing returns
				termScore := float32(math.Log1p(float64(contentCount))) + float32(pathCount)*2.0
				totalScore += termScore
			}
		}

		if totalScore > 0 {
			results = append(results, scored{chunk: c, score: totalScore})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if len(results) > limit {
		results = results[:limit]
	}

	out := make([]store.SearchResult, len(results))
	for i, r := range results {
		out[i] = store.SearchResult{Chunk: r.chunk, Score: r.score}
	}
	return out, nil
}

// reciprocalRankFusion merges vector and text results using RRF.
// Formula: RRF(d) = Σ 1/(k + rank_i(d))
func (s *Searcher) reciprocalRankFusion(vectorResults, textResults []store.SearchResult) []Result {
	k := float32(60)
	if s.cfg.Hybrid.K > 0 {
		k = s.cfg.Hybrid.K
	}

	// Map chunk ID -> RRF score
	rrfScores := make(map[string]float32)
	chunkMap := make(map[string]store.Chunk)

	for rank, r := range vectorResults {
		id := r.Chunk.ID
		rrfScores[id] += 1.0 / (k + float32(rank+1))
		chunkMap[id] = r.Chunk
	}

	for rank, r := range textResults {
		id := r.Chunk.ID
		rrfScores[id] += 1.0 / (k + float32(rank+1))
		if _, exists := chunkMap[id]; !exists {
			chunkMap[id] = r.Chunk
		}
	}

	var results []Result
	for id, score := range rrfScores {
		c := chunkMap[id]
		results = append(results, Result{
			FilePath:  c.FilePath,
			StartLine: c.StartLine,
			EndLine:   c.EndLine,
			Content:   c.Content,
			Score:     score,
		})
	}

	return results
}

// applyBoost adjusts scores based on file path pattern matching.
func (s *Searcher) applyBoost(results []Result) []Result {
	for i := range results {
		path := results[i].FilePath

		// Apply penalties
		for _, rule := range s.cfg.Boost.Penalties {
			if strings.Contains(path, rule.Pattern) {
				results[i].Score *= rule.Factor
			}
		}

		// Apply bonuses
		for _, rule := range s.cfg.Boost.Bonuses {
			if strings.Contains(path, rule.Pattern) {
				results[i].Score *= rule.Factor
			}
		}
	}
	return results
}

// dedup keeps only the highest-scoring chunk per file.
func (s *Searcher) dedup(results []Result) []Result {
	seen := make(map[string]int) // file path -> index in output
	var deduped []Result

	// Sort by score descending first to keep best per file
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	for _, r := range results {
		if _, exists := seen[r.FilePath]; !exists {
			seen[r.FilePath] = len(deduped)
			deduped = append(deduped, r)
		}
	}
	return deduped
}

// storeResultsToResults converts store.SearchResult to search.Result.
func storeResultsToResults(srs []store.SearchResult) []Result {
	results := make([]Result, len(srs))
	for i, sr := range srs {
		results[i] = Result{
			FilePath:  sr.Chunk.FilePath,
			StartLine: sr.Chunk.StartLine,
			EndLine:   sr.Chunk.EndLine,
			Content:   sr.Chunk.Content,
			Score:     sr.Score,
		}
	}
	return results
}
