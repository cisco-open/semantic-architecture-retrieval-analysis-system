/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/trace"
	"github.com/spf13/cobra"
)

// Disambiguation flag constants. Shared by every command that resolves a
// symbol from a name (`saras cfg`, `saras trace`, …) so users get the
// same --file/--language/--parent semantics everywhere.
const (
	flagFile     = "file"
	flagLanguage = "language"
	flagParent   = "parent"
	helpFile     = "Disambiguate by file path substring (e.g. pkg/auth)"
	helpLanguage = "Disambiguate by source language (e.g. go, python, ruby)"
	helpParent   = "Disambiguate by parent (Go receiver, Python class, Ruby module)"
)

// addSelectFlags registers the disambiguation flags on `cmd`. They map
// directly to fields on trace.SelectOptions / cfg.SelectOptions.
func addSelectFlags(cmd *cobra.Command) {
	cmd.Flags().String(flagFile, "", helpFile)
	cmd.Flags().String(flagLanguage, "", helpLanguage)
	cmd.Flags().String(flagParent, "", helpParent)
}

// errAmbiguousResolved is a sentinel returned by command runners once
// they have already emitted the disambiguation list to stderr. The cobra
// runner treats any non-nil error as a non-zero exit; the message is
// suppressed via SilenceErrors so the hint isn't printed twice.
var errAmbiguousResolved = errors.New("ambiguous symbol name (see hints above)")

// printAmbiguous writes the candidate list to cmd.ErrOrStderr() and
// silences cobra's default "Error:" prefix + usage banner so the user
// sees only the friendly hint. Returns errAmbiguousResolved so the
// caller can `return` it directly to ensure a non-zero exit.
func printAmbiguous(cmd *cobra.Command, amb *trace.AmbiguousSymbolError) error {
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	fmt.Fprint(cmd.ErrOrStderr(), amb.Error())
	return errAmbiguousResolved
}

// parseSymbolRef splits a positional argument of the form
// `path:symbolName` into its components. The path may be relative to
// projectRoot or absolute; if absolute and inside the project tree it
// is rewritten to be relative so it can be used as a file-path
// substring filter. Returns ("", arg) when arg has no recognisable path
// prefix — this lets ordinary names like `Login` and identifiers that
// happen to contain a colon (`Class::method`) pass through unchanged.
//
// Used by both `saras cfg` and `saras trace` to support shorthand like
// `saras cfg pkg/auth/login.go:authenticate` and
// `saras trace /Users/me/repo/pkg/auth/login.go:Login`.
func parseSymbolRef(arg, projectRoot string) (string, string) {
	idx := strings.LastIndex(arg, ":")
	if idx <= 0 || idx >= len(arg)-1 {
		return "", arg
	}
	pathPart := arg[:idx]
	namePart := arg[idx+1:]
	if !looksLikePath(pathPart) {
		return "", arg
	}
	if filepath.IsAbs(pathPart) && projectRoot != "" {
		if rel, rerr := filepath.Rel(projectRoot, pathPart); rerr == nil &&
			!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
			pathPart = rel
		}
	}
	return pathPart, namePart
}

// looksLikePath reports whether the prefix of a `path:name` argument is
// plausibly a path (so `Class:method` doesn't get treated as a file
// reference). A token counts as a path when it contains a path
// separator or has a file extension.
func looksLikePath(s string) bool {
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, "/\\") {
		return true
	}
	if filepath.Ext(s) != "" {
		return true
	}
	return false
}

// selectOptionsFromCmd reads the disambiguation flags off `cmd` and
// merges them with the path inferred from the positional argument.
// Explicit flags win over the inferred path so users can override on
// the command line. The returned value is a trace.SelectOptions, which
// is also the underlying type of cfg.SelectOptions.
func selectOptionsFromCmd(cmd *cobra.Command, pathHint string) trace.SelectOptions {
	opts := trace.SelectOptions{File: pathHint}
	if v, _ := cmd.Flags().GetString(flagFile); v != "" {
		opts.File = v
	}
	if v, _ := cmd.Flags().GetString(flagLanguage); v != "" {
		opts.Language = v
	}
	if v, _ := cmd.Flags().GetString(flagParent); v != "" {
		opts.Parent = v
	}
	return opts
}
