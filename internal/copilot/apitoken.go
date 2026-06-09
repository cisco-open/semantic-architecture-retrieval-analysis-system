/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// CopilotTokenEndpoint is the GitHub endpoint that exchanges a user OAuth
// token for a short-lived Copilot API token. The endpoint is well-known and
// used by every public Copilot editor integration.
const CopilotTokenEndpoint = "https://api.github.com/copilot_internal/v2/token"

// UserAgent is sent on every Copilot-related request. GitHub's gateway
// validates that this looks like a known editor client.
const UserAgent = "saras-cli/1.0 (https://github.com/cisco-open/semantic-architecture-retrieval-analysis-system)"

// apiTokenResponse models the JSON returned by the Copilot token endpoint.
type apiTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

// APIToken is a short-lived Copilot API token plus its expiry.
type APIToken struct {
	Value     string
	ExpiresAt time.Time
}

// IsExpired reports whether the token has expired or will expire within the
// given slack window. We refresh proactively to avoid mid-request 401s.
func (t APIToken) IsExpired(slack time.Duration) bool {
	if t.Value == "" {
		return true
	}
	return time.Now().Add(slack).After(t.ExpiresAt)
}

// TokenSource fetches and caches Copilot API tokens. Callers obtain a token
// via Get; the source transparently refreshes when the current token is
// near expiry. All operations are safe for concurrent use.
//
// The cache is two-tier:
//   - in-memory (the `current` field): hot path within a single process
//   - on-disk (PersistedAPIToken, see apitoken_cache.go): survives across
//     CLI invocations until the token nears expiry
//
// The on-disk cache is enabled by default and can be disabled per-instance
// via the persist field (used by tests that do not want to touch the user
// config dir).
type TokenSource struct {
	mu       sync.Mutex
	current  APIToken
	oauth    func() (string, error)
	client   *http.Client
	endpoint string // overridable for tests
	persist  bool   // when true, mirror the cache to disk
}

// NewTokenSource constructs a TokenSource that reads the persisted GitHub
// OAuth token at use time. This allows 'saras copilot login' to be run after
// the source has been constructed without recreating dependent objects.
//
// The returned source mirrors its in-memory cache to the per-user config
// directory so refresh is transparent across CLI invocations.
func NewTokenSource() *TokenSource {
	return &TokenSource{
		oauth: func() (string, error) {
			pt, err := LoadPersistedToken()
			if err != nil {
				return "", err
			}
			return pt.AccessToken, nil
		},
		client:   &http.Client{Timeout: 30 * time.Second},
		endpoint: CopilotTokenEndpoint,
		persist:  true,
	}
}

// NewTokenSourceWithOAuth constructs a TokenSource that uses the given
// OAuth token (useful for tests or when the token came from elsewhere).
// Disk persistence is disabled by default so tests do not pollute the real
// user config directory.
func NewTokenSourceWithOAuth(oauthToken string) *TokenSource {
	return &TokenSource{
		oauth: func() (string, error) {
			if oauthToken == "" {
				return "", ErrNotLoggedIn
			}
			return oauthToken, nil
		},
		client:   &http.Client{Timeout: 30 * time.Second},
		endpoint: CopilotTokenEndpoint,
		persist:  false,
	}
}

// refreshSlack is the headroom kept before token expiry. We refresh
// proactively to avoid mid-request 401s and to give long-running streams
// some breathing room.
const refreshSlack = 90 * time.Second

// Get returns a valid Copilot API token, refreshing it if necessary.
//
// Lookup order:
//  1. In-memory cache (fast path, no syscalls)
//  2. On-disk cache (when persist=true) — populated by the previous CLI
//     invocation. The cached token is bound to the OAuth fingerprint so
//     switching accounts invalidates it automatically.
//  3. Remote fetch via fetchAPIToken using the current OAuth token. The
//     fresh token is written back to both caches.
func (s *TokenSource) Get(ctx context.Context) (APIToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Tier 1: in-memory.
	if !s.current.IsExpired(refreshSlack) {
		return s.current, nil
	}

	// Resolve OAuth token first — we'll need it either to validate the
	// on-disk cache (via fingerprint) or to fetch a fresh API token.
	oauthTok, err := s.oauth()
	if err != nil {
		return APIToken{}, err
	}
	wantFP := oauthFingerprint(oauthTok)

	// Tier 2: on-disk.
	if s.persist {
		if cached, lerr := LoadCachedAPIToken(); lerr == nil && cached != nil {
			t := APIToken{Value: cached.Value, ExpiresAt: cached.ExpiresAt}
			// Discard the cache if the OAuth fingerprint changed (user
			// switched accounts) or the token is near expiry.
			fpOK := cached.OAuthFingerprint == "" || cached.OAuthFingerprint == wantFP
			if fpOK && !t.IsExpired(refreshSlack) {
				s.current = t
				return t, nil
			}
			// Stale on-disk entry — delete so we don't try it again.
			_ = DeleteCachedAPIToken()
		}
	}

	// Tier 3: remote fetch.
	endpoint := s.endpoint
	if endpoint == "" {
		endpoint = CopilotTokenEndpoint
	}
	tok, err := fetchAPIToken(ctx, s.client, endpoint, oauthTok)
	if err != nil {
		return APIToken{}, err
	}
	s.current = tok

	if s.persist {
		_ = SaveCachedAPIToken(&PersistedAPIToken{
			Value:            tok.Value,
			ExpiresAt:        tok.ExpiresAt,
			FetchedAt:        time.Now().UTC(),
			OAuthFingerprint: wantFP,
		})
	}
	return tok, nil
}

// Invalidate clears the cached token (in memory AND on disk) so the next
// Get() forces a refresh. Use after a 401 response from the Copilot API.
func (s *TokenSource) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = APIToken{}
	if s.persist {
		_ = DeleteCachedAPIToken()
	}
}

// fetchAPIToken makes a single call to the Copilot token endpoint. The
// endpoint URL is parameterised so tests can substitute a local httptest
// server; production callers should use CopilotTokenEndpoint.
func fetchAPIToken(ctx context.Context, client *http.Client, endpoint, oauthToken string) (APIToken, error) {
	if oauthToken == "" {
		return APIToken{}, ErrNotLoggedIn
	}
	if endpoint == "" {
		endpoint = CopilotTokenEndpoint
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return APIToken{}, fmt.Errorf("build copilot token request: %w", err)
	}
	req.Header.Set("Authorization", "token "+oauthToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Editor-Version", EditorVersion)
	req.Header.Set("Editor-Plugin-Version", EditorPluginVersion)

	resp, err := client.Do(req)
	if err != nil {
		return APIToken{}, fmt.Errorf("fetch copilot api token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return APIToken{}, fmt.Errorf("read copilot api token: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return APIToken{}, fmt.Errorf("github rejected the saved OAuth token (status %d). Run 'saras copilot login' to re-authenticate", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return APIToken{}, fmt.Errorf("copilot token endpoint returned %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var out apiTokenResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return APIToken{}, fmt.Errorf("parse copilot token response: %w", err)
	}
	if out.Token == "" {
		return APIToken{}, errors.New("copilot token endpoint returned no token (your account may not have an active Copilot subscription)")
	}
	if out.ExpiresAt == 0 {
		// Endpoint sometimes omits expires_at; default to 25 minutes.
		out.ExpiresAt = time.Now().Add(25 * time.Minute).Unix()
	}
	return APIToken{
		Value:     out.Token,
		ExpiresAt: time.Unix(out.ExpiresAt, 0),
	}, nil
}
