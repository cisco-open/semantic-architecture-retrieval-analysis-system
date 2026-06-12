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

func init() { Register(&ProtobufParser{}) }

// ProtobufParser extracts symbols from Protocol Buffers definition files.
// Supports proto2 and proto3 syntax.
type ProtobufParser struct{}

func (p *ProtobufParser) Name() string         { return "protobuf" }
func (p *ProtobufParser) Extensions() []string { return []string{".proto"} }

func (p *ProtobufParser) FlowHints() FlowHints {
	return FlowHints{
		Keywords: []string{
			"syntax", "package", "import", "option",
			"message", "enum", "service", "rpc",
			"oneof", "map", "repeated", "optional", "required",
			"reserved", "extensions", "extend",
			"returns", "stream",
			"string", "int32", "int64", "uint32", "uint64",
			"sint32", "sint64", "fixed32", "fixed64",
			"sfixed32", "sfixed64", "bool", "float", "double", "bytes",
		},
		CommentPrefixes: []string{"//"},
	}
}

func (p *ProtobufParser) IsTestFile(path string) bool {
	lower := strings.ToLower(path)
	parts := strings.Split(lower, "/")
	for _, part := range parts {
		switch part {
		case "test", "tests", "testdata", "fixtures":
			return true
		}
	}
	return strings.Contains(lower, "_test.proto")
}

var (
	// syntax = "proto3";
	protoSyntaxPattern = regexp.MustCompile(`^\s*syntax\s*=\s*"(proto[23])"`)
	// package foo.bar;
	protoPackagePattern = regexp.MustCompile(`^\s*package\s+([\w.]+)\s*;`)
	// import "path/to/file.proto";
	protoImportPattern = regexp.MustCompile(`^\s*import\s+(?:public\s+|weak\s+)?"([^"]+)"`)
	// message Name {
	protoMessagePattern = regexp.MustCompile(`^\s*message\s+(\w+)\s*\{?`)
	// enum Name {
	protoEnumPattern = regexp.MustCompile(`^\s*enum\s+(\w+)\s*\{?`)
	// service Name {
	protoServicePattern = regexp.MustCompile(`^\s*service\s+(\w+)\s*\{?`)
	// rpc MethodName (Request) returns (Response) { ... }
	protoRPCPattern = regexp.MustCompile(`^\s*rpc\s+(\w+)\s*\(`)
	// oneof name {
	protoOneofPattern = regexp.MustCompile(`^\s*oneof\s+(\w+)\s*\{?`)
	// option name = value;
	protoOptionPattern = regexp.MustCompile(`^\s*option\s+([\w.()]+)\s*=`)
	// extend Name {
	protoExtendPattern = regexp.MustCompile(`^\s*extend\s+(\w+)\s*\{?`)
)

func (p *ProtobufParser) ExtractSymbols(content string) []Symbol {
	lines := strings.Split(content, "\n")
	var symbols []Symbol
	braceDepth := 0
	inType := false
	typeName := ""
	typeDepth := 0
	inService := false
	serviceName := ""
	serviceDepth := 0

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}

		// Skip block comments
		if strings.HasPrefix(trimmed, "/*") {
			continue
		}

		prevDepth := braceDepth
		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")

		// Track type/service block exit
		if inType && braceDepth <= typeDepth {
			inType = false
			typeName = ""
		}
		if inService && braceDepth <= serviceDepth {
			inService = false
			serviceName = ""
		}

		// syntax
		if m := protoSyntaxPattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed,
			})
			continue
		}

		// package
		if m := protoPackagePattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindPackage, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed,
			})
			continue
		}

		// import
		if m := protoImportPattern.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindImport, StartLine: lineNum, EndLine: lineNum,
				Signature: trimmed,
			})
			continue
		}

		// service
		if m := protoServicePattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindInterface, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed,
			})
			inService = true
			serviceDepth = prevDepth
			serviceName = m[1]
			continue
		}

		// rpc
		if m := protoRPCPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := lineNum
			if strings.Contains(trimmed, "{") {
				endLine = findBraceEnd(lines, i, prevDepth)
			}
			parent := ""
			if inService {
				parent = serviceName
			}
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindMethod, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed, Parent: parent,
			})
			continue
		}

		// enum
		if m := protoEnumPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			parent := ""
			if inType {
				parent = typeName
			}
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindEnum, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed, Parent: parent,
			})
			continue
		}

		// message
		if m := protoMessagePattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			parent := ""
			if inType {
				parent = typeName
			}
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindStruct, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed, Parent: parent,
			})
			inType = true
			typeDepth = prevDepth
			typeName = m[1]
			continue
		}

		// oneof
		if m := protoOneofPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			parent := ""
			if inType {
				parent = typeName
			}
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindProperty, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed, Parent: parent,
			})
			continue
		}

		// extend
		if m := protoExtendPattern.FindStringSubmatch(trimmed); m != nil {
			endLine := findBraceEnd(lines, i, prevDepth)
			symbols = append(symbols, Symbol{
				Name: m[1], Kind: KindType, StartLine: lineNum, EndLine: endLine,
				Signature: trimmed, Parent: "extend",
			})
			continue
		}

		// top-level option
		if !inType && !inService {
			if m := protoOptionPattern.FindStringSubmatch(trimmed); m != nil {
				symbols = append(symbols, Symbol{
					Name: m[1], Kind: KindConstant, StartLine: lineNum, EndLine: lineNum,
					Signature: trimmed,
				})
				continue
			}
		}
	}

	return symbols
}
