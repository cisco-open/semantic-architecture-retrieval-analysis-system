/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	version string
	rootCmd = &cobra.Command{
		Use:   "saras",
		Short: "AI-native code intelligence CLI",
		Long: `saras is an AI-native code intelligence tool that indexes your codebase,
watches for changes, and lets you search and ask questions about your code
using natural language.

It runs 100% locally by default with Ollama, and also supports LM Studio
and any OpenAI-compatible API.

Commands:
  init     Initialize saras in a project directory
  watch    Start the indexing daemon (watches for file changes)
  search   Semantic code search
  ask      Ask a question about your codebase (LLM-powered)
  trace    Explore callers, callees, and the call graph
  flow     Show execution call-flow (aliases: architecture, arch)
  map      Generate an architecture overview
  serve    Start the MCP server for AI agent integration
  update   Update saras to the latest version`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
)

// SetVersion sets the build version string shown by the version command.
func SetVersion(v string) {
	version = v
}

// Version returns the current build version.
func Version() string {
	return version
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

// GetRootCmd returns the root cobra command (useful for doc generation).
func GetRootCmd() *cobra.Command {
	return rootCmd
}

func init() {
	rootCmd.PersistentFlags().Bool("stylize-output", false, "Render LLM markdown output with terminal styling (colors, syntax highlighting)")
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print saras version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "saras version %s\n", version)
	},
}
