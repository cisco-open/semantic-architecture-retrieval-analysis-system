/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"strings"
	"testing"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/cfg"
)

// TestRenderCFGFormats verifies that the renderCFG helper produces output in
// each supported format and rejects unknown formats.
func TestRenderCFGFormats(t *testing.T) {
	c := &cfg.CFG{
		Function:  "demo",
		File:      "demo.go",
		Language:  "go",
		StartLine: 1,
		EndLine:   3,
		EntryID:   0,
		ExitID:    1,
		Blocks: []*cfg.Block{
			{ID: 0, Kind: cfg.KindEntry, Label: "entry"},
			{ID: 1, Kind: cfg.KindExit, Label: "exit"},
		},
		Edges: []cfg.Edge{{From: 0, To: 1}},
	}

	cases := []struct {
		format     string
		wantSubstr string
	}{
		{"mermaid", "flowchart TD"},
		{"text", "Control Flow Graph"},
		{"json", `"function": "demo"`},
		{"paths", "Function: demo"},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			// Pass nil context: this test pins the bare-format
			// behaviour. --with-context coverage lives in
			// TestRenderCFG_WithContext below.
			out, err := renderCFG(c, tc.format, 0, nil)
			if err != nil {
				t.Fatalf("renderCFG(%s): %v", tc.format, err)
			}
			if !strings.Contains(out, tc.wantSubstr) {
				t.Errorf("renderCFG(%s) missing %q in output:\n%s",
					tc.format, tc.wantSubstr, out)
			}
		})
	}

	if _, err := renderCFG(c, "graphviz", 0, nil); err == nil {
		t.Errorf("renderCFG(graphviz) should error on unknown format")
	}
}

// TestParseSymbolRef pins down the path-prefix shorthand
// (`pkg/auth/login.go:authenticate`) shared by `saras cfg` and
// `saras trace` to disambiguate symbols on the command line.
func TestParseSymbolRef(t *testing.T) {
	cases := []struct {
		name        string
		arg         string
		projectRoot string
		wantFile    string
		wantFunc    string
	}{
		{
			name:     "bare function name",
			arg:      "Login",
			wantFile: "",
			wantFunc: "Login",
		},
		{
			name:     "relative file path",
			arg:      "pkg/auth/login.go:Login",
			wantFile: "pkg/auth/login.go",
			wantFunc: "Login",
		},
		{
			name:     "relative directory path",
			arg:      "pkg/auth:Login",
			wantFile: "pkg/auth",
			wantFunc: "Login",
		},
		{
			name:     "filename with extension only",
			arg:      "login.go:Login",
			wantFile: "login.go",
			wantFunc: "Login",
		},
		{
			name:        "absolute path inside project becomes relative",
			arg:         "/proj/pkg/auth/login.go:Login",
			projectRoot: "/proj",
			wantFile:    "pkg/auth/login.go",
			wantFunc:    "Login",
		},
		{
			name:        "absolute path outside project preserved",
			arg:         "/other/login.go:Login",
			projectRoot: "/proj",
			wantFile:    "/other/login.go",
			wantFunc:    "Login",
		},
		{
			name:     "double colon (Ruby/PHP) treated as plain name",
			arg:      "MyClass::method",
			wantFile: "",
			wantFunc: "MyClass::method",
		},
		{
			name:     "single colon without path-shaped prefix is plain name",
			arg:      "Foo:bar",
			wantFile: "",
			wantFunc: "Foo:bar",
		},
		{
			name:     "trailing colon is plain name",
			arg:      "pkg/auth:",
			wantFile: "",
			wantFunc: "pkg/auth:",
		},
		{
			name:     "leading colon is plain name",
			arg:      ":Login",
			wantFile: "",
			wantFunc: ":Login",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotFile, gotFunc := parseSymbolRef(tc.arg, tc.projectRoot)
			if gotFile != tc.wantFile || gotFunc != tc.wantFunc {
				t.Errorf("parseSymbolRef(%q, %q) = (%q, %q); want (%q, %q)",
					tc.arg, tc.projectRoot,
					gotFile, gotFunc,
					tc.wantFile, tc.wantFunc)
			}
		})
	}
}
