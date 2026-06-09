/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package copilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoadCachedAPIToken_MissingReturnsNil(t *testing.T) {
	withTempHome(t)
	got, err := LoadCachedAPIToken()
	if err != nil {
		t.Fatalf("LoadCachedAPIToken on missing file: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil cached token, got %+v", got)
	}
}

func TestSaveCachedAPIToken_Roundtrip(t *testing.T) {
	withTempHome(t)

	want := &PersistedAPIToken{
		Value:            "copilot_secret_xyz",
		ExpiresAt:        time.Now().Add(25 * time.Minute).Round(time.Second),
		OAuthFingerprint: "abcdef012345",
	}
	if err := SaveCachedAPIToken(want); err != nil {
		t.Fatalf("SaveCachedAPIToken: %v", err)
	}

	got, err := LoadCachedAPIToken()
	if err != nil {
		t.Fatalf("LoadCachedAPIToken: %v", err)
	}
	if got == nil {
		t.Fatal("expected token, got nil")
	}
	if got.Value != want.Value {
		t.Errorf("Value mismatch: got %q want %q", got.Value, want.Value)
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("ExpiresAt mismatch: got %v want %v", got.ExpiresAt, want.ExpiresAt)
	}
	if got.OAuthFingerprint != want.OAuthFingerprint {
		t.Errorf("OAuthFingerprint mismatch: got %q want %q",
			got.OAuthFingerprint, want.OAuthFingerprint)
	}
	if got.FetchedAt.IsZero() {
		t.Error("FetchedAt should have been set by SaveCachedAPIToken")
	}

	if runtime.GOOS != "windows" {
		path, _ := APITokenCachePath()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("cache file perms %o, want 0600", perm)
		}
	}
}

func TestSaveCachedAPIToken_RejectsEmpty(t *testing.T) {
	withTempHome(t)
	if err := SaveCachedAPIToken(nil); err == nil {
		t.Error("expected error saving nil token")
	}
	if err := SaveCachedAPIToken(&PersistedAPIToken{Value: ""}); err == nil {
		t.Error("expected error saving empty token")
	}
}

func TestDeleteCachedAPIToken_Idempotent(t *testing.T) {
	withTempHome(t)
	if err := DeleteCachedAPIToken(); err != nil {
		t.Errorf("delete on missing cache should be a no-op, got %v", err)
	}
	if err := SaveCachedAPIToken(&PersistedAPIToken{
		Value:     "x",
		ExpiresAt: time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteCachedAPIToken(); err != nil {
		t.Errorf("delete failed: %v", err)
	}
	got, _ := LoadCachedAPIToken()
	if got != nil {
		t.Errorf("after delete, expected nil, got %+v", got)
	}
}

func TestSaveCachedAPIToken_OverwritesExisting(t *testing.T) {
	withTempHome(t)
	old := &PersistedAPIToken{
		Value:     "old",
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	if err := SaveCachedAPIToken(old); err != nil {
		t.Fatal(err)
	}
	fresh := &PersistedAPIToken{
		Value:     "fresh",
		ExpiresAt: time.Now().Add(25 * time.Minute),
	}
	if err := SaveCachedAPIToken(fresh); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCachedAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "fresh" {
		t.Errorf("expected fresh token to win, got %q", got.Value)
	}
}

func TestOAuthFingerprint(t *testing.T) {
	if oauthFingerprint("") != "" {
		t.Error("empty input should produce empty fingerprint")
	}
	fp1 := oauthFingerprint("gho_token_one")
	fp2 := oauthFingerprint("gho_token_two")
	fp1b := oauthFingerprint("gho_token_one")
	if fp1 == fp2 {
		t.Errorf("different tokens should have different fingerprints, both = %q", fp1)
	}
	if fp1 != fp1b {
		t.Errorf("fingerprint should be deterministic: %q vs %q", fp1, fp1b)
	}
	if len(fp1) != 12 {
		t.Errorf("fingerprint length = %d, want 12", len(fp1))
	}
	if got := oauthFingerprint("gho_secret_token_xyz"); got == "gho_secret_token_xyz" {
		t.Error("fingerprint must not echo the input token")
	}
}

// ---------------------------------------------------------------------------
// End-to-end refresh behavior of TokenSource with on-disk cache
// ---------------------------------------------------------------------------

// fakeTokenServer is a small helper that counts how many times the
// token-exchange endpoint is hit. Used to assert that the on-disk cache
// actually saves round-trips.
type fakeTokenServer struct {
	srv    *httptest.Server
	hits   int32
	expiry time.Duration
}

func newFakeTokenServer(t *testing.T, expiry time.Duration) *fakeTokenServer {
	t.Helper()
	f := &fakeTokenServer{expiry: expiry}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.hits, 1)
		_ = json.NewEncoder(w).Encode(apiTokenResponse{
			Token:     "exchanged_at_" + time.Now().Format(time.RFC3339Nano),
			ExpiresAt: time.Now().Add(f.expiry).Unix(),
		})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTokenServer) URL() string  { return f.srv.URL }
func (f *fakeTokenServer) Hits() int32  { return atomic.LoadInt32(&f.hits) }

// newPersistingTokenSource returns a TokenSource configured exactly like
// production (persist=true) but pointing at a fake token endpoint.
func newPersistingTokenSource(oauth, endpoint string) *TokenSource {
	return &TokenSource{
		oauth: func() (string, error) {
			if oauth == "" {
				return "", ErrNotLoggedIn
			}
			return oauth, nil
		},
		client:   &http.Client{Timeout: 5 * time.Second},
		endpoint: endpoint,
		persist:  true,
	}
}

func TestTokenSource_FirstGetWritesDiskCache(t *testing.T) {
	withTempHome(t)
	srv := newFakeTokenServer(t, 25*time.Minute)
	src := newPersistingTokenSource("gho_one", srv.URL())

	tok, err := src.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if tok.Value == "" {
		t.Fatal("expected non-empty token")
	}
	if got := srv.Hits(); got != 1 {
		t.Errorf("token server hits = %d, want 1", got)
	}

	cached, err := LoadCachedAPIToken()
	if err != nil {
		t.Fatalf("LoadCachedAPIToken: %v", err)
	}
	if cached == nil {
		t.Fatal("expected on-disk cache after Get(), found none")
	}
	if cached.Value != tok.Value {
		t.Errorf("on-disk cache value mismatch: %q vs %q", cached.Value, tok.Value)
	}
	if cached.OAuthFingerprint != oauthFingerprint("gho_one") {
		t.Errorf("on-disk fingerprint = %q, want %q",
			cached.OAuthFingerprint, oauthFingerprint("gho_one"))
	}
}

func TestTokenSource_SecondProcessReusesDiskCache(t *testing.T) {
	withTempHome(t)
	srv := newFakeTokenServer(t, 25*time.Minute)

	// First "process" — warms the disk cache.
	first := newPersistingTokenSource("gho_user", srv.URL())
	if _, err := first.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := srv.Hits(); got != 1 {
		t.Fatalf("after first Get, hits = %d, want 1", got)
	}

	// Second "process" — fresh TokenSource, empty in-memory cache, same
	// disk state. It should NOT hit the token server.
	second := newPersistingTokenSource("gho_user", srv.URL())
	tok, err := second.Get(context.Background())
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if tok.Value == "" {
		t.Fatal("expected cached value")
	}
	if got := srv.Hits(); got != 1 {
		t.Errorf("disk cache should have served the second Get; got %d hits, want 1", got)
	}
}

func TestTokenSource_DiscardsCacheOnAccountSwitch(t *testing.T) {
	withTempHome(t)
	srv := newFakeTokenServer(t, 25*time.Minute)

	// Warm cache as user A.
	a := newPersistingTokenSource("gho_user_A", srv.URL())
	if _, err := a.Get(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Fresh TokenSource impersonating user B should NOT reuse user A's
	// cached API token, even though it's still valid.
	b := newPersistingTokenSource("gho_user_B", srv.URL())
	if _, err := b.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := srv.Hits(); got != 2 {
		t.Errorf("account switch should force a refresh; got %d hits, want 2", got)
	}

	// And the new cache should be bound to user B's fingerprint.
	cached, err := LoadCachedAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	if cached == nil {
		t.Fatal("expected new cache for user B")
	}
	if cached.OAuthFingerprint != oauthFingerprint("gho_user_B") {
		t.Errorf("cache bound to %q, want %q",
			cached.OAuthFingerprint, oauthFingerprint("gho_user_B"))
	}
}

func TestTokenSource_DiscardsExpiredDiskCache(t *testing.T) {
	withTempHome(t)
	// Manually plant an expired cache entry.
	if err := SaveCachedAPIToken(&PersistedAPIToken{
		Value:            "stale",
		ExpiresAt:        time.Now().Add(-time.Minute),
		OAuthFingerprint: oauthFingerprint("gho_user"),
	}); err != nil {
		t.Fatal(err)
	}

	srv := newFakeTokenServer(t, 25*time.Minute)
	src := newPersistingTokenSource("gho_user", srv.URL())
	tok, err := src.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok.Value == "stale" {
		t.Error("expired disk cache should have been discarded")
	}
	if got := srv.Hits(); got != 1 {
		t.Errorf("expired cache should trigger refresh; got %d hits, want 1", got)
	}

	// And the stale file should have been removed/replaced.
	cached, _ := LoadCachedAPIToken()
	if cached == nil {
		t.Fatal("expected fresh cache after refresh")
	}
	if cached.Value == "stale" {
		t.Error("disk cache still holds the stale value")
	}
}

func TestTokenSource_InvalidateDeletesDiskCache(t *testing.T) {
	withTempHome(t)
	srv := newFakeTokenServer(t, 25*time.Minute)
	src := newPersistingTokenSource("gho_user", srv.URL())

	if _, err := src.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cached, _ := LoadCachedAPIToken(); cached == nil {
		t.Fatal("expected cache after Get")
	}

	src.Invalidate()

	if cached, _ := LoadCachedAPIToken(); cached != nil {
		t.Errorf("Invalidate() should have removed the on-disk cache, found %+v", cached)
	}
	// And the in-memory cache should be empty too.
	src.mu.Lock()
	defer src.mu.Unlock()
	if src.current.Value != "" {
		t.Errorf("Invalidate() should have cleared in-memory cache, found %q", src.current.Value)
	}
}

func TestTokenSource_NonPersistDoesNotTouchDisk(t *testing.T) {
	withTempHome(t)
	srv := newFakeTokenServer(t, 25*time.Minute)
	src := &TokenSource{
		oauth:    func() (string, error) { return "gho_user", nil },
		client:   &http.Client{Timeout: 5 * time.Second},
		endpoint: srv.URL(),
		persist:  false, // explicit
	}
	if _, err := src.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cached, _ := LoadCachedAPIToken(); cached != nil {
		t.Errorf("persist=false should not write to disk; found %+v", cached)
	}
}
