<h1 align="center">Semantic Architecture & Retrieval Analysis System (SARAS)</h1>

<p align="center">
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go" alt="Go Version"></a>
  <a href="https://www.apache.org/licenses/LICENSE-2.0"><img src="https://img.shields.io/badge/License-Apache%202.0-blue.svg" alt="License: Apache 2.0"></a>
  <a href="https://github.com/cisco-open/semantic-architecture-retrieval-analysis-system/releases"><img src="https://img.shields.io/github/v/release/cisco-open/semantic-architecture-retrieval-analysis-system?style=flat" alt="Release"></a>
</p>

**Codebase intelligence for AI agents and developers.** Reduce LLM context length by giving your agent precise, relevant code instead of entire files.

SARAS indexes your codebase into vector embeddings and exposes semantic search, RAG-powered Q&A, symbol tracing, and architecture mapping. Everything is accessible via CLI or [MCP](https://modelcontextprotocol.io/). Instead of stuffing thousands of lines into an LLM's context window, agents can query SARAS to retrieve only the symbols, definitions, and code paths they actually need.

## Why SARAS?

AI coding agents today hit context window limits fast. A single "explain how auth works" prompt can pull in dozens of files. SARAS solves this by acting as a **local codebase knowledge layer**:

- **Smaller context, better answers.** Agents retrieve targeted code chunks via semantic search instead of dumping whole files into the prompt.
- **Symbol-level precision.** Trace a function's callers and callees across your entire codebase without loading everything into context.
- **Architecture-aware.** Agents can get a structural overview of the project in a few hundred tokens instead of browsing every directory.
- **Works with any LLM.** Ollama, LM Studio, OpenAI, or any OpenAI-compatible endpoint.

## Features

- **Multi-language** symbol-aware indexing for Go, Python, JavaScript, TypeScript, Java, C, C++, C#, Rust, Kotlin, Swift, Ruby, PHP, Zig, SQL, Cypher, HCL/Terraform, Protobuf, Groovy/Jenkinsfile, COBOL, CSS, HTML, XML (pluggable for more)
- **Semantic search** to find code by meaning, not just keywords
- **Ask** natural language questions about your codebase using a RAG pipeline
- **Trace** symbol definitions, references, callers, and callees across all supported languages
- **Map** your architecture with package structure and dependency views
- **Flow** visualization of call trees from entry points across the codebase
- **CFG** generation for per-function control flow graphs with full path enumeration
- **Watch** for file changes and re-index automatically
- **MCP Server** to expose all tools to AI agents via Model Context Protocol
- **Cross-repo dependencies** to link and query other SARAS-initialized repositories

## Prerequisites

SARAS needs an **embedding model** to index your code and an **LLM** for the Ask and AGENTS.md features. You can use any of these providers:

| Provider | Setup |
|----------|-------|
| [Ollama](https://ollama.com/) | `ollama pull nomic-embed-text && ollama pull gemma4` |
| [LM Studio](https://lmstudio.ai/) | Download an embedding model and a chat model |
| OpenAI-compatible | Any endpoint that speaks the OpenAI API (requires API key) |
| [GitHub Copilot](https://github.com/features/copilot) | `saras copilot login` (uses your existing Copilot subscription, no API key) |

## Quick Start

### Install

```bash
# From source
go install github.com/cisco-open/semantic-architecture-retrieval-analysis-system/cmd/saras@latest

# Or download binary
curl -sSfL https://raw.githubusercontent.com/cisco-open/semantic-architecture-retrieval-analysis-system/main/install.sh | bash
```

### Update

```bash
saras update
```

Checks GitHub for the latest release and updates the binary in-place. No update is performed if you're already on the latest version.

### Initialize

```bash
cd your-project
saras init
```

This creates a `.saras/` directory with configuration. The interactive setup lets you choose your embedding provider (Ollama, LM Studio, OpenAI-compatible, or GitHub Copilot). When Ollama is selected, saras auto-fetches your installed models and presents them as selectable lists for both embedding and LLM models.

### Sign in with GitHub Copilot (optional)

If you already pay for GitHub Copilot, you can skip the model setup entirely
and use Copilot for both embeddings and chat:

```bash
saras copilot login    # one-time GitHub device-code OAuth flow
saras copilot status   # check sign-in state
saras copilot logout   # remove the locally stored token
```

`saras copilot login` opens a browser to `https://github.com/login/device`,
asks you to paste the displayed user code, and stores the resulting OAuth
token under your user config dir (`~/.config/saras/copilot.json`, mode
`0600`). The token is **never** written inside the project directory, so it
won't end up in `git status`. After signing in, choose **copilot** as the
provider during `saras init` (or set `provider: copilot` under `embedder:`
and/or `llm:` in `.saras/config.yaml`). Saras uses the same public OAuth
client id as other editor integrations (copilot.vim, copilot.lua); no
custom GitHub App registration is required.

### Search

```bash
saras search "authentication flow"
saras search "database connection" --limit 20
saras search "error handling" --json
```

### Ask

```bash
saras ask "how does the login flow work?"
saras ask "what database connections are used?" --no-tui
saras ask --with-arch "how does auth work?"            # include call-flow tree in context
saras ask --with-arch=handleAuth "explain error paths"  # call-flow from specific function
```

With `--no-tui`, responses stream to stdout in real-time as the LLM generates them.
With `--with-arch`, a compact call-flow tree (depth 3) is prepended to the RAG context, giving the LLM both structural and code-level insight. For deeper analysis, use `saras flow explain full`.

### Trace

```bash
saras trace Login                                # full trace: def, refs, callers, callees
saras trace handleRequest --callers
saras trace NewDB --callees
saras trace SomeType --refs                      # references only
```

#### Disambiguating symbol names

When the same name appears in more than one file, language, or class,
`saras trace` follows the same conventions as `saras cfg`:

- For the **definition lookup** and **`--callees`** (which both need a
  unique target), the same `--file` / `--language` / `--parent` flags
  apply, plus the `path:symbolName` shorthand (relative or absolute
  path).
- For **`--refs`** and **`--callers`**, lookups are name-based and the
  flags act as hints only. References and callers across the entire
  project are still reported.

```bash
# All four forms point trace at a specific Login() definition:
saras trace pkg/auth/login.go:Login
saras trace /Users/me/repo/pkg/auth/login.go:Login
saras trace Login --file pkg/auth/login.go
saras trace Login --language go --parent UserService

# Polyglot repo: pick the python definition explicitly.
saras trace login --language python
saras trace login --callees --language python      # error if still ambiguous

# Methods on different receivers (Go) or classes (Python/Ruby):
saras trace authenticate --parent SessionManager --callees
```

When `saras trace symbol` matches multiple definitions and **no**
disambiguators were supplied, the full-trace mode prints a one-line
warning naming the first match and proceeds (preserving the legacy
ergonomics for quick exploration). With **any** disambiguator (or with
`--callees`), an ambiguous lookup fails fast and prints the candidate
list:

```text
$ saras trace login --callees
function "login" is ambiguous (3 matches):
  1. pkg/auth/login.go:18-46         (go)      [login function]
  2. services/admin/login.py:1-9     (python)  [login function]
  3. lib/login.rb:1-7                (ruby)    [login function]

Disambiguate with one or more of:
  --file <path-substring>   e.g. --file pkg/auth
  --language <name>         e.g. --language python
  --parent <type-or-class>  e.g. --parent UserService
```

### Map

```bash
saras map                          # directory tree
saras map --format markdown        # full architecture report
saras map --format summary         # compact overview
saras map -f markdown -o ARCH.md   # write to file
```

### Flow

Show how execution flows from entry points through your codebase as a call tree.
Aliases: `saras architecture`, `saras arch`

```bash
saras flow                            # all entry points (main, commands, handlers)
saras flow full                       # same as above (explicit)
saras flow runSearch                  # call tree from a specific function
saras flow --depth 3                  # limit tree depth (default: 8)
saras flow -o FLOW.md                 # write to file
saras flow explain                    # concise LLM summary of all entry points
saras flow explain full               # exhaustive deep-dive (depth 12, detailed analysis)
saras flow explain runSearch          # explain a specific function's flow
saras flow explain --no-tui           # plain stdout output
saras flow explain full --no-tui      # deep-dive, plain stdout
```

Entry points are auto-detected across all supported languages: `main()` and `init()` in Go, `main` in C/C++/Rust/Zig/Kotlin, `Main` in C#, `main()` guarded by `if __name__` in Python, `public static void main` in Java, plus Cobra command handlers (`RunE`/`Run`) and HTTP handlers. Each language plugin provides its own keyword list, comment syntax, and entry point heuristics for accurate call graph analysis. Ambiguous method resolutions (e.g. interface methods implemented on multiple types) are omitted for accuracy. Markers in the output: `(cycle)` means recursion, `(↩)` means a node was already expanded elsewhere, and `(...)` means the depth limit was reached.

### Control Flow Graph (CFG)

Generate intra-procedural Control Flow Graph (CFG) for a single function and
enumerate every execution path through it.

```bash
saras cfg authenticate                   # Mermaid diagram (default)
saras cfg authenticate --format text     # block / edge / path summary
saras cfg authenticate --format json     # machine-readable CFG + paths
saras cfg authenticate --format paths    # just the paths and lines visited
saras cfg authenticate -o cfg.md         # write Mermaid to a file

saras cfg paths authenticate             # path enumeration only
saras cfg explain authenticate           # LLM walkthrough of every path
```

Each path is annotated with the branch decisions it took (e.g.
`if user == nil = false`, `switch op = case "add"`, `loop i < n: 0 iterations`)
so reviewers can map every test back to the exact CFG edges it covers.

#### Surrounding code context (`--with-context`)

`saras cfg`, `saras cfg paths`, and `saras cfg explain` all accept an
opt-in `--with-context` flag that attaches the **surrounding code**
to the output:

- The file's package declaration, import block, and any sibling
  type/constant declarations near the top of the file.
- The receiver or parent type definition (Go method receiver, Python
  class, Ruby module, etc.), including its fields.
- Definitions of every type or interface referenced by the function
  body, the receiver, or any of its callees, traced one hop deep.
- Signatures of every function/method called by the target.

```bash
# Pipe path enumeration + context into your clipboard for AI handoff
saras cfg paths GenerateMarkdown --with-context | pbcopy

# JSON for programmatic consumers (context lives under "context")
saras cfg GenerateMarkdown --format json --with-context

# text/paths get a markdown appendix; mermaid is left alone (with a
# stderr note explaining --with-context is a no-op for diagrams).
saras cfg GenerateMarkdown --format text --with-context
```

#### Inlining callees (`--inline-callees`)

By default each `saras cfg` call produces an *intra-procedural* CFG.
Calls to helpers show up as a single block with no visibility into
the helper's own branches. Pass `--inline-callees` to splice each
project-internal helper's CFG into the caller, so every enumerated
path walks the helper's branches too:

```bash
# Plain CFG of outer: helpers are opaque single blocks.
saras cfg paths outer

# Inline helpers (depth 2 by default): paths now walk every branch
# inside each project-internal callee.
saras cfg paths outer --inline-callees

# Drill deeper if the helper itself calls helpers worth expanding.
saras cfg paths outer --inline-callees --max-inline-depth 3

# Works with --with-context and JSON too.
saras cfg outer --format json --inline-callees
saras cfg explain outer --inline-callees --no-tui
```

- Each callee is resolved through the project symbol index and only
  inlined when it has a **unique** project-internal definition.
  Standard-library / third-party calls (e.g. `fmt.Println`,
  `strconv.Itoa`) are left as plain call sites silently to avoid
  cluttering the `notes` field.
- **Ambiguous** project callees (same name in multiple files /
  classes) are also left as call sites with a `notes` entry like
  `call to helper not inlined: 3 candidates …`. Disambiguate by
  running `saras cfg helper --file <path>` directly.
- **Mutual recursion** (A calls B calls A) is detected via an in-progress
  set and bailed out of with `recursive call to A not inlined`.
  Self-recursion (`fact(n-1)` from `fact`) is filtered upstream by
  `trace.FindCallees` so it never tries to expand.
- `--max-inline-depth` (default `2`) caps recursion. Higher values
  produce richer CFGs but explode block count and Mermaid render
  time on deep call graphs. When the budget is exhausted, the CFG
  picks up a `max inline depth reached: deeper callees not expanded`
  note so the truncation is explicit.

Inlined material is clearly marked in every output format:

- Block labels are prefixed with `[helperName]` (e.g.
  `[buildCFG] if err != nil`).
- The edge from the call site to the helper's entry block is
  labelled `call helperName`, so Mermaid arrows show the boundary
  and JSON consumers can post-filter on the call edge.

#### Disambiguating function names

When the same function name appears in more than one file, language, or
class, `saras cfg` refuses to guess and prints the candidate list:

```text
$ saras cfg login
function "login" is ambiguous (3 matches):
  1. pkg/auth/login.go:18-46     (go)      [login]
  2. services/admin/login.py:1-9 (python)  [login]
  3. lib/login.rb:1-7            (ruby)    [login]

Disambiguate with one or more of:
  --file <path-substring>   e.g. --file pkg/auth
  --language <name>         e.g. --language python
  --parent <type-or-class>  e.g. --parent UserService
```

Pick a candidate by adding any combination of the disambiguation flags or
by using the `path:functionName` shorthand (relative or absolute path):

```bash
# All four forms resolve the same Go login() in pkg/auth/login.go:
saras cfg pkg/auth/login.go:login
saras cfg /Users/me/repo/pkg/auth/login.go:login
saras cfg login --file pkg/auth/login.go
saras cfg login --language go --parent UserService

# Methods on different receivers (Go) or classes (Python/Ruby):
saras cfg authenticate --parent SessionManager
saras cfg authenticate --parent OAuthClient

# Drill into a specific language in a polyglot repo:
saras cfg login --language python
saras cfg paths login --language ruby
saras cfg explain login --file services/admin
```

The same flags work on every `cfg` subcommand (`paths`, `explain`).
When a single match remains, the lookup proceeds; when the filters
narrow it to zero matches, the error mentions which filters were
applied so you know what to broaden.

### Reindex

```bash
saras reindex                  # full re-index with progress
```

### Watch

```bash
saras watch                    # live TUI dashboard
saras watch --no-tui           # log mode
saras watch --index-first=false  # skip initial index
saras watch --index-only       # full index then exit (with progress)
```

### MCP Server

Expose saras tools to AI agents via the Model Context Protocol (SSE transport).

```bash
saras serve                      # start on 127.0.0.1:9420
saras serve --addr 0.0.0.0:8080 # custom address
```

**SSE endpoints:**

| Method | Path | Description |
|--------|------|-------------|
| GET | `/sse` | SSE connection for MCP clients |
| POST | `/message` | Send JSON-RPC messages to the server |

**Available tools:** `search`, `ask`, `trace`, `map`, `symbols`, `dep_list`

The server name advertised to MCP clients matches the project directory name.

Connect from Devin, Cursor, or any MCP-compatible client:
```
URL: http://127.0.0.1:9420/sse
```

### Cross-Repository Dependencies

Link other SARAS-initialized repositories as dependencies to query code across repos:

```bash
# Add a dependency (role is required: legacy, shared-lib, reference, service)
saras dep add ../legacy-auth --role legacy
saras dep add ../shared-lib --role shared-lib --name common-lib

# List dependencies
saras dep list

# Remove a dependency
saras dep remove legacy-auth
```

Then use dep flags on any command (`search`, `ask`, `trace`, `flow`, `map`):

```bash
# Search one specific dep only
saras search --from-dep legacy-auth "token validation"

# Search all deps (excludes current repo)
saras search --all-deps "database pool"

# Search current repo + all deps
saras search --with-deps "auth flow"

# Works with all commands
saras ask --from-dep legacy-auth --no-tui "how does session management work?"
saras trace --with-deps HandleRequest
saras flow --from-dep legacy-auth
saras map --all-deps --format summary
```

**Flags** (`--from-dep`, `--all-deps`, `--with-deps`) are mutually exclusive. Results from dependencies are labeled `[role: name]` in output.

**Roles** provide context to the AI about how the dependency relates to your project:
- `legacy`: predecessor codebase being migrated from
- `shared-lib`: shared library or utility repository
- `reference`: reference implementation or design patterns
- `service`: microservice that your project interacts with

Embedding compatibility is validated when adding a dependency (same provider + model + dimensions).

### Install Skills

Install skill files that teach AI coding agents how to use saras:

```bash
saras install skill --cursor     # .cursor/skills/<project>/SKILL.md + .cursor/rules/<project>.mdc
saras install skill --devin      # .devin/skills/<project>/SKILL.md
saras install skill --claude     # .claude/skills/<project>/SKILL.md
saras install skill --codex      # .agents/skills/<project>/SKILL.md
saras install skill --copilot    # .github/skills/<project>/SKILL.md + .github/copilot-instructions.md
saras install skill --all        # all of the above
```

The skill name and folder are derived from the current directory name so they match the project.

Use `--global` to install the skill to your home directory (`~/.cursor/`, `~/.devin/`, etc.) so it's available across all projects:

```bash
saras install skill --devin --global     # ~/.devin/skills/<project>/SKILL.md
saras install skill --cursor --global     # ~/.cursor/skills/<project>/SKILL.md + rules
```

Skill installation also auto-installs SARAS slash commands/workflows for editors that support them:

| Editor | Directory | Format |
|--------|-----------|--------|
| Devin | `.devin/workflows/saras-*.md` | YAML frontmatter + `// turbo` steps |
| Cursor | `.cursor/commands/saras-*.md` | Plain markdown commands |
| Claude Code | `.claude/commands/saras-*.md` | YAML frontmatter commands |
| Copilot | `.github/prompts/saras-*.prompt.md` | Prompt files with `agent` frontmatter |

> Codex relies on `AGENTS.md` and does not support custom commands.

**Available workflows (16):**

| Workflow | Trigger | Description |
|----------|---------|-------------|
| `/saras-search` | "search the code", "find where this is defined" | Semantic codebase search |
| `/saras-ask` | "explain how this works", "how does X work" | RAG-powered Q&A about the codebase |
| `/saras-trace` | "trace this function", "what calls this" | Symbol tracing (callers, callees, refs) |
| `/saras-flow` | "show the execution flow", "what does main call" | Call-tree visualization from entry points |
| `/saras-map` | "show me the architecture", "map the codebase" | Architecture overview and package structure |
| `/saras-cfg` | "explain the control flow of X", "list the paths through X" | CFG generation with path enumeration |
| `/saras-write-tests` | "write tests for X", "generate tests" | Test generation from CFG paths (single function or whole project) |
| `/saras-refactor` | "refactor X", "rename X safely" | Safe refactoring with trace + CFG impact analysis |
| `/saras-debug` | "debug X", "why does X fail" | Root-cause analysis via CFG path matching |
| `/saras-document` | "document X", "add docs to X" | Generate documentation from flow + CFG analysis |
| `/saras-impact` | "impact of changing X", "who depends on X" | Change impact report with risk rating |
| `/saras-api-contract` | "API contract for X", "document this API" | End-to-end API endpoint documentation |
| `/saras-understand-codebase` | "understand this codebase", "onboard me" | Full project overview using map + flow + ask |
| `/saras-cross-repo` | "search dependencies", "find in other repos" | Cross-repo search and queries |
| `/saras-add-dependency` | "add a dependency", "link another repo" | Add a cross-repo dependency |
| `/saras-reindex` | "reindex", "refresh the index" | Re-index the codebase |

### Install AGENTS.md

Generate `AGENTS.md` files using your configured LLM. By default only a root-level file is created; use `--deep` for per-package files:

```bash
saras install agentsmd                     # root AGENTS.md only
saras install agentsmd --deep              # root + per-package AGENTS.md files
saras install agentsmd --deep --min-files 3  # only for packages with 3+ files (default: 2)
saras install agentsmd --with-claudemd     # also create CLAUDE.md with @AGENTS.md import
```
## Configuration

SARAS stores configuration in `.saras/config.yaml`. Key settings:

```yaml
embedder:
  provider: ollama          # ollama, lmstudio, openai
  endpoint: http://localhost:11434
  model: nomic-embed-text
  # api_key: sk-...        # required for openai

llm:
  provider: ollama          # ollama, lmstudio, openai
  endpoint: http://localhost:11434
  model: llama3.2
  # api_key: sk-...        # required for openai

chunking:
  size: 1500
  overlap: 200

search:
  hybrid:
    enabled: true
    k: 60
  boost:
    enabled: true
  dedup:
    enabled: true

watch:
  debounce_ms: 500

ignore:
  - node_modules
  - vendor
  - .git
```

## `.sarasignore`

You can create a `.sarasignore` file in your project root to exclude additional files and directories from indexing, watching, and scanning. It uses the same syntax as `.gitignore`:

```
# Exclude generated code
*.generated.go
*.pb.go

# Exclude specific directories
fixtures/
testdata/
scripts/

# Exclude by pattern
docs/*.draft.md
```

`.sarasignore` patterns are applied **in addition to** `.gitignore` patterns and the `ignore` list in `.saras/config.yaml`. This is useful when you want to exclude files from saras without modifying your `.gitignore` (e.g. large vendored assets, generated code, or documentation that isn't relevant for code search).

## Architecture

```
saras/
├── cmd/saras/          # CLI entrypoint
├── internal/
│   ├── architect/      # Codebase map generator
│   ├── ask/            # RAG pipeline (search + LLM)
│   ├── cli/            # Cobra commands
│   ├── config/         # YAML configuration
│   ├── embedder/       # Embedding providers (Ollama, LMStudio, OpenAI)
│   ├── engine/         # Indexer, chunker, scanner, watcher
│   ├── lang/           # Pluggable language parsers (symbol extraction)
│   ├── mcp/            # MCP server
│   ├── search/         # Vector, text, hybrid search with RRF
│   ├── store/          # Vector store (gob backend)
│   ├── trace/          # Multi-language symbol tracing, call graph
│   └── tui/            # Bubble Tea interactive UIs
```

## Development

```bash
make build              # build binary to bin/saras
make install            # go install
make test               # run tests
make test-verbose       # run tests with -v
make test-coverage      # tests + coverage report
make fmt                # gofmt + goimports
make vet                # go vet
make lint               # golangci-lint
make clean              # remove build artifacts
make release            # goreleaser release
make release-snapshot   # goreleaser snapshot (no publish)
make all                # fmt + vet + lint + test + build
```

## Embedding Providers

| Provider | Endpoint | Model Example |
|----------|----------|---------------|
| Ollama | `http://localhost:11434` | `nomic-embed-text` |
| LM Studio | `http://localhost:1234` | `text-embedding-nomic-embed-text-v1.5` |
| OpenAI | `https://api.openai.com` | `text-embedding-3-small` |

## Supported Languages

Built-in symbol-aware parsing for:

| Language | Extensions | Symbols Extracted |
|----------|------------|-------------------|
| Go | `.go` | functions, methods, structs, interfaces, vars, consts |
| Python | `.py`, `.pyi` | functions, methods, classes, async functions, constants |
| JavaScript | `.js`, `.jsx`, `.mjs`, `.cjs` | functions, arrow functions, classes, methods, consts |
| TypeScript | `.ts`, `.tsx` | functions, classes, interfaces, types, enums, methods |
| Java | `.java` | classes, interfaces, enums, methods, constants |
| C | `.c`, `.h` | functions, structs, enums, typedefs, #defines |
| C++ | `.cpp`, `.cc`, `.cxx`, `.hpp`, `.hxx`, `.hh` | classes, structs, namespaces, enums, methods, functions |
| Rust | `.rs` | functions, methods, structs, enums, traits, impl blocks |
| Kotlin | `.kt`, `.kts` | classes, interfaces, enums, functions, methods, type aliases |
| Ruby | `.rb`, `.rake`, `.gemspec` | modules, classes, methods, constants, attributes |
| PHP | `.php`, `.phtml` | namespaces, classes, interfaces, traits, enums, methods, constants |
| C# | `.cs`, `.csx` | namespaces, classes, structs, interfaces, enums, records, methods, properties, delegates |
| CSS | `.css`, `.scss`, `.less`, `.sass` | selectors, variables, keyframes, mixins |
| HTML | `.html`, `.htm` | elements, ids, classes, scripts, styles |
| XML | `.xml`, `.xsl`, `.xsd`, `.svg`, `.plist` | elements, attributes, namespaces |
| Perl | `.pl`, `.pm`, `.t`, `.psgi` | packages, subroutines, constants, variables, attributes (Moose/Moo) |
| Markdown | `.md`, `.markdown`, `.mdx` | headings, front matter keys, code blocks, link definitions |
| Mermaid | `.mermaid`, `.mmd` | diagrams, subgraphs, nodes, participants, classes, sections |
| Shell | `.sh`, `.bash`, `.zsh`, `.ksh`, `.bats` | functions, exports, variables, aliases, readonly |
| Makefile | `.mk`, `.make`, `Makefile` | targets, variables, defines, .PHONY, includes |
| TOML | `.toml` | tables, array tables, keys |
| YAML | `.yaml`, `.yml` | top-level keys, nested keys, document separators |
| JSON | `.json`, `.jsonc`, `.json5` | top-level object keys |
| Properties | `.properties` | key-value pairs |
| Env | `.env` | variable definitions (plain and export) |
| Dockerfile | `Dockerfile`, `Containerfile`, `.dockerfile` | FROM stages, ARG, ENV, EXPOSE, ENTRYPOINT, CMD, WORKDIR, VOLUME, HEALTHCHECK |
| Zig | `.zig` | functions, structs, enums, unions, constants |
| Python 2 | `.py2`, `.pyw` | functions, classes, methods, constants |
| SQL | `.sql`, `.ddl`, `.dml`, `.pgsql`, `.plsql` | functions, procedures, triggers, tables, views, types, schemas, indexes, sequences, variables |
| Cypher | `.cypher`, `.cql` | node labels, relationship types, constraints, indexes, procedure calls, parameters |
| Swift | `.swift` | functions, methods, classes, structs, enums, protocols, actors, extensions, typealias |
| HCL / Terraform | `.tf`, `.hcl`, `.tfvars` | resources, data sources, variables, outputs, modules, providers, locals |
| Protobuf | `.proto` | messages, enums, services, RPCs, oneofs, packages, imports, options |
| Groovy | `.groovy`, `.gvy`, `.gy`, `.gsh`, `Jenkinsfile` | classes, interfaces, traits, enums, functions, methods, pipeline stages, environment vars, parameters |
| COBOL | `.cob`, `.cbl`, `.cpy`, `.cobol` | programs, sections, paragraphs, data items (01/77), conditions (88), file descriptions, copybooks |

Unsupported file types still get line-based chunking for search and embedding.

### Adding a New Language

Implement the `lang.LanguageParser` interface and call `lang.Register()`:

```go
package myplugin

import "github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/lang"

func init() { lang.Register(&MyLangParser{}) }

type MyLangParser struct{}

func (p *MyLangParser) Name() string                         { return "mylang" }
func (p *MyLangParser) Extensions() []string                 { return []string{".ml"} }
func (p *MyLangParser) IsTestFile(path string) bool           { return false }
func (p *MyLangParser) ExtractSymbols(content string) []lang.Symbol {
    // your parsing logic here
    return nil
}
```

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on how to report bugs, suggest features, and submit pull requests.

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md).

## License

Distributed under the Apache-2.0 License. See [LICENSE](LICENSE) for more
information.
