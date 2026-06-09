/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package copilot

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// EditorVersion identifies the editor making the request. GitHub's
	// Copilot gateway uses this to enable / disable features per editor.
	// Saras impersonates a recent Neovim Copilot client, the same value
	// used by other CLI integrations.
	EditorVersion = "Neovim/0.9.0"

	// EditorPluginVersion identifies the plugin / SDK version.
	EditorPluginVersion = "copilot.vim/1.16.0"

	// IntegrationID tells the Copilot backend which client experience is
	// invoking the API ("vscode-chat" enables the chat models endpoint).
	IntegrationID = "vscode-chat"

	// DefaultEndpoint is the base URL of the public GitHub Copilot API.
	// Unlike OpenAI it does NOT use a /v1 prefix; callers should append
	// /chat/completions or /embeddings directly.
	DefaultEndpoint = "https://api.githubcopilot.com"
)

// Transport is an http.RoundTripper that authenticates outgoing requests to
// the GitHub Copilot API. It:
//
//   - obtains and caches a short-lived Copilot API token via TokenSource
//   - injects Authorization: Bearer <token>
//   - injects the Copilot-required Editor-Version / Copilot-Integration-Id /
//     Editor-Plugin-Version / User-Agent headers
//   - on a single 401 response, invalidates the cached token and retries
//     once (handles tokens that expire mid-request)
//
// The wrapped transport defaults to http.DefaultTransport.
type Transport struct {
	source   *TokenSource
	base     http.RoundTripper
	intentID string
}

// NewTransport wraps base with Copilot authentication. If base is nil,
// http.DefaultTransport is used. Pass nil for source to construct a
// TokenSource that reads the persisted OAuth token on demand.
func NewTransport(source *TokenSource, base http.RoundTripper) *Transport {
	if source == nil {
		source = NewTokenSource()
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return &Transport{source: source, base: base, intentID: IntegrationID}
}

// WithIntegrationID overrides the Copilot-Integration-Id header value.
func (t *Transport) WithIntegrationID(id string) *Transport {
	if id == "" {
		return t
	}
	clone := *t
	clone.intentID = id
	return &clone
}

// Source exposes the underlying token source. Useful for callers that want
// to pre-warm the cache (e.g. `saras copilot status`).
func (t *Transport) Source() *TokenSource { return t.source }

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request so we can safely mutate headers and (if needed)
	// retry with a refreshed token.
	first := cloneRequest(req)
	if err := t.authorize(first); err != nil {
		return nil, err
	}

	resp, err := t.base.RoundTrip(first)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	// 401: token may have expired or been revoked. Drain & close the
	// response body, invalidate the cache, then retry exactly once.
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	t.source.Invalidate()

	second := cloneRequest(req)
	if err := t.authorize(second); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(second)
}

func (t *Transport) authorize(req *http.Request) error {
	tok, err := t.source.Get(req.Context())
	if err != nil {
		return fmt.Errorf("copilot auth: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok.Value)
	if req.Header.Get("Editor-Version") == "" {
		req.Header.Set("Editor-Version", EditorVersion)
	}
	if req.Header.Get("Editor-Plugin-Version") == "" {
		req.Header.Set("Editor-Plugin-Version", EditorPluginVersion)
	}
	if req.Header.Get("Copilot-Integration-Id") == "" {
		req.Header.Set("Copilot-Integration-Id", t.intentID)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", UserAgent)
	}
	if req.Header.Get("OpenAI-Intent") == "" {
		req.Header.Set("OpenAI-Intent", "conversation-panel")
	}
	return nil
}

// cloneRequest returns a deep-enough copy of req that header mutations on
// the clone do not affect the original. The body is shared; callers that
// need to retry POSTs must set req.GetBody so net/http can re-read it
// (which net/http does for byte/strings.Reader-backed bodies by default).
func cloneRequest(req *http.Request) *http.Request {
	r2 := req.Clone(req.Context())
	if req.Body != nil && req.GetBody != nil {
		body, err := req.GetBody()
		if err == nil {
			r2.Body = body
		}
	}
	return r2
}

// NewHTTPClient returns an *http.Client wired to a Copilot transport with a
// sensible default timeout. Pass nil source to use the persisted OAuth
// token. The timeout should be generous because LLM responses can take a
// long time to stream.
func NewHTTPClient(source *TokenSource, timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	return &http.Client{
		Transport: NewTransport(source, nil),
		Timeout:   timeout,
	}
}
