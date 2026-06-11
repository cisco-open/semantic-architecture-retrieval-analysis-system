/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package ask

import (
	"fmt"
	"strings"
)

// maxChunkRecursion caps the number of times we binary-split a chunk
// that is still too large.
const maxChunkRecursion = 3

// SplitByParagraphs splits text at double-newline boundaries, returning
// non-empty sections. This naturally splits flow trees by entry point and
// CFG sections by block/path boundaries.
func SplitByParagraphs(text string) []string {
	raw := strings.Split(text, "\n\n")
	var parts []string
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

// BinarySplit halves the text at the nearest paragraph boundary to the
// midpoint. If no paragraph boundary exists, it splits at the midpoint
// newline. Returns two non-empty halves.
func BinarySplit(text string) []string {
	mid := len(text) / 2

	// Try to find a paragraph break near the midpoint.
	// Search outward from mid for a "\n\n".
	bestIdx := -1
	for delta := 0; delta < mid; delta++ {
		if mid+delta+1 < len(text) && text[mid+delta] == '\n' && text[mid+delta+1] == '\n' {
			bestIdx = mid + delta
			break
		}
		if mid-delta > 0 && text[mid-delta] == '\n' && text[mid-delta-1] == '\n' {
			bestIdx = mid - delta
			break
		}
	}

	// Fallback: split at nearest newline.
	if bestIdx < 0 {
		for delta := 0; delta < mid; delta++ {
			if mid+delta < len(text) && text[mid+delta] == '\n' {
				bestIdx = mid + delta
				break
			}
			if mid-delta >= 0 && text[mid-delta] == '\n' {
				bestIdx = mid - delta
				break
			}
		}
	}

	// Last resort: split at exact midpoint.
	if bestIdx < 0 {
		bestIdx = mid
	}

	left := strings.TrimSpace(text[:bestIdx])
	right := strings.TrimSpace(text[bestIdx:])
	if left == "" {
		return []string{right}
	}
	if right == "" {
		return []string{left}
	}
	return []string{left, right}
}

// SplitContext splits a context string into chunks suitable for serial
// LLM calls. It first tries paragraph-based splitting; if that produces
// only a single chunk (unsplittable by paragraphs), it falls back to
// binary splitting. The depth parameter prevents infinite recursion.
func SplitContext(text string, depth int) []string {
	if depth >= maxChunkRecursion {
		return []string{text}
	}

	parts := SplitByParagraphs(text)
	if len(parts) <= 1 {
		// Cannot split by paragraphs — binary split.
		halves := BinarySplit(text)
		if len(halves) <= 1 {
			return halves
		}
		// Recursively try to split each half if needed.
		return halves
	}

	// Merge tiny adjacent paragraphs to avoid too many chunks.
	// Target: each chunk should be at least 1/4 of the original text.
	minChunkLen := len(text) / 8
	if minChunkLen < 200 {
		minChunkLen = 200
	}

	var merged []string
	var current strings.Builder
	for _, p := range parts {
		if current.Len() > 0 && current.Len()+len(p) > minChunkLen {
			merged = append(merged, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(p)
	}
	if current.Len() > 0 {
		merged = append(merged, current.String())
	}

	if len(merged) <= 1 {
		// Merging collapsed everything — binary split instead.
		return BinarySplit(text)
	}

	return merged
}

// EstimateTokens returns a rough token count for a set of messages using
// a ~4 chars/token heuristic. This is intentionally conservative — it's
// used only to avoid obviously-doomed requests, not for precise billing.
func EstimateTokens(messages []Message) int {
	total := 0
	for _, m := range messages {
		total += len(m.Content)
	}
	return total / 4
}

// buildChunkUserMessage builds the user message for chunk i of n in the
// sliding-window protocol. priorSummary is the LLM's response to the
// previous chunk (empty for the first chunk).
func buildChunkUserMessage(i, n int, chunk, priorSummary, question string) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("The full context was too large to process at once. You are receiving it in %d parts.\n", n))
	b.WriteString(fmt.Sprintf("This is PART %d of %d.\n\n", i+1, n))

	if priorSummary != "" {
		b.WriteString("Summary of prior parts:\n")
		b.WriteString(priorSummary)
		b.WriteString("\n\n---\nNew data for this part:\n")
	}

	b.WriteString(chunk)
	b.WriteString("\n\nOriginal question: ")
	b.WriteString(question)

	if i < n-1 {
		b.WriteString("\n\nAnalyze THIS portion, building on any prior summary. Be thorough but concise — your response will be carried forward as context for the next part.")
	}

	return b.String()
}

// buildFinalChunkUserMessage builds the user message for the last chunk
// in the sliding window. It instructs the LLM to produce the complete
// final answer.
func buildFinalChunkUserMessage(chunk, priorSummary, question string, n int) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("This is the FINAL PART (%d of %d).\n\n", n, n))

	if priorSummary != "" {
		b.WriteString("Summary of all prior parts:\n")
		b.WriteString(priorSummary)
		b.WriteString("\n\n---\nFinal data:\n")
	}

	b.WriteString(chunk)
	b.WriteString("\n\nOriginal question: ")
	b.WriteString(question)
	b.WriteString("\n\nProvide your COMPLETE final answer, synthesizing the analysis of all parts into one cohesive response.")

	return b.String()
}
