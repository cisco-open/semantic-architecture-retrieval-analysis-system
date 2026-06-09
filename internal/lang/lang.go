/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package lang

import (
	"path/filepath"
	"strings"
	"sync"
)

// SymbolKind classifies extracted symbols.
type SymbolKind int

const (
	KindFunction SymbolKind = iota
	KindMethod
	KindClass
	KindType
	KindInterface
	KindStruct
	KindEnum
	KindVariable
	KindConstant
	KindImport
	KindPackage
	KindModule
	KindTrait
	KindProperty
)

func (k SymbolKind) String() string {
	switch k {
	case KindFunction:
		return "function"
	case KindMethod:
		return "method"
	case KindClass:
		return "class"
	case KindType:
		return "type"
	case KindInterface:
		return "interface"
	case KindStruct:
		return "struct"
	case KindEnum:
		return "enum"
	case KindVariable:
		return "variable"
	case KindConstant:
		return "constant"
	case KindImport:
		return "import"
	case KindPackage:
		return "package"
	case KindModule:
		return "module"
	case KindTrait:
		return "trait"
	case KindProperty:
		return "property"
	default:
		return "unknown"
	}
}

// Symbol represents a code symbol extracted from source.
type Symbol struct {
	Name      string     `json:"name"`
	Kind      SymbolKind `json:"kind"`
	StartLine int        `json:"start_line"` // 1-indexed
	EndLine   int        `json:"end_line"`   // 1-indexed, inclusive
	Signature string     `json:"signature"`
	Parent    string     `json:"parent,omitempty"`
}

// LanguageParser extracts symbols from source code of a specific language.
// Community parsers implement this interface and call Register().
type LanguageParser interface {
	// Name returns the language name (e.g. "go", "python", "rust").
	Name() string

	// Extensions returns file extensions this parser handles (e.g. [".go"], [".py"]).
	Extensions() []string

	// ExtractSymbols parses source content and returns discovered symbols.
	ExtractSymbols(content string) []Symbol

	// IsTestFile returns true if the file path is a test file for this language.
	IsTestFile(path string) bool
}

// ---------------------------------------------------------------------------
// Global registry
// ---------------------------------------------------------------------------

// FilenameMatcher is an optional interface that parsers can implement
// to match files by their base filename (e.g. "Makefile", "Dockerfile").
type FilenameMatcher interface {
	Filenames() []string
}

var (
	registryMu sync.RWMutex
	parsers    = make(map[string]LanguageParser) // extension → parser
	byName     = make(map[string]LanguageParser) // name → parser
	byFilename = make(map[string]LanguageParser) // lowercase filename → parser
)

// Register adds a language parser to the global registry.
// It maps each of the parser's extensions to the parser.
// Registering a parser with an already-claimed extension overwrites silently.
func Register(p LanguageParser) {
	registryMu.Lock()
	defer registryMu.Unlock()

	byName[p.Name()] = p
	for _, ext := range p.Extensions() {
		ext = normalizeExt(ext)
		parsers[ext] = p
	}
	if fm, ok := p.(FilenameMatcher); ok {
		for _, name := range fm.Filenames() {
			byFilename[strings.ToLower(name)] = p
		}
	}
}

// ParserForFile returns the registered parser for a file path, or nil.
// It first checks by file extension, then falls back to exact filename matching.
func ParserForFile(path string) LanguageParser {
	ext := normalizeExt(filepath.Ext(path))
	registryMu.RLock()
	defer registryMu.RUnlock()
	if p := parsers[ext]; p != nil {
		return p
	}
	// Fallback: match by base filename (e.g. Makefile, Dockerfile)
	base := strings.ToLower(filepath.Base(path))
	return byFilename[base]
}

// ParserByName returns a parser by language name, or nil.
func ParserByName(name string) LanguageParser {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return byName[strings.ToLower(name)]
}

// RegisteredLanguages returns the names of all registered languages.
func RegisteredLanguages() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	return names
}

// SupportedExtensions returns all registered file extensions.
func SupportedExtensions() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	exts := make([]string, 0, len(parsers))
	for e := range parsers {
		exts = append(exts, e)
	}
	return exts
}

// IsSupported returns true if the file extension has a registered parser.
func IsSupported(path string) bool {
	return ParserForFile(path) != nil
}

// ---------------------------------------------------------------------------
// Flow hints — optional per-language metadata for call-graph analysis
// ---------------------------------------------------------------------------

// FlowHints provides language-specific metadata that improves accuracy of
// entry-point detection and call-graph analysis in the "flow" command.
type FlowHints struct {
	// EntryFunctions lists function names that serve as entry points
	// (e.g. "main" for Go/C/Rust, "Main" for C#).
	EntryFunctions []string

	// IsEntryFile reports whether file content can host an entry point
	// (e.g. "package main" for Go). Nil means always true.
	IsEntryFile func(content string) bool

	// Keywords lists language keywords and builtins to exclude from
	// call detection (e.g. "if", "for", "make", "len").
	Keywords []string

	// CommentPrefixes lists single-line comment prefixes (e.g. "//", "#").
	CommentPrefixes []string
}

// FlowHinter is an optional interface that language parsers can implement
// to provide hints for the flow command's call-graph analysis.
// Parsers that do not implement this interface get sensible defaults.
type FlowHinter interface {
	FlowHints() FlowHints
}

// GetFlowHints returns flow hints for the language of the given file path.
// If the parser does not implement FlowHinter, default hints are returned.
func GetFlowHints(path string) FlowHints {
	p := ParserForFile(path)
	if p == nil {
		return defaultFlowHints()
	}
	if fh, ok := p.(FlowHinter); ok {
		return fh.FlowHints()
	}
	return defaultFlowHints()
}

// GetFlowHintsByName returns flow hints for the named language.
func GetFlowHintsByName(name string) FlowHints {
	p := ParserByName(name)
	if p == nil {
		return defaultFlowHints()
	}
	if fh, ok := p.(FlowHinter); ok {
		return fh.FlowHints()
	}
	return defaultFlowHints()
}

// AllFlowHints returns merged flow hints across all registered languages.
// This is useful for building a combined keyword skip list.
func AllFlowHints() FlowHints {
	registryMu.RLock()
	defer registryMu.RUnlock()

	kw := make(map[string]bool)
	cp := make(map[string]bool)
	ef := make(map[string]bool)

	for _, p := range byName {
		var h FlowHints
		if fh, ok := p.(FlowHinter); ok {
			h = fh.FlowHints()
		} else {
			h = defaultFlowHints()
		}
		for _, k := range h.Keywords {
			kw[k] = true
		}
		for _, c := range h.CommentPrefixes {
			cp[c] = true
		}
		for _, e := range h.EntryFunctions {
			ef[e] = true
		}
	}

	return FlowHints{
		EntryFunctions:  mapKeys(ef),
		Keywords:        mapKeys(kw),
		CommentPrefixes: mapKeys(cp),
	}
}

func defaultFlowHints() FlowHints {
	return FlowHints{
		EntryFunctions:  []string{"main"},
		Keywords:        cFamilyKeywords(),
		CommentPrefixes: []string{"//"},
	}
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Shared keyword sets that parsers can compose.

func cFamilyKeywords() []string {
	return []string{
		"if", "for", "while", "else", "switch", "case", "return",
		"break", "continue", "do", "goto", "sizeof", "typeof",
		"new", "delete", "throw", "try", "catch", "finally",
		"this", "super", "class", "struct", "enum", "interface",
		"import", "package", "namespace", "using",
	}
}

func normalizeExt(ext string) string {
	ext = strings.ToLower(ext)
	if ext != "" && ext[0] != '.' {
		ext = "." + ext
	}
	return ext
}
