package gobind

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const buildGo = `package lib

import "github.com/kratejs/krate/packages/native/krate"

type Info struct {
	Name string ` + "`json:\"name\"`" + `
	N    int    ` + "`json:\"n\"`" + `
}

// Edition is evaluated during the build.
//
//krate:build
func Edition(name string, n int) Info { return Info{Name: "Krate " + name, N: n * 2} }

//krate:build
func Fails() (string, error) { return "", krate.Errorf("nope", "cannot") }

func Runtime() string { return "r" }
`

func TestBuildFunctionsEvaluate(t *testing.T) {
	root := newApp(t, "app")
	dir := filepath.Join(root, "src", "bk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bk.go"), []byte(buildGo), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, ".krate", "native")
	res, ds, err := Generate(root, []Import{{File: "src/bk/bk.go", Names: []string{"Edition", "Fails", "Runtime"}}}, out)
	if err != nil || len(ds) > 0 {
		t.Fatalf("%v %+v", err, ds)
	}
	m := res.Modules[0]
	if strings.Join(m.Build, ",") != "Edition,Fails" {
		t.Fatalf("build = %v", m.Build)
	}
	stub, _ := os.ReadFile(m.Stub)
	if !strings.Contains(string(stub), `__baked("src/bk/bk.Edition", [a0, a1])`) || !strings.Contains(string(stub), `__call("src/bk/bk.Runtime"`) {
		t.Fatalf("stub:\n%s", stub)
	}
	dts, _ := os.ReadFile(m.DTS)
	if !strings.Contains(string(dts), "export function Edition(name: string, n: number): Info;") {
		t.Fatalf("dts:\n%s", dts)
	}
	rs, err := Evaluate(root, out, res, []BakeCall{
		{ID: "src/bk/bk.Edition", Args: `["x",21]`},
		{ID: "src/bk/bk.Fails", Args: `[]`},
		{ID: "src/bk/bk.Edition", Args: `["x"]`},
	})
	if be, ok := err.(*BakeError); ok {
		t.Fatalf("%s\n%s", be.Msg, be.Output)
	}
	if err != nil {
		t.Fatal(err)
	}
	if string(rs[0].OK) != `{"name":"Krate x","n":42}` {
		t.Fatalf("%+v", rs[0])
	}
	if rs[1].Code != "nope" || rs[1].Err != "cannot" {
		t.Fatalf("%+v", rs[1])
	}
	if rs[2].Err == "" {
		t.Fatalf("missing argument must fail: %+v", rs[2])
	}
}

func TestBuildFunctionPlacementErrors(t *testing.T) {
	root := newApp(t, "app")
	dir := filepath.Join(root, "src", "bk")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "bk.go"), []byte("package lib\n\n//krate:build\nfunc Nothing() {}\n\n//krate:build\nfunc Ch() <-chan int { return nil }\n"), 0o644)
	_, ds, err := Generate(root, []Import{{File: "src/bk/bk.go", Names: []string{"Nothing", "Ch"}}}, filepath.Join(root, ".krate", "native"))
	if err != nil || len(ds) != 2 || ds[0].Code != "KR7244" {
		t.Fatalf("%v %+v", err, ds)
	}
}
