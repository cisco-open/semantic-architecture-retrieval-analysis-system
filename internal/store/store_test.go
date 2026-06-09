/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package store

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []float32
		expected float32
		epsilon  float32
	}{
		{"identical", []float32{1, 0, 0}, []float32{1, 0, 0}, 1.0, 0.001},
		{"orthogonal", []float32{1, 0, 0}, []float32{0, 1, 0}, 0.0, 0.001},
		{"opposite", []float32{1, 0, 0}, []float32{-1, 0, 0}, -1.0, 0.001},
		{"similar", []float32{1, 1, 0}, []float32{1, 0, 0}, 0.7071, 0.001},
		{"empty", []float32{}, []float32{}, 0, 0.001},
		{"mismatched", []float32{1, 2}, []float32{1, 2, 3}, 0, 0.001},
		{"zero_vector", []float32{0, 0, 0}, []float32{1, 2, 3}, 0, 0.001},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cosineSimilarity(tc.a, tc.b)
			if math.Abs(float64(got-tc.expected)) > float64(tc.epsilon) {
				t.Errorf("expected ~%f, got %f", tc.expected, got)
			}
		})
	}
}

func newTestStore(t *testing.T) *GobStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.gob")
	return NewGobStore(path)
}

func TestSaveAndGetChunks(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	chunks := []Chunk{
		{ID: "f1_0", FilePath: "main.go", StartLine: 1, EndLine: 10, Content: "func main()", Vector: []float32{1, 0, 0}, Hash: "h1", ContentHash: "ch1"},
		{ID: "f1_1", FilePath: "main.go", StartLine: 11, EndLine: 20, Content: "func foo()", Vector: []float32{0, 1, 0}, Hash: "h2", ContentHash: "ch2"},
		{ID: "f2_0", FilePath: "util.go", StartLine: 1, EndLine: 5, Content: "func bar()", Vector: []float32{0, 0, 1}, Hash: "h3", ContentHash: "ch3"},
	}

	if err := s.SaveChunks(ctx, chunks); err != nil {
		t.Fatalf("SaveChunks failed: %v", err)
	}

	got, err := s.GetChunksForFile(ctx, "main.go")
	if err != nil {
		t.Fatalf("GetChunksForFile failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 chunks for main.go, got %d", len(got))
	}
	// Should be sorted by StartLine
	if got[0].StartLine != 1 || got[1].StartLine != 11 {
		t.Errorf("chunks not sorted by start line: %d, %d", got[0].StartLine, got[1].StartLine)
	}

	got2, err := s.GetChunksForFile(ctx, "util.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got2) != 1 {
		t.Errorf("expected 1 chunk for util.go, got %d", len(got2))
	}

	// Non-existent file
	got3, err := s.GetChunksForFile(ctx, "nope.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got3) != 0 {
		t.Errorf("expected 0 chunks for nope.go, got %d", len(got3))
	}
}

func TestSaveChunksSetsUpdatedAt(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	before := time.Now().Add(-time.Second)
	chunks := []Chunk{
		{ID: "c1", FilePath: "a.go", Content: "x", Vector: []float32{1}},
	}
	if err := s.SaveChunks(ctx, chunks); err != nil {
		t.Fatal(err)
	}

	all, _ := s.GetAllChunks(ctx)
	if len(all) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(all))
	}
	if all[0].UpdatedAt.Before(before) {
		t.Error("UpdatedAt should have been set automatically")
	}
}

func TestDeleteByFile(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	chunks := []Chunk{
		{ID: "c1", FilePath: "a.go", Content: "a", Vector: []float32{1}},
		{ID: "c2", FilePath: "a.go", Content: "b", Vector: []float32{0}},
		{ID: "c3", FilePath: "b.go", Content: "c", Vector: []float32{1}},
	}
	s.SaveChunks(ctx, chunks)

	if err := s.DeleteByFile(ctx, "a.go"); err != nil {
		t.Fatalf("DeleteByFile failed: %v", err)
	}

	all, _ := s.GetAllChunks(ctx)
	if len(all) != 1 {
		t.Fatalf("expected 1 chunk after delete, got %d", len(all))
	}
	if all[0].FilePath != "b.go" {
		t.Errorf("wrong file remaining: %s", all[0].FilePath)
	}
}

func TestDeleteByFileNonExistent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	// Should not error on non-existent file
	if err := s.DeleteByFile(ctx, "nope.go"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearch(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	chunks := []Chunk{
		{ID: "c1", FilePath: "a.go", Content: "auth handler", Vector: []float32{1, 0, 0}},
		{ID: "c2", FilePath: "b.go", Content: "database conn", Vector: []float32{0, 1, 0}},
		{ID: "c3", FilePath: "c.go", Content: "auth middleware", Vector: []float32{0.9, 0.1, 0}},
	}
	s.SaveChunks(ctx, chunks)

	results, err := s.Search(ctx, []float32{1, 0, 0}, 2, SearchOptions{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// c1 should be most similar (exact match)
	if results[0].Chunk.ID != "c1" {
		t.Errorf("expected c1 first, got %s", results[0].Chunk.ID)
	}
	if results[0].Score < 0.99 {
		t.Errorf("expected score ~1.0, got %f", results[0].Score)
	}
	// c3 should be second
	if results[1].Chunk.ID != "c3" {
		t.Errorf("expected c3 second, got %s", results[1].Chunk.ID)
	}
}

func TestSearchWithPathPrefix(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	chunks := []Chunk{
		{ID: "c1", FilePath: "src/auth.go", Content: "auth", Vector: []float32{1, 0}},
		{ID: "c2", FilePath: "test/auth_test.go", Content: "test", Vector: []float32{1, 0}},
	}
	s.SaveChunks(ctx, chunks)

	results, err := s.Search(ctx, []float32{1, 0}, 10, SearchOptions{PathPrefix: "src/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result with path prefix, got %d", len(results))
	}
	if results[0].Chunk.FilePath != "src/auth.go" {
		t.Errorf("unexpected file: %s", results[0].Chunk.FilePath)
	}
}

func TestSearchSkipsEmptyVectors(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	chunks := []Chunk{
		{ID: "c1", FilePath: "a.go", Content: "a", Vector: []float32{1, 0}},
		{ID: "c2", FilePath: "b.go", Content: "b", Vector: nil},
		{ID: "c3", FilePath: "c.go", Content: "c", Vector: []float32{}},
	}
	s.SaveChunks(ctx, chunks)

	results, _ := s.Search(ctx, []float32{1, 0}, 10, SearchOptions{})
	if len(results) != 1 {
		t.Errorf("expected 1 result (skip empty vectors), got %d", len(results))
	}
}

func TestSearchLimitRespected(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	chunks := make([]Chunk, 20)
	for i := range chunks {
		chunks[i] = Chunk{
			ID:       fmt.Sprintf("c%d", i),
			FilePath: fmt.Sprintf("f%d.go", i),
			Content:  "content",
			Vector:   []float32{1, 0},
		}
	}
	s.SaveChunks(ctx, chunks)

	results, _ := s.Search(ctx, []float32{1, 0}, 5, SearchOptions{})
	if len(results) != 5 {
		t.Errorf("expected 5 results, got %d", len(results))
	}
}

func TestDocumentCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Save
	doc := Document{Path: "main.go", Hash: "abc123", ModTime: time.Now(), ChunkIDs: []string{"c1", "c2"}}
	if err := s.SaveDocument(ctx, doc); err != nil {
		t.Fatalf("SaveDocument failed: %v", err)
	}

	// Get
	got, err := s.GetDocument(ctx, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected document, got nil")
	}
	if got.Hash != "abc123" {
		t.Errorf("expected hash abc123, got %s", got.Hash)
	}
	if len(got.ChunkIDs) != 2 {
		t.Errorf("expected 2 chunk IDs, got %d", len(got.ChunkIDs))
	}

	// Get non-existent
	got2, err := s.GetDocument(ctx, "nope.go")
	if err != nil {
		t.Fatal(err)
	}
	if got2 != nil {
		t.Error("expected nil for non-existent document")
	}

	// List
	paths, err := s.ListDocuments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "main.go" {
		t.Errorf("unexpected paths: %v", paths)
	}

	// Delete
	if err := s.DeleteDocument(ctx, "main.go"); err != nil {
		t.Fatal(err)
	}
	got3, _ := s.GetDocument(ctx, "main.go")
	if got3 != nil {
		t.Error("expected nil after delete")
	}
}

func TestListDocumentsSorted(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	for _, p := range []string{"z.go", "a.go", "m.go"} {
		s.SaveDocument(ctx, Document{Path: p})
	}

	paths, _ := s.ListDocuments(ctx)
	if len(paths) != 3 {
		t.Fatalf("expected 3 paths, got %d", len(paths))
	}
	if paths[0] != "a.go" || paths[1] != "m.go" || paths[2] != "z.go" {
		t.Errorf("paths not sorted: %v", paths)
	}
}

func TestPersistAndLoad(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.gob")

	s1 := NewGobStore(path)
	s1.SaveChunks(ctx, []Chunk{
		{ID: "c1", FilePath: "a.go", Content: "hello", Vector: []float32{1, 2, 3}, ContentHash: "ch1"},
	})
	s1.SaveDocument(ctx, Document{Path: "a.go", Hash: "h1", ChunkIDs: []string{"c1"}})

	if err := s1.Persist(ctx); err != nil {
		t.Fatalf("Persist failed: %v", err)
	}

	// Load into new store
	s2 := NewGobStore(path)
	if err := s2.Load(ctx); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	all, _ := s2.GetAllChunks(ctx)
	if len(all) != 1 {
		t.Fatalf("expected 1 chunk after load, got %d", len(all))
	}
	if all[0].Content != "hello" {
		t.Errorf("unexpected content: %s", all[0].Content)
	}
	if len(all[0].Vector) != 3 {
		t.Errorf("expected 3-dim vector, got %d", len(all[0].Vector))
	}

	doc, _ := s2.GetDocument(ctx, "a.go")
	if doc == nil {
		t.Fatal("expected document after load")
	}
	if doc.Hash != "h1" {
		t.Errorf("expected hash h1, got %s", doc.Hash)
	}
}

func TestLoadNonExistentFile(t *testing.T) {
	ctx := context.Background()
	s := NewGobStore(filepath.Join(t.TempDir(), "does-not-exist.gob"))

	// Should not error — fresh store
	if err := s.Load(ctx); err != nil {
		t.Fatalf("Load should succeed for non-existent file, got: %v", err)
	}

	all, _ := s.GetAllChunks(ctx)
	if len(all) != 0 {
		t.Errorf("expected empty store, got %d chunks", len(all))
	}
}

func TestGetStats(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	s.SaveChunks(ctx, []Chunk{
		{ID: "c1", FilePath: "a.go", Vector: []float32{1}},
		{ID: "c2", FilePath: "a.go", Vector: []float32{1}},
		{ID: "c3", FilePath: "b.go", Vector: []float32{1}},
	})
	s.SaveDocument(ctx, Document{Path: "a.go"})
	s.SaveDocument(ctx, Document{Path: "b.go"})

	stats, err := s.GetStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalFiles != 2 {
		t.Errorf("expected 2 files, got %d", stats.TotalFiles)
	}
	if stats.TotalChunks != 3 {
		t.Errorf("expected 3 chunks, got %d", stats.TotalChunks)
	}
}

func TestListFilesWithStats(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	now := time.Now()
	s.SaveChunks(ctx, []Chunk{
		{ID: "c1", FilePath: "a.go", Vector: []float32{1}},
		{ID: "c2", FilePath: "a.go", Vector: []float32{1}},
		{ID: "c3", FilePath: "b.go", Vector: []float32{1}},
	})
	s.SaveDocument(ctx, Document{Path: "a.go", ModTime: now})
	s.SaveDocument(ctx, Document{Path: "b.go", ModTime: now})

	files, err := s.ListFilesWithStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	// Sorted by path
	if files[0].Path != "a.go" {
		t.Errorf("expected a.go first, got %s", files[0].Path)
	}
	if files[0].ChunkCount != 2 {
		t.Errorf("expected 2 chunks for a.go, got %d", files[0].ChunkCount)
	}
	if files[1].ChunkCount != 1 {
		t.Errorf("expected 1 chunk for b.go, got %d", files[1].ChunkCount)
	}
}

func TestGetAllChunks(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	s.SaveChunks(ctx, []Chunk{
		{ID: "c1", FilePath: "a.go"},
		{ID: "c2", FilePath: "b.go"},
	})

	all, err := s.GetAllChunks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(all))
	}
}

func TestLookupByContentHash(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	s.SaveChunks(ctx, []Chunk{
		{ID: "c1", FilePath: "a.go", ContentHash: "hash1", Vector: []float32{1, 2, 3}},
		{ID: "c2", FilePath: "b.go", ContentHash: "hash2", Vector: []float32{4, 5, 6}},
		{ID: "c3", FilePath: "c.go", ContentHash: "hash3", Vector: nil}, // no vector
	})

	// Found
	vec, found, err := s.LookupByContentHash(ctx, "hash1")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected to find hash1")
	}
	if len(vec) != 3 || vec[0] != 1 {
		t.Errorf("unexpected vector: %v", vec)
	}

	// Not found
	_, found, err = s.LookupByContentHash(ctx, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("expected not found for nonexistent hash")
	}

	// Hash exists but no vector
	_, found, err = s.LookupByContentHash(ctx, "hash3")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("expected not found for hash with empty vector")
	}
}

func TestClose(t *testing.T) {
	s := newTestStore(t)
	if err := s.Close(); err != nil {
		t.Errorf("expected nil from Close, got: %v", err)
	}
}

func TestSaveChunksOverwrite(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	s.SaveChunks(ctx, []Chunk{
		{ID: "c1", FilePath: "a.go", Content: "old"},
	})
	s.SaveChunks(ctx, []Chunk{
		{ID: "c1", FilePath: "a.go", Content: "new"},
	})

	all, _ := s.GetAllChunks(ctx)
	if len(all) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(all))
	}
	if all[0].Content != "new" {
		t.Errorf("expected overwritten content 'new', got '%s'", all[0].Content)
	}
}

func TestDeleteDocumentNonExistent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	// Should not error
	if err := s.DeleteDocument(ctx, "nope.go"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Ensure GobStore implements both interfaces.
var (
	_ VectorStore    = (*GobStore)(nil)
	_ EmbeddingCache = (*GobStore)(nil)
)
