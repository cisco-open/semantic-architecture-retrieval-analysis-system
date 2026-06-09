/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/store"
)

// mockEmbedder implements embedder.Embedder for testing.
type mockEmbedder struct {
	dims       int
	callCount  int
	failAt     int // fail at this item index within a batch (-1 = never)
	batchSizes []int
}

func newMockEmbedder(dims int) *mockEmbedder {
	return &mockEmbedder{dims: dims, failAt: -1}
}

func (m *mockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	m.callCount++
	vec := make([]float32, m.dims)
	for i := range vec {
		vec[i] = float32(m.callCount) / float32(m.dims)
	}
	return vec, nil
}

func (m *mockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	m.batchSizes = append(m.batchSizes, len(texts))
	results := make([][]float32, len(texts))
	for i := range texts {
		if m.failAt == i {
			return nil, fmt.Errorf("mock embedding failure at index %d", i)
		}
		vec, err := m.Embed(ctx, texts[i])
		if err != nil {
			return nil, err
		}
		results[i] = vec
	}
	return results, nil
}

func (m *mockEmbedder) Dimensions() int { return m.dims }
func (m *mockEmbedder) Close() error    { return nil }

func setupIndexerProject(t *testing.T) (string, *store.GobStore, *mockEmbedder, *Indexer) {
	t.Helper()
	root := t.TempDir()

	writeFile(t, root, "main.go", "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n")
	writeFile(t, root, "util.go", "package main\n\nfunc helper() string {\n\treturn \"help\"\n}\n")
	writeFile(t, root, "README.md", "# Project\n\nA sample project for testing the indexer.\n")

	storePath := filepath.Join(root, ".saras", "index.gob")
	os.MkdirAll(filepath.Dir(storePath), 0755)
	st := store.NewGobStore(storePath)

	emb := newMockEmbedder(3)
	chunker := NewChunker(512, 50)
	scanner := NewScanner(root, []string{".saras"})

	idx := NewIndexer(root, st, emb, chunker, scanner)
	return root, st, emb, idx
}

func TestIndexAll(t *testing.T) {
	ctx := context.Background()
	_, st, _, idx := setupIndexerProject(t)

	stats, err := idx.IndexAll(ctx)
	if err != nil {
		t.Fatalf("IndexAll failed: %v", err)
	}

	if stats.FilesIndexed < 2 {
		t.Errorf("expected at least 2 files indexed, got %d", stats.FilesIndexed)
	}
	if stats.ChunksCreated < 2 {
		t.Errorf("expected at least 2 chunks created, got %d", stats.ChunksCreated)
	}
	if stats.Duration <= 0 {
		t.Error("expected positive duration")
	}

	// Verify documents were stored
	docs, _ := st.ListDocuments(ctx)
	if len(docs) < 2 {
		t.Errorf("expected at least 2 documents, got %d", len(docs))
	}

	// Verify chunks have vectors
	allChunks, _ := st.GetAllChunks(ctx)
	for _, c := range allChunks {
		if len(c.Vector) == 0 {
			t.Errorf("chunk %s has no vector", c.ID)
		}
		if c.FilePath == "" {
			t.Error("chunk has empty FilePath")
		}
	}
}

func TestIndexAllWithProgress(t *testing.T) {
	ctx := context.Background()
	_, _, _, idx := setupIndexerProject(t)

	var progressCalls []ProgressInfo
	stats, err := idx.IndexAllWithProgress(ctx, func(info ProgressInfo) {
		progressCalls = append(progressCalls, info)
	})
	if err != nil {
		t.Fatalf("IndexAllWithProgress failed: %v", err)
	}

	if len(progressCalls) == 0 {
		t.Error("expected progress callbacks")
	}

	// Verify progress is monotonic
	for i, p := range progressCalls {
		if p.Current != i+1 {
			t.Errorf("progress %d: expected Current=%d, got %d", i, i+1, p.Current)
		}
		if p.Total < 2 {
			t.Errorf("progress %d: expected Total >= 2, got %d", i, p.Total)
		}
		if p.CurrentFile == "" {
			t.Errorf("progress %d: empty CurrentFile", i)
		}
	}

	_ = stats
}

func TestIndexAllIdempotent(t *testing.T) {
	ctx := context.Background()
	_, st, emb, idx := setupIndexerProject(t)

	// First index
	stats1, err := idx.IndexAll(ctx)
	if err != nil {
		t.Fatalf("first IndexAll failed: %v", err)
	}

	callsAfterFirst := emb.callCount

	// Second index — should skip unchanged files
	stats2, err := idx.IndexAll(ctx)
	if err != nil {
		t.Fatalf("second IndexAll failed: %v", err)
	}

	if stats2.FilesIndexed != 0 {
		t.Errorf("expected 0 files re-indexed on second pass, got %d", stats2.FilesIndexed)
	}
	if stats2.FilesSkipped < stats1.FilesIndexed {
		t.Errorf("expected all files skipped on second pass: skipped=%d, first indexed=%d",
			stats2.FilesSkipped, stats1.FilesIndexed)
	}

	// No additional embedding calls
	if emb.callCount != callsAfterFirst {
		t.Errorf("expected no new embedding calls, got %d additional", emb.callCount-callsAfterFirst)
	}

	// Chunks should be the same
	allChunks, _ := st.GetAllChunks(ctx)
	if len(allChunks) != stats1.ChunksCreated {
		t.Errorf("chunk count changed: %d -> %d", stats1.ChunksCreated, len(allChunks))
	}
}

func TestIndexAllDetectsModifiedFiles(t *testing.T) {
	ctx := context.Background()
	root, _, emb, idx := setupIndexerProject(t)

	// First index
	idx.IndexAll(ctx)
	callsAfterFirst := emb.callCount

	// Modify a file
	writeFile(t, root, "main.go", "package main\n\nfunc main() {\n\t// modified\n}\n")

	// Second index — should re-index modified file
	stats, err := idx.IndexAll(ctx)
	if err != nil {
		t.Fatalf("second IndexAll failed: %v", err)
	}

	if stats.FilesIndexed < 1 {
		t.Error("expected at least 1 file re-indexed after modification")
	}
	if emb.callCount <= callsAfterFirst {
		t.Error("expected new embedding calls for modified file")
	}
}

func TestIndexAllRemovesDeletedFiles(t *testing.T) {
	ctx := context.Background()
	root, st, _, idx := setupIndexerProject(t)

	// First index
	idx.IndexAll(ctx)

	// Delete a file
	os.Remove(filepath.Join(root, "util.go"))

	// Second index
	stats, err := idx.IndexAll(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if stats.FilesRemoved < 1 {
		t.Error("expected at least 1 file removed")
	}

	// Verify it's gone from the store
	doc, _ := st.GetDocument(ctx, "util.go")
	if doc != nil {
		t.Error("expected util.go document to be removed")
	}

	chunks, _ := st.GetChunksForFile(ctx, "util.go")
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for deleted file, got %d", len(chunks))
	}
}

func TestIndexFile(t *testing.T) {
	ctx := context.Background()
	root, st, _, idx := setupIndexerProject(t)

	indexed, err := idx.IndexFile(ctx, filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}
	if !indexed {
		t.Error("expected file to be indexed")
	}

	chunks, _ := st.GetChunksForFile(ctx, filepath.Join(root, "main.go"))
	if len(chunks) == 0 {
		t.Error("expected chunks after indexing")
	}
}

func TestIndexFileEmbedsLargeFileInBatches(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	var content strings.Builder
	for i := 0; i < 405; i++ {
		fmt.Fprintf(&content, "line-%04d abcdefghijkl\n", i)
	}
	writeFile(t, root, "large.txt", content.String())

	storePath := filepath.Join(root, ".saras", "index.gob")
	os.MkdirAll(filepath.Dir(storePath), 0755)
	st := store.NewGobStore(storePath)
	emb := newMockEmbedder(3)
	chunker := NewChunker(4, 0)
	scanner := NewScanner(root, []string{".saras"})
	idx := NewIndexer(root, st, emb, chunker, scanner)

	indexed, err := idx.IndexFile(ctx, "large.txt")
	if err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}
	if !indexed {
		t.Fatal("expected file to be indexed")
	}

	wantBatchSizes := []int{200, 200, 5}
	if !reflect.DeepEqual(emb.batchSizes, wantBatchSizes) {
		t.Fatalf("expected batch sizes %v, got %v", wantBatchSizes, emb.batchSizes)
	}

	chunks, err := st.GetChunksForFile(ctx, "large.txt")
	if err != nil {
		t.Fatalf("GetChunksForFile failed: %v", err)
	}
	if len(chunks) != 405 {
		t.Fatalf("expected 405 chunks, got %d", len(chunks))
	}
}

func TestIndexFileTwiceSkips(t *testing.T) {
	ctx := context.Background()
	root, _, emb, idx := setupIndexerProject(t)
	path := filepath.Join(root, "main.go")

	idx.IndexFile(ctx, path)
	callsAfter := emb.callCount

	indexed, err := idx.IndexFile(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if indexed {
		t.Error("expected file to be skipped on second index")
	}
	if emb.callCount != callsAfter {
		t.Error("expected no new embedding calls")
	}
}

func TestRemoveFile(t *testing.T) {
	ctx := context.Background()
	root, st, _, idx := setupIndexerProject(t)
	path := filepath.Join(root, "main.go")

	idx.IndexFile(ctx, path)

	if err := idx.RemoveFile(ctx, path); err != nil {
		t.Fatalf("RemoveFile failed: %v", err)
	}

	doc, _ := st.GetDocument(ctx, path)
	if doc != nil {
		t.Error("expected document to be removed")
	}

	chunks, _ := st.GetChunksForFile(ctx, path)
	if len(chunks) != 0 {
		t.Error("expected chunks to be removed")
	}
}

func TestIndexAllContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	_, _, _, idx := setupIndexerProject(t)

	cancel() // cancel immediately

	_, err := idx.IndexAll(ctx)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestIndexAllWithContentHashCache(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	// Create two files with identical content
	content := "package main\n\nfunc identical() string {\n\treturn \"same content in two files\"\n}\n"
	writeFile(t, root, "a.go", content)
	writeFile(t, root, "b.go", content)

	storePath := filepath.Join(root, ".saras", "index.gob")
	os.MkdirAll(filepath.Dir(storePath), 0755)
	st := store.NewGobStore(storePath)
	emb := newMockEmbedder(3)
	chunker := NewChunker(512, 50)
	scanner := NewScanner(root, []string{".saras"})
	idx := NewIndexer(root, st, emb, chunker, scanner)

	stats, err := idx.IndexAll(ctx)
	if err != nil {
		t.Fatalf("IndexAll failed: %v", err)
	}

	if stats.FilesIndexed != 2 {
		t.Errorf("expected 2 files indexed, got %d", stats.FilesIndexed)
	}

	// The second file should reuse embeddings from the first via content hash cache
	// So we should have fewer embedding calls than total chunks
	allChunks, _ := st.GetAllChunks(ctx)
	if emb.callCount > len(allChunks) {
		t.Logf("embedding calls=%d, total chunks=%d (cache may have saved calls)", emb.callCount, len(allChunks))
	}
}

func TestComputeFileHash(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0644)

	hash1 := computeFileHash(path)
	if hash1 == "" {
		t.Fatal("expected non-empty hash")
	}

	hash2 := computeFileHash(path)
	if hash1 != hash2 {
		t.Error("same file should produce same hash")
	}

	os.WriteFile(path, []byte("modified"), 0644)
	hash3 := computeFileHash(path)
	if hash3 == hash1 {
		t.Error("modified file should produce different hash")
	}
}

func TestComputeFileHashMissing(t *testing.T) {
	hash := computeFileHash("/nonexistent/file")
	if hash != "" {
		t.Error("expected empty hash for missing file")
	}
}

func TestIsAbsPath(t *testing.T) {
	if !isAbsPath("/usr/local/bin") {
		t.Error("expected /usr/local/bin to be absolute")
	}
	if isAbsPath("relative/path") {
		t.Error("expected relative/path to not be absolute")
	}
	if isAbsPath("") {
		t.Error("expected empty string to not be absolute")
	}
}
