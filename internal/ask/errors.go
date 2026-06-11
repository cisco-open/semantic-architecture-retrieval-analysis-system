/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package ask

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// TokenLimitError is returned when an LLM rejects a request because the
// prompt token count exceeds the model's maximum context window.
type TokenLimitError struct {
	StatusCode int    // HTTP status (typically 400)
	Limit      int    // reported token limit (0 if not parseable)
	PromptLen  int    // reported prompt token count (0 if not parseable)
	Body       string // raw response body for diagnostics
}

func (e *TokenLimitError) Error() string {
	if e.Limit > 0 && e.PromptLen > 0 {
		return fmt.Sprintf("prompt token count %d exceeds model limit %d", e.PromptLen, e.Limit)
	}
	return fmt.Sprintf("LLM rejected request (status %d): prompt exceeds token limit", e.StatusCode)
}

// tokenCountRe matches patterns like "token count of 13435" or "13435 tokens".
var tokenCountRe = regexp.MustCompile(`(?:token count of |prompt.{0,20}?(\d+)\s*tokens|(\d+)\s*token)`)

// limitRe matches patterns like "limit of 12288" or "maximum of 12288".
var limitRe = regexp.MustCompile(`(?:limit of |maximum of |max.{0,20}?)(\d+)`)

// isTokenLimitError inspects an HTTP status code and response body to
// determine whether the error is a token-limit overflow. Returns nil if
// the error does not match any known pattern.
func isTokenLimitError(status int, body []byte) *TokenLimitError {
	if status != 400 {
		return nil
	}

	bodyStr := strings.ToLower(string(body))

	// Known error code patterns across providers:
	//   OpenAI / GitHub Copilot: "model_max_prompt_tokens_exceeded"
	//   Generic:                 "exceeds the limit"
	//   Ollama:                  "context length exceeded", "too long"
	//   Anthropic:               "prompt is too long"
	patterns := []string{
		"max_prompt_tokens_exceeded",
		"exceeds the limit",
		"context length",
		"too long",
		"prompt is too long",
		"maximum context length",
		"token limit",
		"context_length_exceeded",
	}

	matched := false
	for _, p := range patterns {
		if strings.Contains(bodyStr, p) {
			matched = true
			break
		}
	}
	if !matched {
		return nil
	}

	tle := &TokenLimitError{
		StatusCode: status,
		Body:       string(body),
	}

	// Try to extract numeric details from the body.
	// First attempt: structured JSON with an "error" object.
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
		tle.extractNumbers(errResp.Error.Message)
	}
	if tle.Limit == 0 {
		// Fallback: scan the raw body.
		tle.extractNumbers(string(body))
	}

	return tle
}

// extractNumbers tries to pull prompt-token-count and limit numbers from
// a message string. It is intentionally lenient: any number it finds in
// a "token count of X" or "limit of Y" pattern wins.
func (e *TokenLimitError) extractNumbers(msg string) {
	// Look for all numbers in the message and use heuristics.
	numRe := regexp.MustCompile(`\d+`)
	nums := numRe.FindAllString(msg, -1)

	if len(nums) >= 2 {
		// Common pattern: "prompt token count of X exceeds the limit of Y"
		// The larger number is the prompt length, the smaller is the limit,
		// unless the message explicitly says otherwise.
		a, _ := strconv.Atoi(nums[0])
		b, _ := strconv.Atoi(nums[1])
		if a > b {
			e.PromptLen = a
			e.Limit = b
		} else {
			e.PromptLen = b
			e.Limit = a
		}
		return
	}

	// Single number — try context from surrounding text.
	if len(nums) == 1 {
		n, _ := strconv.Atoi(nums[0])
		lower := strings.ToLower(msg)
		if strings.Contains(lower, "limit") || strings.Contains(lower, "maximum") {
			e.Limit = n
		} else {
			e.PromptLen = n
		}
	}
}
