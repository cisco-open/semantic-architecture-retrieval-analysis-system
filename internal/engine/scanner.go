/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package engine

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileMeta holds metadata about a discovered source file.
type FileMeta struct {
	Path    string    // relative path from project root
	AbsPath string    // absolute path on disk
	ModTime time.Time // last modification time
	Size    int64     // file size in bytes
}

// Scanner walks a project directory and discovers indexable source files,
// respecting .gitignore / .sarasignore patterns and a configurable ignore list.
type Scanner struct {
	root    string
	ignorer *IgnoreMatcher
}

// NewScanner creates a new file scanner rooted at the given directory.
// ignoreList is the set of directory/file names to always skip (e.g. node_modules, .git).
func NewScanner(root string, ignoreList []string) *Scanner {
	return &Scanner{
		root:    root,
		ignorer: NewIgnoreMatcher(root, ignoreList),
	}
}

// ScanAll returns metadata for every indexable file under the project root.
func (s *Scanner) ScanAll() ([]FileMeta, error) {
	var files []FileMeta
	err := filepath.Walk(s.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}

		relPath, _ := filepath.Rel(s.root, path)
		if relPath == "." {
			return nil
		}

		name := info.Name()

		// Skip hidden files/dirs (except .gitignore / .sarasignore)
		if strings.HasPrefix(name, ".") && name != ".gitignore" && name != ".sarasignore" {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Check ignore list + gitignore / sarasignore patterns
		if s.ignorer.IsIgnored(relPath, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			return nil
		}

		// Skip binary / non-text files by extension
		if !isTextFile(name) {
			return nil
		}

		// Skip very large files (>1MB)
		if info.Size() > 1*1024*1024 {
			return nil
		}

		files = append(files, FileMeta{
			Path:    relPath,
			AbsPath: path,
			ModTime: info.ModTime(),
			Size:    info.Size(),
		})
		return nil
	})
	return files, err
}

// ScanChanged returns files modified after the given time.
func (s *Scanner) ScanChanged(since time.Time) ([]FileMeta, error) {
	all, err := s.ScanAll()
	if err != nil {
		return nil, err
	}
	var changed []FileMeta
	for _, f := range all {
		if f.ModTime.After(since) {
			changed = append(changed, f)
		}
	}
	return changed, nil
}

// isTextFile returns true if the file extension indicates a text/source file.
var textExtensions = map[string]bool{
	// Programming languages
	".go": true, ".js": true, ".ts": true, ".jsx": true, ".tsx": true,
	".py": true, ".rb": true, ".rs": true, ".java": true, ".kt": true,
	".c": true, ".h": true, ".cpp": true, ".hpp": true, ".cc": true,
	".cs": true, ".swift": true, ".m": true, ".mm": true,
	".php": true, ".lua": true, ".zig": true, ".nim": true,
	".scala": true, ".clj": true, ".ex": true, ".exs": true,
	".erl": true, ".hs": true, ".ml": true, ".fs": true, ".fsx": true,
	".r": true, ".jl": true, ".dart": true, ".v": true, ".vhdl": true,
	".pl": true, ".pm": true, ".sh": true, ".bash": true, ".zsh": true,
	".fish": true, ".ps1": true, ".bat": true, ".cmd": true,
	// Web
	".html": true, ".htm": true, ".css": true, ".scss": true, ".sass": true,
	".less": true, ".vue": true, ".svelte": true, ".astro": true,
	// Data / Config
	".json": true, ".yaml": true, ".yml": true, ".toml": true, ".xml": true,
	".ini": true, ".cfg": true, ".conf": true, ".env": true, ".properties": true,
	// Documentation
	".md": true, ".rst": true, ".txt": true, ".adoc": true,
	// Templates
	".tmpl": true, ".tpl": true, ".ejs": true, ".hbs": true, ".pug": true,
	// Build / CI
	".mk": true, ".cmake": true, ".gradle": true,
	// SQL
	".sql": true,
	// Proto
	".proto": true,
	// Docker
	".dockerfile": true,
	// GraphQL
	".graphql": true, ".gql": true,
}

func isTextFile(name string) bool {
	// Special filenames without extensions
	lower := strings.ToLower(name)
	switch lower {
	case "makefile", "dockerfile", "rakefile", "gemfile",
		"cmakelists.txt", "go.mod", "go.sum", "cargo.toml", "cargo.lock",
		"package.json", "tsconfig.json", ".gitignore", ".sarasignore", ".dockerignore":
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	return textExtensions[ext]
}
