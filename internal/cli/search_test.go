/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"bytes"
	"strings"
	"testing"

	searchpkg "github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/search"
	"github.com/spf13/cobra"
)

func TestPrintSearchJSON(t *testing.T) {
	results := []searchpkg.Result{
		{FilePath: "src/auth.go", StartLine: 1, EndLine: 10, Score: 0.95},
		{FilePath: "src/db.go", StartLine: 5, EndLine: 15, Score: 0.80},
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	err := printSearchJSON(cmd, results)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "src/auth.go") {
		t.Error("expected auth.go in JSON output")
	}
	if !strings.Contains(output, "0.9500") {
		t.Error("expected score in JSON output")
	}
	if !strings.HasPrefix(output, "[") {
		t.Error("expected JSON array start")
	}
	if !strings.Contains(output, "]") {
		t.Error("expected JSON array end")
	}
}

func TestPrintSearchJSONEmpty(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	err := printSearchJSON(cmd, nil)
	if err != nil {
		t.Fatal(err)
	}

	output := strings.TrimSpace(buf.String())
	if output != "[\n]" {
		t.Errorf("expected empty JSON array, got: %s", output)
	}
}

func TestPrintSearchPlain(t *testing.T) {
	results := []searchpkg.Result{
		{FilePath: "src/auth.go", StartLine: 1, EndLine: 10, Content: "func Login(user, pass string) error {\n\treturn validate(user, pass)\n}", Score: 0.95},
		{FilePath: "src/db.go", StartLine: 5, EndLine: 15, Content: "func Connect(dsn string) (*DB, error) {\n\treturn sql.Open(\"postgres\", dsn)\n}", Score: 0.80},
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	err := printSearchPlain(cmd, "login auth", results)
	if err != nil {
		t.Fatal(err)
	}

	output := buf.String()

	if !strings.Contains(output, "login auth") {
		t.Error("expected query in output")
	}
	if !strings.Contains(output, "2 results") {
		t.Error("expected result count in output")
	}
	if !strings.Contains(output, "src/auth.go") {
		t.Error("expected file path in output")
	}
	if !strings.Contains(output, "0.95") {
		t.Error("expected score in output")
	}
	if !strings.Contains(output, "func Login") {
		t.Error("expected content snippet in output")
	}
}

func TestPrintSearchPlainEmpty(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	err := printSearchPlain(cmd, "nonexistent", nil)
	if err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if !strings.Contains(output, "0 results") {
		t.Error("expected 0 results in output")
	}
}

func TestPrintSearchPlainLongContent(t *testing.T) {
	longLine := strings.Repeat("x", 200)
	results := []searchpkg.Result{
		{FilePath: "a.go", StartLine: 1, EndLine: 1, Content: longLine, Score: 0.5},
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	err := printSearchPlain(cmd, "test", results)
	if err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if !strings.Contains(output, "...") {
		t.Error("expected truncation indicator for long content")
	}
}

func TestMinHelper(t *testing.T) {
	if min(3, 5) != 3 {
		t.Error("min(3,5) should be 3")
	}
	if min(5, 3) != 3 {
		t.Error("min(5,3) should be 3")
	}
	if min(4, 4) != 4 {
		t.Error("min(4,4) should be 4")
	}
}
