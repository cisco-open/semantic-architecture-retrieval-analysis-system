/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package ask

import (
	"testing"
)

func TestIsTokenLimitError_OpenAI(t *testing.T) {
	body := []byte(`{"error":{"message":"prompt token count of 13435 exceeds the limit of 12288","code":"model_max_prompt_tokens_exceeded"}}`)
	tle := isTokenLimitError(400, body)
	if tle == nil {
		t.Fatal("expected TokenLimitError, got nil")
	}
	if tle.PromptLen != 13435 {
		t.Errorf("expected PromptLen 13435, got %d", tle.PromptLen)
	}
	if tle.Limit != 12288 {
		t.Errorf("expected Limit 12288, got %d", tle.Limit)
	}
}

func TestIsTokenLimitError_ContextLength(t *testing.T) {
	body := []byte(`{"error":{"message":"This model's maximum context length is 8192 tokens","type":"invalid_request_error"}}`)
	tle := isTokenLimitError(400, body)
	if tle == nil {
		t.Fatal("expected TokenLimitError, got nil")
	}
	if tle.Limit == 0 && tle.PromptLen == 0 {
		t.Error("expected at least one numeric field to be extracted")
	}
}

func TestIsTokenLimitError_Ollama(t *testing.T) {
	body := []byte(`{"error":"context length exceeded: prompt is too long"}`)
	tle := isTokenLimitError(400, body)
	if tle == nil {
		t.Fatal("expected TokenLimitError, got nil")
	}
}

func TestIsTokenLimitError_NonMatch(t *testing.T) {
	body := []byte(`{"error":{"message":"invalid api key","code":"invalid_api_key"}}`)
	tle := isTokenLimitError(400, body)
	if tle != nil {
		t.Errorf("expected nil for non-token-limit error, got %+v", tle)
	}
}

func TestIsTokenLimitError_Non400(t *testing.T) {
	body := []byte(`{"error":{"message":"prompt token count exceeds the limit"}}`)
	tle := isTokenLimitError(500, body)
	if tle != nil {
		t.Errorf("expected nil for status 500, got %+v", tle)
	}
}

func TestTokenLimitError_Error(t *testing.T) {
	tle := &TokenLimitError{StatusCode: 400, PromptLen: 13435, Limit: 12288}
	msg := tle.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
	if !contains(msg, "13435") || !contains(msg, "12288") {
		t.Errorf("expected numbers in error message, got: %s", msg)
	}
}

func TestTokenLimitError_ErrorNoNumbers(t *testing.T) {
	tle := &TokenLimitError{StatusCode: 400}
	msg := tle.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
