/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package ask

import (
	"strings"
	"testing"
)

func TestSplitByParagraphs(t *testing.T) {
	text := "Entry A\ndetails\n\nEntry B\ndetails\n\nEntry C"
	parts := SplitByParagraphs(text)
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d: %v", len(parts), parts)
	}
	if !strings.HasPrefix(parts[0], "Entry A") {
		t.Errorf("part 0: %q", parts[0])
	}
	if !strings.HasPrefix(parts[1], "Entry B") {
		t.Errorf("part 1: %q", parts[1])
	}
}

func TestSplitByParagraphs_NoParagraphs(t *testing.T) {
	text := "single block of text with no double newlines"
	parts := SplitByParagraphs(text)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
}

func TestSplitByParagraphs_Empty(t *testing.T) {
	parts := SplitByParagraphs("")
	if len(parts) != 0 {
		t.Fatalf("expected 0 parts, got %d", len(parts))
	}
}

func TestBinarySplit(t *testing.T) {
	text := "first half\n\nsecond half"
	parts := BinarySplit(text)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d: %v", len(parts), parts)
	}
	if parts[0] == "" || parts[1] == "" {
		t.Errorf("expected non-empty halves: %v", parts)
	}
}

func TestBinarySplit_NoNewlines(t *testing.T) {
	text := strings.Repeat("a", 100)
	parts := BinarySplit(text)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	total := len(parts[0]) + len(parts[1])
	if total != 100 {
		t.Errorf("expected combined length 100, got %d", total)
	}
}

func TestSplitContext_Paragraphs(t *testing.T) {
	// Build content with clear paragraph breaks.
	var b strings.Builder
	for i := 0; i < 5; i++ {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.Repeat("x", 300))
	}
	text := b.String()

	chunks := SplitContext(text, 0)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	// Verify all content is preserved.
	combined := strings.Join(chunks, "\n\n")
	// Content should be roughly equivalent (whitespace normalization OK).
	if len(combined) < len(text)/2 {
		t.Errorf("content seems lost: combined=%d, original=%d", len(combined), len(text))
	}
}

func TestSplitContext_FallbackBinary(t *testing.T) {
	// Single block with no paragraph breaks.
	text := strings.Repeat("a\n", 500)
	chunks := SplitContext(text, 0)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks from binary split, got %d", len(chunks))
	}
}

func TestSplitContext_MaxRecursion(t *testing.T) {
	text := "unsplittable"
	chunks := SplitContext(text, maxChunkRecursion)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk at max recursion, got %d", len(chunks))
	}
}

func TestBuildChunkUserMessage_FirstChunk(t *testing.T) {
	msg := buildChunkUserMessage(0, 3, "data chunk 1", "", "what does this code do?")
	if !strings.Contains(msg, "PART 1 of 3") {
		t.Error("expected part numbering")
	}
	if strings.Contains(msg, "Summary of prior parts") {
		t.Error("first chunk should not have prior summary")
	}
	if !strings.Contains(msg, "data chunk 1") {
		t.Error("expected chunk data")
	}
	if !strings.Contains(msg, "what does this code do?") {
		t.Error("expected question")
	}
}

func TestBuildChunkUserMessage_WithPrior(t *testing.T) {
	msg := buildChunkUserMessage(1, 3, "data chunk 2", "prior analysis", "the question")
	if !strings.Contains(msg, "PART 2 of 3") {
		t.Error("expected part numbering")
	}
	if !strings.Contains(msg, "prior analysis") {
		t.Error("expected prior summary")
	}
	if !strings.Contains(msg, "data chunk 2") {
		t.Error("expected chunk data")
	}
}

func TestBuildFinalChunkUserMessage(t *testing.T) {
	msg := buildFinalChunkUserMessage("last chunk data", "accumulated summary", "the question", 3)
	if !strings.Contains(msg, "FINAL PART") {
		t.Error("expected FINAL PART marker")
	}
	if !strings.Contains(msg, "accumulated summary") {
		t.Error("expected prior summary")
	}
	if !strings.Contains(msg, "last chunk data") {
		t.Error("expected chunk data")
	}
	if !strings.Contains(msg, "COMPLETE final answer") {
		t.Error("expected synthesis instruction")
	}
}

func TestBuildFinalChunkUserMessage_NoPrior(t *testing.T) {
	msg := buildFinalChunkUserMessage("only chunk", "", "q", 1)
	if strings.Contains(msg, "Summary of all prior") {
		t.Error("should not mention prior summary when empty")
	}
}

func TestEstimateTokens(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: strings.Repeat("a", 400)},
		{Role: "user", Content: strings.Repeat("b", 1600)},
	}
	est := EstimateTokens(msgs)
	// (400 + 1600) / 4 = 500
	if est != 500 {
		t.Errorf("expected 500 tokens, got %d", est)
	}
}

func TestEstimateTokens_Empty(t *testing.T) {
	est := EstimateTokens(nil)
	if est != 0 {
		t.Errorf("expected 0 tokens for nil messages, got %d", est)
	}
}
