/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package search

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/store"
)

// ---------------------------------------------------------------------------
// Mock embedder
// ---------------------------------------------------------------------------

type mockEmbedder struct {
	dims    int
	vectors map[string][]float32 // query -> vector
}

func newMockEmbedder(dims int) *mockEmbedder {
	return &mockEmbedder{dims: dims, vectors: make(map[string][]float32)}
}

func (m *mockEmbedder) SetVector(query string, vec []float32) {
	m.vectors[query] = vec
}

func (m *mockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if vec, ok := m.vectors[text]; ok {
		return vec, nil
	}
	// Default: return a deterministic vector based on text length
	vec := make([]float32, m.dims)
	for i := range vec {
		vec[i] = float32(len(text)%10) / 10.0
	}
	return vec, nil
}

func (m *mockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := m.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		results[i] = v
	}
	return results, nil
}

func (m *mockEmbedder) Dimensions() int { return m.dims }
func (m *mockEmbedder) Close() error    { return nil }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func setupSearchStore(t *testing.T) *store.GobStore {
	t.Helper()
	st := store.NewGobStore(t.TempDir() + "/test.gob")

	chunks := []store.Chunk{
		{ID: "auth_0", FilePath: "src/auth.go", StartLine: 1, EndLine: 10, Content: "func Login(user, pass string) error { return validateCredentials(user, pass) }", Vector: []float32{1, 0, 0}},
		{ID: "auth_1", FilePath: "src/auth.go", StartLine: 11, EndLine: 20, Content: "func Logout(token string) error { return invalidateSession(token) }", Vector: []float32{0.9, 0.1, 0}},
		{ID: "db_0", FilePath: "src/db.go", StartLine: 1, EndLine: 15, Content: "func Connect(dsn string) (*DB, error) { return sql.Open(\"postgres\", dsn) }", Vector: []float32{0, 1, 0}},
		{ID: "handler_0", FilePath: "src/handler.go", StartLine: 1, EndLine: 10, Content: "func HandleLogin(w http.ResponseWriter, r *http.Request) { Login(r.Form) }", Vector: []float32{0.8, 0.2, 0}},
		{ID: "test_0", FilePath: "test/auth_test.go", StartLine: 1, EndLine: 10, Content: "func TestLogin(t *testing.T) { err := Login(\"user\", \"pass\") }", Vector: []float32{0.95, 0.05, 0}},
		{ID: "mock_0", FilePath: "mocks/auth_mock.go", StartLine: 1, EndLine: 5, Content: "type MockAuth struct { LoginFunc func() }", Vector: []float32{0.7, 0.3, 0}},
		{ID: "doc_0", FilePath: "docs/auth.md", StartLine: 1, EndLine: 5, Content: "# Authentication\nThe auth module handles login and logout.", Vector: []float32{0.6, 0, 0.4}},
	}

	st.SaveChunks(context.Background(), chunks)
	return st
}

func defaultSearchConfig() config.SearchConfig {
	return config.SearchConfig{
		Boost: config.BoostConfig{
			Enabled: true,
			Penalties: []config.BoostRule{
				{Pattern: "test/", Factor: 0.5},
				{Pattern: "_test.", Factor: 0.5},
				{Pattern: "mocks/", Factor: 0.4},
				{Pattern: ".md", Factor: 0.6},
				{Pattern: "docs/", Factor: 0.6},
			},
			Bonuses: []config.BoostRule{
				{Pattern: "src/", Factor: 1.1},
			},
		},
		Hybrid: config.HybridConfig{
			Enabled: false,
			K:       60,
		},
		Dedup: config.DedupConfig{
			Enabled: false,
		},
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSearchBasic(t *testing.T) {
	st := setupSearchStore(t)
	emb := newMockEmbedder(3)
	emb.SetVector("login authentication", []float32{1, 0, 0})

	s := NewSearcher(st, emb, defaultSearchConfig())

	results, err := s.Search(context.Background(), "login authentication", 5)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected results")
	}

	// auth_0 should be the top result (exact vector match + /src/ bonus)
	if results[0].FilePath != "src/auth.go" {
		t.Errorf("expected src/auth.go first, got %s", results[0].FilePath)
	}
}

func TestSearchLimitRespected(t *testing.T) {
	st := setupSearchStore(t)
	emb := newMockEmbedder(3)
	emb.SetVector("login", []float32{0.9, 0.1, 0})

	s := NewSearcher(st, emb, defaultSearchConfig())
	results, err := s.Search(context.Background(), "login", 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}

func TestSearchDefaultLimit(t *testing.T) {
	st := setupSearchStore(t)
	emb := newMockEmbedder(3)
	emb.SetVector("query", []float32{0.5, 0.5, 0})

	s := NewSearcher(st, emb, defaultSearchConfig())
	results, err := s.Search(context.Background(), "query", 0) // 0 defaults to 10
	if err != nil {
		t.Fatal(err)
	}

	if len(results) > 10 {
		t.Errorf("expected at most 10 results with default limit, got %d", len(results))
	}
}

func TestSearchBoostPenalties(t *testing.T) {
	st := setupSearchStore(t)
	emb := newMockEmbedder(3)
	// Vector that matches test and auth equally
	emb.SetVector("login test", []float32{0.95, 0.05, 0})

	cfg := defaultSearchConfig()
	cfg.Dedup.Enabled = false

	s := NewSearcher(st, emb, cfg)
	results, err := s.Search(context.Background(), "login test", 10)
	if err != nil {
		t.Fatal(err)
	}

	// Find test file result and src file result
	var testScore, srcScore float32
	for _, r := range results {
		if r.FilePath == "test/auth_test.go" {
			testScore = r.Score
		}
		if r.FilePath == "src/auth.go" {
			srcScore = r.Score
		}
	}

	if testScore >= srcScore {
		t.Errorf("test file (score=%.4f) should score lower than src file (score=%.4f) after boost", testScore, srcScore)
	}
}

func TestSearchBoostBonuses(t *testing.T) {
	st := setupSearchStore(t)
	emb := newMockEmbedder(3)
	emb.SetVector("database connection", []float32{0, 1, 0})

	cfg := defaultSearchConfig()
	s := NewSearcher(st, emb, cfg)
	results, err := s.Search(context.Background(), "database connection", 3)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) == 0 {
		t.Fatal("expected results")
	}

	// src/db.go should get the /src/ bonus
	if results[0].FilePath != "src/db.go" {
		t.Errorf("expected src/db.go first, got %s", results[0].FilePath)
	}
}

func TestSearchNoBoost(t *testing.T) {
	st := setupSearchStore(t)
	emb := newMockEmbedder(3)
	emb.SetVector("login", []float32{0.95, 0.05, 0})

	cfg := defaultSearchConfig()
	cfg.Boost.Enabled = false

	s := NewSearcher(st, emb, cfg)
	results, err := s.Search(context.Background(), "login", 10)
	if err != nil {
		t.Fatal(err)
	}

	// Without boost, test file should appear based on raw vector similarity
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	// test/auth_test.go has vector [0.95, 0.05, 0] which is very close to query
	found := false
	for _, r := range results {
		if r.FilePath == "test/auth_test.go" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected test file in results when boost is disabled")
	}
}

func TestSearchDedup(t *testing.T) {
	st := setupSearchStore(t)
	emb := newMockEmbedder(3)
	emb.SetVector("auth login", []float32{1, 0, 0})

	cfg := defaultSearchConfig()
	cfg.Dedup.Enabled = true

	s := NewSearcher(st, emb, cfg)
	results, err := s.Search(context.Background(), "auth login", 10)
	if err != nil {
		t.Fatal(err)
	}

	// Check no duplicate file paths
	seen := make(map[string]bool)
	for _, r := range results {
		if seen[r.FilePath] {
			t.Errorf("duplicate file path in deduped results: %s", r.FilePath)
		}
		seen[r.FilePath] = true
	}
}

func TestSearchNoDedupAllowsDuplicates(t *testing.T) {
	st := setupSearchStore(t)
	emb := newMockEmbedder(3)
	emb.SetVector("auth", []float32{0.95, 0.05, 0})

	cfg := defaultSearchConfig()
	cfg.Dedup.Enabled = false

	s := NewSearcher(st, emb, cfg)
	results, err := s.Search(context.Background(), "auth", 10)
	if err != nil {
		t.Fatal(err)
	}

	// src/auth.go has 2 chunks - both should appear
	count := 0
	for _, r := range results {
		if r.FilePath == "src/auth.go" {
			count++
		}
	}
	if count < 2 {
		t.Errorf("expected 2 results for src/auth.go without dedup, got %d", count)
	}
}

func TestSearchHybrid(t *testing.T) {
	st := setupSearchStore(t)
	emb := newMockEmbedder(3)
	// Vector search favours db.go
	emb.SetVector("database connect", []float32{0, 1, 0})

	cfg := defaultSearchConfig()
	cfg.Hybrid.Enabled = true
	cfg.Hybrid.K = 60
	cfg.Dedup.Enabled = false

	s := NewSearcher(st, emb, cfg)
	results, err := s.Search(context.Background(), "database connect", 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) == 0 {
		t.Fatal("expected results from hybrid search")
	}
}

func TestSearchHybridRRFScoring(t *testing.T) {
	st := setupSearchStore(t)
	emb := newMockEmbedder(3)
	// Query that matches auth via vector but "Login" via text
	emb.SetVector("Login function", []float32{0.9, 0.1, 0})

	cfg := defaultSearchConfig()
	cfg.Hybrid.Enabled = true
	cfg.Hybrid.K = 60
	cfg.Boost.Enabled = false
	cfg.Dedup.Enabled = false

	s := NewSearcher(st, emb, cfg)
	results, err := s.Search(context.Background(), "Login function", 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) == 0 {
		t.Fatal("expected results")
	}

	// Results should be sorted by score
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted: %f > %f at position %d", results[i].Score, results[i-1].Score, i)
		}
	}
}

func TestSearchResultsSorted(t *testing.T) {
	st := setupSearchStore(t)
	emb := newMockEmbedder(3)
	emb.SetVector("anything", []float32{0.5, 0.3, 0.2})

	s := NewSearcher(st, emb, defaultSearchConfig())
	results, err := s.Search(context.Background(), "anything", 10)
	if err != nil {
		t.Fatal(err)
	}

	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted by score at position %d: %f > %f", i, results[i].Score, results[i-1].Score)
		}
	}
}

func TestSearchEmptyStore(t *testing.T) {
	st := store.NewGobStore(t.TempDir() + "/empty.gob")
	emb := newMockEmbedder(3)
	emb.SetVector("test", []float32{1, 0, 0})

	s := NewSearcher(st, emb, defaultSearchConfig())
	results, err := s.Search(context.Background(), "test", 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results from empty store, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// Text search tests
// ---------------------------------------------------------------------------

func TestTextSearch(t *testing.T) {
	st := setupSearchStore(t)
	emb := newMockEmbedder(3)

	cfg := defaultSearchConfig()
	cfg.Hybrid.Enabled = true

	s := NewSearcher(st, emb, cfg)
	results, err := s.textSearch(context.Background(), "Login", 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) == 0 {
		t.Fatal("expected text search results for 'Login'")
	}

	// Should find chunks containing "Login"
	for _, r := range results {
		if r.Score <= 0 {
			t.Errorf("expected positive score, got %f", r.Score)
		}
	}
}

func TestTextSearchCaseInsensitive(t *testing.T) {
	st := setupSearchStore(t)
	emb := newMockEmbedder(3)
	s := NewSearcher(st, emb, defaultSearchConfig())

	results1, _ := s.textSearch(context.Background(), "login", 10)
	results2, _ := s.textSearch(context.Background(), "LOGIN", 10)

	if len(results1) != len(results2) {
		t.Errorf("case insensitive search should return same count: %d vs %d", len(results1), len(results2))
	}
}

func TestTextSearchNoResults(t *testing.T) {
	st := setupSearchStore(t)
	emb := newMockEmbedder(3)
	s := NewSearcher(st, emb, defaultSearchConfig())

	results, err := s.textSearch(context.Background(), "zzz_nonexistent_term", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestTextSearchMultipleTerms(t *testing.T) {
	st := setupSearchStore(t)
	emb := newMockEmbedder(3)
	s := NewSearcher(st, emb, defaultSearchConfig())

	// "Login" appears in auth and handler chunks. "validateCredentials" only in auth_0.
	results, _ := s.textSearch(context.Background(), "Login validateCredentials", 10)

	if len(results) == 0 {
		t.Fatal("expected results")
	}

	// auth_0 should score highest (matches both terms)
	if results[0].Chunk.ID != "auth_0" {
		t.Errorf("expected auth_0 to score highest, got %s", results[0].Chunk.ID)
	}
}

// ---------------------------------------------------------------------------
// RRF tests
// ---------------------------------------------------------------------------

func TestReciprocalRankFusion(t *testing.T) {
	cfg := defaultSearchConfig()
	cfg.Hybrid.K = 60
	s := &Searcher{cfg: cfg}

	vectorResults := []store.SearchResult{
		{Chunk: store.Chunk{ID: "a", FilePath: "a.go", Content: "aaa"}, Score: 0.9},
		{Chunk: store.Chunk{ID: "b", FilePath: "b.go", Content: "bbb"}, Score: 0.8},
		{Chunk: store.Chunk{ID: "c", FilePath: "c.go", Content: "ccc"}, Score: 0.7},
	}

	textResults := []store.SearchResult{
		{Chunk: store.Chunk{ID: "b", FilePath: "b.go", Content: "bbb"}, Score: 5.0},
		{Chunk: store.Chunk{ID: "d", FilePath: "d.go", Content: "ddd"}, Score: 3.0},
		{Chunk: store.Chunk{ID: "a", FilePath: "a.go", Content: "aaa"}, Score: 1.0},
	}

	merged := s.reciprocalRankFusion(vectorResults, textResults)

	if len(merged) != 4 { // a, b, c, d
		t.Fatalf("expected 4 merged results, got %d", len(merged))
	}

	// b appears at rank 2 in vector and rank 1 in text → highest RRF
	scoreMap := make(map[string]float32)
	for _, r := range merged {
		scoreMap[r.FilePath] = r.Score
	}

	// b should have highest RRF score (rank 2 + rank 1)
	bScore := scoreMap["b.go"]
	// a has rank 1 + rank 3
	aScore := scoreMap["a.go"]

	// RRF(b) = 1/(60+2) + 1/(60+1) = 0.01613 + 0.01639 = 0.03252
	// RRF(a) = 1/(60+1) + 1/(60+3) = 0.01639 + 0.01587 = 0.03226
	// b should be slightly higher than a
	if bScore <= aScore {
		t.Errorf("expected b (%.5f) > a (%.5f) in RRF", bScore, aScore)
	}

	// c only appears in vector (rank 3)
	cScore := scoreMap["c.go"]
	if cScore >= aScore {
		t.Errorf("expected c (%.5f) < a (%.5f)", cScore, aScore)
	}
}

// ---------------------------------------------------------------------------
// Boost tests
// ---------------------------------------------------------------------------

func TestApplyBoost(t *testing.T) {
	cfg := defaultSearchConfig()
	s := &Searcher{cfg: cfg}

	results := []Result{
		{FilePath: "src/auth.go", Score: 1.0},
		{FilePath: "test/auth_test.go", Score: 1.0},
		{FilePath: "mocks/auth_mock.go", Score: 1.0},
		{FilePath: "docs/auth.md", Score: 1.0},
	}

	boosted := s.applyBoost(results)

	// src/auth.go gets 1.1x bonus
	if math.Abs(float64(boosted[0].Score-1.1)) > 0.01 {
		t.Errorf("src file: expected ~1.1, got %f", boosted[0].Score)
	}

	// test file gets 0.5x * 0.5x penalty (matches both "test/" and "_test.")
	if math.Abs(float64(boosted[1].Score-0.25)) > 0.01 {
		t.Errorf("test file: expected ~0.25, got %f", boosted[1].Score)
	}

	// mocks file gets 0.4x penalty
	if math.Abs(float64(boosted[2].Score-0.4)) > 0.01 {
		t.Errorf("mock file: expected ~0.4, got %f", boosted[2].Score)
	}

	// docs file gets 0.6x penalty (from .md) * 0.6 (from /docs/) = 0.36
	expected := float32(0.6 * 0.6)
	if math.Abs(float64(boosted[3].Score-expected)) > 0.01 {
		t.Errorf("docs file: expected ~%f, got %f", expected, boosted[3].Score)
	}
}

func TestApplyBoostNoRules(t *testing.T) {
	cfg := config.SearchConfig{Boost: config.BoostConfig{Enabled: true}}
	s := &Searcher{cfg: cfg}

	results := []Result{{FilePath: "a.go", Score: 0.9}}
	boosted := s.applyBoost(results)

	if boosted[0].Score != 0.9 {
		t.Errorf("expected unchanged score 0.9, got %f", boosted[0].Score)
	}
}

// ---------------------------------------------------------------------------
// Dedup tests
// ---------------------------------------------------------------------------

func TestDedup(t *testing.T) {
	s := &Searcher{}

	results := []Result{
		{FilePath: "a.go", Score: 0.9, StartLine: 1},
		{FilePath: "a.go", Score: 0.7, StartLine: 20},
		{FilePath: "b.go", Score: 0.8, StartLine: 1},
		{FilePath: "c.go", Score: 0.6, StartLine: 1},
	}

	deduped := s.dedup(results)

	if len(deduped) != 3 {
		t.Fatalf("expected 3 deduped results, got %d", len(deduped))
	}

	// a.go should appear once with the higher score
	for _, r := range deduped {
		if r.FilePath == "a.go" && r.Score != 0.9 {
			t.Errorf("expected a.go with score 0.9, got %f", r.Score)
		}
	}
}

func TestDedupEmpty(t *testing.T) {
	s := &Searcher{}
	deduped := s.dedup(nil)
	if len(deduped) != 0 {
		t.Errorf("expected 0 results, got %d", len(deduped))
	}
}

func TestDedupSingleResult(t *testing.T) {
	s := &Searcher{}
	results := []Result{{FilePath: "a.go", Score: 0.5}}
	deduped := s.dedup(results)
	if len(deduped) != 1 {
		t.Errorf("expected 1 result, got %d", len(deduped))
	}
}

// ---------------------------------------------------------------------------
// storeResultsToResults
// ---------------------------------------------------------------------------

func TestStoreResultsToResults(t *testing.T) {
	srs := []store.SearchResult{
		{Chunk: store.Chunk{FilePath: "a.go", StartLine: 1, EndLine: 10, Content: "aaa"}, Score: 0.9},
		{Chunk: store.Chunk{FilePath: "b.go", StartLine: 5, EndLine: 15, Content: "bbb"}, Score: 0.7},
	}

	results := storeResultsToResults(srs)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].FilePath != "a.go" || results[0].Score != 0.9 {
		t.Errorf("unexpected first result: %+v", results[0])
	}
	if results[1].Content != "bbb" {
		t.Errorf("unexpected second result content: %s", results[1].Content)
	}
}

func TestStoreResultsToResultsEmpty(t *testing.T) {
	results := storeResultsToResults(nil)
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// Suppress unused import warning
var _ = fmt.Sprintf
