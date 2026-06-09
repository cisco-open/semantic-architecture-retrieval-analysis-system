/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package copilot

import (
	"errors"
	"os"
	"runtime"
	"testing"
	"time"
)

// withTempHome points $HOME and XDG_CONFIG_HOME at a temp directory so the
// production TokenPath() function resolves to a per-test location and the
// user's real token (if any) is never touched.
func withTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir+"/.config")
	if runtime.GOOS == "darwin" {
		// os.UserConfigDir() on darwin returns ~/Library/Application Support
		// — by pointing HOME at the temp dir we keep the test hermetic.
	}
	return dir
}

func TestLoadPersistedTokenMissingReturnsErrNotLoggedIn(t *testing.T) {
	withTempHome(t)
	_, err := LoadPersistedToken()
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("expected ErrNotLoggedIn, got %v", err)
	}
}

func TestSaveAndLoadPersistedToken(t *testing.T) {
	withTempHome(t)

	tok := &PersistedToken{
		AccessToken: "gho_unit_test_token",
		TokenType:   "bearer",
		Scope:       "read:user",
		User:        "octocat",
	}
	if err := SavePersistedToken(tok); err != nil {
		t.Fatalf("SavePersistedToken: %v", err)
	}

	got, err := LoadPersistedToken()
	if err != nil {
		t.Fatalf("LoadPersistedToken: %v", err)
	}
	if got.AccessToken != tok.AccessToken {
		t.Errorf("token roundtrip mismatch: got %q", got.AccessToken)
	}
	if got.User != "octocat" {
		t.Errorf("user roundtrip mismatch: got %q", got.User)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should have been set by SavePersistedToken")
	}

	if runtime.GOOS != "windows" {
		path, _ := TokenPath()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Errorf("token file perms %o, want 0600", perm)
		}
	}
}

func TestSavePersistedTokenRejectsEmpty(t *testing.T) {
	withTempHome(t)
	if err := SavePersistedToken(&PersistedToken{}); err == nil {
		t.Error("expected error saving empty token")
	}
	if err := SavePersistedToken(nil); err == nil {
		t.Error("expected error saving nil token")
	}
}

func TestDeletePersistedTokenIdempotent(t *testing.T) {
	withTempHome(t)
	if err := DeletePersistedToken(); err != nil {
		t.Errorf("delete on missing file should be a no-op, got %v", err)
	}
	if err := SavePersistedToken(&PersistedToken{AccessToken: "gho_x"}); err != nil {
		t.Fatal(err)
	}
	if err := DeletePersistedToken(); err != nil {
		t.Errorf("delete failed: %v", err)
	}
	if _, err := LoadPersistedToken(); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("after delete, expected ErrNotLoggedIn, got %v", err)
	}
}

func TestAPITokenIsExpired(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if !(APIToken{}).IsExpired(0) {
			t.Error("empty token should be expired")
		}
	})
	t.Run("future", func(t *testing.T) {
		tok := APIToken{Value: "x", ExpiresAt: time.Now().Add(10 * time.Minute)}
		if tok.IsExpired(0) {
			t.Error("future token should not be expired")
		}
	})
	t.Run("slack triggers refresh", func(t *testing.T) {
		tok := APIToken{Value: "x", ExpiresAt: time.Now().Add(30 * time.Second)}
		if !tok.IsExpired(60 * time.Second) {
			t.Error("token within slack window should be considered expired")
		}
	})
	t.Run("past", func(t *testing.T) {
		tok := APIToken{Value: "x", ExpiresAt: time.Now().Add(-time.Second)}
		if !tok.IsExpired(0) {
			t.Error("past token should be expired")
		}
	})
}
