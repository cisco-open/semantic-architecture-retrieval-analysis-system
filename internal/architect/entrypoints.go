/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package architect

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/lang"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/trace"
)

// EntryPointKind classifies the type of entry point.
type EntryPointKind string

const (
	EntryMain    EntryPointKind = "main"
	EntryInit    EntryPointKind = "init"
	EntryCommand EntryPointKind = "command"
	EntryHandler EntryPointKind = "handler"
)

// EntryPoint represents a detected entry point in the codebase.
type EntryPoint struct {
	Symbol trace.Symbol   `json:"symbol"`
	Kind   EntryPointKind `json:"kind"`
	Label  string         `json:"label"`
}

// FlowNode represents a node in the call flow tree.
type FlowNode struct {
	Name     string      `json:"name"`
	FilePath string      `json:"file_path"`
	Line     int         `json:"line"`
	Children []*FlowNode `json:"children,omitempty"`
	Ref      bool        `json:"-"` // already expanded elsewhere
	Cycle    bool        `json:"-"` // creates a cycle
	DepthCap bool        `json:"-"` // stopped due to depth limit
}

// FlowTree is the call tree rooted at a single entry point or function.
type FlowTree struct {
	Entry *EntryPoint `json:"entry_point,omitempty"`
	Root  *FlowNode   `json:"root"`
}

// FlowMapper generates entry-point-driven call flow maps.
type FlowMapper struct {
	root         string
	ignoreList   []string
	tracer       *trace.Tracer
	maxDepth     int
	skipKeywords map[string]bool
	commentPfx   []string
}

const defaultMaxDepth = 8

// NewFlowMapper creates a new flow mapper.
func NewFlowMapper(root string, ignoreList []string, maxDepth int) *FlowMapper {
	if maxDepth <= 0 {
		maxDepth = defaultMaxDepth
	}

	// Build combined keyword skip set from all language plugins
	hints := lang.AllFlowHints()
	skip := make(map[string]bool, len(hints.Keywords))
	for _, kw := range hints.Keywords {
		skip[kw] = true
	}

	return &FlowMapper{
		root:         root,
		ignoreList:   ignoreList,
		tracer:       trace.NewTracer(root, ignoreList),
		maxDepth:     maxDepth,
		skipKeywords: skip,
		commentPfx:   hints.CommentPrefixes,
	}
}

// GenerateFullFlow produces call flow trees for all detected entry points.
func (fm *FlowMapper) GenerateFullFlow(ctx context.Context) ([]*FlowTree, error) {
	symbols, err := fm.tracer.ExtractSymbols(ctx)
	if err != nil {
		return nil, fmt.Errorf("extract symbols: %w", err)
	}

	entries, err := fm.detectEntryPoints(ctx, symbols)
	if err != nil {
		return nil, fmt.Errorf("detect entry points: %w", err)
	}

	idx := buildSymbolIndex(symbols)

	var trees []*FlowTree
	for i := range entries {
		ep := &entries[i]
		expanded := make(map[string]bool)
		visiting := make(map[string]bool)
		root := fm.buildCallTree(ep.Symbol.Name, ep.Symbol, idx, visiting, expanded, 0)
		trees = append(trees, &FlowTree{Entry: ep, Root: root})
	}

	return trees, nil
}

// GenerateFunctionFlow produces a call flow tree for a specific function.
func (fm *FlowMapper) GenerateFunctionFlow(ctx context.Context, funcName string) (*FlowTree, error) {
	symbols, err := fm.tracer.ExtractSymbols(ctx)
	if err != nil {
		return nil, fmt.Errorf("extract symbols: %w", err)
	}

	idx := buildSymbolIndex(symbols)
	sym, ok := idx.resolve(funcName, "")
	if !ok {
		return nil, fmt.Errorf("function %q not found in project", funcName)
	}

	expanded := make(map[string]bool)
	visiting := make(map[string]bool)
	root := fm.buildCallTree(funcName, sym, idx, visiting, expanded, 0)
	return &FlowTree{Root: root}, nil
}

// FormatFlowTrees renders multiple flow trees as a human-readable string.
func FormatFlowTrees(trees []*FlowTree) string {
	var b strings.Builder
	b.WriteString("Entry Points\n\n")

	for i, tree := range trees {
		if i > 0 {
			b.WriteString("\n")
		}
		label := ""
		if tree.Entry != nil {
			if tree.Entry.Label != "" {
				label = fmt.Sprintf(" (%s: %s)", tree.Entry.Kind, tree.Entry.Label)
			} else {
				label = fmt.Sprintf(" (%s)", tree.Entry.Kind)
			}
		}
		b.WriteString(fmt.Sprintf("%s [%s:%d]%s\n",
			tree.Root.Name, tree.Root.FilePath, tree.Root.Line, label))
		formatChildren(&b, tree.Root.Children, "")
	}

	return b.String()
}

// FormatFlowTree renders a single flow tree.
func FormatFlowTree(tree *FlowTree) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s [%s:%d]\n", tree.Root.Name, tree.Root.FilePath, tree.Root.Line))
	formatChildren(&b, tree.Root.Children, "")
	return b.String()
}

// ---------------------------------------------------------------------------
// Symbol index — resolves function names with package-local preference
// ---------------------------------------------------------------------------

type symbolIndex struct {
	byName map[string][]trace.Symbol
}

func buildSymbolIndex(symbols []trace.Symbol) *symbolIndex {
	idx := &symbolIndex{byName: make(map[string][]trace.Symbol)}
	for _, s := range symbols {
		if s.Kind == trace.KindFunction || s.Kind == trace.KindMethod {
			idx.byName[s.Name] = append(idx.byName[s.Name], s)
		}
	}
	return idx
}

// resolve returns the best match for a function name.
// When callerFile is non-empty, same-package symbols are preferred.
// Returns false for ambiguous names (e.g. interface methods implemented
// on multiple types) to avoid showing inaccurate resolutions.
func (idx *symbolIndex) resolve(name, callerFile string) (trace.Symbol, bool) {
	syms := idx.byName[name]
	if len(syms) == 0 {
		return trace.Symbol{}, false
	}
	if len(syms) == 1 {
		return syms[0], true
	}
	// Multiple candidates — prefer same directory
	if callerFile != "" {
		callerDir := filepath.Dir(callerFile)
		var sameDir []trace.Symbol
		for _, s := range syms {
			if filepath.Dir(s.FilePath) == callerDir {
				sameDir = append(sameDir, s)
			}
		}
		if len(sameDir) == 1 {
			return sameDir[0], true
		}
	}
	// Ambiguous — skip to avoid inaccurate output
	return trace.Symbol{}, false
}

// ---------------------------------------------------------------------------
// Recursive call tree builder
// ---------------------------------------------------------------------------

func (fm *FlowMapper) buildCallTree(
	funcName string,
	sym trace.Symbol,
	idx *symbolIndex,
	visiting map[string]bool, // current recursion stack (reset on backtrack)
	expanded map[string]bool, // globally expanded nodes (never reset)
	depth int,
) *FlowNode {
	node := &FlowNode{
		Name:     funcName,
		FilePath: sym.FilePath,
		Line:     sym.Line,
	}

	// Cycle detection
	if visiting[funcName] {
		node.Cycle = true
		return node
	}

	// Already expanded in another branch
	if expanded[funcName] {
		node.Ref = true
		return node
	}

	// Depth limit
	if depth >= fm.maxDepth {
		node.DepthCap = true
		return node
	}

	visiting[funcName] = true
	defer func() { visiting[funcName] = false }()
	expanded[funcName] = true

	// Read the function body
	body, err := readFuncBody(filepath.Join(fm.root, sym.FilePath), sym.Line, sym.EndLine)
	if err != nil || body == "" {
		return node
	}

	// Find calls to known project functions
	calls := fm.findCallsInBody(body, funcName, sym.FilePath, idx)

	for _, callee := range calls {
		calleeSym, _ := idx.resolve(callee, sym.FilePath)
		child := fm.buildCallTree(callee, calleeSym, idx, visiting, expanded, depth+1)
		node.Children = append(node.Children, child)
	}

	return node
}

// readFuncBody reads lines from a file between start and end line numbers.
func readFuncBody(absPath string, start, end int) (string, error) {
	if end <= 0 || start <= 0 {
		return "", nil
	}
	f, err := os.Open(absPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum >= start && lineNum <= end {
			lines = append(lines, scanner.Text())
		}
		if lineNum > end {
			break
		}
	}
	return strings.Join(lines, "\n"), scanner.Err()
}

var callPattern = regexp.MustCompile(`\b([a-zA-Z_]\w*)\s*\(`)
var stringLiteral = regexp.MustCompile("`[^`]*`" + `|"[^"]*"|'[^']*'`)

// findCallsInBody extracts names of known project functions called in a function body.
// It uses the FlowMapper's language-derived keyword skip set and comment prefixes.
func (fm *FlowMapper) findCallsInBody(body, selfName, callerFile string, idx *symbolIndex) []string {
	var calls []string
	seen := make(map[string]bool)

	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)

		// Skip comment lines using language-provided prefixes
		if isCommentLine(trimmed, fm.commentPfx) {
			continue
		}

		// Strip string literals to avoid false positives
		cleaned := stringLiteral.ReplaceAllString(trimmed, `""`)

		// Strip inline comments (// and #)
		for _, pfx := range fm.commentPfx {
			if pfx == "//" || pfx == "#" {
				if ci := strings.Index(cleaned, pfx); ci > 0 {
					cleaned = cleaned[:ci]
				}
			}
		}

		matches := callPattern.FindAllStringSubmatch(cleaned, -1)
		for _, m := range matches {
			name := m[1]
			if name == selfName {
				continue
			}
			if fm.skipKeywords[name] {
				continue
			}
			if _, ok := idx.resolve(name, callerFile); ok && !seen[name] {
				seen[name] = true
				calls = append(calls, name)
			}
		}
	}

	sort.Strings(calls)
	return calls
}

// isCommentLine checks if a line is a comment using the given prefixes.
func isCommentLine(trimmed string, prefixes []string) bool {
	for _, pfx := range prefixes {
		if strings.HasPrefix(trimmed, pfx) {
			return true
		}
	}
	// Always check block comment continuation
	if strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Entry point detection
// ---------------------------------------------------------------------------

func (fm *FlowMapper) detectEntryPoints(ctx context.Context, symbols []trace.Symbol) ([]EntryPoint, error) {
	var entries []EntryPoint

	// 1. Detect language-defined entry point functions (main, init, Main, etc.)
	for _, s := range symbols {
		if s.Kind != trace.KindFunction {
			continue
		}

		hints := lang.GetFlowHints(s.FilePath)
		for _, entryName := range hints.EntryFunctions {
			if s.Name != entryName {
				continue
			}

			// If the language provides an IsEntryFile check, verify it
			if hints.IsEntryFile != nil {
				content, err := os.ReadFile(filepath.Join(fm.root, s.FilePath))
				if err != nil || !hints.IsEntryFile(string(content)) {
					continue
				}
			}

			kind := EntryMain
			label := ""
			if entryName == "init" {
				kind = EntryInit
				label = filepath.Dir(s.FilePath)
			}
			entries = append(entries, EntryPoint{
				Symbol: s, Kind: kind, Label: label,
			})
		}
	}

	// 2. Detect Cobra command handlers
	cobraEntries := fm.detectCobraHandlers(symbols)
	entries = append(entries, cobraEntries...)

	// 3. Detect HTTP handlers
	httpEntries := fm.detectHTTPHandlers(symbols)
	entries = append(entries, httpEntries...)

	// 4. Detect framework convention-based entry points (Struts, Servlet, etc.)
	fwEntries := fm.detectFrameworkEntryPoints(symbols)
	entries = append(entries, fwEntries...)

	// Sort: main first, then commands, then handlers, then init
	sort.Slice(entries, func(i, j int) bool {
		oi, oj := entryKindOrder(entries[i].Kind), entryKindOrder(entries[j].Kind)
		if oi != oj {
			return oi < oj
		}
		return entries[i].Symbol.Name < entries[j].Symbol.Name
	})

	return deduplicateEntries(entries), nil
}

func entryKindOrder(k EntryPointKind) int {
	switch k {
	case EntryMain:
		return 0
	case EntryCommand:
		return 1
	case EntryHandler:
		return 2
	case EntryInit:
		return 3
	default:
		return 4
	}
}

// --- Cobra command handler detection ----------------------------------------

var cobraRunPattern = regexp.MustCompile(`Run[E]?\s*:\s*(\w+)`)
var cobraUsePattern = regexp.MustCompile(`Use:\s*"([^"]+)"`)

func (fm *FlowMapper) detectCobraHandlers(symbols []trace.Symbol) []EntryPoint {
	funcMap := make(map[string]trace.Symbol)
	for _, s := range symbols {
		if s.Kind == trace.KindFunction || s.Kind == trace.KindMethod {
			if _, exists := funcMap[s.Name]; !exists {
				funcMap[s.Name] = s
			}
		}
	}

	var entries []EntryPoint

	filepath.Walk(fm.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return fm.skipDir(info.Name())
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)
		if !strings.Contains(content, "cobra") {
			return nil
		}

		matches := cobraRunPattern.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			handlerName := m[1]
			if sym, ok := funcMap[handlerName]; ok {
				label := extractCobraUse(content, handlerName)
				if label == "" {
					label = handlerName
				}
				entries = append(entries, EntryPoint{
					Symbol: sym, Kind: EntryCommand, Label: label,
				})
			}
		}
		return nil
	})

	return entries
}

// extractCobraUse finds the Use field near a RunE/Run assignment.
func extractCobraUse(content, handlerName string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.Contains(line, handlerName) &&
			(strings.Contains(line, "RunE") || strings.Contains(line, "Run:")) {
			start := i - 20
			if start < 0 {
				start = 0
			}
			for j := start; j <= i; j++ {
				m := cobraUsePattern.FindStringSubmatch(lines[j])
				if len(m) > 1 {
					// Return just the command name, not args
					parts := strings.Fields(m[1])
					return parts[0]
				}
			}
		}
	}
	return ""
}

// --- HTTP handler detection -------------------------------------------------

// httpHandlerPatterns matches function references passed to route registrations.
// Covers Go net/http, Gin, Echo, Chi, Fiber, Express.js, Ktor, Sinatra, Laravel.
var httpHandlerPatterns = []*regexp.Regexp{
	// Go: http.HandleFunc("/", handler), mux.Handle("/", handler)
	regexp.MustCompile(`\.HandleFunc\s*\(\s*"[^"]*"\s*,\s*(\w+)`),
	regexp.MustCompile(`\.Handle\s*\(\s*"[^"]*"\s*,\s*(\w+)`),
	// Gin/Echo/Fiber/Express/Ktor: .GET("/", handler), .Post("/", handler), etc.
	regexp.MustCompile(`\.(GET|Get|get|POST|Post|post|PUT|Put|put|DELETE|Delete|delete|PATCH|Patch|patch|HEAD|Head|head|OPTIONS|Options|options)\s*\(\s*"[^"]*"\s*,\s*(\w+)`),
	// Chi: r.Get("/", handler), r.Route("/", handler)
	regexp.MustCompile(`\.(Route|Group|With|Mount)\s*\(\s*"[^"]*"\s*,\s*(\w+)`),
	// Python Flask/FastAPI: @app.route("/")\ndef handler
	// (handled by annotation detection below)
}

// annotationHandlerPatterns matches annotation/decorator-based handler declarations.
// The method on the NEXT line after the annotation is captured as the handler.
var annotationHandlerPatterns = []*regexp.Regexp{
	// Java Spring Boot: @GetMapping, @PostMapping, @RequestMapping, @PutMapping, @DeleteMapping, @PatchMapping
	regexp.MustCompile(`@(?:Get|Post|Put|Delete|Patch|Request)Mapping`),
	// Java Struts: @Action, @Result
	regexp.MustCompile(`@Action`),
	// Java JAX-RS: @GET, @POST, @PUT, @DELETE, @Path
	regexp.MustCompile(`@(?:GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\b`),
	// C# ASP.NET: [HttpGet], [HttpPost], [HttpPut], [HttpDelete], [Route(...)]
	regexp.MustCompile(`\[Http(?:Get|Post|Put|Delete|Patch|Head|Options)`),
	regexp.MustCompile(`\[Route\(`),
	// Python Flask: @app.route, @blueprint.route
	regexp.MustCompile(`@\w+\.route\s*\(`),
	// Python FastAPI: @app.get, @app.post, @router.get, @router.post
	regexp.MustCompile(`@\w+\.(?:get|post|put|delete|patch|head|options)\s*\(`),
	// Python Django: path(..., views.handler) is handled by funcref patterns
	// Ruby Rails: get "/", to: "controller#action" (handled separately)
	// Kotlin Ktor: get("/") { ... } (handled by funcref patterns above)
}

// methodAfterAnnotation extracts the method/function name declared on the line(s)
// following an annotation match.
var methodAfterAnnotation = []*regexp.Regexp{
	// Java/Kotlin: public ResponseEntity handler(...) or fun handler(...)
	regexp.MustCompile(`(?:public|private|protected|internal)?\s*(?:static\s+)?(?:(?:fun|void|\w+(?:<[^>]*>)?)\s+)(\w+)\s*\(`),
	// Python: def handler(...)
	regexp.MustCompile(`^\s*(?:async\s+)?def\s+(\w+)\s*\(`),
	// C#: public IActionResult Handler(...) or public async Task<...> Handler(...)
	regexp.MustCompile(`(?:public|private|protected|internal)\s+(?:(?:static|async|virtual|override)\s+)*(?:\w+(?:<[^>]*>)?\s+)(\w+)\s*\(`),
}

// djangoURLPatterns detects Django path()/url() handler references.
var djangoURLPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:path|re_path|url)\s*\(\s*['"][^'"]*['"]\s*,\s*(\w+)`),
	regexp.MustCompile(`(?:path|re_path|url)\s*\(\s*['"][^'"]*['"]\s*,\s*\w+\.(\w+)`),
}

// railsRoutePatterns detects Rails route handler references.
var railsRoutePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:get|post|put|patch|delete|match)\s+['"][^'"]*['"].*(?:to:|=>)\s*['"]\w+#(\w+)`),
}

// laravelRoutePatterns detects Laravel route handler references.
var laravelRoutePatterns = []*regexp.Regexp{
	regexp.MustCompile(`Route::(?:get|post|put|patch|delete|match)\s*\(\s*['"][^'"]*['"]\s*,\s*\[?[^,\]]*,?\s*['"]?(\w+)['"]?\s*\]?\s*\)`),
	regexp.MustCompile(`Route::(?:get|post|put|patch|delete|match)\s*\(\s*['"][^'"]*['"]\s*,\s*['"]?(\w+)['"]?\s*\)`),
}

func (fm *FlowMapper) detectHTTPHandlers(symbols []trace.Symbol) []EntryPoint {
	funcMap := make(map[string]trace.Symbol)
	for _, s := range symbols {
		if s.Kind == trace.KindFunction || s.Kind == trace.KindMethod {
			if _, exists := funcMap[s.Name]; !exists {
				funcMap[s.Name] = s
			}
		}
	}

	seen := make(map[string]bool)
	var entries []EntryPoint

	addHandler := func(name string) {
		if seen[name] {
			return
		}
		if sym, ok := funcMap[name]; ok {
			seen[name] = true
			entries = append(entries, EntryPoint{
				Symbol: sym, Kind: EntryHandler, Label: name,
			})
		}
	}

	filepath.Walk(fm.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return fm.skipDir(info.Name())
		}

		// Scan all supported source files, not just a few extensions
		if !lang.IsSupported(path) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)

		// 1. Function-reference patterns (handler passed as argument)
		for _, pattern := range httpHandlerPatterns {
			for _, m := range pattern.FindAllStringSubmatch(content, -1) {
				addHandler(m[len(m)-1])
			}
		}

		// 2. Django URL patterns
		for _, pattern := range djangoURLPatterns {
			for _, m := range pattern.FindAllStringSubmatch(content, -1) {
				addHandler(m[1])
			}
		}

		// 3. Rails route patterns
		for _, pattern := range railsRoutePatterns {
			for _, m := range pattern.FindAllStringSubmatch(content, -1) {
				addHandler(m[1])
			}
		}

		// 4. Laravel route patterns
		for _, pattern := range laravelRoutePatterns {
			for _, m := range pattern.FindAllStringSubmatch(content, -1) {
				addHandler(m[1])
			}
		}

		// 5. Annotation/decorator-based handlers (Spring, JAX-RS, ASP.NET, Flask, FastAPI)
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			for _, ap := range annotationHandlerPatterns {
				if !ap.MatchString(trimmed) {
					continue
				}
				// Look ahead up to 5 lines for the method declaration
				for j := i + 1; j < len(lines) && j <= i+5; j++ {
					nextLine := lines[j]
					for _, mp := range methodAfterAnnotation {
						if m := mp.FindStringSubmatch(nextLine); len(m) > 1 {
							addHandler(m[1])
							goto nextAnnotation
						}
					}
					// Stop if we hit another annotation
					if strings.HasPrefix(strings.TrimSpace(nextLine), "@") ||
						strings.HasPrefix(strings.TrimSpace(nextLine), "[") {
						break
					}
				}
			nextAnnotation:
			}
		}

		return nil
	})

	return entries
}

// --- Framework convention-based entry point detection -----------------------

// frameworkClassEntry maps a class-inheritance/annotation pattern to the
// convention entry-method names that should be treated as handlers.
type frameworkClassEntry struct {
	// classPattern matches the class declaration line (e.g. "extends ActionSupport")
	classPattern *regexp.Regexp
	// methodNames are the convention entry methods for that framework
	methodNames []string
	// label is the framework name for display
	label string
}

var frameworkConventions = []frameworkClassEntry{
	// Java Struts: classes extending ActionSupport or implementing Action
	{
		classPattern: regexp.MustCompile(`(?:extends\s+(?:ActionSupport|Action|BaseAction))|(?:implements\s+\w*Action)`),
		methodNames:  []string{"execute", "doExecute", "input", "validate"},
		label:        "struts",
	},
	// Java Servlet: classes extending HttpServlet
	{
		classPattern: regexp.MustCompile(`extends\s+HttpServlet`),
		methodNames:  []string{"doGet", "doPost", "doPut", "doDelete", "doPatch", "service", "init"},
		label:        "servlet",
	},
	// Java Spring: @Controller or @RestController annotated classes
	{
		classPattern: regexp.MustCompile(`@(?:Rest)?Controller`),
		methodNames:  nil, // all methods with @*Mapping annotations (handled by HTTP handler detection)
		label:        "spring",
	},
	// Java JAX-RS: @Path annotated classes
	{
		classPattern: regexp.MustCompile(`@Path\s*\(`),
		methodNames:  nil, // methods with @GET/@POST etc. (handled by HTTP handler detection)
		label:        "jax-rs",
	},
	// C# ASP.NET: classes extending Controller or ControllerBase
	{
		classPattern: regexp.MustCompile(`(?:extends|:)\s*(?:Controller|ControllerBase|ApiController)`),
		methodNames:  []string{"Index", "Create", "Edit", "Delete", "Details", "Update"},
		label:        "aspnet",
	},
	// Python Django: class-based views extending View, ListView, etc.
	{
		classPattern: regexp.MustCompile(`class\s+\w+\(.*(?:View|ListView|DetailView|CreateView|UpdateView|DeleteView|FormView|TemplateView)`),
		methodNames:  []string{"get", "post", "put", "patch", "delete", "dispatch", "get_queryset", "get_context_data", "form_valid"},
		label:        "django",
	},
}

func (fm *FlowMapper) detectFrameworkEntryPoints(symbols []trace.Symbol) []EntryPoint {
	// Build method lookup: "ClassName.MethodName" and plain "MethodName"
	type methodKey struct {
		parent string
		name   string
	}
	methodMap := make(map[methodKey]trace.Symbol)
	plainMap := make(map[string]trace.Symbol)
	for _, s := range symbols {
		if s.Kind == trace.KindMethod {
			methodMap[methodKey{parent: s.Parent, name: s.Name}] = s
		}
		if (s.Kind == trace.KindFunction || s.Kind == trace.KindMethod) && s.Name != "" {
			if _, exists := plainMap[s.Name]; !exists {
				plainMap[s.Name] = s
			}
		}
	}

	seen := make(map[string]bool)
	var entries []EntryPoint

	addEntry := func(sym trace.Symbol, label string) {
		key := sym.Name + ":" + sym.FilePath
		if seen[key] {
			return
		}
		seen[key] = true
		entries = append(entries, EntryPoint{
			Symbol: sym, Kind: EntryHandler, Label: label,
		})
	}

	filepath.Walk(fm.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return fm.skipDir(info.Name())
		}
		if !lang.IsSupported(path) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)

		for _, fc := range frameworkConventions {
			if !fc.classPattern.MatchString(content) {
				continue
			}

			if len(fc.methodNames) == 0 {
				// Annotation-based frameworks — already handled by HTTP handler detection
				continue
			}

			// Find the class name from the file
			lines := strings.Split(content, "\n")
			for _, line := range lines {
				if !fc.classPattern.MatchString(line) {
					continue
				}
				// Extract class name
				classNamePat := regexp.MustCompile(`(?:class|struct)\s+(\w+)`)
				m := classNamePat.FindStringSubmatch(line)
				if m == nil {
					continue
				}
				className := m[1]

				// Look for convention methods in this class
				for _, methodName := range fc.methodNames {
					// Try ClassName.MethodName first
					if sym, ok := methodMap[methodKey{parent: className, name: methodName}]; ok {
						addEntry(sym, fc.label+":"+className+"."+methodName)
					} else if sym, ok := plainMap[methodName]; ok {
						// Fallback: match by method name in the same file
						if sym.FilePath == path {
							addEntry(sym, fc.label+":"+methodName)
						}
					}
				}
			}
		}

		return nil
	})

	return entries
}

// --- Shared helpers ---------------------------------------------------------

func (fm *FlowMapper) skipDir(name string) error {
	if strings.HasPrefix(name, ".") && name != "." {
		return filepath.SkipDir
	}
	for _, ig := range fm.ignoreList {
		if name == ig {
			return filepath.SkipDir
		}
	}
	return nil
}

func deduplicateEntries(entries []EntryPoint) []EntryPoint {
	seen := make(map[string]bool)
	var result []EntryPoint
	for _, e := range entries {
		key := e.Symbol.Name + ":" + e.Symbol.FilePath
		if !seen[key] {
			seen[key] = true
			result = append(result, e)
		}
	}
	return result
}

// --- Tree rendering ---------------------------------------------------------

func formatChildren(b *strings.Builder, children []*FlowNode, prefix string) {
	for i, child := range children {
		isLast := i == len(children)-1
		connector := "├── "
		childPrefix := prefix + "│   "
		if isLast {
			connector = "└── "
			childPrefix = prefix + "    "
		}

		suffix := ""
		if child.Cycle {
			suffix = " (cycle)"
		} else if child.Ref {
			suffix = " (↩)"
		} else if child.DepthCap {
			suffix = " (...)"
		}

		b.WriteString(fmt.Sprintf("%s%s%s [%s:%d]%s\n",
			prefix, connector, child.Name, child.FilePath, child.Line, suffix))

		if !child.Cycle && !child.Ref && !child.DepthCap {
			formatChildren(b, child.Children, childPrefix)
		}
	}
}
