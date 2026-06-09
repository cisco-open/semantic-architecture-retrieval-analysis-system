/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package architect

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/lang"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/trace"
)

// PackageInfo describes a package/module/directory in the project.
type PackageInfo struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	Files      []string `json:"files"`
	Functions  int      `json:"functions"`
	Types      int      `json:"types"`
	Interfaces int      `json:"interfaces"`
	Lines      int      `json:"lines"`
}

// Dependency represents an import relationship between packages.
type Dependency struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// CodebaseMap is the full architecture map of a project.
type CodebaseMap struct {
	ProjectRoot  string         `json:"project_root"`
	Packages     []PackageInfo  `json:"packages"`
	Dependencies []Dependency   `json:"dependencies"`
	TotalFiles   int            `json:"total_files"`
	TotalLines   int            `json:"total_lines"`
	Symbols      []trace.Symbol `json:"symbols,omitempty"`
}

// Mapper generates codebase architecture maps.
type Mapper struct {
	root       string
	ignoreList []string
	tracer     *trace.Tracer
}

// NewMapper creates a new architecture mapper.
func NewMapper(root string, ignoreList []string) *Mapper {
	return &Mapper{
		root:       root,
		ignoreList: ignoreList,
		tracer:     trace.NewTracer(root, ignoreList),
	}
}

// GenerateMap produces a full codebase map.
func (m *Mapper) GenerateMap(ctx context.Context) (*CodebaseMap, error) {
	cmap := &CodebaseMap{ProjectRoot: m.root}

	// Extract symbols
	symbols, err := m.tracer.ExtractSymbols(ctx)
	if err != nil {
		return nil, fmt.Errorf("extract symbols: %w", err)
	}
	cmap.Symbols = symbols

	// Discover packages
	pkgs, err := m.discoverPackages(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover packages: %w", err)
	}

	// Enrich packages with symbol counts
	for i := range pkgs {
		m.enrichPackage(&pkgs[i], symbols)
	}

	cmap.Packages = pkgs

	// Count totals
	for _, pkg := range pkgs {
		cmap.TotalFiles += len(pkg.Files)
		cmap.TotalLines += pkg.Lines
	}

	// Discover dependencies
	deps, err := m.discoverDependencies(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover dependencies: %w", err)
	}
	cmap.Dependencies = deps

	return cmap, nil
}

// GenerateTree produces a directory tree string.
func (m *Mapper) GenerateTree(ctx context.Context) (string, error) {
	var b strings.Builder
	b.WriteString(filepath.Base(m.root) + "/\n")

	err := m.buildTree(ctx, m.root, "", &b, true)
	if err != nil {
		return "", err
	}

	return b.String(), nil
}

// GenerateMarkdown produces a markdown summary of the codebase.
func (m *Mapper) GenerateMarkdown(ctx context.Context) (string, error) {
	cmap, err := m.GenerateMap(ctx)
	if err != nil {
		return "", err
	}

	var b strings.Builder

	b.WriteString("# Codebase Architecture\n\n")
	b.WriteString(fmt.Sprintf("**Project:** `%s`\n\n", filepath.Base(m.root)))
	b.WriteString(fmt.Sprintf("- **Files:** %d\n", cmap.TotalFiles))
	b.WriteString(fmt.Sprintf("- **Lines:** %d\n", cmap.TotalLines))
	b.WriteString(fmt.Sprintf("- **Packages:** %d\n\n", len(cmap.Packages)))

	// Packages
	b.WriteString("## Packages\n\n")
	for _, pkg := range cmap.Packages {
		b.WriteString(fmt.Sprintf("### `%s` (%s)\n\n", pkg.Name, pkg.Path))
		b.WriteString(fmt.Sprintf("- Files: %d\n", len(pkg.Files)))
		b.WriteString(fmt.Sprintf("- Functions: %d\n", pkg.Functions))
		b.WriteString(fmt.Sprintf("- Types: %d\n", pkg.Types))
		b.WriteString(fmt.Sprintf("- Interfaces: %d\n", pkg.Interfaces))
		b.WriteString(fmt.Sprintf("- Lines: %d\n\n", pkg.Lines))

		if len(pkg.Files) > 0 {
			b.WriteString("Files:\n")
			for _, f := range pkg.Files {
				b.WriteString(fmt.Sprintf("- `%s`\n", f))
			}
			b.WriteString("\n")
		}
	}

	// Dependencies
	if len(cmap.Dependencies) > 0 {
		b.WriteString("## Internal Dependencies\n\n")
		for _, d := range cmap.Dependencies {
			b.WriteString(fmt.Sprintf("- `%s` → `%s`\n", d.From, d.To))
		}
		b.WriteString("\n")
	}

	// Key symbols
	funcs := filterSymbols(cmap.Symbols, trace.KindFunction)
	if len(funcs) > 0 {
		b.WriteString("## Key Functions\n\n")
		max := 30
		if len(funcs) < max {
			max = len(funcs)
		}
		for _, s := range funcs[:max] {
			b.WriteString(fmt.Sprintf("- `%s` (%s:%d)\n", s.Name, s.FilePath, s.Line))
		}
		b.WriteString("\n")
	}

	types := filterSymbols(cmap.Symbols, trace.KindType)
	ifaces := filterSymbols(cmap.Symbols, trace.KindInterface)
	allTypes := append(types, ifaces...)
	if len(allTypes) > 0 {
		b.WriteString("## Types & Interfaces\n\n")
		for _, s := range allTypes {
			b.WriteString(fmt.Sprintf("- `%s` (%s, %s:%d)\n", s.Name, s.Kind, s.FilePath, s.Line))
		}
		b.WriteString("\n")
	}

	return b.String(), nil
}

func (m *Mapper) discoverPackages(ctx context.Context) ([]PackageInfo, error) {
	pkgMap := make(map[string]*PackageInfo)

	err := filepath.Walk(m.root, func(path string, info os.FileInfo, err error) error {
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
			for _, ig := range m.ignoreList {
				if name == ig {
					return filepath.SkipDir
				}
			}
			return nil
		}

		if !lang.IsSupported(path) {
			return nil
		}

		relPath, err := filepath.Rel(m.root, path)
		if err != nil {
			return nil
		}

		dir := filepath.Dir(relPath)
		if dir == "." {
			dir = "."
		}

		pkg, ok := pkgMap[dir]
		if !ok {
			pkg = &PackageInfo{Path: dir}
			pkgMap[dir] = pkg
		}

		pkg.Files = append(pkg.Files, filepath.Base(relPath))

		// Count lines
		lines, _ := countLines(path)
		pkg.Lines += lines

		// Extract package/module name from first file
		if pkg.Name == "" {
			pkg.Name = extractPackageName(path)
			if pkg.Name == "" {
				pkg.Name = filepath.Base(dir)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Convert to slice
	var pkgs []PackageInfo
	for _, p := range pkgMap {
		sort.Strings(p.Files)
		pkgs = append(pkgs, *p)
	}

	sort.Slice(pkgs, func(i, j int) bool {
		return pkgs[i].Path < pkgs[j].Path
	})

	return pkgs, nil
}

func (m *Mapper) enrichPackage(pkg *PackageInfo, symbols []trace.Symbol) {
	for _, s := range symbols {
		dir := filepath.Dir(s.FilePath)
		if dir != pkg.Path {
			continue
		}
		switch s.Kind {
		case trace.KindFunction, trace.KindMethod:
			pkg.Functions++
		case trace.KindType:
			pkg.Types++
		case trace.KindInterface:
			pkg.Interfaces++
		}
	}
}

func (m *Mapper) discoverDependencies(ctx context.Context) ([]Dependency, error) {
	var deps []Dependency
	seen := make(map[string]bool)

	err := filepath.Walk(m.root, func(path string, info os.FileInfo, err error) error {
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
			for _, ig := range m.ignoreList {
				if name == ig {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Dependency discovery currently supports Go import analysis.
		// For other languages, directory-level grouping still works via packages.
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		relPath, _ := filepath.Rel(m.root, path)
		fromPkg := filepath.Dir(relPath)

		imports := extractImports(path)
		for _, imp := range imports {
			// Only track internal dependencies
			if isInternalImport(imp, m.root) {
				toPkg := lastPathSegments(imp, 2)
				key := fromPkg + "→" + toPkg
				if !seen[key] && fromPkg != toPkg {
					seen[key] = true
					deps = append(deps, Dependency{From: fromPkg, To: toPkg})
				}
			}
		}

		return nil
	})

	sort.Slice(deps, func(i, j int) bool {
		if deps[i].From != deps[j].From {
			return deps[i].From < deps[j].From
		}
		return deps[i].To < deps[j].To
	})

	return deps, err
}

func (m *Mapper) buildTree(ctx context.Context, dir, prefix string, b *strings.Builder, isLast bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	// Filter entries
	var filtered []os.DirEntry
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		skip := false
		for _, ig := range m.ignoreList {
			if name == ig {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		filtered = append(filtered, e)
	}

	for i, e := range filtered {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		isLastEntry := i == len(filtered)-1
		connector := "├── "
		childPrefix := prefix + "│   "
		if isLastEntry {
			connector = "└── "
			childPrefix = prefix + "    "
		}

		name := e.Name()
		if e.IsDir() {
			b.WriteString(prefix + connector + name + "/\n")
			m.buildTree(ctx, filepath.Join(dir, name), childPrefix, b, isLastEntry)
		} else {
			b.WriteString(prefix + connector + name + "\n")
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func countLines(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, nil
	}
	count := 1
	for _, b := range data {
		if b == '\n' {
			count++
		}
	}
	return count, nil
}

func extractPackageName(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	buf := make([]byte, 1024)
	n, _ := f.Read(buf)
	content := string(buf[:n])

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		// Go: package <name>
		if strings.HasPrefix(line, "package ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
		// Java/Kotlin: package <name>;
		if (strings.HasSuffix(path, ".java") || strings.HasSuffix(path, ".kt")) &&
			strings.HasPrefix(line, "package ") {
			pkg := strings.TrimPrefix(line, "package ")
			pkg = strings.TrimSuffix(pkg, ";")
			pkg = strings.TrimSpace(pkg)
			if pkg != "" {
				return pkg
			}
		}
		// C#: namespace <name>
		if strings.HasSuffix(path, ".cs") && strings.HasPrefix(line, "namespace ") {
			ns := strings.TrimPrefix(line, "namespace ")
			ns = strings.TrimSuffix(ns, "{")
			ns = strings.TrimSuffix(ns, ";")
			ns = strings.TrimSpace(ns)
			if ns != "" {
				return ns
			}
		}
		// Rust: mod <name> or pub mod <name>
		if strings.HasSuffix(path, ".rs") {
			if strings.HasPrefix(line, "mod ") || strings.HasPrefix(line, "pub mod ") {
				parts := strings.Fields(line)
				for i, p := range parts {
					if p == "mod" && i+1 < len(parts) {
						name := strings.TrimSuffix(parts[i+1], "{")
						name = strings.TrimSuffix(name, ";")
						if name != "" {
							return name
						}
					}
				}
			}
		}
	}
	return ""
}

func extractImports(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	content := string(data)
	var imports []string

	// Simple import extraction
	inBlock := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)

		if trimmed == "import (" {
			inBlock = true
			continue
		}
		if inBlock && trimmed == ")" {
			inBlock = false
			continue
		}

		if inBlock {
			imp := strings.Trim(trimmed, `"`)
			// Handle aliased imports
			parts := strings.Fields(trimmed)
			if len(parts) == 2 {
				imp = strings.Trim(parts[1], `"`)
			} else if len(parts) == 1 {
				imp = strings.Trim(parts[0], `"`)
			}
			if imp != "" {
				imports = append(imports, imp)
			}
		}

		// Single import
		if strings.HasPrefix(trimmed, `import "`) {
			imp := strings.TrimPrefix(trimmed, `import "`)
			imp = strings.TrimSuffix(imp, `"`)
			imports = append(imports, imp)
		}
	}

	return imports
}

func isInternalImport(imp, root string) bool {
	// Check if it looks like a Go module path from this project
	modPath := extractModulePath(root)
	if modPath != "" && strings.HasPrefix(imp, modPath) {
		return true
	}
	// Also check relative-style imports
	return strings.Contains(imp, "/internal/") || strings.HasPrefix(imp, "./")
}

func extractModulePath(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

func lastPathSegments(path string, n int) string {
	parts := strings.Split(path, "/")
	if len(parts) <= n {
		return path
	}
	return strings.Join(parts[len(parts)-n:], "/")
}

func filterSymbols(symbols []trace.Symbol, kind trace.SymbolKind) []trace.Symbol {
	var result []trace.Symbol
	for _, s := range symbols {
		if s.Kind == kind {
			result = append(result, s)
		}
	}
	return result
}
