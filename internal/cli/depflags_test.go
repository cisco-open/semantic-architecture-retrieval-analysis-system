/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"testing"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/search"
)

func TestFormatResultLabel(t *testing.T) {
	tests := []struct {
		name     string
		result   search.Result
		expected string
	}{
		{
			name:     "current repo (empty dep)",
			result:   search.Result{FilePath: "main.go"},
			expected: "",
		},
		{
			name:     "dep result",
			result:   search.Result{FilePath: "auth.go", DepName: "auth-svc", DepRole: "legacy"},
			expected: "[legacy: auth-svc] ",
		},
		{
			name:     "service dep",
			result:   search.Result{FilePath: "api.go", DepName: "api", DepRole: "service"},
			expected: "[service: api] ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatResultLabel(tc.result)
			if got != tc.expected {
				t.Errorf("formatResultLabel() = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestAddDepFlags(t *testing.T) {
	// Ensure addDepFlags registers the three flags without panicking
	cmd := searchCmd
	// Flags should already be registered via init()
	if cmd.Flags().Lookup("from-dep") == nil {
		t.Error("expected --from-dep flag to be registered on search")
	}
	if cmd.Flags().Lookup("all-deps") == nil {
		t.Error("expected --all-deps flag to be registered on search")
	}
	if cmd.Flags().Lookup("with-deps") == nil {
		t.Error("expected --with-deps flag to be registered on search")
	}
}

func TestParseDepFlags(t *testing.T) {
	cmd := searchCmd

	// Default values
	fromDep, allDeps, withDeps := parseDepFlags(cmd)
	if fromDep != "" {
		t.Errorf("expected empty fromDep, got %q", fromDep)
	}
	if allDeps {
		t.Error("expected allDeps=false")
	}
	if withDeps {
		t.Error("expected withDeps=false")
	}
}
