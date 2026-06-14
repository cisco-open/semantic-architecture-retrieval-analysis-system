/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and SARAS Contributors
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package lang

import (
	"regexp"
	"strings"
)

func init() { Register(&HCLParser{}) }

// HCLParser extracts symbols from HCL / Terraform files.
// Supports Terraform resources, data sources, variables, outputs, modules,
// locals, providers, and generic HCL blocks.
type HCLParser struct{}

func (p *HCLParser) Name() string         { return "hcl" }
func (p *HCLParser) Extensions() []string { return []string{".tf", ".hcl", ".tfvars"} }

func (p *HCLParser) FlowHints() FlowHints {
	return FlowHints{
		Keywords: []string{
			"resource", "data", "variable", "output", "module",
			"locals", "provider", "terraform", "backend",
			"for_each", "count", "depends_on", "lifecycle",
			"provisioner", "connection", "dynamic",
			"moved", "import", "check",
		},
		CommentPrefixes: []string{"#", "//"},
	}
}

func (p *HCLParser) IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	parts := strings.Split(lower, "/")
	for _, part := range parts {
		switch part {
		case "test", "tests", "testdata", "fixtures":
			return true
		}
	}
	return strings.HasSuffix(lower, "_test.tf") ||
		strings.HasSuffix(lower, "_test.hcl")
}

var (
	// resource "type" "name" {
	hclResourcePattern = regexp.MustCompile(`^\s*resource\s+"([\w-]+)"\s+"([\w-]+)"`)
	// data "type" "name" {
	hclDataPattern = regexp.MustCompile(`^\s*data\s+"([\w-]+)"\s+"([\w-]+)"`)
	// variable "name" {
	hclVariablePattern = regexp.MustCompile(`^\s*variable\s+"([\w-]+)"`)
	// output "name" {
	hclOutputPattern = regexp.MustCompile(`^\s*output\s+"([\w-]+)"`)
	// module "name" {
	hclModulePattern = regexp.MustCompile(`^\s*module\s+"([\w-]+)"`)
	// provider "name" {
	hclProviderPattern = regexp.MustCompile(`^\s*provider\s+"([\w-]+)"`)
	// locals {
	hclLocalsPattern = regexp.MustCompile(`^\s*locals\s*\{`)
	// key = value inside locals block
	hclLocalVarPattern = regexp.MustCompile(`^\s*(\w+)\s*=`)
	// terraform { ... }
	hclTerraformPattern = regexp.MustCompile(`^\s*terraform\s*\{`)
	// moved { ... }
	hclMovedPattern = regexp.MustCompile(`^\s*moved\s*\{`)
	// import { ... }
	hclImportPattern = regexp.MustCompile(`^\s*import\s*\{`)
)

func (p *HCLParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	braceDepth := 0
	inLocals := false
	localsDepth := 0

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
			continue
		}

		// Skip block comments
		if strings.HasPrefix(trimmed, "/*") {
			continue
		}

		prevDepth := braceDepth
		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")

		// Track locals block exit
		if inLocals && braceDepth <= localsDepth {
			inLocals = false
		}

		// resource "type" "name"
		if m := hclResourcePattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: m[1] + "." + m[2], Kind: KindType, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			continue
		}

		// data "type" "name"
		if m := hclDataPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: "data." + m[1] + "." + m[2], Kind: KindType, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			continue
		}

		// variable "name"
		if m := hclVariablePattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindVariable, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			continue
		}

		// output "name"
		if m := hclOutputPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindVariable, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed, Parent: "output",
			})
			continue
		}

		// module "name"
		if m := hclModulePattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindModule, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			continue
		}

		// provider "name"
		if m := hclProviderPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindModule, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed, Parent: "provider",
			})
			continue
		}

		// locals { ... }
		if hclLocalsPattern.MatchString(trimmed) {
			inLocals = true
			localsDepth = prevDepth
			continue
		}

		// Local variables inside locals block (top-level assignments)
		if inLocals && prevDepth == localsDepth+1 {
			if m := hclLocalVarPattern.FindStringSubmatch(trimmed); m != nil {
				symbols = append(symbols, Symbol{
					Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum,
					Signature: trimmed, Parent: "locals",
				})
				continue
			}
		}

		// terraform block
		if hclTerraformPattern.MatchString(trimmed) {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: "terraform", Kind: KindModule, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			continue
		}

		// moved block
		if hclMovedPattern.MatchString(trimmed) {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: "moved", Kind: KindType, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			continue
		}

		// import block
		if hclImportPattern.MatchString(trimmed) {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: "import", Kind: KindImport, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			continue
		}
	}

	return symbols
}
