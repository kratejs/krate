// go-test-list prints, from source, the top-level tests go test -list . would
// print for the packages of the current module, without compiling them:
// TestXxx(*testing.T), FuzzXxx(*testing.F) and the examples with an output
// comment, in the test files go list selects for the current GOOS, GOARCH and
// build tags. It mirrors cmd/go's rules (cmd/go/internal/load/test.go).
//
// Usage, from a module directory:
//
//	go run go-test-list.go [-goos os] [-goarch arch] [-tags list] [packages]
//
// -goos and -goarch pick the target the way GOOS and GOARCH do for go test
// (the helper itself still builds for the host).
// It prints one JSON object: {"<import path>": ["TestA", ...], ...}.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type pkg struct {
	ImportPath   string
	Dir          string
	TestGoFiles  []string
	XTestGoFiles []string
	Error        *struct{ Err string }
}

func main() {
	tags := flag.String("tags", "", "build tags, as go test -tags")
	goos := flag.String("goos", "", "target GOOS (default: the environment's)")
	goarch := flag.String("goarch", "", "target GOARCH (default: the environment's)")
	flag.Parse()
	patterns := flag.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	args := []string{"list", "-e", "-json=ImportPath,Dir,TestGoFiles,XTestGoFiles,Error"}
	if *tags != "" {
		args = append(args, "-tags", *tags)
	}
	args = append(args, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Env = os.Environ()
	if *goos != "" {
		cmd.Env = append(cmd.Env, "GOOS="+*goos)
	}
	if *goarch != "" {
		cmd.Env = append(cmd.Env, "GOARCH="+*goarch)
	}
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		fail("go list: %v", err)
	}
	result := map[string][]string{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p pkg
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			fail("decoding go list: %v", err)
		}
		if p.Error != nil {
			fail("%s: %s", p.ImportPath, p.Error.Err)
		}
		names, err := testNames(p)
		if err != nil {
			fail("%s: %v", p.ImportPath, err)
		}
		if len(names) > 0 {
			result[p.ImportPath] = names
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	if err := enc.Encode(result); err != nil {
		fail("%v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "go-test-list: "+format+"\n", args...)
	os.Exit(1)
}

// testNames lists the tests of one package's in-package and external test
// files, each set sorted as one file list, the way go test builds them.
func testNames(p pkg) ([]string, error) {
	seen := map[string]bool{}
	var names []string
	for _, files := range [][]string{p.TestGoFiles, p.XTestGoFiles} {
		fset := token.NewFileSet()
		var parsed []*ast.File
		for _, f := range files {
			af, err := parser.ParseFile(fset, filepath.Join(p.Dir, f), nil, parser.ParseComments)
			if err != nil {
				return nil, err
			}
			parsed = append(parsed, af)
			for _, d := range af.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Recv != nil {
					continue
				}
				name := fn.Name.Name
				switch {
				case name == "TestMain" && isTestFunc(fn, "M"):
				case isTest(name, "Test") && isTestFunc(fn, "T"),
					isTest(name, "Fuzz") && isTestFunc(fn, "F"):
					if !seen[name] {
						seen[name] = true
						names = append(names, name)
					}
				}
			}
		}
		// doc.Examples finds the examples and their output comments as go
		// test does; one without an output comment is compiled, not run.
		for _, e := range doc.Examples(parsed...) {
			if e.Output == "" && !e.EmptyOutput {
				continue
			}
			name := "Example" + e.Name
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names, nil
}

// isTest is cmd/go's isTest: name is prefix, or prefix followed by a rune that
// is not lower case.
func isTest(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	if len(name) == len(prefix) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(name[len(prefix):])
	return !unicode.IsLower(r)
}

// isTestFunc is cmd/go's isTestFunc: one parameter of type *<pkg>.<arg> (or
// *<arg>), no type parameters. go test rejects other signatures with a
// build error, so the real listing fails where this one skips.
func isTestFunc(fn *ast.FuncDecl, arg string) bool {
	if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 ||
		fn.Type.Params.List == nil ||
		len(fn.Type.Params.List) != 1 ||
		len(fn.Type.Params.List[0].Names) > 1 {
		return false
	}
	if fn.Type.TypeParams != nil && len(fn.Type.TypeParams.List) > 0 {
		return false
	}
	ptr, ok := fn.Type.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	if name, ok := ptr.X.(*ast.Ident); ok && name.Name == arg {
		return true
	}
	if sel, ok := ptr.X.(*ast.SelectorExpr); ok && sel.Sel.Name == arg {
		return true
	}
	return false
}
