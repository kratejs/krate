package tsstrip

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// collectPositions returns the position of every node reachable from prog, in
// a deterministic walk order.
func collectPositions(prog *ast.Program) []ast.Pos {
	var out []ast.Pos
	seen := map[uintptr]bool{}
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() || seen[v.Pointer()] {
				return
			}
			seen[v.Pointer()] = true
			if v.Elem().Kind() == reflect.Struct {
				if f := v.Elem().FieldByName("Position"); f.IsValid() && f.Type() == posType {
					out = append(out, f.Interface().(ast.Pos))
				}
			}
			walk(v.Elem())
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i))
				}
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		}
	}
	walk(reflect.ValueOf(prog))
	return out
}

// TestCorpusPositions parses the repo's own .tsx sources both directly and
// through Strip. Where the direct parse is lossless (no type syntax), every
// node must land on the same original position: diagnostics, Go component
// call ids and source rewrites are keyed by it.
func TestCorpusPositions(t *testing.T) {
	repo := filepath.Join("..", "..", "..", "..")
	roots := []string{
		filepath.Join(repo, "examples", "src"),
		filepath.Join(repo, "packages", "components", "src"),
		filepath.Join(repo, "packages", "web", "src"),
		filepath.Join(repo, "packages", "base-docs-theme"),
		filepath.Join(repo, "packages", "create-krate-app", "templates"),
	}
	checked, stripped := 0, 0
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if d.Name() == "node_modules" || d.Name() == "dist" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".tsx") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			src := string(data)
			got, errs := Parse(path, src, Options{})
			if len(errs) > 0 {
				t.Errorf("%s: %v", path, errs)
				return nil
			}
			stripped++
			p := parser.New(lexer.New(src).Tokenize())
			direct := p.ParseProgram()
			if len(p.Errors()) > 0 || p.DroppedTypes() > 0 {
				return nil
			}
			a, b := collectPositions(direct), collectPositions(got)
			if len(a) != len(b) {
				t.Errorf("%s: %d vs %d nodes", path, len(a), len(b))
				return nil
			}
			for i := range a {
				if a[i] != b[i] {
					t.Errorf("%s: node %d at %+v, want %+v", path, i, b[i], a[i])
					break
				}
			}
			checked++
			return nil
		})
	}
	if stripped == 0 {
		t.Skip("no corpus files found")
	}
	t.Logf("stripped %d files, compared positions in %d", stripped, checked)
}
