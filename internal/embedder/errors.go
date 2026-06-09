/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package embedder

import (
	"errors"
	"fmt"
)

// ContextLengthError is returned when input exceeds the model's context window.
type ContextLengthError struct {
	MaxTokens      int
	RequestTokens  int
	ChunkIndex     int
	ProviderDetail string
}

func (e *ContextLengthError) Error() string {
	if e.MaxTokens > 0 {
		return fmt.Sprintf("input exceeds context length: %d tokens requested, max %d (chunk %d): %s",
			e.RequestTokens, e.MaxTokens, e.ChunkIndex, e.ProviderDetail)
	}
	return fmt.Sprintf("input exceeds context length (chunk %d): %s", e.ChunkIndex, e.ProviderDetail)
}

// NewContextLengthError creates a new ContextLengthError.
func NewContextLengthError(maxTokens, requestTokens, chunkIndex int, detail string) *ContextLengthError {
	return &ContextLengthError{
		MaxTokens:      maxTokens,
		RequestTokens:  requestTokens,
		ChunkIndex:     chunkIndex,
		ProviderDetail: detail,
	}
}

// AsContextLengthError extracts a *ContextLengthError from err if present.
func AsContextLengthError(err error) *ContextLengthError {
	var ctxErr *ContextLengthError
	if errors.As(err, &ctxErr) {
		return ctxErr
	}
	return nil
}
