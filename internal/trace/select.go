/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package trace

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/lang"
)

// SelectOptions narrows a symbol-name lookup to a single candidate when
// the name appears more than once in the project. Every field is optional;
// non-empty fields are AND-combined. When the resulting set is empty an
// error is returned; when it contains more than one entry an
// *AmbiguousSymbolError is returned listing every match.
//
// SelectOptions is shared by every saras command that resolves symbols
// from a name — `saras trace`, `saras cfg`, etc. — so users get
// consistent --file/--language/--parent semantics across the CLI.
type SelectOptions struct {
	// File restricts matches to symbols whose FilePath contains this
	// substring. Use a path fragment ("pkg/auth", "math_test.go", …)
	// rather than an absolute path.
	File string
	// Language restricts matches to a specific lang.LanguageParser.Name()
	// — e.g. "python", "go", "ruby". Useful when the same symbol name
	// exists in multiple languages within a polyglot repo.
	Language string
	// Parent restricts matches to a specific receiver / class / module —
	// the value of Symbol.Parent (e.g. a Go receiver type name or a
	// Python class name).
	Parent string
	// Kinds restricts matches to specific SymbolKinds — e.g. functions
	// only, types only. An empty slice means "any kind".
	Kinds []SymbolKind
}

// IsZero reports whether no disambiguators were supplied.
func (o SelectOptions) IsZero() bool {
	return o.File == "" && o.Language == "" && o.Parent == "" && len(o.Kinds) == 0
}

// matchesKind reports whether s.Kind is allowed by o.Kinds. An empty
// Kinds list permits every kind.
func (o SelectOptions) matchesKind(k SymbolKind) bool {
	if len(o.Kinds) == 0 {
		return true
	}
	for _, want := range o.Kinds {
		if k == want {
			return true
		}
	}
	return false
}

// Candidate describes a single symbol match returned by FindCandidates.
// It carries the resolved language name so callers can render
// disambiguation hints without re-resolving.
type Candidate struct {
	Symbol   Symbol
	Language string
}

// String renders a candidate in the canonical disambiguation format
// used throughout saras: `file:startLine[-endLine]  (lang) [parent.name (kind)]`.
func (c Candidate) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s:%d", c.Symbol.FilePath, c.Symbol.Line)
	if c.Symbol.EndLine > c.Symbol.Line {
		fmt.Fprintf(&b, "-%d", c.Symbol.EndLine)
	}
	if c.Language != "" {
		fmt.Fprintf(&b, "  (%s)", c.Language)
	}
	if c.Symbol.Parent != "" {
		fmt.Fprintf(&b, "  [%s.%s %s]", c.Symbol.Parent, c.Symbol.Name, c.Symbol.Kind)
	} else {
		fmt.Fprintf(&b, "  [%s %s]", c.Symbol.Name, c.Symbol.Kind)
	}
	return b.String()
}

// AmbiguousSymbolError is returned when a symbol-name lookup matches
// more than one symbol and the supplied SelectOptions did not narrow
// it to exactly one. The error message lists every candidate with its
// file, line range, language, parent, and kind so the user can re-run
// with the right disambiguator.
//
// Subject lets callers surface a domain-specific noun in the message
// ("function" for `saras cfg`, "symbol" for `saras trace`). When empty,
// the error defaults to "symbol".
type AmbiguousSymbolError struct {
	Subject    string
	Name       string
	Candidates []Candidate
}

func (e *AmbiguousSymbolError) Error() string {
	subject := e.Subject
	if subject == "" {
		subject = "symbol"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %q is ambiguous (%d matches):\n",
		subject, e.Name, len(e.Candidates))
	for i, c := range e.Candidates {
		fmt.Fprintf(&b, "  %d. %s\n", i+1, c.String())
	}
	b.WriteString(
		"\nDisambiguate with one or more of:\n" +
			"  --file <path-substring>   e.g. --file pkg/auth\n" +
			"  --language <name>         e.g. --language python\n" +
			"  --parent <type-or-class>  e.g. --parent UserService\n",
	)
	return b.String()
}

// FindCandidates returns every symbol whose name matches `name`,
// filtered by `opts`. Results are stable-sorted by (file, line) so the
// same input produces the same output across runs.
//
// Identical (file, line, endLine, parent) tuples are de-duplicated —
// some language parsers emit a function and a corresponding method
// entry that point at the same span.
func (t *Tracer) FindCandidates(
	ctx context.Context,
	name string,
	opts SelectOptions,
) ([]Candidate, error) {
	matches, err := t.FindSymbol(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("find symbol: %w", err)
	}

	out := make([]Candidate, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, s := range matches {
		if !opts.matchesKind(s.Kind) {
			continue
		}
		// Symbols without a usable line range can't be disambiguated
		// by file:line and would render confusingly in candidate lists.
		// We keep them only when the user supplied no Kinds filter and
		// no specific disambiguator that depends on line ranges.
		if s.Line <= 0 {
			continue
		}

		key := fmt.Sprintf("%s:%d-%d:%s",
			s.FilePath, s.Line, s.EndLine, s.Parent)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		langName := ""
		if p := lang.ParserForFile(filepath.Join(t.root, s.FilePath)); p != nil {
			langName = p.Name()
		}
		c := Candidate{Symbol: s, Language: langName}

		if opts.File != "" && !strings.Contains(s.FilePath, opts.File) {
			continue
		}
		if opts.Language != "" && !strings.EqualFold(langName, opts.Language) {
			continue
		}
		if opts.Parent != "" && !strings.EqualFold(s.Parent, opts.Parent) {
			continue
		}
		out = append(out, c)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Symbol.FilePath != out[j].Symbol.FilePath {
			return out[i].Symbol.FilePath < out[j].Symbol.FilePath
		}
		return out[i].Symbol.Line < out[j].Symbol.Line
	})
	return out, nil
}

// SelectOne returns the unique Candidate or surfaces a precise error.
// Behaviour matrix:
//
//	0 candidates → fmt.Errorf("%s %q not found in project[…]", subject, name)
//	1 candidate  → return it
//	N candidates → *AmbiguousSymbolError listing every match
//
// `subject` is the noun used in error messages ("function", "symbol").
// When empty it defaults to "symbol". The not-found message advises on
// broadening filters when the user supplied any SelectOptions.
func SelectOne(
	subject, name string,
	cands []Candidate,
	opts SelectOptions,
) (Candidate, error) {
	if subject == "" {
		subject = "symbol"
	}
	switch len(cands) {
	case 0:
		if !opts.IsZero() {
			return Candidate{}, fmt.Errorf(
				"%s %q not found with the supplied filters "+
					"(file=%q, language=%q, parent=%q); try broadening the search",
				subject, name, opts.File, opts.Language, opts.Parent)
		}
		return Candidate{}, fmt.Errorf("%s %q not found in project", subject, name)
	case 1:
		return cands[0], nil
	default:
		return Candidate{}, &AmbiguousSymbolError{
			Subject:    subject,
			Name:       name,
			Candidates: cands,
		}
	}
}
