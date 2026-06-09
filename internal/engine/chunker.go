/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	DefaultChunkSize    = 512    // tokens
	DefaultChunkOverlap = 50     // tokens
	CharsPerToken       = 4      // rough approximation: 4 chars ≈ 1 token for code
	MinChunkChars       = 16     // minimum characters for a chunk to be meaningful
	MaxFileSizeForAST   = 500000 // 500KB — skip AST for very large files
)

// ChunkInfo represents a chunk of source code ready for embedding.
type ChunkInfo struct {
	ID           string
	FilePath     string
	StartLine    int
	EndLine      int
	Content      string // display content (includes file path header)
	EmbedContent string // content sent to embedder
	Hash         string
	ContentHash  string // SHA256 of raw content (path-independent)
	SymbolName   string // if this chunk corresponds to a named symbol
}

// Chunker splits source files into embeddable chunks.
// It prefers splitting on AST-level boundaries (function/class definitions)
// and falls back to line-boundary splitting when AST parsing is unavailable.
type Chunker struct {
	chunkSize int // in tokens
	overlap   int // in tokens
}

// NewChunker creates a chunker with the given size and overlap (in tokens).
func NewChunker(chunkSize, overlap int) *Chunker {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	if overlap < 0 {
		overlap = DefaultChunkOverlap
	}
	if overlap >= chunkSize {
		overlap = chunkSize / 10
	}
	return &Chunker{chunkSize: chunkSize, overlap: overlap}
}

// Chunk splits content into chunks using line-boundary splitting.
// It tries to break at blank lines or function boundaries.
func (c *Chunker) Chunk(filePath string, content string) []ChunkInfo {
	if len(strings.TrimSpace(content)) == 0 {
		return nil
	}

	maxChars := c.chunkSize * CharsPerToken
	overlapChars := c.overlap * CharsPerToken

	lines := strings.SplitAfter(content, "\n")

	var chunks []ChunkInfo
	chunkIdx := 0
	lineIdx := 0

	for lineIdx < len(lines) {
		// Accumulate lines up to maxChars
		var builder strings.Builder
		startLineNum := lineIdx + 1
		charCount := 0

		for lineIdx < len(lines) && charCount+len(lines[lineIdx]) <= maxChars {
			builder.WriteString(lines[lineIdx])
			charCount += len(lines[lineIdx])
			lineIdx++
		}

		// If we couldn't fit even one line, force-take it (very long line)
		if builder.Len() == 0 && lineIdx < len(lines) {
			builder.WriteString(lines[lineIdx])
			lineIdx++
		}

		chunkText := builder.String()
		if len(strings.TrimSpace(chunkText)) < MinChunkChars {
			continue
		}

		endLineNum := startLineNum + strings.Count(chunkText, "\n")
		if strings.HasSuffix(chunkText, "\n") {
			endLineNum--
		}
		if endLineNum < startLineNum {
			endLineNum = startLineNum
		}

		embedContent := fmt.Sprintf("File: %s\n\n%s", filePath, chunkText)

		rawHash := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d:%s", filePath, startLineNum, endLineNum, chunkText)))
		contentHash := sha256.Sum256([]byte(chunkText))
		chunkID := fmt.Sprintf("%s_%d", filePath, chunkIdx)

		chunks = append(chunks, ChunkInfo{
			ID:           chunkID,
			FilePath:     filePath,
			StartLine:    startLineNum,
			EndLine:      endLineNum,
			Content:      chunkText,
			EmbedContent: embedContent,
			Hash:         hex.EncodeToString(rawHash[:8]),
			ContentHash:  hex.EncodeToString(contentHash[:]),
		})

		chunkIdx++

		// Back up by overlap amount (in lines)
		if overlapChars > 0 && lineIdx < len(lines) {
			backupChars := 0
			backupLines := 0
			for i := lineIdx - 1; i >= 0 && backupChars < overlapChars; i-- {
				backupChars += len(lines[i])
				backupLines++
			}
			if backupLines > 0 && lineIdx-backupLines > (startLineNum-1) {
				lineIdx -= backupLines
			}
		}
	}

	return chunks
}

// ChunkBySymbols splits content into chunks based on symbol boundaries.
// Each symbol (function, type, etc.) becomes its own chunk. Content between
// symbols is grouped into "interstitial" chunks. Falls back to Chunk() if
// no symbols are provided.
func (c *Chunker) ChunkBySymbols(filePath string, content string, symbols []SymbolBoundary) []ChunkInfo {
	if len(symbols) == 0 {
		return c.Chunk(filePath, content)
	}

	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	maxChars := c.chunkSize * CharsPerToken

	var chunks []ChunkInfo
	chunkIdx := 0
	lastEnd := 0 // last line consumed (0-indexed)

	for _, sym := range symbols {
		startIdx := sym.StartLine - 1 // convert to 0-indexed
		endIdx := sym.EndLine - 1

		if startIdx < 0 {
			startIdx = 0
		}
		if endIdx >= totalLines {
			endIdx = totalLines - 1
		}
		if startIdx > endIdx {
			continue
		}

		// Interstitial content between last symbol and this one
		if startIdx > lastEnd {
			interLines := lines[lastEnd:startIdx]
			interText := strings.Join(interLines, "\n")
			if len(strings.TrimSpace(interText)) >= MinChunkChars {
				chunks = append(chunks, c.makeChunk(filePath, interText, lastEnd+1, startIdx, &chunkIdx, "")...)
			}
		}

		// The symbol itself
		symLines := lines[startIdx : endIdx+1]
		symText := strings.Join(symLines, "\n")

		if len(symText) <= maxChars {
			// Fits in one chunk
			embedContent := fmt.Sprintf("File: %s\nSymbol: %s\n\n%s", filePath, sym.Name, symText)
			rawHash := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d:%s", filePath, sym.StartLine, sym.EndLine, symText)))
			contentHash := sha256.Sum256([]byte(symText))

			chunks = append(chunks, ChunkInfo{
				ID:           fmt.Sprintf("%s_%d", filePath, chunkIdx),
				FilePath:     filePath,
				StartLine:    sym.StartLine,
				EndLine:      sym.EndLine,
				Content:      symText,
				EmbedContent: embedContent,
				Hash:         hex.EncodeToString(rawHash[:8]),
				ContentHash:  hex.EncodeToString(contentHash[:]),
				SymbolName:   sym.Name,
			})
			chunkIdx++
		} else {
			// Symbol is too large — sub-chunk it
			subChunks := c.makeChunk(filePath, symText, sym.StartLine, sym.EndLine, &chunkIdx, sym.Name)
			chunks = append(chunks, subChunks...)
		}

		lastEnd = endIdx + 1
	}

	// Trailing content after last symbol
	if lastEnd < totalLines {
		trailLines := lines[lastEnd:]
		trailText := strings.Join(trailLines, "\n")
		if len(strings.TrimSpace(trailText)) >= MinChunkChars {
			chunks = append(chunks, c.makeChunk(filePath, trailText, lastEnd+1, totalLines, &chunkIdx, "")...)
		}
	}

	return chunks
}

// makeChunk creates one or more chunks from a text block, sub-splitting if needed.
func (c *Chunker) makeChunk(filePath, text string, startLine, endLine int, chunkIdx *int, symbolName string) []ChunkInfo {
	maxChars := c.chunkSize * CharsPerToken

	if len(text) <= maxChars {
		header := fmt.Sprintf("File: %s", filePath)
		if symbolName != "" {
			header += fmt.Sprintf("\nSymbol: %s", symbolName)
		}
		embedContent := fmt.Sprintf("%s\n\n%s", header, text)
		rawHash := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d:%s", filePath, startLine, endLine, text)))
		contentHash := sha256.Sum256([]byte(text))

		ci := ChunkInfo{
			ID:           fmt.Sprintf("%s_%d", filePath, *chunkIdx),
			FilePath:     filePath,
			StartLine:    startLine,
			EndLine:      endLine,
			Content:      text,
			EmbedContent: embedContent,
			Hash:         hex.EncodeToString(rawHash[:8]),
			ContentHash:  hex.EncodeToString(contentHash[:]),
			SymbolName:   symbolName,
		}
		*chunkIdx++
		return []ChunkInfo{ci}
	}

	// Sub-split using the basic line-based chunker
	subChunker := NewChunker(c.chunkSize/2, c.overlap/2)
	subChunks := subChunker.Chunk(filePath, text)

	// Adjust line numbers relative to the original file
	textLines := strings.Split(text, "\n")
	_ = textLines

	var result []ChunkInfo
	for i := range subChunks {
		subChunks[i].ID = fmt.Sprintf("%s_%d", filePath, *chunkIdx)
		// Adjust start/end lines to be absolute
		subChunks[i].StartLine += startLine - 1
		subChunks[i].EndLine += startLine - 1
		subChunks[i].SymbolName = symbolName
		result = append(result, subChunks[i])
		*chunkIdx++
	}
	return result
}

// SymbolBoundary marks the line range of a named code symbol.
type SymbolBoundary struct {
	Name      string
	Kind      string // function, class, type, method, etc.
	StartLine int    // 1-indexed
	EndLine   int    // 1-indexed, inclusive
}

// ChunkSize returns the configured chunk size in tokens.
func (c *Chunker) ChunkSize() int { return c.chunkSize }

// Overlap returns the configured overlap in tokens.
func (c *Chunker) Overlap() int { return c.overlap }

// buildLineStarts returns byte offsets of each line start.
func buildLineStarts(content string) []int {
	starts := []int{0}
	for i, r := range content {
		if r == '\n' && i+1 < len(content) {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// alignRuneBoundary adjusts a byte offset to the start of the next valid UTF-8 rune.
func alignRuneBoundary(content string, pos int) int {
	for pos < len(content) && !utf8.RuneStart(content[pos]) {
		pos++
	}
	return pos
}
