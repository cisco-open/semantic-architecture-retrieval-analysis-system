/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cfg

import (
	"sort"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/lang"
)

// Style identifies the syntactic family of a language for the purpose of
// building a control flow graph. The heuristic CFG builder runs a different
// front-end parser per style, but each style emits the same `[]*stmt` tree
// consumed by the style-agnostic builder.
type Style int

const (
	// StyleUnsupported indicates the language has no function-level
	// control flow (markup, config, data) and the heuristic builder
	// cannot produce a CFG. Callers should fall back to the LLM-based
	// `cfg explain` command which is language-agnostic.
	StyleUnsupported Style = iota
	// StyleBrace covers C-family languages where blocks are delimited by
	// `{` and `}` — Go, JavaScript, TypeScript, Java, C, C++, C#, Rust,
	// Kotlin, PHP, Perl, Zig, Swift, Scala, Dart, Groovy, Objective-C.
	StyleBrace
	// StyleIndent covers languages where indentation determines block
	// nesting and `:` opens a body — Python (2 & 3).
	StyleIndent
	// StyleEnd covers languages where blocks are terminated by an `end`
	// keyword — Ruby.
	StyleEnd
	// StyleShell covers POSIX-shell-like languages with mixed block
	// terminators (`fi` / `done` / `esac`) — sh, bash, zsh, ksh.
	StyleShell
)

func (s Style) String() string {
	switch s {
	case StyleBrace:
		return "brace"
	case StyleIndent:
		return "indent"
	case StyleEnd:
		return "end"
	case StyleShell:
		return "shell"
	default:
		return "unsupported"
	}
}

// languageStyle maps each language NAME (as returned by lang.Parser.Name())
// to the parsing style used to build a CFG for sources of that language.
//
// Adding a new language to this table is all that's required to wire it up,
// provided a parser for the chosen Style already exists.
var languageStyle = map[string]Style{
	// --- C-family braces ---
	"go":         StyleBrace,
	"javascript": StyleBrace,
	"typescript": StyleBrace,
	"java":       StyleBrace,
	"c":          StyleBrace,
	"cpp":        StyleBrace,
	"csharp":     StyleBrace,
	"rust":       StyleBrace,
	"kotlin":     StyleBrace,
	"php":        StyleBrace,
	"perl":       StyleBrace,
	"zig":        StyleBrace,
	// Names registered by future bundled parsers; harmless if absent.
	"swift":       StyleBrace,
	"scala":       StyleBrace,
	"dart":        StyleBrace,
	"groovy":      StyleBrace,
	"objective-c": StyleBrace,

	// --- Indentation-based ---
	"python":  StyleIndent,
	"python2": StyleIndent,

	// --- End-keyword ---
	"ruby": StyleEnd,

	// --- POSIX-shell-like ---
	"shell": StyleShell,

	// --- SQL — stored procedures use BEGIN/END blocks ---
	"sql": StyleBrace,

	// --- COBOL — paragraph/section based, no brace/indent style ---
	"cobol": StyleUnsupported,

	// --- Declarative / schema / config — no function-level control flow ---
	"cypher":   StyleUnsupported,
	"hcl":      StyleUnsupported,
	"protobuf": StyleUnsupported,

	// --- Markup / config / data — no function-level control flow ---
	"makefile":   StyleUnsupported,
	"dockerfile": StyleUnsupported,
	"yaml":       StyleUnsupported,
	"json":       StyleUnsupported,
	"toml":       StyleUnsupported,
	"xml":        StyleUnsupported,
	"html":       StyleUnsupported,
	"css":        StyleUnsupported,
	"markdown":   StyleUnsupported,
	"mermaid":    StyleUnsupported,
	"env":        StyleUnsupported,
	"properties": StyleUnsupported,
}

// StyleForLanguage returns the CFG parsing style for the given language
// name (e.g. "python", "ruby"). Returns StyleUnsupported when the language
// is not registered with a strategy.
func StyleForLanguage(name string) Style {
	if s, ok := languageStyle[name]; ok {
		return s
	}
	return StyleUnsupported
}

// StyleForFile returns the CFG parsing style for the file at the given
// path. It looks up the registered language via internal/lang, then maps
// that language to its style. Returns StyleUnsupported when no parser is
// registered for the file or when the language has no CFG support.
func StyleForFile(path string) Style {
	p := lang.ParserForFile(path)
	if p == nil {
		return StyleUnsupported
	}
	return StyleForLanguage(p.Name())
}

// IsSupportedFile reports whether the heuristic CFG builder can handle the
// given source file. Preserved for backward compatibility with callers
// that don't care which Style is used.
func IsSupportedFile(path string) bool {
	return StyleForFile(path) != StyleUnsupported
}

// SupportedLanguages returns the sorted set of language names the
// heuristic CFG builder can produce a CFG for. Useful for help text and
// diagnostic output.
func SupportedLanguages() []string {
	out := make([]string, 0, len(languageStyle))
	for name, st := range languageStyle {
		if st == StyleUnsupported {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
