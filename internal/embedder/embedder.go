/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package embedder

import "context"

// Embedder converts text into vector embeddings for semantic search.
type Embedder interface {
	// Embed returns the vector embedding for a single text input.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch returns vector embeddings for multiple text inputs.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the dimensionality of the embedding vectors produced.
	Dimensions() int

	// Close releases any resources held by the embedder.
	Close() error
}
