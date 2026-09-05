package bundler

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"krate-compiler/internal/ast"
)

// TestWorkerRewrite verifies `new Worker('./x.ts')` and the
// `new Worker(new URL(...), { type: 'module' })` form register the target in
// WorkerFiles and rewrite the worker argument to its hashed /workers/… URL.
func TestWorkerRewrite(t *testing.T) {
	tmp := t.TempDir()
	pagesDir := filepath.Join(tmp, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	workerPath := filepath.Join(pagesDir, "worker.ts")
	if err := os.WriteFile(workerPath, []byte("self.onmessage = () => {};"), 0644); err != nil {
		t.Fatal(err)
	}
	worker2Path := filepath.Join(pagesDir, "loader.js")
	if err := os.WriteFile(worker2Path, []byte("console.log('loader');"), 0644); err != nil {
		t.Fatal(err)
	}

	page := `
		const w1 = new Worker('./worker.ts', { type: 'module' });
		const w2 = new Worker(new URL('./loader.js', import.meta.url), { name: 'load' });
		export default function Page() {
			return <img src="/plain" alt={w1 ? 'x' : 'y'} />;
		}
	`
	pagePath := filepath.Join(pagesDir, "index.tsx")
	if err := os.WriteFile(pagePath, []byte(page), 0644); err != nil {
		t.Fatal(err)
	}

	b := New(tmp)
	bundle, err := b.Bundle(pagePath)
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}

	if len(bundle.WorkerFiles) != 2 {
		t.Fatalf("expected 2 workers, got %v", bundle.WorkerFiles)
	}
	url1 := bundle.WorkerFiles[filepath.Clean(workerPath)]
	var re1 = regexp.MustCompile(`^/workers/worker-[a-z0-9]{6}\.js$`)
	if !re1.MatchString(url1) {
		t.Fatalf("worker URL %q does not match pattern", url1)
	}
	url2 := bundle.WorkerFiles[filepath.Clean(worker2Path)]
	var re2 = regexp.MustCompile(`^/workers/loader-[a-z0-9]{6}\.js$`)
	if !re2.MatchString(url2) {
		t.Fatalf("worker URL %q does not match pattern", url2)
	}

	if !bundle.WorkerEsm[filepath.Clean(workerPath)] {
		t.Fatal("module-style worker not flagged in WorkerEsm")
	}
	if bundle.WorkerEsm[filepath.Clean(worker2Path)] {
		t.Fatal("classic worker incorrectly flagged as ES module")
	}

	// The rewritten literals must appear in the page AST.
	found := map[string]bool{}
	collectWorkerURLs(t, bundle, pagePath, found)
	if !found[url1] {
		t.Fatalf("page AST missing worker URL %q (found %v)", url1, found)
	}
	if !found[url2] {
		t.Fatalf("page AST missing worker URL %q (found %v)", url2, found)
	}
}

// collectWorkerURLs gathers every string literal in the module's AST.
func collectWorkerURLs(t *testing.T, bundle *Bundle, path string, out map[string]bool) {
	t.Helper()
	for _, m := range bundle.Modules {
		if filepath.Clean(m.Path) != filepath.Clean(path) {
			continue
		}
		for _, stmt := range m.Program.Body {
			collectStmtLiterals(stmt, out)
		}
		return
	}
	t.Fatal("module not found")
}

func collectStmtLiterals(stmt ast.Stmt, out map[string]bool) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.VarStmt:
		for _, d := range s.Decls {
			collectExprLiterals(d.Init, out)
		}
	case *ast.ExportStmt:
		if s.Declaration != nil {
			collectStmtLiterals(s.Declaration, out)
		}
	case *ast.FnDecl:
		for _, body := range s.Body {
			collectStmtLiterals(body, out)
		}
	case *ast.ReturnStmt:
		collectExprLiterals(s.Value, out)
	case *ast.ExprStmt:
		collectExprLiterals(s.Expression, out)
	}
}

func collectExprLiterals(expr ast.Expr, out map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Literal:
		if e.Kind == ast.StringLit {
			out[e.Value] = true
		}
	case *ast.NewExpr:
		collectExprLiterals(e.Callee, out)
		for _, a := range e.Args {
			collectExprLiterals(a, out)
		}
	case *ast.CallExpr:
		collectExprLiterals(e.Callee, out)
		for _, a := range e.Args {
			collectExprLiterals(a, out)
		}
	case *ast.BinaryExpr:
		collectExprLiterals(e.Left, out)
		collectExprLiterals(e.Right, out)
	case *ast.ObjectExpr:
		for _, p := range e.Properties {
			collectExprLiterals(p.Value, out)
		}
	case *ast.JSXElement:
		if e.Opening != nil {
			for _, a := range e.Opening.Attributes {
				collectExprLiterals(a.Value, out)
			}
		}
	}
}