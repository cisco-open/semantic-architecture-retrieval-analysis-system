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
	"net/url"
	"strings"
	"time"
)

// PublicCopilotClientID is the OAuth App client id used by editor integrations
// (copilot.vim, copilot.lua, etc.) to authenticate against GitHub Copilot.
// It is a PUBLIC identifier — no client secret is required for the device
// flow. Saras uses the same id so that any user with a valid Copilot
// subscription can sign in without registering a new OAuth app.
const PublicCopilotClientID = "Iv1.b507a08c87ecfe98"

const (
	deviceCodeURL  = "https://github.com/login/device/code"
	accessTokenURL = "https://github.com/login/oauth/access_token"
	userURL        = "https://api.github.com/user"

	// OAuth scope required for Copilot access. read:user is enough to query
	// the Copilot internal token endpoint with the resulting OAuth token.
	oauthScope = "read:user"
)

// DeviceCodeResponse is the response from GitHub's device-code endpoint.
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// accessTokenResponse models the JSON returned by /login/oauth/access_token.
type accessTokenResponse struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type gitHubUser struct {
	Login string `json:"login"`
}

// RequestDeviceCode begins a GitHub device-code OAuth flow and returns the
// codes that the caller should display to the user.
func RequestDeviceCode(ctx context.Context) (*DeviceCodeResponse, error) {
	form := url.Values{}
	form.Set("client_id", PublicCopilotClientID)
	form.Set("scope", oauthScope)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceCodeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build device-code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call device-code endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read device-code response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device-code endpoint returned %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var dc DeviceCodeResponse
	if err := json.Unmarshal(body, &dc); err != nil {
		return nil, fmt.Errorf("parse device-code response: %w", err)
	}
	if dc.DeviceCode == "" || dc.UserCode == "" {
		return nil, fmt.Errorf("device-code endpoint returned empty codes")
	}
	if dc.Interval <= 0 {
		dc.Interval = 5
	}
	return &dc, nil
}

// PollAccessToken polls GitHub's access_token endpoint at the recommended
// interval until the user finishes authorization, the code expires, or ctx
// is cancelled. The returned PersistedToken has CreatedAt set; the caller is
// responsible for persisting it.
func PollAccessToken(ctx context.Context, dc *DeviceCodeResponse) (*PersistedToken, error) {
	if dc == nil || dc.DeviceCode == "" {
		return nil, fmt.Errorf("invalid device-code response")
	}

	interval := time.Duration(dc.Interval) * time.Second
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
	if dc.ExpiresIn <= 0 {
		deadline = time.Now().Add(15 * time.Minute)
	}

	client := &http.Client{Timeout: 20 * time.Second}

	for {
		if time.Now().After(deadline) {
			return nil, errors.New("device-code expired before authorization completed; re-run 'saras copilot login'")
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}

		tok, slowDown, err := exchangeDeviceCode(ctx, client, dc.DeviceCode)
		if err != nil {
			return nil, err
		}
		if slowDown {
			interval += 5 * time.Second
			continue
		}
		if tok == nil {
			// authorization_pending — keep polling
			continue
		}

		pt := &PersistedToken{
			AccessToken: tok.AccessToken,
			TokenType:   tok.TokenType,
			Scope:       tok.Scope,
			CreatedAt:   time.Now().UTC(),
		}

		// Best-effort username lookup so 'saras copilot status' can show
		// "logged in as @user". Failure is non-fatal — the token works
		// without it.
		if login, err := fetchGitHubLogin(ctx, tok.AccessToken); err == nil {
			pt.User = login
		}
		return pt, nil
	}
}

// exchangeDeviceCode performs a single POST to /login/oauth/access_token.
// It returns (token, slowDown, err). When the user has not yet authorized,
// it returns (nil, false, nil) so the caller continues polling.
func exchangeDeviceCode(ctx context.Context, client *http.Client, deviceCode string) (*accessTokenResponse, bool, error) {
	form := url.Values{}
	form.Set("client_id", PublicCopilotClientID)
	form.Set("device_code", deviceCode)
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, accessTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, false, fmt.Errorf("build access-token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("call access-token endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, false, fmt.Errorf("read access-token response: %w", err)
	}

	var out accessTokenResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, false, fmt.Errorf("parse access-token response: %w", err)
	}

	if out.AccessToken != "" {
		return &out, false, nil
	}

	switch out.Error {
	case "authorization_pending":
		return nil, false, nil
	case "slow_down":
		return nil, true, nil
	case "expired_token", "access_denied":
		return nil, false, fmt.Errorf("github oauth: %s", nonEmpty(out.ErrorDescription, out.Error))
	default:
		if out.Error != "" {
			return nil, false, fmt.Errorf("github oauth: %s", nonEmpty(out.ErrorDescription, out.Error))
		}
		return nil, false, fmt.Errorf("github oauth returned %d with no token: %s", resp.StatusCode, truncate(string(body), 200))
	}
}

// fetchGitHubLogin returns the @login of the authenticated user.
func fetchGitHubLogin(ctx context.Context, oauthToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+oauthToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", UserAgent)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github /user returned %d", resp.StatusCode)
	}
	var u gitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return "", err
	}
	return u.Login, nil
}

func nonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
