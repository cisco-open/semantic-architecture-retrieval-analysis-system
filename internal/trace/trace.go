/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package trace

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/lang"
)

// SymbolKind classifies extracted symbols.
type SymbolKind int

const (
	KindFunction SymbolKind = iota
	KindMethod
	KindType
	KindInterface
	KindVariable
	KindConstant
	KindImport
	KindPackage
)

func (k SymbolKind) String() string {
	switch k {
	case KindFunction:
		return "function"
	case KindMethod:
		return "method"
	case KindType:
		return "type"
	case KindInterface:
		return "interface"
	case KindVariable:
		return "variable"
	case KindConstant:
		return "constant"
	case KindImport:
		return "import"
	case KindPackage:
		return "package"
	default:
		return "unknown"
	}
}

// Symbol represents an extracted code symbol.
type Symbol struct {
	Name      string     `json:"name"`
	Kind      SymbolKind `json:"kind"`
	FilePath  string     `json:"file_path"`
	Line      int        `json:"line"`
	EndLine   int        `json:"end_line"`
	Signature string     `json:"signature"`
	Parent    string     `json:"parent,omitempty"`
}

// Reference is a location where a symbol is used.
type Reference struct {
	FilePath string `json:"file_path"`
	Line     int    `json:"line"`
	Context  string `json:"context"`
}

// CallEdge represents a caller→callee relationship.
type CallEdge struct {
	Caller     string `json:"caller"`
	CallerFile string `json:"caller_file"`
	CallerLine int    `json:"caller_line"`
	Callee     string `json:"callee"`
}

// TraceResult holds the output of a trace query.
type TraceResult struct {
	Symbol     *Symbol     `json:"symbol,omitempty"`
	References []Reference `json:"references,omitempty"`
	Callers    []CallEdge  `json:"callers,omitempty"`
	Callees    []CallEdge  `json:"callees,omitempty"`
}

// Tracer extracts symbols and call relationships from source code.
type Tracer struct {
	root       string
	ignoreList []string
}

// NewTracer creates a new tracer for the given project root.
func NewTracer(root string, ignoreList []string) *Tracer {
	return &Tracer{root: root, ignoreList: ignoreList}
}

// ExtractSymbols scans all Go files and extracts symbols.
func (t *Tracer) ExtractSymbols(ctx context.Context) ([]Symbol, error) {
	var symbols []Symbol

	err := filepath.Walk(t.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			for _, ig := range t.ignoreList {
				if name == ig {
					return filepath.SkipDir
				}
			}
			return nil
		}

		if !isSupportedFile(path) {
			return nil
		}

		relPath, err := filepath.Rel(t.root, path)
		if err != nil {
			return nil
		}

		fileSymbols, err := extractFileSymbols(path, relPath)
		if err != nil {
			return nil
		}

		symbols = append(symbols, fileSymbols...)
		return nil
	})

	return symbols, err
}

// FindSymbol finds a symbol by name across the project.
func (t *Tracer) FindSymbol(ctx context.Context, name string) ([]Symbol, error) {
	all, err := t.ExtractSymbols(ctx)
	if err != nil {
		return nil, err
	}

	var matches []Symbol
	for _, s := range all {
		if s.Name == name {
			matches = append(matches, s)
		}
	}
	return matches, nil
}

// FindReferences finds all references to a symbol name.
func (t *Tracer) FindReferences(ctx context.Context, symbolName string) ([]Reference, error) {
	var refs []Reference

	err := filepath.Walk(t.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			for _, ig := range t.ignoreList {
				if name == ig {
					return filepath.SkipDir
				}
			}
			return nil
		}

		if !isSupportedFile(path) {
			return nil
		}

		relPath, err := filepath.Rel(t.root, path)
		if err != nil {
			return nil
		}

		fileRefs, err := findRefsInFile(path, relPath, symbolName)
		if err != nil {
			return nil
		}
		refs = append(refs, fileRefs...)
		return nil
	})

	return refs, err
}

// FindCallers finds all functions that call the named function.
func (t *Tracer) FindCallers(ctx context.Context, funcName string) ([]CallEdge, error) {
	symbols, err := t.ExtractSymbols(ctx)
	if err != nil {
		return nil, err
	}

	// Build a map of functions → file/line for quick lookup
	funcMap := make(map[string]Symbol)
	for _, s := range symbols {
		if s.Kind == KindFunction || s.Kind == KindMethod {
			funcMap[s.Name] = s
		}
	}

	var callers []CallEdge

	err = filepath.Walk(t.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			for _, ig := range t.ignoreList {
				if name == ig {
					return filepath.SkipDir
				}
			}
			return nil
		}

		if !isSupportedFile(path) {
			return nil
		}

		relPath, err := filepath.Rel(t.root, path)
		if err != nil {
			return nil
		}

		edges, err := findCallsInFile(path, relPath, funcName, symbols)
		if err != nil {
			return nil
		}
		callers = append(callers, edges...)
		return nil
	})

	return callers, err
}

// FindCallees finds all functions called by the named function.
func (t *Tracer) FindCallees(ctx context.Context, funcName string) ([]CallEdge, error) {
	symbols, err := t.ExtractSymbols(ctx)
	if err != nil {
		return nil, err
	}

	// Find the target function's body
	var target *Symbol
	for _, s := range symbols {
		if (s.Kind == KindFunction || s.Kind == KindMethod) && s.Name == funcName {
			s := s
			target = &s
			break
		}
	}

	if target == nil {
		return nil, fmt.Errorf("function %q not found", funcName)
	}

	absPath := filepath.Join(t.root, target.FilePath)
	content, err := readLines(absPath, target.Line, target.EndLine)
	if err != nil {
		return nil, err
	}

	// Extract function calls from body
	knownFuncs := make(map[string]bool)
	for _, s := range symbols {
		if s.Kind == KindFunction || s.Kind == KindMethod {
			knownFuncs[s.Name] = true
		}
	}

	var callees []CallEdge
	callPattern := regexp.MustCompile(`\b([a-zA-Z_]\w*)\s*\(`)

	for _, line := range strings.Split(content, "\n") {
		matches := callPattern.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			callee := m[1]
			if callee == funcName {
				continue // skip recursive
			}
			if knownFuncs[callee] {
				callees = append(callees, CallEdge{
					Caller:     funcName,
					CallerFile: target.FilePath,
					CallerLine: target.Line,
					Callee:     callee,
				})
			}
		}
	}

	// Deduplicate callees
	callees = deduplicateEdges(callees)
	return callees, nil
}

// Trace performs a full trace of a symbol: definition, references, callers, callees.
func (t *Tracer) Trace(ctx context.Context, name string) (*TraceResult, error) {
	result := &TraceResult{}

	// Find symbol
	symbols, err := t.FindSymbol(ctx, name)
	if err != nil {
		return nil, err
	}
	if len(symbols) > 0 {
		result.Symbol = &symbols[0]
	}

	// Find references
	refs, err := t.FindReferences(ctx, name)
	if err != nil {
		return nil, err
	}
	result.References = refs

	// If it's a function, find callers and callees
	if result.Symbol != nil && (result.Symbol.Kind == KindFunction || result.Symbol.Kind == KindMethod) {
		callers, err := t.FindCallers(ctx, name)
		if err == nil {
			result.Callers = callers
		}

		callees, err := t.FindCallees(ctx, name)
		if err == nil {
			result.Callees = callees
		}
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// File-level extraction (multi-language via lang registry)
// ---------------------------------------------------------------------------

func extractFileSymbols(absPath, relPath string) ([]Symbol, error) {
	parser := lang.ParserForFile(absPath)
	if parser == nil {
		return nil, nil
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	langSymbols := parser.ExtractSymbols(string(content))

	symbols := make([]Symbol, 0, len(langSymbols))
	for _, ls := range langSymbols {
		symbols = append(symbols, convertLangSymbol(ls, relPath))
	}

	return symbols, nil
}

func convertLangSymbol(ls lang.Symbol, relPath string) Symbol {
	return Symbol{
		Name:      ls.Name,
		Kind:      convertLangKind(ls.Kind),
		FilePath:  relPath,
		Line:      ls.StartLine,
		EndLine:   ls.EndLine,
		Signature: ls.Signature,
		Parent:    ls.Parent,
	}
}

func convertLangKind(k lang.SymbolKind) SymbolKind {
	switch k {
	case lang.KindFunction:
		return KindFunction
	case lang.KindMethod:
		return KindMethod
	case lang.KindClass, lang.KindStruct:
		return KindType
	case lang.KindType:
		return KindType
	case lang.KindInterface:
		return KindInterface
	case lang.KindEnum:
		return KindType
	case lang.KindVariable, lang.KindProperty:
		return KindVariable
	case lang.KindConstant:
		return KindConstant
	case lang.KindImport:
		return KindImport
	case lang.KindPackage, lang.KindModule:
		return KindPackage
	case lang.KindTrait:
		return KindInterface
	default:
		return KindFunction
	}
}

func findRefsInFile(absPath, relPath, symbolName string) ([]Reference, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(symbolName) + `\b`)

	var refs []Reference
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if pattern.MatchString(line) {
			refs = append(refs, Reference{
				FilePath: relPath,
				Line:     lineNum,
				Context:  strings.TrimSpace(line),
			})
		}
	}

	return refs, scanner.Err()
}

func findCallsInFile(absPath, relPath, funcName string, symbols []Symbol) ([]CallEdge, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	callPattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(funcName) + `\s*\(`)

	var edges []CallEdge
	scanner := bufio.NewScanner(f)
	lineNum := 0

	// Determine which function each line belongs to
	fileFuncs := extractFuncRanges(absPath, relPath)

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if callPattern.MatchString(line) {
			caller := findEnclosingFunc(lineNum, fileFuncs)
			if caller != "" && caller != funcName {
				edges = append(edges, CallEdge{
					Caller:     caller,
					CallerFile: relPath,
					CallerLine: lineNum,
					Callee:     funcName,
				})
			}
		}
	}

	return edges, scanner.Err()
}

type funcRange struct {
	Name  string
	Start int
	End   int
}

func extractFuncRanges(absPath, relPath string) []funcRange {
	symbols, err := extractFileSymbols(absPath, relPath)
	if err != nil {
		return nil
	}

	var ranges []funcRange
	for _, s := range symbols {
		if s.Kind == KindFunction || s.Kind == KindMethod {
			ranges = append(ranges, funcRange{Name: s.Name, Start: s.Line, End: s.EndLine})
		}
	}
	return ranges
}

func findEnclosingFunc(line int, ranges []funcRange) string {
	for _, r := range ranges {
		if line >= r.Start && line <= r.End {
			return r.Name
		}
	}
	return ""
}

func readLines(path string, start, end int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum >= start && lineNum <= end {
			lines = append(lines, scanner.Text())
		}
		if lineNum > end {
			break
		}
	}
	return strings.Join(lines, "\n"), scanner.Err()
}

func isSupportedFile(path string) bool {
	parser := lang.ParserForFile(path)
	if parser == nil {
		return false
	}
	return !parser.IsTestFile(path)
}

func deduplicateEdges(edges []CallEdge) []CallEdge {
	seen := make(map[string]bool)
	var result []CallEdge
	for _, e := range edges {
		key := e.Caller + "→" + e.Callee
		if !seen[key] {
			seen[key] = true
			result = append(result, e)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Callee < result[j].Callee
	})
	return result
}
