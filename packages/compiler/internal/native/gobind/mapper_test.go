package gobind

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

const krateStub = `package krate

import "context"

type Ctx interface {
	context.Context
	Window() string
}

type Signal[T any] struct{ v T }

func NewSignal[T any](v T) *Signal[T] { return &Signal[T]{v} }
`

const header = `package p

import (
	"time"

	"github.com/kratejs/krate/packages/native/krate"
)

var _ time.Time
var _ krate.Ctx

`

// The source importer is slow, so every test shares one.
var (
	sharedFset = token.NewFileSet()
	sharedStd  = importer.ForCompiler(sharedFset, "source", nil)
)

type fakeImporter struct {
	std   types.Importer
	krate *types.Package
}

func (f fakeImporter) Import(path string) (*types.Package, error) {
	if path == krateImportPath {
		return f.krate, nil
	}
	return f.std.Import(path)
}

// check type-checks a header plus body and classifies the named imports.
func check(t *testing.T, body string, names ...string) ([]*hostItem, []diag.Diagnostic, string) {
	t.Helper()
	fset, std := sharedFset, sharedStd
	kf, err := parser.ParseFile(fset, "/proj/krate/krate.go", krateStub, 0)
	if err != nil {
		t.Fatal(err)
	}
	kp, err := (&types.Config{Importer: std}).Check(krateImportPath, fset, []*ast.File{kf}, nil)
	if err != nil {
		t.Fatal(err)
	}
	src := header + body
	f, err := parser.ParseFile(fset, "/proj/x.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := (&types.Config{Importer: fakeImporter{std, kp}}).Check("example.com/app/p", fset, []*ast.File{f}, nil)
	if err != nil {
		t.Fatalf("fixture does not type-check: %v", err)
	}
	a := newAnalyzer("/proj", fset)
	m := &module{id: "x", src: "/proj/x.go", pkg: &userPkg{pkg: pkg}}
	mp := newMapper(a)
	for _, n := range names {
		a.classify(m, mp, n, nil, func() token.Position { return token.Position{} })
	}
	return m.items, a.diags, src
}

func lineCol(src, at string) (int, int) {
	i := strings.Index(src[len(header):], at)
	if i < 0 {
		return 0, 0
	}
	i += len(header)
	line := 1 + strings.Count(src[:i], "\n")
	col := i - (strings.LastIndex(src[:i], "\n") + 1) + 1
	return line, col
}

func TestTypeMapping(t *testing.T) {
	tests := []struct {
		name, src, fn string
		want          []string // substrings of the d.ts
	}{
		{"scalars", `func F(a bool, b string, c int, d uint8, e float32, f int64) {}`, "F",
			[]string{"F(a: boolean, b: string, c: number, d: number, e: number, f: number, opts?: CallOptions): Promise<void>"}},
		{"result only", `func F() int { return 0 }`, "F", []string{"F(opts?: CallOptions): Promise<number>"}},
		{"error only", `func F() error { return nil }`, "F", []string{"Promise<void>"}},
		{"value and error", `func F(ctx krate.Ctx) (string, error) { return "", nil }`, "F", []string{"F(opts?: CallOptions): Promise<string>"}},
		{"time and bytes", `func F(t time.Time, b []byte) (time.Time, error) { return t, nil }`, "F",
			[]string{"F(t: Date, b: Uint8Array, opts?: CallOptions): Promise<Date>"}},
		{"slice array map", `func F(a []string, b [3]int, c map[string]bool) {}`, "F",
			[]string{"a: string[], b: number[], c: Record<string, boolean>"}},
		{"pointer", `func F(a *string) *int { return nil }`, "F", []string{"a: string | null", "Promise<number | null>"}},
		{"pointer slice", `func F(a []*string) {}`, "F", []string{"a: (string | null)[]"}},
		{"named and alias", `type S string; type A = S; type N int; func F(a A, b N) {}`, "F",
			[]string{"export type S = string;", "export type N = number;", "a: S, b: N"}},
		{"string-kinded key", `type K string; func F(a map[K]int) {}`, "F", []string{"Record<string, number>"}},
		{"stream", `func F(ctx krate.Ctx) (<-chan int, error) { return nil, nil }`, "F", []string{"F(opts?: CallOptions): AsyncIterable<number>"}},
		{"stream no error", `func F() <-chan string { return nil }`, "F", []string{"AsyncIterable<string>"}},
		{"bidirectional chan", `func F() chan int { return nil }`, "F", []string{"AsyncIterable<number>"}},
		{"signal", `var S = krate.NewSignal([]string{})`, "S", []string{"export const S: ReadonlyHostSignal<string[], string[] | undefined>;"}},
		{"struct tags", `type T struct {
	A int ` + "`json:\"a\"`" + `
	B string ` + "`json:\"b,omitempty\"`" + `
	C string ` + "`json:\"-\"`" + `
	D string ` + "`json:\"-,\"`" + `
	e int
	F float64
}
func F(t T) {}`, "F", []string{"export interface T {\n  a: number;\n  b?: string;\n  \"-\": string;\n  F: number;\n}"}},
		{"embedded", `type B struct{ ID int; Name string }
type T struct {
	B
	Name string
}
func F(t T) {}`, "F", []string{"export interface T {\n  ID: number;\n  Name: string;\n}"}},
		{"embedded conflict dropped", `type A struct{ X int }
type B struct{ X int }
type T struct {
	A
	B
	Y int
}
func F(t T) {}`, "F", []string{"export interface T {\n  Y: number;\n}"}},
		{"embedded tag wins", `type A struct{ X int }
type B struct {
	X int ` + "`json:\"X\"`" + `
}
type T struct {
	A
	B
}
func F(t T) {}`, "F", []string{"export interface T {\n  X: number;\n}"}},
		{"embedded with tag is a field", `type B struct{ ID int }
type T struct {
	B ` + "`json:\"base\"`" + `
}
func F(t T) {}`, "F", []string{"base: B;", "export interface B {\n  ID: number;\n}"}},
		{"recursive", `type T struct{ Kids []T; Next *T }
func F(t T) {}`, "F", []string{"Kids: T[];", "Next: T | null;"}},
		{"recursive named slice", `type T []T
func F(t T) {}`, "F", []string{"export type T = T[];"}},
		{"duration", `func F(d time.Duration) {}`, "F", []string{"export type Duration = number;"}},
		{"anonymous struct", `func F(a struct{ X int; Y []string }) {}`, "F", []string{"a: { X: number; Y: string[] }"}},
		{"generic instance", `type Page[T any] struct{ Items []T }
func F() Page[string] { return Page[string]{} }`, "F", []string{"export interface Page_string {\n  Items: string[];\n}"}},
		{"reserved param", `func F(new string, opts int) {}`, "F", []string{"new_: string, opts_: number"}},
		{"unnamed param", `func F(_ int, _ string) {}`, "F", []string{"arg0: number, arg1: string"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, diags, _ := check(t, tt.src, tt.fn)
			if len(diags) > 0 || len(items) != 1 {
				t.Fatalf("diags = %v items = %d", diags, len(items))
			}
			out := dts(items)
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("d.ts lacks %q:\n%s", w, out)
				}
			}
		})
	}
}

func TestRejections(t *testing.T) {
	tests := []struct {
		name, src, fn string
		code          diag.Code
		at            string // text at the reported position
		msg           string
	}{
		{"any param", `func F(v any) {}`, "F", diag.CodeGoBindType, "v any", `F parameter "v" has type any`},
		{"interface param", `type I interface{ M() }
func F(v I) {}`, "F", diag.CodeGoBindType, "v I", "interface"},
		{"error in value", `func F() (error, error) { return nil, nil }`, "F", diag.CodeGoBindType, "error, error", "has type error"},
		{"any field", `type T struct{ Extra any }
func F(t T) {}`, "F", diag.CodeGoBindType, "Extra", "T.Extra has type any"},
		{"nested field path", `type In struct{ Fn func() }
type T struct{ Items []In }
func F(t T) {}`, "F", diag.CodeGoBindType, "Fn func", "T.Items[].Fn is a func"},
		{"chan field", `type T struct{ C chan int }
func F(t T) {}`, "F", diag.CodeGoBindType, "C chan", "is a channel"},
		{"complex", `func F(c complex128) {}`, "F", diag.CodeGoBindType, "c complex128", "complex128"},
		{"uintptr", `func F(c uintptr) {}`, "F", diag.CodeGoBindType, "c uintptr", "uintptr"},
		{"int map key", `func F(m map[int]string) {}`, "F", diag.CodeGoBindType, "m map", "only string keys"},
		{"embedded pointer", `type B struct{ X int }
type T struct{ *B }
func F(t T) {}`, "F", diag.CodeGoBindType, "B }", "embedded pointer"},
		{"unexported type", `type t struct{ X int }
func F(v t) {}`, "F", diag.CodeGoBindType, "v t", "unexported type t"},
		{"marshaler", `type M struct{ X int }
func (M) MarshalJSON() ([]byte, error) { return nil, nil }
func F(v M) {}`, "F", diag.CodeGoBindMarshaler, "v M", "MarshalJSON"},
		{"text marshaler on ptr", `type M int
func (*M) UnmarshalText([]byte) error { return nil }
func F(v M) {}`, "F", diag.CodeGoBindMarshaler, "v M", "UnmarshalText"},
		{"variadic", `func F(a ...int) {}`, "F", diag.CodeGoBindSignature, "a ...int", "variadic"},
		{"generic func", `func F[T any](a T) {}`, "F", diag.CodeGoBindSignature, "F[T", "generic"},
		{"context param", `func F(ctx context.Context) {}
type context_ int`, "F", diag.CodeGoBindSignature, "", ""},
		{"ctx not first", `func F(a int, ctx krate.Ctx) {}`, "F", diag.CodeGoBindSignature, "ctx krate", "first parameter"},
		{"three results", `func F() (int, int, error) { return 0, 0, nil }`, "F", diag.CodeGoBindSignature, "F()", "3 values"},
		{"second not error", `func F() (int, int) { return 0, 0 }`, "F", diag.CodeGoBindSignature, "int) {", "second result"},
		{"send-only chan", `func F() chan<- int { return nil }`, "F", diag.CodeGoBindSignature, "chan<-", "send-only"},
		{"stream any", `func F() <-chan any { return nil }`, "F", diag.CodeGoBindType, "<-chan any", "stream element"},
		{"signal any", `var S = krate.NewSignal[any](nil)`, "S", diag.CodeGoBindType, "S =", "S signal value"},
		{"not exported", `func f() {}`, "f", diag.CodeGoBindImport, "", "not an exported name"},
		{"missing", `func F() {}`, "G", diag.CodeGoBindImport, "", "not an exported name"},
		{"type import", `type T struct{}`, "T", diag.CodeGoBindImport, "", "is a type"},
		{"const import", `const C = 1`, "C", diag.CodeGoBindImport, "", "is a constant"},
		{"plain var import", `var V = 1`, "V", diag.CodeGoBindImport, "", "variable of type int"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "context param" {
				t.Skip("covered by TestContextParam")
			}
			items, diags, src := check(t, tt.src, tt.fn)
			if len(items) != 0 {
				t.Fatalf("expected no items, got %d", len(items))
			}
			if len(diags) != 1 {
				t.Fatalf("diags = %v", diags)
			}
			d := diags[0]
			if d.Code != tt.code {
				t.Errorf("code = %s, want %s (%s)", d.Code, tt.code, d.Message)
			}
			if !strings.Contains(d.Message, tt.msg) {
				t.Errorf("message %q lacks %q", d.Message, tt.msg)
			}
			if d.Hint == "" {
				t.Error("diagnostic has no hint")
			}
			if tt.at != "" {
				line, col := lineCol(src, tt.at)
				if d.File != "x.go" || d.Line != line || d.Col != col {
					t.Errorf("position = %s:%d:%d, want x.go:%d:%d", d.File, d.Line, d.Col, line, col)
				}
			}
		})
	}
}

func TestContextParam(t *testing.T) {
	fset, std := sharedFset, sharedStd
	src := "package p\nimport \"context\"\nfunc F(ctx context.Context) {}\n"
	f, _ := parser.ParseFile(fset, "/proj/x.go", src, 0)
	pkg, err := (&types.Config{Importer: std}).Check("example.com/app/p", fset, []*ast.File{f}, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := newAnalyzer("/proj", fset)
	m := &module{id: "x", src: "/proj/x.go", pkg: &userPkg{pkg: pkg}}
	a.classify(m, newMapper(a), "F", nil, func() token.Position { return token.Position{} })
	if len(a.diags) != 1 || a.diags[0].Code != diag.CodeGoBindSignature || !strings.Contains(a.diags[0].Message, "context.Context") {
		t.Fatalf("diags = %v", a.diags)
	}
	if a.diags[0].Line != 3 || a.diags[0].Col != 8 {
		t.Errorf("position = %d:%d", a.diags[0].Line, a.diags[0].Col)
	}
}

func TestComponentVersusHost(t *testing.T) {
	fset := token.NewFileSet()
	a := newAnalyzer("/proj", fset)
	m := &module{id: "x", src: "/proj/x.go"}
	a.classify(m, newMapper(a), "Card", map[string]bool{"Card": true}, func() token.Position { return token.Position{} })
	if len(a.diags) != 0 || len(m.items) != 0 || len(m.components) != 1 {
		t.Fatalf("component handling: diags=%v items=%v comps=%v", a.diags, m.items, m.components)
	}
}

func TestDiagnosticsOnRealPackage(t *testing.T) {
	root := newApp(t, "app")
	write := func(rel, body string) {
		t.Helper()
		if err := writeIfChanged(root+"/"+rel, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	write("src/bad/bad.go", "package bad\n\nfunc Bad(v any) {}\n\nfunc Good() int { return 1 }\n")
	write("src/broken/broken.go", "package broken\n\nfunc F() int { return \"x\" }\n")
	write("src/mainpkg/m.go", "package main\n\nfunc F() {}\n\nfunc main() {}\n")

	_, diags, err := Generate(root, []Import{{File: "src/bad/bad.go", Names: []string{"Bad", "Good"}, From: "src/ui.tsx", Line: 2, Col: 3}}, ".krate/native")
	if err != nil || len(diags) != 1 || diags[0].Code != diag.CodeGoBindType || diags[0].File != "src/bad/bad.go" || diags[0].Line != 3 || diags[0].Col != 10 {
		t.Fatalf("bad: %v %v", diags, err)
	}
	_, diags, _ = Generate(root, []Import{{File: "src/broken/broken.go", Names: []string{"F"}}}, ".krate/native")
	if len(diags) == 0 || diags[0].Code != diag.CodeGoBindLoad {
		t.Fatalf("broken: %v", diags)
	}
	_, diags, _ = Generate(root, []Import{{File: "src/mainpkg/m.go", Names: []string{"F"}}}, ".krate/native")
	if len(diags) == 0 || diags[0].Code != diag.CodeGoBindLoad || !strings.Contains(diags[0].Message, "package main") {
		t.Fatalf("main: %v", diags)
	}
	// Import errors point at the UI import when it is known.
	_, diags, _ = Generate(root, []Import{{File: "src/lib/tasks.go", Names: []string{"Nope"}, From: "src/ui.tsx", Line: 4, Col: 9}}, ".krate/native")
	if len(diags) != 1 || diags[0].Code != diag.CodeGoBindImport || diags[0].File != "src/ui.tsx" || diags[0].Line != 4 {
		t.Fatalf("import: %v", diags)
	}
	// A name declared in another file of the package.
	_, diags, _ = Generate(root, []Import{{File: "src/lib/tasks.go", Names: []string{"Status"}}}, ".krate/native")
	if len(diags) != 1 || !strings.Contains(diags[0].Message, "is declared in src/lib/model.go") {
		t.Fatalf("other file: %v", diags)
	}
	// Package directory form: names from any file, id is the directory.
	res, diags, err := Generate(root, []Import{{File: "src/lib", Names: []string{"Version"}}}, ".krate/native")
	if err != nil || len(diags) > 0 || res.Modules[0].ID != "src/lib" {
		t.Fatalf("dir import: %+v %v %v", res, diags, err)
	}
	// Module id collisions and bad paths.
	_, diags, _ = Generate(root, []Import{{File: "../outside.go", Names: []string{"X"}}}, ".krate/native")
	if len(diags) != 1 || diags[0].Code != diag.CodeGoBindModule {
		t.Fatalf("outside: %v", diags)
	}
	elsewhere := filepath.Join(filepath.VolumeName(root)+string(filepath.Separator), "elsewhere", "native")
	_, diags, _ = Generate(root, []Import{{File: "src/lib/tasks.go", Names: []string{"List"}}}, elsewhere)
	if len(diags) != 1 || diags[0].Code != diag.CodeGoBindModule {
		t.Fatalf("outside module output: %v", diags)
	}
}
