/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package engine

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// walkCallback receives one entry during a walkFollow traversal.
//
//	logicalPath: absolute path within the original tree (== filepath.Join(root, relPath)),
//	             preserved even when the entry lives behind a followed symlink, so that
//	             relative paths, AbsPath, and de-duplication stay consistent for callers.
//	relPath:     path relative to root ("." for root itself).
//	info:        fs.FileInfo for the entry. For a symlink-to-file this is the stat of the
//	             TARGET, so Size/ModTime reflect real content rather than the link.
//	isDir:       true for directories, including followed symlinks-to-directories (whose own
//	             info.IsDir() would be false).
//
// Returning fs.SkipDir from a directory entry skips that subtree.
type walkCallback func(logicalPath, relPath string, info fs.FileInfo, isDir bool) error

// walkFollow walks the tree at root much like filepath.WalkDir, but additionally
// descends into directory symlinks whose resolved target stays *under* root. This
// fixes silent skipping of symlinked source directories (common in Python
// virtualenvs and monorepos), where plain filepath.Walk lstats a directory
// symlink, sees a non-directory, and never recurses.
//
// Safety properties:
//   - Containment: a directory symlink is followed only when its EvalSymlinks target
//     is within the resolved root. Escaping links (e.g. a venv's python3.x ->
//     /opt/homebrew/...) are skipped and logged.
//   - Cycle protection: every directory's resolved real path is recorded in a visited
//     set (seeded with the resolved root), so a symlink pointing back at an ancestor
//     cannot cause infinite recursion or double traversal.
//   - A symlinked root is handled: it is resolved once for reading and containment,
//     while logical paths remain rooted at the original root string.
func walkFollow(root string, fn walkCallback) error {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err // root is missing or unreadable
	}
	visited := map[string]bool{resolvedRoot: true}
	return walkFollowDir(root, resolvedRoot, root, resolvedRoot, visited, fn)
}

// walkFollowDir lists realDir from disk while reporting entries under logicalDir.
//
//	root, resolvedRoot: the original and symlink-resolved tree roots (constant per walk).
//	logicalDir:         path of the directory as seen in the logical tree.
//	realDir:            on-disk path actually read (differs from logicalDir below a symlink).
func walkFollowDir(root, resolvedRoot, logicalDir, realDir string, visited map[string]bool, fn walkCallback) error {
	relDir, _ := filepath.Rel(root, logicalDir)

	// Report the directory itself first, honoring fs.SkipDir to prune the subtree.
	// If the consumer skips this directory (e.g. it is hidden or ignored), we do
	// NOT record it as visited — that lets a symlink elsewhere in the tree still
	// follow into the same real directory and surface its content.
	if di, err := os.Stat(realDir); err == nil {
		if e := fn(logicalDir, relDir, di, true); e != nil {
			if e == fs.SkipDir {
				return nil
			}
			return e
		}
	}

	// The directory is being walked: record its real path so that symlinks (and
	// cycles) pointing back at it are treated as duplicates rather than re-walked.
	visited[realDir] = true

	entries, err := os.ReadDir(realDir)
	if err != nil {
		return nil // unreadable directory: skip, matching the old Walk err==nil swallow
	}

	// First pass: regular files and real subdirectories. Real directories are
	// always descended (a tree of real dirs cannot contain a cycle); each records
	// itself as visited on entry. Handling real dirs before symlinks makes the real
	// path win deterministically when both a real dir and a symlink to it exist.
	for _, d := range entries {
		if d.Type()&fs.ModeSymlink != 0 {
			continue // handled in the second pass
		}

		logicalPath := filepath.Join(logicalDir, d.Name())
		realPath := filepath.Join(realDir, d.Name())
		relPath, _ := filepath.Rel(root, logicalPath)

		if d.IsDir() {
			if e := walkFollowDir(root, resolvedRoot, logicalPath, realPath, visited, fn); e != nil {
				return e
			}
			continue
		}

		info, err := d.Info()
		if err != nil {
			continue
		}
		if e := fn(logicalPath, relPath, info, false); e != nil {
			return e
		}
	}

	// Second pass: symlinks. A directory symlink whose real target was already
	// walked (a real sibling, or a cycle) is skipped; one whose target is not yet
	// visited — including content ignored on its real path but exposed here — is
	// followed.
	for _, d := range entries {
		if d.Type()&fs.ModeSymlink == 0 {
			continue
		}

		logicalPath := filepath.Join(logicalDir, d.Name())
		realPath := filepath.Join(realDir, d.Name())
		relPath, _ := filepath.Rel(root, logicalPath)

		target, err := filepath.EvalSymlinks(realPath)
		if err != nil {
			continue // broken symlink
		}
		ti, err := os.Stat(target)
		if err != nil {
			continue
		}
		if ti.IsDir() {
			if !underRoot(target, resolvedRoot) {
				log.Printf("engine: skipping symlink outside root: %s -> %s", logicalPath, target)
				continue
			}
			if visited[target] {
				continue // cycle, or the same real directory already walked
			}
			if e := walkFollowDir(root, resolvedRoot, logicalPath, target, visited, fn); e != nil {
				return e
			}
		} else {
			// Symlink to a regular file: report with the TARGET's stat.
			if e := fn(logicalPath, relPath, ti, false); e != nil {
				return e
			}
		}
	}
	return nil
}

// underRoot reports whether target is resolvedRoot or lies beneath it. It uses
// filepath.Rel rather than a string prefix to avoid matching siblings like
// "/foobar" against a root of "/foo".
func underRoot(target, resolvedRoot string) bool {
	if target == resolvedRoot {
		return true
	}
	rel, err := filepath.Rel(resolvedRoot, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
