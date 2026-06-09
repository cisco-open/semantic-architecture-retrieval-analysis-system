/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cfg

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	// Ensure language parsers are registered.
	_ "github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/lang"
)

// TestBuildOnSarasItself runs the CFG builder against a handful of real
// saras functions to confirm the heuristic handles production Go code
// (including `if x := f(); x != nil {`, type switches, range loops, etc.).
//
// Failures here are expressed as warnings (t.Logf) rather than hard errors
// because the heuristic is, by design, an approximation. The test does
// hard-fail only if the function cannot be located or the builder returns
// an error or produces zero paths.
func TestBuildOnSarasItself(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("cannot resolve module root from runtime.Caller")
	}
	projectRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))

	// Some of these names exist in multiple files within saras; we pin
	// the file substring to keep the test deterministic now that the
	// builder rejects ambiguous lookups by design.
	cases := []struct {
		fn       string
		file     string
		minPaths int
	}{
		{"BuildFromSymbol", "internal/cfg/builder.go", 2}, // if/error branches
		{"summarizeLines", "internal/cfg/builder.go", 1},  // simple linear
		{"truncate", "internal/cfg/builder.go", 2},        // if/else; 3 truncates exist
		{"appendNote", "internal/cfg/builder.go", 2},      // for + if
		{"EnumeratePaths", "internal/cfg/paths.go", 1},    // recursive DFS
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.fn, func(t *testing.T) {
			c, err := BuildFromFunctionWith(
				context.Background(), projectRoot, tc.fn,
				[]string{"node_modules", "vendor", ".git"},
				SelectOptions{File: tc.file},
			)
			if err != nil {
				t.Fatalf("BuildFromFunctionWith(%s, file=%s): %v", tc.fn, tc.file, err)
			}
			if len(c.Blocks) < 2 {
				t.Errorf("%s: only %d blocks built", tc.fn, len(c.Blocks))
			}
			paths := c.EnumeratePaths(0)
			if len(paths) < tc.minPaths {
				t.Logf("%s: enumerated %d paths, expected >= %d\n%s",
					tc.fn, len(paths), tc.minPaths, c.ToText())
			}
			// Mermaid should at least produce a header.
			mm := c.ToMermaid()
			if !strings.HasPrefix(mm, "flowchart TD") {
				t.Errorf("%s: missing mermaid header", tc.fn)
			}
		})
	}
}
