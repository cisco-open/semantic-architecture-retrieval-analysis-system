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

func init() { Register(&GroovyParser{}) }

// GroovyParser extracts symbols from Groovy source files and Jenkinsfiles.
// It handles both general Groovy code and Jenkins pipeline DSL constructs.
type GroovyParser struct{}

func (p *GroovyParser) Name() string         { return "groovy" }
func (p *GroovyParser) Extensions() []string { return []string{".groovy", ".gvy", ".gy", ".gsh"} }

// Filenames returns exact filenames this parser handles (no extension).
func (p *GroovyParser) Filenames() []string {
	return []string{"Jenkinsfile"}
}

func (p *GroovyParser) FlowHints() FlowHints {
	return FlowHints{
		EntryFunctions: []string{"main", "call"},
		Keywords: []string{
			"if", "else", "for", "while", "switch", "case", "default",
			"break", "continue", "return", "throw", "throws",
			"try", "catch", "finally", "assert",
			"def", "class", "interface", "trait", "enum", "extends", "implements",
			"import", "package", "as", "in", "instanceof",
			"static", "final", "abstract", "synchronized",
			"public", "private", "protected",
			"new", "this", "super", "null", "true", "false",
			"println", "print",
			// Jenkins DSL
			"pipeline", "agent", "stages", "stage", "steps", "step",
			"post", "when", "environment", "parameters", "triggers",
			"options", "tools", "input", "parallel", "script",
			"node", "sh", "bat", "echo", "checkout", "dir",
			"withCredentials", "withEnv", "timeout", "retry",
		},
		CommentPrefixes: []string{"//"},
	}
}

func (p *GroovyParser) IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	parts := strings.Split(lower, "/")
	for _, part := range parts {
		switch part {
		case "test", "tests", "testdata", "fixtures":
			return true
		}
	}
	return strings.HasSuffix(lower, "test.groovy") ||
		strings.HasSuffix(lower, "tests.groovy") ||
		strings.HasSuffix(lower, "spec.groovy")
}

var (
	// def functionName(...) or Type functionName(...)
	groovyDefPattern = regexp.MustCompile(`^\s*(?:(?:public|private|protected|static|final|synchronized|abstract)\s+)*(?:def|void|int|long|float|double|boolean|String|Object|Map|List|[\w.<>,\[\]]+)\s+(\w+)\s*\(`)
	// class Name
	groovyClassPattern = regexp.MustCompile(`^\s*(?:(?:public|private|protected|static|final|abstract)\s+)*class\s+(\w+)`)
	// interface Name
	groovyInterfacePattern = regexp.MustCompile(`^\s*(?:(?:public|private|protected|static)\s+)*interface\s+(\w+)`)
	// trait Name
	groovyTraitPattern = regexp.MustCompile(`^\s*(?:(?:public|private|protected|static)\s+)*trait\s+(\w+)`)
	// enum Name
	groovyEnumPattern = regexp.MustCompile(`^\s*(?:(?:public|private|protected)\s+)*enum\s+(\w+)`)
	// package name
	groovyPackagePattern = regexp.MustCompile(`^\s*package\s+([\w.]+)`)
	// import name
	groovyImportPattern = regexp.MustCompile(`^\s*import\s+(?:static\s+)?([\w.*]+)`)
	// @Library('name')
	groovyLibraryPattern = regexp.MustCompile(`^\s*@Library\(\s*['"]([^'"]+)['"]`)
	// pipeline {
	groovyPipelinePattern = regexp.MustCompile(`^\s*pipeline\s*\{`)
	// stage('Name') { or stage("Name") {
	groovyStagePattern = regexp.MustCompile(`^\s*stage\s*\(\s*['"]([^'"]+)['"]`)
	// node('label') { or node {
	groovyNodePattern = regexp.MustCompile(`^\s*node\s*(?:\(\s*['"]([^'"]+)['"]\s*\))?\s*\{`)
	// environment {
	groovyEnvBlockPattern = regexp.MustCompile(`^\s*environment\s*\{`)
	// KEY = value (inside environment block)
	groovyEnvVarPattern = regexp.MustCompile(`^\s*([A-Z_][A-Z0-9_]*)\s*=`)
	// parameters { ... }
	groovyParamsBlockPattern = regexp.MustCompile(`^\s*parameters\s*\{`)
	// string(name: 'PARAM', ...) or booleanParam(name: 'PARAM', ...)
	groovyParamDefPattern = regexp.MustCompile(`(?:string|booleanParam|choice|text|password|file)\s*\(\s*name\s*:\s*['"]([^'"]+)['"]`)
)

func (p *GroovyParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	braceDepth := 0
	inClass := false
	className := ""
	classDepth := 0
	inEnvBlock := false
	envBlockDepth := 0
	inParamsBlock := false
	paramsBlockDepth := 0

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}

		prevDepth := braceDepth
		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")

		if inClass && braceDepth <= classDepth {
			inClass = false
			className = ""
		}
		if inEnvBlock && braceDepth <= envBlockDepth {
			inEnvBlock = false
		}
		if inParamsBlock && braceDepth <= paramsBlockDepth {
			inParamsBlock = false
		}

		// Package
		if m := groovyPackagePattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindPackage, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed,
			})
			continue
		}

		// Import
		if m := groovyImportPattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindImport, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed,
			})
			continue
		}

		// @Library
		if m := groovyLibraryPattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindImport, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed,
			})
			continue
		}

		// pipeline block
		if groovyPipelinePattern.MatchString(trimmed) {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: "pipeline", Kind: KindModule, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			continue
		}

		// stage('Name')
		if m := groovyStagePattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindFunction, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			continue
		}

		// node('label') or node {
		if m := groovyNodePattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			name := "node"
			if m[1] != "" {
				name = "node:" + m[1]
			}
			symbols = append(symbols, Symbol{
				Name: name, Kind: KindType, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			continue
		}

		// environment block
		if groovyEnvBlockPattern.MatchString(trimmed) {
			inEnvBlock = true
			envBlockDepth = prevDepth
			continue
		}

		// Environment variables
		if inEnvBlock && braceDepth == envBlockDepth+1 {
			if m := groovyEnvVarPattern.FindStringSubmatch(trimmed); m != nil {
				symbols = append(symbols, Symbol{
					Name: m[1], Kind: KindVariable, StartLine: lineNum, EndLine: lineNum,
					Signature: trimmed, Parent: "environment",
				})
				continue
			}
		}

		// parameters block
		if groovyParamsBlockPattern.MatchString(trimmed) {
			inParamsBlock = true
			paramsBlockDepth = prevDepth
			continue
		}

		// Parameter definitions
		if inParamsBlock {
			if m := groovyParamDefPattern.FindStringSubmatch(trimmed); m != nil {
				symbols = append(symbols, Symbol{
					Name: m[1], Kind: KindVariable, StartLine: lineNum, EndLine: lineNum,
					Signature: trimmed, Parent: "parameters",
				})
				continue
			}
		}

		// Enum
		if m := groovyEnumPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindEnum, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			inClass = true
			classDepth = prevDepth
			className = m[1]
			continue
		}

		// Trait
		if m := groovyTraitPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindTrait, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			inClass = true
			classDepth = prevDepth
			className = m[1]
			continue
		}

		// Interface
		if m := groovyInterfacePattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindInterface, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			inClass = true
			classDepth = prevDepth
			className = m[1]
			continue
		}

		// Class
		if m := groovyClassPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindClass, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			inClass = true
			classDepth = prevDepth
			className = m[1]
			continue
		}

		// Function / method (def or typed)
		if m := groovyDefPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			kind := KindFunction
			parent := ""
			if inClass {
				kind = KindMethod
				parent = className
			}
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: kind, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed, Parent: parent,
			})
			continue
		}
	}

	return symbols
}
