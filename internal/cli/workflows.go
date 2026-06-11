/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import "strings"

// sarasWorkflow holds canonical workflow data independent of any editor format.
type sarasWorkflow struct {
	name        string // e.g. "saras-search"
	description string // short description for frontmatter / command listing
	body        string // step-by-step instructions (may contain // turbo markers)
}

// editorWorkflow is a rendered workflow ready to write to disk.
type editorWorkflow struct {
	filename string
	content  string
}

// workflowsForEditor returns rendered workflows for the given editor.
// Supported editors: devin, cursor, claude, copilot.
// Codex has no custom command mechanism and returns nil.
func workflowsForEditor(editor string) []editorWorkflow {
	canonical := sarasWorkflows()
	var out []editorWorkflow
	for _, wf := range canonical {
		var ew editorWorkflow
		switch editor {
		case "devin":
			ew = formatDevin(wf)
		case "cursor":
			ew = formatCursor(wf)
		case "claude":
			ew = formatClaude(wf)
		case "copilot":
			ew = formatCopilot(wf)
		default:
			continue
		}
		out = append(out, ew)
	}
	return out
}

// workflowDir returns the workflow/command directory for the given editor + baseDir.
func workflowDir(editor, baseDir string) string {
	switch editor {
	case "devin":
		return baseDir + "/.devin/workflows"
	case "cursor":
		return baseDir + "/.cursor/commands"
	case "claude":
		return baseDir + "/.claude/commands"
	case "copilot":
		return baseDir + "/.github/prompts"
	default:
		return ""
	}
}

// stripTurbo removes Devin-specific "// turbo" annotations from body text.
func stripTurbo(body string) string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "// turbo" {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// workflowDisplayName converts "saras-search" to "SARAS: Search".
func workflowDisplayName(name string) string {
	trimmed := strings.TrimPrefix(name, "saras-")
	words := strings.Split(trimmed, "-")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return "SARAS: " + strings.Join(words, " ")
}

// formatDevin: .devin/workflows/saras-*.md with YAML frontmatter + // turbo.
func formatDevin(wf sarasWorkflow) editorWorkflow {
	content := "---\nname: \"" + workflowDisplayName(wf.name) + "\"\ndescription: " + wf.description + "\ncategory: Workflow\ntags: [workflow, saras]\n---\n\n" + wf.body
	return editorWorkflow{filename: wf.name + ".md", content: content}
}

// formatCursor: .cursor/commands/saras-*.md — YAML frontmatter with name, id, category, description.
func formatCursor(wf sarasWorkflow) editorWorkflow {
	body := stripTurbo(wf.body)
	content := "---\nname: /" + wf.name + "\nid: " + wf.name + "\ncategory: Workflow\ndescription: " + wf.description + "\n---\n\n" + body
	return editorWorkflow{filename: wf.name + ".md", content: content}
}

// formatClaude: .claude/commands/saras/<action>.md — YAML frontmatter with name, description, category, tags.
func formatClaude(wf sarasWorkflow) editorWorkflow {
	body := stripTurbo(wf.body)
	action := strings.TrimPrefix(wf.name, "saras-")
	content := "---\nname: \"" + workflowDisplayName(wf.name) + "\"\ndescription: " + wf.description + "\ncategory: Workflow\ntags: [workflow, saras]\n---\n\n" + body
	return editorWorkflow{filename: "saras/" + action + ".md", content: content}
}

// formatCopilot: .github/prompts/saras-*.prompt.md — .prompt.md with description frontmatter.
func formatCopilot(wf sarasWorkflow) editorWorkflow {
	body := stripTurbo(wf.body)
	content := "---\ndescription: " + wf.description + "\n---\n\n" + body
	return editorWorkflow{filename: wf.name + ".prompt.md", content: content}
}

// sarasWorkflows returns canonical workflow definitions for all SARAS features.
func sarasWorkflows() []sarasWorkflow {
	return []sarasWorkflow{
		{
			name:        "saras-search",
			description: `Search the codebase semantically using saras. Use when asked to "search the code", "find where this is defined", "find usage of", or "look for code related to".`,
			body: `1. Run a semantic search with the user's query:
// turbo
` + "```bash" + `
saras search "<query>" --limit 10 --json
` + "```" + `

2. Parse the JSON results and present the most relevant findings to the user, including file paths, line numbers, and code snippets.

3. If the user wants more context on a specific result, read the referenced file at the indicated line range.

4. If results are insufficient, try rephrasing the query or broadening with --limit 20.
`,
		},
		{
			name:        "saras-ask",
			description: `Ask a question about the codebase using saras RAG pipeline. Use when asked to "explain how this works", "how does X work", "what does this code do", or any natural language question about the codebase.`,
			body: `**Keep every ask atomic.** Each saras ask call should contain one focused question about a single topic. If the user's request covers multiple areas, break it into separate calls and combine the answers yourself.

1. Break the user's request into one or more atomic questions. Each question should target a single concept, function, or flow. Do NOT combine multiple topics into one call.

2. Run a saras ask call for each atomic question:
// turbo
` + "```bash" + `
saras ask --no-tui "<single focused question>"
` + "```" + `

3. If the question is about architecture or control flow, include the call-flow tree for better context:
// turbo
` + "```bash" + `
saras ask --no-tui --with-arch "<single focused question>"
` + "```" + `

4. If the question is about a specific function's behavior, scope the architecture context:
// turbo
` + "```bash" + `
saras ask --no-tui --with-arch=<functionName> "<single focused question>"
` + "```" + `

5. Present the LLM response to the user. If you ran multiple asks, synthesize the answers into a coherent response. If the answer references specific files, offer to open or show them.
`,
		},
		{
			name:        "saras-trace",
			description: `Trace a symbol through the codebase using saras. Use when asked to "trace this function", "what calls this", "where is this used", "find callers of", "find callees of", or "show references to".`,
			body: `1. First, find the symbol definition and all references:
// turbo
` + "```bash" + `
saras trace <SymbolName>
` + "```" + `

2. To find what calls this symbol:
// turbo
` + "```bash" + `
saras trace <SymbolName> --callers
` + "```" + `

3. To find what this symbol calls:
// turbo
` + "```bash" + `
saras trace <SymbolName> --callees
` + "```" + `

4. For a complete trace (definition + references + callers + callees):
// turbo
` + "```bash" + `
saras trace <SymbolName> --full
` + "```" + `

5. Present the trace results organized by category (definition, references, callers, callees). Offer to show the code at any referenced location.
`,
		},
		{
			name:        "saras-flow",
			description: `Visualize execution flow and call trees using saras. Use when asked to "show the execution flow", "show me the call tree", "what does main call", "show entry points", or "explain the control flow".`,
			body: `1. Generate the call-flow tree from all entry points:
// turbo
` + "```bash" + `
saras flow
` + "```" + `

2. To generate from a specific function:
// turbo
` + "```bash" + `
saras flow <FunctionName>
` + "```" + `

3. To limit depth (default 8):
// turbo
` + "```bash" + `
saras flow --depth 3
` + "```" + `

4. For an LLM-powered explanation of the flow:
// turbo
` + "```bash" + `
saras flow explain --no-tui
` + "```" + `

5. For an exhaustive deep-dive analysis:
// turbo
` + "```bash" + `
saras flow explain full --no-tui
` + "```" + `

6. Explain a specific function's flow:
// turbo
` + "```bash" + `
saras flow explain <FunctionName> --no-tui
` + "```" + `

7. Present the flow tree to the user. Markers: (cycle) = recursive, (↩) = already expanded, (...) = depth limit.
`,
		},
		{
			name:        "saras-map",
			description: `Generate a codebase architecture map using saras. Use when asked to "show me the architecture", "map the codebase", "show project structure", "generate an architecture overview", or "what packages exist".`,
			body: `1. Generate a compact summary overview:
// turbo
` + "```bash" + `
saras map --format summary
` + "```" + `

2. For a full markdown architecture report:
// turbo
` + "```bash" + `
saras map --format markdown
` + "```" + `

3. For a directory tree view:
// turbo
` + "```bash" + `
saras map --format tree
` + "```" + `

4. To save the architecture report to a file:
// turbo
` + "```bash" + `
saras map -f markdown -o ARCH.md
` + "```" + `

5. Present the architecture overview to the user. For large projects, start with summary and offer deeper views on request.
`,
		},
		{
			name:        "saras-cfg",
			description: `Generate a Control Flow Graph for a function using saras. Use when asked to "design tests for X", "what are the edge cases of X", "explain the control flow of X", "list the paths through X", or "show me branches in X".`,
			body: `1. Generate the CFG for the target function. Choose the format based on the user's needs:
// turbo
` + "```bash" + `
saras cfg <functionName>                         # Mermaid diagram (default)
saras cfg <functionName> --format text           # block / edge / path summary
saras cfg <functionName> --format paths          # just the execution paths
saras cfg paths <functionName>                   # alias for --format paths
` + "```" + `

2. If the function name is ambiguous (multiple matches), disambiguate:
// turbo
` + "```bash" + `
saras cfg <functionName> --file <path-substring>
saras cfg <functionName> --language <lang>
saras cfg <functionName> --parent <ClassName>
saras cfg <path/to/file.go>:<functionName>       # path:fn shorthand
` + "```" + `

3. For test design, include surrounding code context (types, callee signatures):
// turbo
` + "```bash" + `
saras cfg paths <functionName> --with-context
` + "```" + `

4. To see branches inside called helpers, inline their CFGs:
// turbo
` + "```bash" + `
saras cfg paths <functionName> --inline-callees
saras cfg paths <functionName> --inline-callees --max-inline-depth 3
` + "```" + `

5. For an LLM-powered walkthrough of every execution path:
// turbo
` + "```bash" + `
saras cfg explain <functionName> --no-tui
` + "```" + `

6. Present the results. For test design, map each enumerated path to a test case. For edge-case analysis, highlight paths with error returns or early exits.
`,
		},
		{
			name:        "saras-understand-codebase",
			description: `Get a comprehensive understanding of the codebase using saras. Use when asked to "understand this codebase", "give me an overview", "onboard me to this project", or "explain the project structure".`,
			body: `1. Start with a high-level architecture overview:
// turbo
` + "```bash" + `
saras map --format summary
` + "```" + `

2. Get the full directory structure:
// turbo
` + "```bash" + `
saras map --format tree
` + "```" + `

3. Identify entry points and main execution flows:
// turbo
` + "```bash" + `
saras flow --depth 3
` + "```" + `

4. Ask saras to explain the overall architecture:
// turbo
` + "```bash" + `
saras ask --no-tui --with-arch "what is the overall architecture and main components of this project?"
` + "```" + `

5. Synthesize all the information into a clear summary for the user covering:
   - Project purpose and structure
   - Key packages/modules and their responsibilities
   - Main entry points and execution flows
   - Important patterns and conventions
`,
		},
		{
			name:        "saras-cross-repo",
			description: `Search and query across linked repository dependencies using saras. Use when asked to "search dependencies", "find in other repos", "cross-repo search", "check the legacy code", or "look in shared libs".`,
			body: `1. First check what dependencies are configured:
// turbo
` + "```bash" + `
saras dep list
` + "```" + `

2. To search a specific dependency:
// turbo
` + "```bash" + `
saras search --from-dep <dep-name> "<query>" --json
` + "```" + `

3. To search all dependencies (excludes current repo):
// turbo
` + "```bash" + `
saras search --all-deps "<query>" --json
` + "```" + `

4. To search current repo AND all dependencies:
// turbo
` + "```bash" + `
saras search --with-deps "<query>" --json
` + "```" + `

5. The same flags work on ask, trace, flow, and map:
// turbo
` + "```bash" + `
saras ask --from-dep <dep-name> --no-tui "<question>"
saras trace --with-deps <SymbolName>
saras flow --from-dep <dep-name>
saras map --from-dep <dep-name> --format summary
` + "```" + `

6. Results from dependencies are labeled [role: name] in output. Present findings with clear attribution to the source repository.
`,
		},
		{
			name:        "saras-add-dependency",
			description: `Add a cross-repository dependency to the current saras project. Use when asked to "add a dependency", "link another repo", "connect repository", or "add cross-repo".`,
			body: `1. Confirm the path to the dependency repository and ask the user for the role:
   - **legacy**: predecessor codebase being migrated from
   - **shared-lib**: shared library or utility repository
   - **reference**: reference implementation or design patterns
   - **service**: microservice that this project interacts with

2. Add the dependency:
` + "```bash" + `
saras dep add <path-to-repo> --role <role>
` + "```" + `

3. Optionally specify a custom name (defaults to directory name):
` + "```bash" + `
saras dep add <path-to-repo> --role <role> --name <custom-name>
` + "```" + `

4. Verify the dependency was added:
// turbo
` + "```bash" + `
saras dep list
` + "```" + `

5. Note: The dependency repo must be SARAS-initialized (has .saras/ directory) and must use compatible embeddings (same provider, model, and dimensions).
`,
		},
		{
			name:        "saras-write-tests",
			description: `Write tests for a function using saras CFG path analysis. Use when asked to "write tests for X", "generate tests for X", "create test cases for X", or "test this function".`,
			body: `**This workflow has two modes:**
- **With a function name** (e.g., "write tests for authenticate"): writes tests for that single function.
- **Without a function name** (e.g., "write tests for the project", "generate tests", "test everything"): scans the entire project and writes tests for all untested functions.

---

## Mode A: No function name provided (whole-project test generation)

1. Get the project architecture to build a function inventory:
// turbo
` + "```bash" + `
saras map --format summary
` + "```" + `

2. List all functions by generating the full flow tree:
// turbo
` + "```bash" + `
saras flow --depth 2
` + "```" + `

3. Search for existing test files to identify what is already covered:
// turbo
` + "```bash" + `
saras search "test" --limit 50 --json
` + "```" + `

4. **STOP and confirm with the user before proceeding.** Present:
   - The total number of functions discovered
   - Which files/functions already have tests (and will be skipped)
   - The ordered list of functions that will get new tests
   - Estimated scope (number of test files to create)

   Ask: "I found N functions without test coverage. I'll generate tests for each one, working through the list file-by-file. This may take a while. Proceed?"

   **Do NOT continue until the user confirms.**

5. For each untested function, execute **Mode B** (steps 1–8 below). Work file-by-file:
   - Group functions by source file
   - For each file, generate/append to the corresponding test file following the project's convention (e.g., ` + "`foo.go`" + ` → ` + "`foo_test.go`" + `, ` + "`foo.py`" + ` → ` + "`test_foo.py`" + `, ` + "`foo.ts`" + ` → ` + "`foo.test.ts`" + `)
   - After finishing each file's tests, run the project's test command to verify they compile and pass. Detect the test runner from the project (e.g., ` + "`go test ./...`" + ` for Go, ` + "`pytest`" + ` for Python, ` + "`npm test`" + ` for JS/TS, ` + "`mvn test`" + ` for Java, ` + "`cargo test`" + ` for Rust). Check the Makefile or CI config if unsure.
   - If tests fail, fix them before moving to the next file
   - Report progress to the user: "Completed X/N files (Y tests written)"

6. After all functions are done, present a final summary:
   - Total test files created/modified
   - Total test cases written
   - Any functions that were skipped (and why)
   - Final test suite pass/fail status

---

## Mode B: Specific function name provided

1. Generate the CFG with execution paths and surrounding code context for the target function:
// turbo
` + "```bash" + `
saras cfg paths <functionName> --with-context
` + "```" + `

2. If the function calls project-internal helpers whose branches matter for coverage, inline their CFGs:
// turbo
` + "```bash" + `
saras cfg paths <functionName> --with-context --inline-callees
` + "```" + `

3. If the function name is ambiguous, disambiguate with --file, --language, or --parent as prompted by the CLI output.

4. Parse the output. Each enumerated **Path** represents a unique combination of branches through the function. The **--with-context** appendix provides:
   - The function's source code
   - Receiver / class type definitions
   - Referenced type definitions
   - Callee signatures

5. Design one test case per execution path. For each path:
   - Name the test after the path's distinguishing condition (e.g., ` + "`TestAuthenticate_InvalidToken_ReturnsError`" + `)
   - Set up inputs that force execution down that specific path
   - Assert the expected outcome (return value, side-effect, error)
   - Use the context appendix to construct real types — avoid mocks unless the callee is an interface or external dependency

6. If you need to understand how a callee behaves to set up the test, ask saras:
// turbo
` + "```bash" + `
saras ask --no-tui "what does <calleeName> return when given <condition>?"
` + "```" + `

7. Write the test file. Follow the project's existing test conventions (test framework, file naming, directory structure). Use the paths as a checklist — every path should have a corresponding test.

8. Present a summary mapping each CFG path to its test case so the user can verify coverage.
`,
		},
		{
			name:        "saras-refactor",
			description: `Plan a safe refactoring using saras trace and CFG analysis. Use when asked to "refactor X", "rename X safely", "change the signature of X", or "move X".`,
			body: `1. Get a full trace of the symbol to understand its usage across the codebase:
// turbo
` + "```bash" + `
saras trace <symbolName> --full
` + "```" + `

2. Identify all callers that will be affected by the change:
// turbo
` + "```bash" + `
saras trace <symbolName> --callers
` + "```" + `

3. For each significant caller, generate CFG paths to understand how it uses the symbol:
// turbo
` + "```bash" + `
saras cfg paths <callerFunction> --with-context
` + "```" + `

4. If the symbol is a function, generate its own CFG to understand internal branching:
// turbo
` + "```bash" + `
saras cfg paths <symbolName> --with-context --inline-callees
` + "```" + `

5. If you need to understand the broader architectural role of the symbol:
// turbo
` + "```bash" + `
saras ask --no-tui "what is the role of <symbolName> in the architecture? what invariants does it maintain?"
` + "```" + `

6. Produce a refactoring plan with:
   - **Impact summary**: list every file and function affected (from trace)
   - **Change checklist**: one item per call site, describing what needs to change
   - **Risk assessment**: which callers have complex branching around the symbol (from CFG paths)
   - **Test impact**: which existing tests exercise the affected paths
   - **Suggested order**: safest sequence of edits to avoid breaking intermediate states

7. Execute the refactoring following the plan. After each file edit, verify correctness before moving to the next call site.
`,
		},
		{
			name:        "saras-debug",
			description: `Diagnose and fix a bug using saras trace and CFG path analysis. Use when asked to "debug X", "why does X fail", "find the bug in X", "fix the error in X", or "X is broken".`,
			body: `1. Generate the CFG with all execution paths for the suspect function:
// turbo
` + "```bash" + `
saras cfg paths <functionName> --with-context --inline-callees
` + "```" + `

2. Examine each path. Match the user's bug description (error message, unexpected behavior, crash) against the path conditions to narrow down which path is likely triggered.

3. Trace the function to understand what calls it and how inputs arrive:
// turbo
` + "```bash" + `
saras trace <functionName> --callers
` + "```" + `

4. If the bug involves a callee misbehaving, trace into it:
// turbo
` + "```bash" + `
saras trace <calleeName> --full
` + "```" + `

5. Ask saras for deeper understanding of specific conditions:
// turbo
` + "```bash" + `
saras ask --no-tui "under what conditions does <functionName> return <error/unexpected value>?"
` + "```" + `

6. Identify the root cause by mapping the bug symptoms to a specific CFG path:
   - Which path produces the observed behavior?
   - What input conditions trigger that path?
   - Is the bug in this function or in a callee?

7. Propose a fix targeting the root cause (not symptoms). Include:
   - The specific path and condition causing the bug
   - The minimal code change to fix it
   - A regression test case covering that path (use the CFG path as the test template)

8. Implement the fix and regression test. Verify other paths are not affected.
`,
		},
		{
			name:        "saras-document",
			description: `Generate code documentation for a function or module using saras analysis. Use when asked to "document X", "add docs to X", "write documentation for X", or "explain X for the README".`,
			body: `1. Get the execution flow to understand the function's role in the system:
// turbo
` + "```bash" + `
saras flow <functionName>
` + "```" + `

2. Generate CFG paths with context to understand all behaviors:
// turbo
` + "```bash" + `
saras cfg paths <functionName> --with-context
` + "```" + `

3. If the function has significant internal helpers, inline them:
// turbo
` + "```bash" + `
saras cfg paths <functionName> --with-context --inline-callees
` + "```" + `

4. Ask saras for a high-level summary:
// turbo
` + "```bash" + `
saras ask --no-tui "what is the purpose and contract of <functionName>? what are its preconditions and postconditions?"
` + "```" + `

5. Write documentation covering:
   - **Purpose**: one-line summary of what the function does
   - **Parameters**: each parameter's role, valid values, and constraints
   - **Return values**: what each return value means, including error cases
   - **Error conditions**: enumerate from CFG paths — each path ending in an error return becomes a documented error case
   - **Side effects**: any state mutations, I/O, or external calls (visible in the CFG and context)
   - **Example usage**: if applicable, a minimal usage example

6. Format the documentation following the project's existing doc style (Go doc comments, JSDoc, docstrings, etc.). Place it directly above the function definition.

7. If documenting a module/package, also run:
// turbo
` + "```bash" + `
saras map --format summary
` + "```" + `
And write a package-level doc comment covering the module's responsibility, key types, and entry points.
`,
		},
		{
			name:        "saras-impact",
			description: `Analyze the impact of changing a symbol using saras trace and CFG analysis. Use when asked to "what breaks if I change X", "impact of changing X", "who depends on X", "is it safe to change X", or "blast radius of X".`,
			body: `1. Get a full trace to find all references to the symbol:
// turbo
` + "```bash" + `
saras trace <symbolName> --full
` + "```" + `

2. Get all direct callers:
// turbo
` + "```bash" + `
saras trace <symbolName> --callers
` + "```" + `

3. Get all callees to understand downstream dependencies:
// turbo
` + "```bash" + `
saras trace <symbolName> --callees
` + "```" + `

4. For each caller, check how tightly coupled it is to the symbol by examining its CFG paths:
// turbo
` + "```bash" + `
saras cfg paths <callerFunction> --with-context
` + "```" + `

5. Check if the symbol is used in cross-repo dependencies:
// turbo
` + "```bash" + `
saras search --with-deps "<symbolName>" --json
` + "```" + `

6. Produce an impact report with:
   - **Direct dependents**: functions that call/reference the symbol (from trace --callers)
   - **Transitive dependents**: functions that call the direct dependents (trace each caller's callers if needed)
   - **Coupling assessment**: for each caller, how many CFG paths depend on the symbol's behavior
   - **Test coverage**: which existing tests exercise the affected paths
   - **Cross-repo impact**: any dependencies that reference this symbol
   - **Risk rating**: low (few callers, simple usage), medium (multiple callers, some branching), high (many callers, complex branching, cross-repo)
   - **Recommendation**: safe to change / change with caution / needs migration plan
`,
		},
		{
			name:        "saras-api-contract",
			description: `Document an API endpoint end-to-end using saras flow, CFG, and trace analysis. Use when asked to "document this API", "what does this endpoint do", "API contract for X", "describe the request/response for X", or "endpoint documentation".`,
			body: `1. Generate the call-flow tree from the handler function:
// turbo
` + "```bash" + `
saras flow <handlerFunction>
` + "```" + `

2. Generate CFG paths with inlined callees to see every branch the request can take:
// turbo
` + "```bash" + `
saras cfg paths <handlerFunction> --with-context --inline-callees
` + "```" + `

3. Trace the handler to understand its dependencies:
// turbo
` + "```bash" + `
saras trace <handlerFunction> --callees
` + "```" + `

4. Ask saras about request/response shapes:
// turbo
` + "```bash" + `
saras ask --no-tui "what are the request parameters and response format for <handlerFunction>?"
` + "```" + `

5. Search for route registration to find the HTTP method and path:
// turbo
` + "```bash" + `
saras search "<handlerFunction>" --limit 10 --json
` + "```" + `

6. Produce an API contract document covering:
   - **Endpoint**: HTTP method + path (from route registration)
   - **Authentication / Authorization**: any auth middleware or checks (visible in flow tree)
   - **Request format**: parameters, headers, body schema (from context types)
   - **Validation**: input validation rules (from early CFG paths that return 400)
   - **Success response**: status code + body schema (from the happy-path CFG path)
   - **Error responses**: map each error-returning CFG path to an HTTP status code and error body
   - **Side effects**: database writes, external API calls, queue publishes (from callees)
   - **Rate limiting / caching**: if visible in the flow tree
   - **Example request/response**: one happy-path example and one error example
`,
		},
		{
			name:        "saras-reindex",
			description: `Reindex the codebase with saras. Use when asked to "reindex", "refresh the index", "update the search index", or when search results seem stale.`,
			body: `1. Run a full reindex:
` + "```bash" + `
saras reindex
` + "```" + `

2. Confirm reindexing completed successfully and inform the user that search, ask, and trace results will now reflect the latest code.
`,
		},
	}
}
