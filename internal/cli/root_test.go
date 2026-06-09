/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"bytes"
	"testing"
)

func TestSetVersion(t *testing.T) {
	SetVersion("1.2.3")
	if Version() != "1.2.3" {
		t.Errorf("expected version 1.2.3, got %s", Version())
	}
}

func TestVersionCommand(t *testing.T) {
	SetVersion("0.1.0-test")

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"version"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}

	out := buf.String()
	if out != "saras version 0.1.0-test\n" {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestRootCommandNoArgs(t *testing.T) {
	rootCmd.SetArgs([]string{})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("root command with no args should not error: %v", err)
	}
}

func TestGetRootCmd(t *testing.T) {
	cmd := GetRootCmd()
	if cmd == nil {
		t.Fatal("GetRootCmd returned nil")
	}
	if cmd.Use != "saras" {
		t.Errorf("expected Use=saras, got %s", cmd.Use)
	}
}
