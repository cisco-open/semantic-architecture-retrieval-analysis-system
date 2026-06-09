/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cfg

import (
	"context"
	"fmt"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/trace"
)

// SelectOptions, Candidate, and AmbiguousFunctionError are aliases for the
// generalised primitives in `internal/trace`. Keeping cfg-flavoured names
// preserves source-compat for existing callers (`*cfg.AmbiguousFunctionError`
// still resolves via `errors.As`) while letting `trace`, `cfg`, and any
// future command share the same disambiguation logic.
//
// Use trace.SelectOptions / trace.Candidate / trace.AmbiguousSymbolError
// directly for new non-CFG callers — the cfg names are intentionally
// scoped to the function-resolution use case.

// SelectOptions narrows a function-name lookup. See trace.SelectOptions.
type SelectOptions = trace.SelectOptions

// Candidate describes a single match. See trace.Candidate.
type Candidate = trace.Candidate

// AmbiguousFunctionError is the cfg-specific spelling of
// trace.AmbiguousSymbolError. The "function" subject is set by
// FindFunctionSymbols / SelectOne so the error message reads naturally
// in cfg contexts ("function %q is ambiguous").
type AmbiguousFunctionError = trace.AmbiguousSymbolError

// FindFunctionSymbols returns every function/method whose name matches
// `name`, filtered by `opts` and post-filtered to drop matches in
// languages with no CFG strategy (markup/config). Results are
// stable-sorted by (file, line) so the same input produces the same
// output across runs.
func FindFunctionSymbols(
	ctx context.Context,
	projectRoot string,
	ignore []string,
	name string,
	opts SelectOptions,
) ([]Candidate, error) {
	tracer := trace.NewTracer(projectRoot, ignore)

	// Pin the lookup to function/method symbols. Callers can still set
	// File/Language/Parent on opts; we OR-merge the kinds in.
	scoped := opts
	scoped.Kinds = []trace.SymbolKind{trace.KindFunction, trace.KindMethod}

	cands, err := tracer.FindCandidates(ctx, name, scoped)
	if err != nil {
		return nil, err
	}

	// When the user didn't pin a language, hide candidates whose host
	// language has no registered CFG strategy — markup/config matches
	// would only confuse the user since the cfg builder cannot turn
	// them into a graph anyway.
	if opts.Language == "" {
		filtered := cands[:0]
		for _, c := range cands {
			if StyleForLanguage(c.Language) != StyleUnsupported {
				filtered = append(filtered, c)
			}
		}
		cands = filtered
	}
	return cands, nil
}

// SelectOne returns the unique Candidate or surfaces a precise error.
// On multiple matches it returns *AmbiguousFunctionError with the
// "function" subject set so the rendered message reads naturally.
func SelectOne(name string, cands []Candidate, opts SelectOptions) (Candidate, error) {
	c, err := trace.SelectOne("function", name, cands, opts)
	if err != nil {
		return c, err
	}
	if c.Symbol.EndLine <= 0 || c.Symbol.EndLine < c.Symbol.Line {
		return c, fmt.Errorf("function %q has no usable line range", name)
	}
	return c, nil
}
