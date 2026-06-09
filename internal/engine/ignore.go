/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package engine

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// IgnoreMatcher consolidates .gitignore, .sarasignore and a hard-coded
// ignore list into a single predicate used by both Scanner and Watcher.
type IgnoreMatcher struct {
	root       string
	ignoreList []string
	patterns   []ignorePattern
}

// ignorePattern is a parsed gitignore-style rule.
type ignorePattern struct {
	pattern  string
	negated  bool
	dirOnly  bool
	anchored bool // contains a slash (matches from root)
}

// NewIgnoreMatcher builds an IgnoreMatcher that loads patterns from
// .gitignore and .sarasignore (if present) in the project root.
func NewIgnoreMatcher(root string, ignoreList []string) *IgnoreMatcher {
	m := &IgnoreMatcher{
		root:       root,
		ignoreList: ignoreList,
	}
	m.patterns = m.loadAllPatterns()
	return m
}

// IsIgnored returns true when relPath (relative to root) should be skipped.
func (m *IgnoreMatcher) IsIgnored(relPath string, isDir bool) bool {
	// Check every path component against the hard-coded ignore list.
	parts := strings.Split(relPath, string(filepath.Separator))
	for _, part := range parts {
		if m.isInIgnoreList(part) {
			return true
		}
	}

	// Check gitignore / sarasignore patterns.
	return m.matchesPatterns(relPath, isDir)
}

// IsNameIgnored checks a single name (basename) against the ignore list.
func (m *IgnoreMatcher) IsNameIgnored(name string) bool {
	return m.isInIgnoreList(name)
}

// --- internal helpers -------------------------------------------------------

func (m *IgnoreMatcher) isInIgnoreList(name string) bool {
	for _, ig := range m.ignoreList {
		if name == ig {
			return true
		}
	}
	return false
}

func (m *IgnoreMatcher) loadAllPatterns() []ignorePattern {
	var all []ignorePattern
	all = append(all, loadIgnoreFile(filepath.Join(m.root, ".gitignore"))...)
	all = append(all, loadIgnoreFile(filepath.Join(m.root, ".sarasignore"))...)
	return all
}

func loadIgnoreFile(path string) []ignorePattern {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var patterns []ignorePattern
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, parseIgnoreLine(line))
	}
	return patterns
}

func parseIgnoreLine(line string) ignorePattern {
	p := ignorePattern{}

	if strings.HasPrefix(line, "!") {
		p.negated = true
		line = line[1:]
	}

	if strings.HasSuffix(line, "/") {
		p.dirOnly = true
		line = strings.TrimSuffix(line, "/")
	}

	// A pattern with a slash (not just trailing) is anchored to root
	if strings.Contains(line, "/") {
		p.anchored = true
	}

	p.pattern = line
	return p
}

func (m *IgnoreMatcher) matchesPatterns(relPath string, isDir bool) bool {
	matched := false
	for _, p := range m.patterns {
		if p.dirOnly && !isDir {
			continue
		}

		doesMatch := false
		if p.anchored {
			// Match against full relative path
			doesMatch, _ = filepath.Match(p.pattern, relPath)
		} else {
			// Match against basename
			doesMatch, _ = filepath.Match(p.pattern, filepath.Base(relPath))
			if !doesMatch {
				// Also try matching against full path for patterns like "dir/file"
				doesMatch, _ = filepath.Match(p.pattern, relPath)
			}
		}

		if doesMatch {
			if p.negated {
				matched = false
			} else {
				matched = true
			}
		}
	}
	return matched
}
