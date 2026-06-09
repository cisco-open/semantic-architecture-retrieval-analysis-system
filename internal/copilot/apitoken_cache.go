/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package copilot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// The Copilot API token returned by the GitHub token-exchange endpoint is
// short-lived (typically 25–30 minutes). Holding it only in memory means
// every fresh `saras` invocation pays a round-trip to GitHub before doing
// real work, and rapid invocations can hit GitHub's rate-limit on the
// token-exchange endpoint.
//
// To keep refresh transparent across CLI runs we mirror the in-memory cache
// to disk in the same per-user config directory used for the OAuth token,
// with the same 0600/0700 permissions and an atomic write-then-rename to
// avoid partial reads.

const apiTokenCacheFileName = "copilot_api.json"

// PersistedAPIToken is the on-disk shape of the cached short-lived Copilot
// API token. It carries enough metadata for diagnostics (FetchedAt, User)
// but the token Value itself is treated as a secret.
type PersistedAPIToken struct {
	Value     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	FetchedAt time.Time `json:"fetched_at"`
	// OAuthFingerprint binds the cached API token to the OAuth token that
	// produced it. If the user runs `saras copilot login` with a new
	// account, the fingerprint changes and we discard the stale cache.
	OAuthFingerprint string `json:"oauth_fingerprint,omitempty"`
}

// APITokenCachePath returns the absolute path of the cache file.
func APITokenCachePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, apiTokenCacheFileName), nil
}

// LoadCachedAPIToken reads the cached API token from disk. Returns
// (nil, nil) when no cache file exists (this is normal on first use).
// Other errors (malformed JSON, missing fields) are returned so the caller
// can decide whether to fall back to a fresh fetch.
func LoadCachedAPIToken() (*PersistedAPIToken, error) {
	path, err := APITokenCachePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read copilot api token cache: %w", err)
	}
	var t PersistedAPIToken
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parse copilot api token cache: %w", err)
	}
	if t.Value == "" {
		return nil, nil
	}
	return &t, nil
}

// SaveCachedAPIToken writes the cached API token to disk atomically with
// 0600 permissions.
func SaveCachedAPIToken(t *PersistedAPIToken) error {
	if t == nil || t.Value == "" {
		return errors.New("refuse to cache empty copilot api token")
	}
	path, err := APITokenCachePath()
	if err != nil {
		return err
	}
	if t.FetchedAt.IsZero() {
		t.FetchedAt = time.Now().UTC()
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal copilot api token cache: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, tokenFileMode); err != nil {
		return fmt.Errorf("write copilot api token cache: %w", err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(tmp, tokenFileMode)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("install copilot api token cache: %w", err)
	}
	return nil
}

// DeleteCachedAPIToken removes the on-disk API token cache. Safe to call
// when no file exists.
func DeleteCachedAPIToken() error {
	path, err := APITokenCachePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove copilot api token cache: %w", err)
	}
	return nil
}

// oauthFingerprint returns a short, non-secret tag that uniquely identifies
// an OAuth token without exposing it. We use the first 12 characters of the
// token's SHA-256 hash — enough to detect a switched login while remaining
// useless to an attacker who only sees the cache file.
//
// Storing the fingerprint (and not the token itself) means an attacker who
// reads `copilot_api.json` cannot recover the user's GitHub OAuth token.
func oauthFingerprint(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	hex := hex.EncodeToString(sum[:])
	if len(hex) < 12 {
		return hex
	}
	return hex[:12]
}
