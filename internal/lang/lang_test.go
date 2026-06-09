/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package lang

import (
	"sort"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// SymbolKind tests
// ---------------------------------------------------------------------------

func TestSymbolKindString(t *testing.T) {
	tests := []struct {
		kind SymbolKind
		want string
	}{
		{KindFunction, "function"},
		{KindMethod, "method"},
		{KindClass, "class"},
		{KindType, "type"},
		{KindInterface, "interface"},
		{KindStruct, "struct"},
		{KindEnum, "enum"},
		{KindVariable, "variable"},
		{KindConstant, "constant"},
		{KindImport, "import"},
		{KindPackage, "package"},
		{KindModule, "module"},
		{KindTrait, "trait"},
		{KindProperty, "property"},
		{SymbolKind(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("SymbolKind(%d).String() = %s, want %s", tt.kind, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Registry tests
// ---------------------------------------------------------------------------

func TestParserForFile(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"main.go", "go"},
		{"app.py", "python"},
		{"index.js", "javascript"},
		{"app.tsx", "typescript"},
		{"Main.java", "java"},
		{"main.c", "c"},
		{"main.cpp", "cpp"},
		{"main.rs", "rust"},
		{"Main.kt", "kotlin"},
		{"main.zig", "zig"},
		{"legacy.py2", "python2"},
		{"app.pyw", "python2"},
		{"style.css", "css"},
		{"app.scss", "css"},
		{"index.html", "html"},
		{"page.htm", "html"},
		{"config.xml", "xml"},
		{"icon.svg", ""},
		{"app.rb", "ruby"},
		{"index.php", "php"},
		{"Program.cs", "csharp"},
		{"script.pl", "perl"},
		{"Module.pm", "perl"},
		{"README.md", "markdown"},
		{"doc.markdown", "markdown"},
		{"page.mdx", "markdown"},
		{"diagram.mermaid", "mermaid"},
		{"flow.mmd", "mermaid"},
		{"deploy.sh", "shell"},
		{"script.bash", "shell"},
		{"setup.zsh", "shell"},
		{"build.mk", "makefile"},
		{"Makefile", "makefile"},
		{"config.toml", "toml"},
		{"config.yaml", "yaml"},
		{"config.yml", "yaml"},
		{"data.json", "json"},
		{"settings.jsonc", "json"},
		{".env", "env"},
		{"app.properties", "properties"},
		{"Dockerfile", "dockerfile"},
		{"Containerfile", "dockerfile"},
		{"app.dockerfile", "dockerfile"},
		{"data.csv", ""},
	}
	for _, tt := range tests {
		p := ParserForFile(tt.path)
		if tt.want == "" {
			if p != nil {
				t.Errorf("ParserForFile(%s) = %s, want nil", tt.path, p.Name())
			}
		} else {
			if p == nil {
				t.Errorf("ParserForFile(%s) = nil, want %s", tt.path, tt.want)
			} else if p.Name() != tt.want {
				t.Errorf("ParserForFile(%s) = %s, want %s", tt.path, p.Name(), tt.want)
			}
		}
	}
}

func TestParserByName(t *testing.T) {
	p := ParserByName("python")
	if p == nil || p.Name() != "python" {
		t.Error("expected python parser")
	}
	p = ParserByName("nonexistent")
	if p != nil {
		t.Error("expected nil for unknown language")
	}
}

func TestRegisteredLanguages(t *testing.T) {
	langs := RegisteredLanguages()
	if len(langs) < 28 {
		t.Errorf("expected at least 28 languages, got %d: %v", len(langs), langs)
	}
	sort.Strings(langs)

	expected := []string{"c", "cpp", "csharp", "css", "dockerfile", "env", "go", "html", "java", "javascript", "json", "kotlin", "makefile", "markdown", "mermaid", "perl", "php", "properties", "python", "python2", "ruby", "rust", "shell", "toml", "typescript", "xml", "yaml", "zig"}
	for _, e := range expected {
		found := false
		for _, l := range langs {
			if l == e {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected language %s in registry", e)
		}
	}
}

func TestSupportedExtensions(t *testing.T) {
	exts := SupportedExtensions()
	if len(exts) < 17 {
		t.Errorf("expected at least 14 extensions, got %d", len(exts))
	}
}

func TestIsSupported(t *testing.T) {
	if !IsSupported("main.go") {
		t.Error("expected .go to be supported")
	}
	if IsSupported("data.csv") {
		t.Error("expected .csv to not be supported")
	}
}

func TestNormalizeExt(t *testing.T) {
	tests := []struct{ in, want string }{
		{".go", ".go"},
		{"go", ".go"},
		{".GO", ".go"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeExt(tt.in); got != tt.want {
			t.Errorf("normalizeExt(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Go parser tests
// ---------------------------------------------------------------------------

func TestGoParser(t *testing.T) {
	src := `package main

import "fmt"

const MaxRetries = 3

var DefaultTimeout = 30

type Config struct {
	Host string
	Port int
}

type Handler interface {
	Handle() error
}

func main() {
	fmt.Println("hello")
}

func (c *Config) Validate() error {
	return nil
}
`
	p := ParserForFile("main.go")
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "main", KindPackage)
	assertHasSymbol(t, symbols, "MaxRetries", KindConstant)
	assertHasSymbol(t, symbols, "DefaultTimeout", KindVariable)
	assertHasSymbol(t, symbols, "Config", KindStruct)
	assertHasSymbol(t, symbols, "Handler", KindInterface)
	assertHasSymbol(t, symbols, "main", KindFunction)
	assertHasSymbol(t, symbols, "Validate", KindMethod)

	// Check method parent
	for _, s := range symbols {
		if s.Name == "Validate" && s.Kind == KindMethod {
			if s.Parent != "Config" {
				t.Errorf("expected Validate parent=Config, got %s", s.Parent)
			}
		}
	}
}

func TestGoParserIsTestFile(t *testing.T) {
	p := &GoParser{}
	if !p.IsTestFile("main_test.go") {
		t.Error("expected test file")
	}
	if p.IsTestFile("main.go") {
		t.Error("expected non-test file")
	}
}

// ---------------------------------------------------------------------------
// Python parser tests
// ---------------------------------------------------------------------------

func TestPythonParser(t *testing.T) {
	src := `MAX_RETRIES = 3

class AuthService:
    def __init__(self, db):
        self.db = db

    def login(self, user, password):
        return self.validate(user, password)

    async def logout(self, user):
        pass

def standalone_func():
    return True

async def async_helper():
    pass
`
	p := ParserForFile("auth.py")
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "MAX_RETRIES", KindConstant)
	assertHasSymbol(t, symbols, "AuthService", KindClass)
	assertHasSymbol(t, symbols, "__init__", KindMethod)
	assertHasSymbol(t, symbols, "login", KindMethod)
	assertHasSymbol(t, symbols, "logout", KindMethod)
	assertHasSymbol(t, symbols, "standalone_func", KindFunction)
	assertHasSymbol(t, symbols, "async_helper", KindFunction)

	for _, s := range symbols {
		if s.Name == "login" {
			if s.Parent != "AuthService" {
				t.Errorf("expected login parent=AuthService, got %s", s.Parent)
			}
		}
	}
}

func TestPythonParserIsTestFile(t *testing.T) {
	p := &PythonParser{}
	if !p.IsTestFile("test_auth.py") {
		t.Error("expected test file")
	}
	if !p.IsTestFile("auth_test.py") {
		t.Error("expected test file")
	}
	if p.IsTestFile("auth.py") {
		t.Error("expected non-test file")
	}
}

// ---------------------------------------------------------------------------
// JavaScript parser tests
// ---------------------------------------------------------------------------

func TestJavaScriptParser(t *testing.T) {
	src := `const API_URL = "https://api.example.com";

function handleRequest(req, res) {
    const body = req.body;
    return processData(body);
}

const fetchData = async (url) => {
    return fetch(url);
};

class UserController {
    constructor(db) {
        this.db = db;
    }

    getUser(id) {
        return this.db.find(id);
    }

    async updateUser(id, data) {
        return this.db.update(id, data);
    }
}

export function formatDate(date) {
    return date.toISOString();
}
`
	p := ParserForFile("app.js")
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "API_URL", KindConstant)
	assertHasSymbol(t, symbols, "handleRequest", KindFunction)
	assertHasSymbol(t, symbols, "fetchData", KindFunction)
	assertHasSymbol(t, symbols, "UserController", KindClass)
	assertHasSymbol(t, symbols, "getUser", KindMethod)
	assertHasSymbol(t, symbols, "updateUser", KindMethod)
	assertHasSymbol(t, symbols, "formatDate", KindFunction)
}

func TestJavaScriptParserIsTestFile(t *testing.T) {
	p := &JavaScriptParser{}
	if !p.IsTestFile("app.test.js") {
		t.Error("expected test file")
	}
	if !p.IsTestFile("app.spec.js") {
		t.Error("expected test file")
	}
	if p.IsTestFile("app.js") {
		t.Error("expected non-test file")
	}
}

// ---------------------------------------------------------------------------
// TypeScript parser tests
// ---------------------------------------------------------------------------

func TestTypeScriptParser(t *testing.T) {
	src := `export interface UserService {
    getUser(id: string): Promise<User>;
    deleteUser(id: string): void;
}

export type UserID = string;

export enum Role {
    Admin = "admin",
    User = "user",
}

export abstract class BaseController {
    constructor(protected db: Database) {}

    abstract handle(): void;

    protected log(msg: string): void {
        console.log(msg);
    }
}

export const MAX_PAGE_SIZE = 100;

export async function fetchUsers(): Promise<User[]> {
    return [];
}

export const processData = (data: any) => {
    return transform(data);
};
`
	p := ParserForFile("service.ts")
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "UserService", KindInterface)
	assertHasSymbol(t, symbols, "UserID", KindType)
	assertHasSymbol(t, symbols, "Role", KindEnum)
	assertHasSymbol(t, symbols, "BaseController", KindClass)
	assertHasSymbol(t, symbols, "log", KindMethod)
	assertHasSymbol(t, symbols, "MAX_PAGE_SIZE", KindConstant)
	assertHasSymbol(t, symbols, "fetchUsers", KindFunction)
	assertHasSymbol(t, symbols, "processData", KindFunction)
}

func TestTypeScriptParserIsTestFile(t *testing.T) {
	p := &TypeScriptParser{}
	if !p.IsTestFile("app.test.ts") {
		t.Error("expected test file")
	}
	if !p.IsTestFile("app.spec.tsx") {
		t.Error("expected test file")
	}
	if p.IsTestFile("app.ts") {
		t.Error("expected non-test file")
	}
}

// ---------------------------------------------------------------------------
// Java parser tests
// ---------------------------------------------------------------------------

func TestJavaParser(t *testing.T) {
	src := `package com.example.auth;

public class AuthService {
    private static final int MAX_RETRIES = 3;

    public boolean login(String user, String pass) {
        return validate(user, pass);
    }

    private boolean validate(String user, String pass) {
        return true;
    }
}

public interface Repository {
    void save(Object entity);
    Object findById(String id);
}

public enum Status {
    ACTIVE,
    INACTIVE
}
`
	p := ParserForFile("AuthService.java")
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "com.example.auth", KindPackage)
	assertHasSymbol(t, symbols, "AuthService", KindClass)
	assertHasSymbol(t, symbols, "login", KindMethod)
	assertHasSymbol(t, symbols, "validate", KindMethod)
	assertHasSymbol(t, symbols, "Repository", KindInterface)
	assertHasSymbol(t, symbols, "Status", KindEnum)
	assertHasSymbol(t, symbols, "MAX_RETRIES", KindConstant)
}

func TestJavaParserIsTestFile(t *testing.T) {
	p := &JavaParser{}
	if !p.IsTestFile("AuthServiceTest.java") {
		t.Error("expected test file")
	}
	if p.IsTestFile("AuthService.java") {
		t.Error("expected non-test file")
	}
}

// ---------------------------------------------------------------------------
// C parser tests
// ---------------------------------------------------------------------------

func TestCParser(t *testing.T) {
	src := `#define MAX_BUFFER 1024

typedef struct node {
    int value;
    struct node *next;
} Node;

enum color {
    RED,
    GREEN,
    BLUE
};

typedef int handle_t;

int process_data(int *data, int len) {
    for (int i = 0; i < len; i++) {
        data[i] *= 2;
    }
    return 0;
}

static void helper(void) {
    return;
}
`
	p := ParserForFile("main.c")
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "MAX_BUFFER", KindConstant)
	assertHasSymbol(t, symbols, "node", KindStruct)
	assertHasSymbol(t, symbols, "color", KindEnum)
	assertHasSymbol(t, symbols, "handle_t", KindType)
	assertHasSymbol(t, symbols, "process_data", KindFunction)
	assertHasSymbol(t, symbols, "helper", KindFunction)
}

func TestCParserIsTestFile(t *testing.T) {
	p := &CParser{}
	if !p.IsTestFile("test_main.c") {
		t.Error("expected test file")
	}
	if p.IsTestFile("main.c") {
		t.Error("expected non-test file")
	}
}

// ---------------------------------------------------------------------------
// C++ parser tests
// ---------------------------------------------------------------------------

func TestCppParser(t *testing.T) {
	src := `namespace myapp {

class Engine {
public:
    void start() {
        init();
    }

private:
    void init() {}
};

struct Config {
    int timeout;
    std::string host;
};

enum class Status {
    Running,
    Stopped
};

} // namespace myapp

int main(int argc, char **argv) {
    return 0;
}

void Engine::shutdown() {
    cleanup();
}
`
	p := ParserForFile("main.cpp")
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "myapp", KindModule)
	assertHasSymbol(t, symbols, "Engine", KindClass)
	assertHasSymbol(t, symbols, "Config", KindStruct)
	assertHasSymbol(t, symbols, "Status", KindEnum)
	assertHasSymbol(t, symbols, "main", KindFunction)
	assertHasSymbol(t, symbols, "shutdown", KindMethod)
}

func TestCppParserIsTestFile(t *testing.T) {
	p := &CppParser{}
	if !p.IsTestFile("test_engine.cpp") {
		t.Error("expected test file")
	}
	if p.IsTestFile("engine.cpp") {
		t.Error("expected non-test file")
	}
}

func TestCppParserExtensions(t *testing.T) {
	exts := []string{".cpp", ".cc", ".cxx", ".hpp", ".hxx", ".hh"}
	for _, ext := range exts {
		p := ParserForFile("file" + ext)
		if p == nil || p.Name() != "cpp" {
			t.Errorf("expected cpp parser for %s", ext)
		}
	}
}

// ---------------------------------------------------------------------------
// Rust parser tests
// ---------------------------------------------------------------------------

func TestRustParser(t *testing.T) {
	src := `mod utils;

pub const MAX_SIZE: usize = 1024;

pub trait Handler {
    fn handle(&self) -> Result<(), Error>;
}

pub struct Server {
    addr: String,
    port: u16,
}

impl Server {
    pub fn new(addr: String, port: u16) -> Self {
        Server { addr, port }
    }

    pub async fn start(&self) -> Result<(), Error> {
        Ok(())
    }
}

pub enum Status {
    Running,
    Stopped,
}

pub type Result<T> = std::result::Result<T, Error>;

fn helper() -> bool {
    true
}
`
	p := ParserForFile("main.rs")
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "utils", KindModule)
	assertHasSymbol(t, symbols, "MAX_SIZE", KindConstant)
	assertHasSymbol(t, symbols, "Handler", KindTrait)
	assertHasSymbol(t, symbols, "Server", KindStruct)
	assertHasSymbol(t, symbols, "new", KindMethod)
	assertHasSymbol(t, symbols, "start", KindMethod)
	assertHasSymbol(t, symbols, "Status", KindEnum)
	assertHasSymbol(t, symbols, "Result", KindType)
	assertHasSymbol(t, symbols, "helper", KindFunction)

	for _, s := range symbols {
		if s.Name == "new" && s.Kind == KindMethod {
			if s.Parent != "Server" {
				t.Errorf("expected new parent=Server, got %s", s.Parent)
			}
		}
	}
}

func TestRustParserIsTestFile(t *testing.T) {
	p := &RustParser{}
	if !p.IsTestFile("tests/integration.rs") {
		t.Error("expected test file")
	}
	if p.IsTestFile("main.rs") {
		t.Error("expected non-test file")
	}
}

// ---------------------------------------------------------------------------
// Kotlin parser tests
// ---------------------------------------------------------------------------

func TestKotlinParser(t *testing.T) {
	src := `package com.example.app

const val MAX_RETRIES = 3

interface Repository {
    fun findById(id: String): Entity?
    fun save(entity: Entity)
}

data class User(
    val name: String,
    val email: String
)

enum class Role {
    ADMIN,
    USER
}

class AuthService(private val repo: Repository) {
    fun login(user: String, pass: String): Boolean {
        return validate(user, pass)
    }

    private fun validate(user: String, pass: String): Boolean {
        return true
    }
}

fun topLevelFunc(): String {
    return "hello"
}

typealias UserList = List<User>
`
	p := ParserForFile("App.kt")
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "com.example.app", KindPackage)
	assertHasSymbol(t, symbols, "MAX_RETRIES", KindConstant)
	assertHasSymbol(t, symbols, "Repository", KindInterface)
	assertHasSymbol(t, symbols, "User", KindClass)
	assertHasSymbol(t, symbols, "Role", KindEnum)
	assertHasSymbol(t, symbols, "AuthService", KindClass)
	assertHasSymbol(t, symbols, "login", KindMethod)
	assertHasSymbol(t, symbols, "validate", KindMethod)
	assertHasSymbol(t, symbols, "topLevelFunc", KindFunction)
	assertHasSymbol(t, symbols, "UserList", KindType)
}

func TestKotlinParserIsTestFile(t *testing.T) {
	p := &KotlinParser{}
	if !p.IsTestFile("AuthServiceTest.kt") {
		t.Error("expected test file")
	}
	if p.IsTestFile("AuthService.kt") {
		t.Error("expected non-test file")
	}
}

// ---------------------------------------------------------------------------
// Zig parser tests
// ---------------------------------------------------------------------------

func TestZigParser(t *testing.T) {
	src := `const std = @import("std");

pub const MAX_SIZE: usize = 1024;

var global_state: i32 = 0;

pub const Config = struct {
    host: []const u8,
    port: u16,
};

pub const Status = enum {
    running,
    stopped,
};

pub const Result = union(enum) {
    ok: i32,
    err: []const u8,
};

pub fn init(allocator: std.mem.Allocator) !void {
    _ = allocator;
}

fn helper() bool {
    return true;
}

test "basic init" {
    try init(std.testing.allocator);
}
`
	p := ParserForFile("main.zig")
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "MAX_SIZE", KindConstant)
	assertHasSymbol(t, symbols, "global_state", KindVariable)
	assertHasSymbol(t, symbols, "Config", KindStruct)
	assertHasSymbol(t, symbols, "Status", KindEnum)
	assertHasSymbol(t, symbols, "Result", KindType)
	assertHasSymbol(t, symbols, "init", KindFunction)
	assertHasSymbol(t, symbols, "helper", KindFunction)
	assertHasSymbol(t, symbols, "basic init", KindFunction)
}

func TestZigParserIsTestFile(t *testing.T) {
	p := &ZigParser{}
	if !p.IsTestFile("test_main.zig") {
		t.Error("expected test file")
	}
	if p.IsTestFile("main.zig") {
		t.Error("expected non-test file")
	}
}

// ---------------------------------------------------------------------------
// Python2 parser tests
// ---------------------------------------------------------------------------

func TestPython2Parser(t *testing.T) {
	src := `MAX_RETRIES = 3

class OldStyleClass:
    def __init__(self, name):
        self.name = name

    def get_name(self):
        return self.name

class NewStyleClass(object):
    def __init__(self, value):
        self.value = value

    def process(self):
        print self.value
        return self.value

def standalone_func():
    return True
`
	p := ParserByName("python2")
	if p == nil {
		t.Fatal("python2 parser not found")
	}
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "MAX_RETRIES", KindConstant)
	assertHasSymbol(t, symbols, "OldStyleClass", KindClass)
	assertHasSymbol(t, symbols, "__init__", KindMethod)
	assertHasSymbol(t, symbols, "get_name", KindMethod)
	assertHasSymbol(t, symbols, "NewStyleClass", KindClass)
	assertHasSymbol(t, symbols, "process", KindMethod)
	assertHasSymbol(t, symbols, "standalone_func", KindFunction)
}

func TestPython2ParserIsTestFile(t *testing.T) {
	p := &Python2Parser{}
	if !p.IsTestFile("test_legacy.py2") {
		t.Error("expected test file")
	}
	if p.IsTestFile("legacy.py2") {
		t.Error("expected non-test file")
	}
}

func TestPython2ParserExtensions(t *testing.T) {
	for _, ext := range []string{".py2", ".pyw"} {
		p := ParserForFile("file" + ext)
		if p == nil || p.Name() != "python2" {
			t.Errorf("expected python2 parser for %s", ext)
		}
	}
}

// ---------------------------------------------------------------------------
// CSS parser tests
// ---------------------------------------------------------------------------

func TestCSSParser(t *testing.T) {
	src := `:root {
    --primary-color: #333;
    --font-size: 16px;
}

body {
    margin: 0;
}

.container {
    max-width: 1200px;
}

#main-content {
    padding: 20px;
}

@media (max-width: 768px) {
    .container {
        padding: 10px;
    }
}

@keyframes fadeIn {
    from { opacity: 0; }
    to { opacity: 1; }
}

@font-face {
    font-family: "MyFont";
    src: url("font.woff2");
}
`
	p := ParserForFile("style.css")
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "--primary-color", KindVariable)
	assertHasSymbol(t, symbols, "--font-size", KindVariable)
	assertHasSymbol(t, symbols, "body", KindClass)
	assertHasSymbol(t, symbols, ".container", KindClass)
	assertHasSymbol(t, symbols, "#main-content", KindClass)
	assertHasSymbol(t, symbols, "fadeIn", KindFunction)
	assertHasSymbol(t, symbols, "@font-face", KindType)
}

func TestCSSParserExtensions(t *testing.T) {
	for _, ext := range []string{".css", ".scss", ".less", ".sass"} {
		p := ParserForFile("file" + ext)
		if p == nil || p.Name() != "css" {
			t.Errorf("expected css parser for %s", ext)
		}
	}
}

// ---------------------------------------------------------------------------
// HTML parser tests
// ---------------------------------------------------------------------------

func TestHTMLParser(t *testing.T) {
	src := `<!DOCTYPE html>
<html>
<head>
    <meta name="viewport" content="width=device-width">
    <title>Test</title>
</head>
<body>
    <header id="top-nav">
        <nav>Links</nav>
    </header>
    <main id="content">
        <section id="intro">
            <h1>Hello</h1>
        </section>
        <my-component id="widget"></my-component>
    </main>
    <footer>Copyright</footer>
</body>
</html>
`
	p := ParserForFile("index.html")
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "DOCTYPE html", KindModule)
	assertHasSymbol(t, symbols, "header#top-nav", KindStruct)
	assertHasSymbol(t, symbols, "main#content", KindStruct)
	assertHasSymbol(t, symbols, "section#intro", KindStruct)
	assertHasSymbol(t, symbols, "my-component#widget", KindClass)
	assertHasSymbol(t, symbols, "meta:viewport", KindProperty)
}

func TestHTMLParserExtensions(t *testing.T) {
	for _, ext := range []string{".html", ".htm"} {
		p := ParserForFile("file" + ext)
		if p == nil || p.Name() != "html" {
			t.Errorf("expected html parser for %s", ext)
		}
	}
}

// ---------------------------------------------------------------------------
// XML parser tests
// ---------------------------------------------------------------------------

func TestXMLParser(t *testing.T) {
	src := `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
    <modelVersion>4.0.0</modelVersion>
    <groupId>com.example</groupId>
    <artifactId>myapp</artifactId>
    <dependencies>
        <dependency name="junit"/>
        <dependency name="mockito"/>
    </dependencies>
</project>
`
	p := ParserForFile("pom.xml")
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "xml-declaration", KindModule)
	assertHasSymbol(t, symbols, "xmlns", KindImport)
	assertHasSymbol(t, symbols, "xmlns:xsi", KindImport)
	assertHasSymbol(t, symbols, "project", KindClass)
	assertHasSymbol(t, symbols, "dependency[junit]", KindProperty)
	assertHasSymbol(t, symbols, "dependency[mockito]", KindProperty)
}

func TestXMLParserExtensions(t *testing.T) {
	for _, ext := range []string{".xml", ".xsl", ".xslt", ".xsd", ".plist", ".xaml"} {
		p := ParserForFile("file" + ext)
		if p == nil || p.Name() != "xml" {
			t.Errorf("expected xml parser for %s", ext)
		}
	}
}

// ---------------------------------------------------------------------------
// Ruby parser tests
// ---------------------------------------------------------------------------

func TestRubyParser(t *testing.T) {
	src := `module Authentication
  MAX_ATTEMPTS = 3

  class AuthService
    attr_reader :db, :logger

    def initialize(db)
      @db = db
    end

    def login(user, password)
      validate(user, password)
    end

    private

    def validate(user, password)
      true
    end
  end

  def self.configure
    yield config
  end
end

class User
  attr_accessor :name, :email

  def to_s
    name
  end
end

def standalone_helper
  true
end
`
	p := ParserForFile("auth.rb")
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "Authentication", KindModule)
	assertHasSymbol(t, symbols, "MAX_ATTEMPTS", KindConstant)
	assertHasSymbol(t, symbols, "AuthService", KindClass)
	assertHasSymbol(t, symbols, "db", KindProperty)
	assertHasSymbol(t, symbols, "initialize", KindMethod)
	assertHasSymbol(t, symbols, "login", KindMethod)
	assertHasSymbol(t, symbols, "validate", KindMethod)
	assertHasSymbol(t, symbols, "User", KindClass)
	assertHasSymbol(t, symbols, "name", KindProperty)
	assertHasSymbol(t, symbols, "to_s", KindMethod)
	assertHasSymbol(t, symbols, "standalone_helper", KindFunction)
}

func TestRubyParserIsTestFile(t *testing.T) {
	p := &RubyParser{}
	if !p.IsTestFile("auth_test.rb") {
		t.Error("expected test file")
	}
	if !p.IsTestFile("auth_spec.rb") {
		t.Error("expected spec file")
	}
	if p.IsTestFile("auth.rb") {
		t.Error("expected non-test file")
	}
}

func TestRubyParserExtensions(t *testing.T) {
	for _, ext := range []string{".rb", ".rake", ".gemspec"} {
		p := ParserForFile("file" + ext)
		if p == nil || p.Name() != "ruby" {
			t.Errorf("expected ruby parser for %s", ext)
		}
	}
}

// ---------------------------------------------------------------------------
// PHP parser tests
// ---------------------------------------------------------------------------

func TestPHPParser(t *testing.T) {
	src := `<?php
namespace App\Services;

define('VERSION', '1.0');

interface Repository {
    public function findById(string $id): ?Entity;
    public function save(Entity $entity): void;
}

trait Loggable {
    public function log(string $msg): void {
        echo $msg;
    }
}

abstract class BaseService {
    const MAX_RETRIES = 3;

    abstract public function execute(): void;

    protected function retry(callable $fn): mixed {
        return $fn();
    }
}

class AuthService extends BaseService {
    use Loggable;

    public function execute(): void {
        $this->login();
    }

    public function login(): bool {
        return true;
    }

    private function validate(string $user): bool {
        return true;
    }
}

enum Status {
    case Active;
    case Inactive;
}

function helper(): bool {
    return true;
}
`
	p := ParserForFile("AuthService.php")
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, `App\Services`, KindPackage)
	assertHasSymbol(t, symbols, "VERSION", KindConstant)
	assertHasSymbol(t, symbols, "Repository", KindInterface)
	assertHasSymbol(t, symbols, "Loggable", KindTrait)
	assertHasSymbol(t, symbols, "BaseService", KindClass)
	assertHasSymbol(t, symbols, "MAX_RETRIES", KindConstant)
	assertHasSymbol(t, symbols, "execute", KindMethod)
	assertHasSymbol(t, symbols, "AuthService", KindClass)
	assertHasSymbol(t, symbols, "login", KindMethod)
	assertHasSymbol(t, symbols, "validate", KindMethod)
	assertHasSymbol(t, symbols, "Status", KindEnum)
	assertHasSymbol(t, symbols, "helper", KindFunction)

	for _, s := range symbols {
		if s.Name == "login" && s.Kind == KindMethod {
			if s.Parent != "AuthService" {
				t.Errorf("expected login parent=AuthService, got %s", s.Parent)
			}
		}
	}
}

func TestPHPParserIsTestFile(t *testing.T) {
	p := &PHPParser{}
	if !p.IsTestFile("AuthServiceTest.php") {
		t.Error("expected test file")
	}
	if p.IsTestFile("AuthService.php") {
		t.Error("expected non-test file")
	}
}

func TestPHPParserExtensions(t *testing.T) {
	for _, ext := range []string{".php", ".phtml"} {
		p := ParserForFile("file" + ext)
		if p == nil || p.Name() != "php" {
			t.Errorf("expected php parser for %s", ext)
		}
	}
}

// ---------------------------------------------------------------------------
// C# parser tests
// ---------------------------------------------------------------------------

func TestCSharpParser(t *testing.T) {
	src := `using System;
using System.Collections.Generic;

namespace MyApp.Services
{
    public interface IRepository
    {
        void Save(object entity);
        object FindById(string id);
    }

    public enum Status
    {
        Active,
        Inactive
    }

    public struct Point
    {
        public int X { get; set; }
        public int Y { get; set; }
    }

    public abstract class BaseService
    {
        public const int MaxRetries = 3;

        public event EventHandler Changed;

        public abstract void Execute();

        protected virtual void OnChanged()
        {
            Changed?.Invoke(this, EventArgs.Empty);
        }
    }

    public class AuthService : BaseService
    {
        public string Name { get; set; }

        public override void Execute()
        {
            Login("admin", "pass");
        }

        public bool Login(string user, string pass)
        {
            return Validate(user, pass);
        }

        private bool Validate(string user, string pass)
        {
            return true;
        }
    }

    public delegate void ActionHandler(string action);

    public record UserRecord(string Name, string Email);
}
`
	p := ParserForFile("AuthService.cs")
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "System", KindImport)
	assertHasSymbol(t, symbols, "System.Collections.Generic", KindImport)
	assertHasSymbol(t, symbols, "MyApp.Services", KindPackage)
	assertHasSymbol(t, symbols, "IRepository", KindInterface)
	assertHasSymbol(t, symbols, "Status", KindEnum)
	assertHasSymbol(t, symbols, "Point", KindStruct)
	assertHasSymbol(t, symbols, "BaseService", KindClass)
	assertHasSymbol(t, symbols, "MaxRetries", KindConstant)
	assertHasSymbol(t, symbols, "AuthService", KindClass)
	assertHasSymbol(t, symbols, "Login", KindMethod)
	assertHasSymbol(t, symbols, "Validate", KindMethod)
	assertHasSymbol(t, symbols, "ActionHandler", KindType)
	assertHasSymbol(t, symbols, "UserRecord", KindClass)

	for _, s := range symbols {
		if s.Name == "Login" && s.Kind == KindMethod {
			if s.Parent != "AuthService" {
				t.Errorf("expected Login parent=AuthService, got %s", s.Parent)
			}
		}
	}
}

func TestCSharpParserIsTestFile(t *testing.T) {
	p := &CSharpParser{}
	if !p.IsTestFile("AuthServiceTest.cs") {
		t.Error("expected test file")
	}
	if p.IsTestFile("AuthService.cs") {
		t.Error("expected non-test file")
	}
}

func TestCSharpParserExtensions(t *testing.T) {
	for _, ext := range []string{".cs", ".csx"} {
		p := ParserForFile("file" + ext)
		if p == nil || p.Name() != "csharp" {
			t.Errorf("expected csharp parser for %s", ext)
		}
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestEmptyContent(t *testing.T) {
	langs := RegisteredLanguages()
	for _, name := range langs {
		p := ParserByName(name)
		symbols := p.ExtractSymbols("")
		if len(symbols) != 0 {
			t.Errorf("%s: expected no symbols for empty content, got %d", name, len(symbols))
		}
	}
}

func TestSingleLineContent(t *testing.T) {
	p := ParserForFile("main.go")
	symbols := p.ExtractSymbols("package main")
	assertHasSymbol(t, symbols, "main", KindPackage)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Perl parser tests
// ---------------------------------------------------------------------------

func TestPerlParser(t *testing.T) {
	src := `package MyApp::Auth;

use strict;
use warnings;
use parent 'MyApp::Base';
use constant MAX_RETRIES => 3;

our $VERSION = '1.0';

has 'username' => (
    is => 'ro',
    isa => 'Str',
);

has 'password' => (
    is => 'rw',
);

sub new {
    my ($class, %args) = @_;
    return bless \%args, $class;
}

sub authenticate {
    my ($self, $token) = @_;
    if ($self->{username}) {
        return $self->_validate($token);
    }
    return 0;
}

sub _validate {
    my ($self, $token) = @_;
    return defined $token;
}

BEGIN {
    print "Loading module\n";
}

1;
`
	p := &PerlParser{}
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "MyApp::Auth", KindPackage)
	assertHasSymbol(t, symbols, "MAX_RETRIES", KindConstant)
	assertHasSymbol(t, symbols, "$VERSION", KindVariable)
	assertHasSymbol(t, symbols, "username", KindProperty)
	assertHasSymbol(t, symbols, "password", KindProperty)
	assertHasSymbol(t, symbols, "new", KindMethod)
	assertHasSymbol(t, symbols, "authenticate", KindMethod)
	assertHasSymbol(t, symbols, "_validate", KindMethod)
	assertHasSymbol(t, symbols, "BEGIN", KindFunction)

	// Verify parent is set for methods
	for _, s := range symbols {
		if s.Name == "authenticate" {
			if s.Parent != "MyApp::Auth" {
				t.Errorf("expected parent MyApp::Auth for authenticate, got %q", s.Parent)
			}
		}
	}
}

func TestPerlParserScript(t *testing.T) {
	src := `#!/usr/bin/perl
use strict;
use warnings;

sub main {
    my @args = @ARGV;
    process(@args);
}

sub process {
    my (@items) = @_;
    foreach my $item (@items) {
        print "$item\n";
    }
}

main();
`
	p := &PerlParser{}
	symbols := p.ExtractSymbols(src)

	// No package declaration, so subs should be functions
	assertHasSymbol(t, symbols, "main", KindFunction)
	assertHasSymbol(t, symbols, "process", KindFunction)

	for _, s := range symbols {
		if s.Name == "main" && s.Kind == KindFunction {
			if s.Parent != "" {
				t.Errorf("expected no parent for top-level sub, got %q", s.Parent)
			}
		}
	}
}

func TestPerlParserPOD(t *testing.T) {
	src := `package Foo;

sub visible {
    return 1;
}

=head1 NAME

Foo - A test module

=head1 METHODS

=cut

sub also_visible {
    return 2;
}
`
	p := &PerlParser{}
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "visible", KindMethod)
	assertHasSymbol(t, symbols, "also_visible", KindMethod)
}

func TestPerlParserIsTestFile(t *testing.T) {
	p := &PerlParser{}
	if !p.IsTestFile("auth.t") {
		t.Error("expected .t to be test file")
	}
	if !p.IsTestFile("t/basic.t") {
		t.Error("expected t/ path to be test file")
	}
	if !p.IsTestFile("lib/test/foo.pl") {
		t.Error("expected /test/ path to be test file")
	}
	if p.IsTestFile("lib/Foo.pm") {
		t.Error("expected lib/Foo.pm to NOT be test file")
	}
	if p.IsTestFile("script.pl") {
		t.Error("expected script.pl to NOT be test file")
	}
}

func TestPerlParserExtensions(t *testing.T) {
	for _, ext := range []string{".pl", ".pm", ".t", ".psgi"} {
		p := ParserForFile("file" + ext)
		if p == nil || p.Name() != "perl" {
			t.Errorf("expected perl parser for %s", ext)
		}
	}
}

func TestPerlParserFlowHints(t *testing.T) {
	p := &PerlParser{}
	h := p.FlowHints()
	if len(h.Keywords) == 0 {
		t.Error("expected non-empty keywords")
	}
	if len(h.CommentPrefixes) == 0 || h.CommentPrefixes[0] != "#" {
		t.Error("expected # as comment prefix")
	}
}

// ---------------------------------------------------------------------------
// Markdown parser tests
// ---------------------------------------------------------------------------

func TestMarkdownParser(t *testing.T) {
	src := `---
title: My Guide
author: Alice
tags: [go, cli]
---

# Introduction

Some text here.

## Getting Started

More text.

### Installation

Run the command.

### Configuration

Edit the config file.

## Advanced Usage

Deep stuff.

` + "```go" + `
func main() {
    fmt.Println("hello")
}
` + "```" + `

` + "```bash" + `
echo hello
` + "```" + `

[homepage]: https://example.com
[docs]: https://docs.example.com
`
	p := &MarkdownParser{}
	symbols := p.ExtractSymbols(src)

	// Front matter keys
	assertHasSymbol(t, symbols, "title", KindProperty)
	assertHasSymbol(t, symbols, "author", KindProperty)
	assertHasSymbol(t, symbols, "tags", KindProperty)

	// Headings
	assertHasSymbol(t, symbols, "Introduction", KindModule)
	assertHasSymbol(t, symbols, "Getting Started", KindModule)
	assertHasSymbol(t, symbols, "Installation", KindFunction)
	assertHasSymbol(t, symbols, "Configuration", KindFunction)
	assertHasSymbol(t, symbols, "Advanced Usage", KindModule)

	// Code blocks
	assertHasSymbol(t, symbols, "code:go", KindModule)
	assertHasSymbol(t, symbols, "code:bash", KindModule)

	// Link definitions
	assertHasSymbol(t, symbols, "homepage", KindConstant)
	assertHasSymbol(t, symbols, "docs", KindConstant)
}

func TestMarkdownParserNoFrontMatter(t *testing.T) {
	src := `# Title

Some content.

## Section A

Text.
`
	p := &MarkdownParser{}
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "Title", KindModule)
	assertHasSymbol(t, symbols, "Section A", KindModule)

	// No front matter symbols
	for _, s := range symbols {
		if s.Kind == KindProperty {
			t.Errorf("unexpected front matter symbol: %s", s.Name)
		}
	}
}

func TestMarkdownParserCodeBlockSkip(t *testing.T) {
	src := "# Top\n\n" + "```python" + "\n# This is NOT a heading\n## Also not a heading\n" + "```" + "\n\n## Real Section\n"
	p := &MarkdownParser{}
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "Top", KindModule)
	assertHasSymbol(t, symbols, "Real Section", KindModule)
	assertHasSymbol(t, symbols, "code:python", KindModule)

	// Should NOT have headings from inside the code block
	for _, s := range symbols {
		if s.Name == "This is NOT a heading" || s.Name == "Also not a heading" {
			t.Errorf("should not extract heading from inside code block: %s", s.Name)
		}
	}
}

func TestMarkdownParserIsTestFile(t *testing.T) {
	p := &MarkdownParser{}
	if p.IsTestFile("README.md") {
		t.Error("markdown files should never be test files")
	}
}

func TestMarkdownParserExtensions(t *testing.T) {
	for _, ext := range []string{".md", ".markdown", ".mdx"} {
		p := ParserForFile("file" + ext)
		if p == nil || p.Name() != "markdown" {
			t.Errorf("expected markdown parser for %s", ext)
		}
	}
}

func TestMarkdownParserFlowHints(t *testing.T) {
	p := &MarkdownParser{}
	h := p.FlowHints()
	if len(h.CommentPrefixes) == 0 || h.CommentPrefixes[0] != "<!--" {
		t.Error("expected <!-- as comment prefix")
	}
}

// ---------------------------------------------------------------------------
// Mermaid parser tests
// ---------------------------------------------------------------------------

func TestMermaidParserFlowchart(t *testing.T) {
	src := `flowchart TD
    A[Start] --> B{Decision}
    B -->|Yes| C[Process]
    B -->|No| D[End]
    subgraph Validation
        E[Check Input] --> F[Validate]
    end
`
	p := &MermaidParser{}
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "flowchart", KindModule)
	assertHasSymbol(t, symbols, "A", KindStruct)
	assertHasSymbol(t, symbols, "B", KindStruct)
	assertHasSymbol(t, symbols, "C", KindStruct)
	assertHasSymbol(t, symbols, "D", KindStruct)
	assertHasSymbol(t, symbols, "Validation", KindClass)
}

func TestMermaidParserSequenceDiagram(t *testing.T) {
	src := `sequenceDiagram
    participant Alice
    participant Bob as Robert
    Alice->>Bob: Hello
    Bob-->>Alice: Hi
`
	p := &MermaidParser{}
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "sequenceDiagram", KindModule)
	assertHasSymbol(t, symbols, "Alice", KindType)
	assertHasSymbol(t, symbols, "Robert", KindType)
}

func TestMermaidParserGantt(t *testing.T) {
	src := `gantt
    title Project Plan
    section Design
        Task 1 :a1, 2024-01-01, 30d
    section Development
        Task 2 :a2, after a1, 20d
`
	p := &MermaidParser{}
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "gantt", KindModule)
	assertHasSymbol(t, symbols, "Project Plan", KindProperty)
	assertHasSymbol(t, symbols, "Design", KindFunction)
	assertHasSymbol(t, symbols, "Development", KindFunction)
}

func TestMermaidParserClassDiagram(t *testing.T) {
	src := `classDiagram
    class Animal
    class Dog
    Animal <|-- Dog
`
	p := &MermaidParser{}
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "classDiagram", KindModule)
	assertHasSymbol(t, symbols, "Animal", KindClass)
	assertHasSymbol(t, symbols, "Dog", KindClass)
}

func TestMermaidParserSubgraphEndLine(t *testing.T) {
	src := `graph TD
    subgraph MyGroup
        A --> B
    end
`
	p := &MermaidParser{}
	symbols := p.ExtractSymbols(src)

	for _, s := range symbols {
		if s.Name == "MyGroup" {
			if s.EndLine != 4 {
				t.Errorf("expected subgraph EndLine=4, got %d", s.EndLine)
			}
			return
		}
	}
	t.Error("subgraph MyGroup not found")
}

func TestMermaidParserComments(t *testing.T) {
	src := `graph TD
    %% This is a comment
    A[Node]
`
	p := &MermaidParser{}
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "A", KindStruct)
	// Comment should not produce a symbol
	for _, s := range symbols {
		if strings.Contains(s.Name, "comment") {
			t.Error("comments should not produce symbols")
		}
	}
}

func TestMermaidParserIsTestFile(t *testing.T) {
	p := &MermaidParser{}
	if p.IsTestFile("diagram.mermaid") {
		t.Error("mermaid files should never be test files")
	}
}

func TestMermaidParserExtensions(t *testing.T) {
	for _, ext := range []string{".mermaid", ".mmd"} {
		p := ParserForFile("file" + ext)
		if p == nil || p.Name() != "mermaid" {
			t.Errorf("expected mermaid parser for %s", ext)
		}
	}
}

func TestMermaidParserFlowHints(t *testing.T) {
	p := &MermaidParser{}
	h := p.FlowHints()
	if len(h.CommentPrefixes) == 0 || h.CommentPrefixes[0] != "%%" {
		t.Error("expected %% as comment prefix")
	}
}

// ---------------------------------------------------------------------------
// Shell parser tests
// ---------------------------------------------------------------------------

func TestShellParser(t *testing.T) {
	src := `#!/bin/bash
set -euo pipefail

export PATH="/usr/local/bin:$PATH"
readonly VERSION="1.0"
DB_HOST="localhost"

function setup_env() {
    local tmp=$(mktemp -d)
    echo "$tmp"
}

cleanup() {
    rm -rf "$TMP_DIR"
}

alias ll='ls -la'

main() {
    setup_env
    cleanup
}

main "$@"
`
	p := &ShellParser{}
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "setup_env", KindFunction)
	assertHasSymbol(t, symbols, "cleanup", KindFunction)
	assertHasSymbol(t, symbols, "main", KindFunction)
	assertHasSymbol(t, symbols, "PATH", KindVariable)
	assertHasSymbol(t, symbols, "VERSION", KindConstant)
	assertHasSymbol(t, symbols, "DB_HOST", KindVariable)
	assertHasSymbol(t, symbols, "ll", KindFunction)
}

func TestShellParserIsTestFile(t *testing.T) {
	p := &ShellParser{}
	if !p.IsTestFile("test.bats") {
		t.Error("expected .bats to be test file")
	}
	if !p.IsTestFile("run_test.sh") {
		t.Error("expected _test.sh to be test file")
	}
	if p.IsTestFile("deploy.sh") {
		t.Error("expected deploy.sh NOT to be test file")
	}
}

func TestShellParserExtensions(t *testing.T) {
	for _, ext := range []string{".sh", ".bash", ".zsh", ".ksh", ".bats"} {
		p := ParserForFile("file" + ext)
		if p == nil || p.Name() != "shell" {
			t.Errorf("expected shell parser for %s", ext)
		}
	}
}

// ---------------------------------------------------------------------------
// Makefile parser tests
// ---------------------------------------------------------------------------

func TestMakefileParser(t *testing.T) {
	src := `.PHONY: build install test lint clean

BINARY    := saras
VERSION   ?= $(shell git describe --tags)

build:
	@mkdir -p bin
	go build -o bin/$(BINARY) ./cmd/saras

test:
	go test ./...

install: build
	cp bin/$(BINARY) /usr/local/bin/

clean:
	rm -rf bin
`
	p := &MakefileParser{}
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, ".PHONY", KindProperty)
	assertHasSymbol(t, symbols, "BINARY", KindVariable)
	assertHasSymbol(t, symbols, "VERSION", KindVariable)
	assertHasSymbol(t, symbols, "build", KindFunction)
	assertHasSymbol(t, symbols, "test", KindFunction)
	assertHasSymbol(t, symbols, "install", KindFunction)
	assertHasSymbol(t, symbols, "clean", KindFunction)
}

func TestMakefileParserFilenameMatch(t *testing.T) {
	for _, name := range []string{"Makefile", "makefile", "GNUmakefile"} {
		p := ParserForFile(name)
		if p == nil || p.Name() != "makefile" {
			t.Errorf("expected makefile parser for %q", name)
		}
	}
}

func TestMakefileParserExtensions(t *testing.T) {
	for _, ext := range []string{".mk", ".make"} {
		p := ParserForFile("file" + ext)
		if p == nil || p.Name() != "makefile" {
			t.Errorf("expected makefile parser for %s", ext)
		}
	}
}

// ---------------------------------------------------------------------------
// TOML parser tests
// ---------------------------------------------------------------------------

func TestTOMLParser(t *testing.T) {
	src := `[package]
name = "myapp"
version = "0.1.0"

[dependencies]
serde = "1.0"

[[bin]]
name = "cli"
path = "src/main.rs"

[[bin]]
name = "server"
path = "src/server.rs"
`
	p := &TOMLParser{}
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "[package]", KindModule)
	assertHasSymbol(t, symbols, "[dependencies]", KindModule)
	assertHasSymbol(t, symbols, "[[bin]]", KindClass)
	assertHasSymbol(t, symbols, "name", KindProperty)
	assertHasSymbol(t, symbols, "version", KindProperty)
	assertHasSymbol(t, symbols, "serde", KindProperty)
}

func TestTOMLParserExtensions(t *testing.T) {
	p := ParserForFile("Cargo.toml")
	if p == nil || p.Name() != "toml" {
		t.Error("expected toml parser for .toml")
	}
}

// ---------------------------------------------------------------------------
// YAML parser tests
// ---------------------------------------------------------------------------

func TestYAMLParser(t *testing.T) {
	src := `name: myapp
version: 1.0

server:
  host: localhost
  port: 8080

database:
  driver: postgres
  host: db.example.com
`
	p := &YAMLParser{}
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "name", KindModule)
	assertHasSymbol(t, symbols, "version", KindModule)
	assertHasSymbol(t, symbols, "server", KindModule)
	assertHasSymbol(t, symbols, "database", KindModule)
	assertHasSymbol(t, symbols, "host", KindProperty)
	assertHasSymbol(t, symbols, "port", KindProperty)
}

func TestYAMLParserDocSeparator(t *testing.T) {
	src := "---\nname: doc1\n---\nname: doc2\n"
	p := &YAMLParser{}
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "---", KindModule)
}

func TestYAMLParserExtensions(t *testing.T) {
	for _, ext := range []string{".yaml", ".yml"} {
		p := ParserForFile("config" + ext)
		if p == nil || p.Name() != "yaml" {
			t.Errorf("expected yaml parser for %s", ext)
		}
	}
}

// ---------------------------------------------------------------------------
// JSON parser tests
// ---------------------------------------------------------------------------

func TestJSONParser(t *testing.T) {
	src := `{
  "name": "myapp",
  "version": "1.0.0",
  "scripts": {
    "build": "tsc",
    "test": "jest"
  },
  "dependencies": {
    "express": "^4.18.0"
  },
  "main": "index.js"
}
`
	p := &JSONParser{}
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "name", KindProperty)
	assertHasSymbol(t, symbols, "version", KindProperty)
	assertHasSymbol(t, symbols, "scripts", KindModule)
	assertHasSymbol(t, symbols, "dependencies", KindModule)
	assertHasSymbol(t, symbols, "main", KindProperty)
}

func TestJSONParserExtensions(t *testing.T) {
	for _, ext := range []string{".json", ".jsonc", ".json5"} {
		p := ParserForFile("file" + ext)
		if p == nil || p.Name() != "json" {
			t.Errorf("expected json parser for %s", ext)
		}
	}
}

// ---------------------------------------------------------------------------
// Env file parser tests
// ---------------------------------------------------------------------------

func TestEnvFileParser(t *testing.T) {
	src := `# Database config
DB_HOST=localhost
DB_PORT=5432
export DB_PASSWORD=secret
API_KEY=abc123

# Feature flags
ENABLE_CACHE=true
`
	p := &EnvFileParser{}
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "DB_HOST", KindVariable)
	assertHasSymbol(t, symbols, "DB_PORT", KindVariable)
	assertHasSymbol(t, symbols, "DB_PASSWORD", KindVariable)
	assertHasSymbol(t, symbols, "API_KEY", KindVariable)
	assertHasSymbol(t, symbols, "ENABLE_CACHE", KindVariable)

	if len(symbols) != 5 {
		t.Errorf("expected 5 symbols, got %d", len(symbols))
	}
}

func TestEnvFileParserExtension(t *testing.T) {
	p := ParserForFile(".env")
	if p == nil || p.Name() != "env" {
		t.Error("expected env parser for .env")
	}
}

// ---------------------------------------------------------------------------
// Properties parser tests
// ---------------------------------------------------------------------------

func TestPropertiesParser(t *testing.T) {
	src := `# Application config
app.name=MyApp
app.version=1.0
server.port=8080
database.url: jdbc:postgresql://localhost/mydb
! Legacy comment
debug.enabled=false
`
	p := &PropertiesParser{}
	symbols := p.ExtractSymbols(src)

	assertHasSymbol(t, symbols, "app.name", KindProperty)
	assertHasSymbol(t, symbols, "app.version", KindProperty)
	assertHasSymbol(t, symbols, "server.port", KindProperty)
	assertHasSymbol(t, symbols, "database.url", KindProperty)
	assertHasSymbol(t, symbols, "debug.enabled", KindProperty)
}

func TestPropertiesParserExtension(t *testing.T) {
	p := ParserForFile("app.properties")
	if p == nil || p.Name() != "properties" {
		t.Error("expected properties parser for .properties")
	}
}

// ---------------------------------------------------------------------------
// Dockerfile parser tests
// ---------------------------------------------------------------------------

func TestDockerfileParser(t *testing.T) {
	src := `FROM golang:1.22-alpine AS builder
ARG VERSION=dev
ENV CGO_ENABLED=0
WORKDIR /app
COPY . .
RUN go build -o /bin/app

FROM alpine:3.19 AS runtime
LABEL maintainer="dev@example.com"
COPY --from=builder /bin/app /usr/local/bin/app
EXPOSE 8080
VOLUME /data
HEALTHCHECK CMD wget -q --spider http://localhost:8080/healthz
ENTRYPOINT ["app"]
CMD ["serve"]
`
	p := &DockerfileParser{}
	symbols := p.ExtractSymbols(src)

	// Build stages
	assertHasSymbol(t, symbols, "builder", KindModule)
	assertHasSymbol(t, symbols, "runtime", KindModule)

	// ARG / ENV
	assertHasSymbol(t, symbols, "VERSION", KindVariable)
	assertHasSymbol(t, symbols, "CGO_ENABLED", KindVariable)

	// Directives
	assertHasSymbol(t, symbols, "WORKDIR /app", KindProperty)
	assertHasSymbol(t, symbols, "EXPOSE 8080", KindProperty)
	assertHasSymbol(t, symbols, "VOLUME /data", KindProperty)
	assertHasSymbol(t, symbols, "ENTRYPOINT", KindFunction)
	assertHasSymbol(t, symbols, "CMD", KindFunction)
	assertHasSymbol(t, symbols, "HEALTHCHECK", KindFunction)
}

func TestDockerfileParserFilenameMatch(t *testing.T) {
	for _, name := range []string{"Dockerfile", "Containerfile"} {
		p := ParserForFile(name)
		if p == nil || p.Name() != "dockerfile" {
			t.Errorf("expected dockerfile parser for %q", name)
		}
	}
}

func TestDockerfileParserExtensions(t *testing.T) {
	for _, ext := range []string{".dockerfile", ".containerfile"} {
		p := ParserForFile("app" + ext)
		if p == nil || p.Name() != "dockerfile" {
			t.Errorf("expected dockerfile parser for %s", ext)
		}
	}
}

func assertHasSymbol(t *testing.T, symbols []Symbol, name string, kind SymbolKind) {
	t.Helper()
	for _, s := range symbols {
		if s.Name == name && s.Kind == kind {
			return
		}
	}
	t.Errorf("expected symbol %s (%s) not found in %d symbols", name, kind, len(symbols))
	for _, s := range symbols {
		t.Logf("  have: %s (%s) at line %d", s.Name, s.Kind, s.StartLine)
	}
}
