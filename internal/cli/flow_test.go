/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and SARAS Contributors
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"strings"
	"testing"
)

func TestFlowCommandPrimaryName(t *testing.T) {
	if archCmd.Use != "flow [function]" {
		t.Errorf("expected Use='flow [function]', got %q", archCmd.Use)
	}
}

func TestFlowCommandAliases(t *testing.T) {
	aliases := archCmd.Aliases
	want := map[string]bool{"architecture": true, "arch": true}

	if len(aliases) != len(want) {
		t.Fatalf("expected %d aliases, got %d: %v", len(want), len(aliases), aliases)
	}
	for _, a := range aliases {
		if !want[a] {
			t.Errorf("unexpected alias %q", a)
		}
	}
}

func TestFlowCommandHasExpectedFlags(t *testing.T) {
	flags := []string{"depth", "output"}
	for _, name := range flags {
		if archCmd.Flags().Lookup(name) == nil {
			t.Errorf("expected flag --%s on flow command", name)
		}
	}
}

func TestFlowDepthFlagDefault(t *testing.T) {
	f := archCmd.Flags().Lookup("depth")
	if f == nil {
		t.Fatal("depth flag not found")
	}
	if f.DefValue != "8" {
		t.Errorf("expected depth default 8, got %s", f.DefValue)
	}
}

func TestFlowExplainSubcommand(t *testing.T) {
	found := false
	for _, sub := range archCmd.Commands() {
		if sub.Use == "explain [function]" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'explain' subcommand on flow command")
	}
}

func TestFlowExplainHasExpectedFlags(t *testing.T) {
	flags := []string{"depth", "output", "no-tui", "max-tokens", "temperature", "model"}
	for _, name := range flags {
		if archExplainCmd.Flags().Lookup(name) == nil {
			t.Errorf("expected flag --%s on flow explain command", name)
		}
	}
}

func TestFlowExplainMaxTokensDefault(t *testing.T) {
	f := archExplainCmd.Flags().Lookup("max-tokens")
	if f == nil {
		t.Fatal("max-tokens flag not found")
	}
	if f.DefValue != "4096" {
		t.Errorf("expected max-tokens default 4096, got %s", f.DefValue)
	}
}

func TestFlowHelpTextReferencesPrimaryName(t *testing.T) {
	if !strings.Contains(archCmd.Long, "saras flow") {
		t.Error("flow command long description should reference 'saras flow'")
	}
}

func TestFlowExplainHelpTextReferencesPrimaryName(t *testing.T) {
	if !strings.Contains(archExplainCmd.Long, "saras flow explain") {
		t.Error("flow explain long description should reference 'saras flow explain'")
	}
}

func TestFlowExplainSystemPromptIdentity(t *testing.T) {
	if !strings.Contains(flowExplainSystemPrompt, "You are SARAS") {
		t.Error("expected system prompt to identify as SARAS")
	}
}

func TestFlowExplainFullSystemPromptIdentity(t *testing.T) {
	if !strings.Contains(flowExplainFullSystemPrompt, "You are SARAS") {
		t.Error("expected full system prompt to identify as SARAS")
	}
}

func TestFlowCommandRegisteredOnRoot(t *testing.T) {
	found := false
	for _, sub := range rootCmd.Commands() {
		if sub == archCmd {
			found = true
			break
		}
	}
	if !found {
		t.Error("flow command not registered on root command")
	}
}
