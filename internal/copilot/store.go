/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package copilot

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// PersistedToken is the long-lived GitHub OAuth token saved to disk.
// It grants access to the Copilot API token exchange endpoint.
type PersistedToken struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type,omitempty"`
	Scope       string    `json:"scope,omitempty"`
	User        string    `json:"user,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// ErrNotLoggedIn is returned when no persisted token exists.
var ErrNotLoggedIn = errors.New("not logged in to GitHub Copilot (run 'saras copilot login')")

const (
	tokenFileName = "copilot.json"
	tokenFileMode = 0600 // owner read/write only
	tokenDirMode  = 0700 // owner-only directory
)

// configDir returns the per-user saras config directory used for storing
// secrets that must not live inside the project tree.
//
//	~/.config/saras   (Linux / generic Unix)
//	~/Library/Application Support/saras  (macOS, via os.UserConfigDir)
//	%AppData%\saras   (Windows, via os.UserConfigDir)
//
// The directory is created with 0700 to prevent other local users from
// reading the stored OAuth token.
func configDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("locate user config dir: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	dir := filepath.Join(base, "saras")
	if err := os.MkdirAll(dir, tokenDirMode); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	// Tighten permissions if the directory pre-existed with wider mode.
	if runtime.GOOS != "windows" {
		_ = os.Chmod(dir, tokenDirMode)
	}
	return dir, nil
}

// TokenPath returns the absolute path where the GitHub OAuth token is stored.
func TokenPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, tokenFileName), nil
}

// LoadPersistedToken reads the saved GitHub OAuth token. Returns
// ErrNotLoggedIn if the file is missing.
func LoadPersistedToken() (*PersistedToken, error) {
	path, err := TokenPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotLoggedIn
		}
		return nil, fmt.Errorf("read copilot token: %w", err)
	}
	var t PersistedToken
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parse copilot token file: %w", err)
	}
	if t.AccessToken == "" {
		return nil, ErrNotLoggedIn
	}
	return &t, nil
}

// SavePersistedToken writes the GitHub OAuth token to disk with 0600
// permissions. The write is atomic (write to temp file then rename) so
// readers never see a partially written file.
func SavePersistedToken(t *PersistedToken) error {
	if t == nil || t.AccessToken == "" {
		return fmt.Errorf("refuse to save empty copilot token")
	}
	path, err := TokenPath()
	if err != nil {
		return err
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal copilot token: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, tokenFileMode); err != nil {
		return fmt.Errorf("write copilot token: %w", err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(tmp, tokenFileMode)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("install copilot token: %w", err)
	}
	return nil
}

// DeletePersistedToken removes the on-disk token. It is safe to call when
// the file does not exist.
func DeletePersistedToken() error {
	path, err := TokenPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove copilot token: %w", err)
	}
	return nil
}
