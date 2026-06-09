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
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// preSeed primes the TokenSource cache with a known Copilot API token so
// tests can run without contacting GitHub.
func preSeed(s *TokenSource, value string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = APIToken{Value: value, ExpiresAt: time.Now().Add(ttl)}
}

func TestTransportInjectsRequiredHeaders(t *testing.T) {
	var captured http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	src := NewTokenSourceWithOAuth("gho_unused")
	preSeed(src, "copilot_xyz", time.Hour)

	client := &http.Client{Transport: NewTransport(src, nil)}
	req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	if got := captured.Get("Authorization"); got != "Bearer copilot_xyz" {
		t.Errorf("Authorization = %q", got)
	}
	wantHeaders := map[string]string{
		"Editor-Version":         EditorVersion,
		"Editor-Plugin-Version":  EditorPluginVersion,
		"Copilot-Integration-Id": IntegrationID,
		"User-Agent":             UserAgent,
		"OpenAI-Intent":          "conversation-panel",
	}
	for h, want := range wantHeaders {
		if got := captured.Get(h); got != want {
			t.Errorf("header %s = %q, want %q", h, got, want)
		}
	}
}

func TestTransportPreservesUserHeaders(t *testing.T) {
	var captured http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	src := NewTokenSourceWithOAuth("gho_unused")
	preSeed(src, "copilot_xyz", time.Hour)

	client := &http.Client{Transport: NewTransport(src, nil)}
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("User-Agent", "custom-ua/1.0")
	req.Header.Set("OpenAI-Intent", "test-intent")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	if got := captured.Get("User-Agent"); got != "custom-ua/1.0" {
		t.Errorf("User-Agent should be preserved when caller sets it; got %q", got)
	}
	if got := captured.Get("OpenAI-Intent"); got != "test-intent" {
		t.Errorf("OpenAI-Intent should be preserved when caller sets it; got %q", got)
	}
}

func TestTransportInvalidatesOn401(t *testing.T) {
	var apiCalls int32
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&apiCalls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(apiSrv.Close)

	// Stand up a fake GitHub copilot token endpoint that hands out a fresh
	// API token on demand. This lets the Transport go through its real
	// invalidate-and-refresh path on 401.
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(apiTokenResponse{
			Token:     "refreshed",
			ExpiresAt: time.Now().Add(25 * time.Minute).Unix(),
		})
	}))
	t.Cleanup(tokenSrv.Close)

	src := &TokenSource{
		oauth:    func() (string, error) { return "gho_unused", nil },
		client:   tokenSrv.Client(),
		endpoint: tokenSrv.URL,
	}
	// Pre-seed with a token so the first request goes out without an
	// extra network hop.
	preSeed(src, "initial", time.Hour)

	client := &http.Client{Transport: NewTransport(src, nil)}
	req, _ := http.NewRequest(http.MethodGet, apiSrv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	if atomic.LoadInt32(&apiCalls) != 2 {
		t.Errorf("api server saw %d calls, want 2", apiCalls)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("final status = %d, want 200", resp.StatusCode)
	}

	// The cache should now hold the refreshed token, not the original.
	src.mu.Lock()
	got := src.current.Value
	src.mu.Unlock()
	if got != "refreshed" {
		t.Errorf("expected cached token to be refreshed, got %q", got)
	}
}

func TestTokenSourceUsesCacheWhenFresh(t *testing.T) {
	src := NewTokenSourceWithOAuth("gho_dummy")
	preSeed(src, "cached", 30*time.Minute)

	tok, err := src.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if tok.Value != "cached" {
		t.Errorf("expected cached token, got %q", tok.Value)
	}
}

func TestTokenSourceErrorsWhenNotLoggedIn(t *testing.T) {
	src := NewTokenSourceWithOAuth("")
	if _, err := src.Get(context.Background()); err == nil {
		t.Error("expected error when no OAuth token is configured")
	}
}
