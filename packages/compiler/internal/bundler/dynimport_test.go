package bundler

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// TestDynamicImportRewrite verifies that `import('./widget.ts')` resolves and
// registers the target in DynImportFiles, and rewrites the import argument to
// its hashed /chunks/... URL so the built page fetches the bundled module.
func TestDynamicImportRewrite(t *testing.T) {
	tmp := t.TempDir()
	pagesDir := filepath.Join(tmp, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	widgetPath := filepath.Join(pagesDir, "widget.ts")
	if err := os.WriteFile(widgetPath, []byte("export function hello() { return 'hi'; }"), 0644); err != nil {
		t.Fatal(err)
	}
	lazyPath := filepath.Join(pagesDir, "lazy.tsx")
	if err := os.WriteFile(lazyPath, []byte("export default function Lazy() { return <div>lazy</div>; }"), 0644); err != nil {
		t.Fatal(err)
	}

	page := `
		async function loadWidget() {
			const mod = await import('./widget.ts');
			return mod.hello();
		}
		export default function Page() {
			return <div onClick={() => import('./lazy.tsx').then(m => m.default)}>go</div>;
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

	if len(bundle.DynImportFiles) != 2 {
		t.Fatalf("expected 2 dynamic-import chunks, got %v", bundle.DynImportFiles)
	}
	urlWidget := bundle.DynImportFiles[filepath.Clean(widgetPath)]
	var reWidget = regexp.MustCompile(`^/chunks/widget-[a-z0-9]{6}\.js$`)
	if !reWidget.MatchString(urlWidget) {
		t.Fatalf("widget chunk URL %q does not match pattern", urlWidget)
	}
	urlLazy := bundle.DynImportFiles[filepath.Clean(lazyPath)]
	var reLazy = regexp.MustCompile(`^/chunks/lazy-[a-z0-9]{6}\.js$`)
	if !reLazy.MatchString(urlLazy) {
		t.Fatalf("lazy chunk URL %q does not match pattern", urlLazy)
	}

	// The rewritten literals must appear in the page AST.
	found := map[string]bool{}
	for _, m := range bundle.Modules {
		for _, stmt := range m.Program.Body {
			collectDynImportLiterals(stmt, found)
		}
	}
	if !found[urlWidget] {
		t.Fatalf("page AST missing widget chunk URL %q (found %v)", urlWidget, found)
	}
	if !found[urlLazy] {
		t.Fatalf("page AST missing lazy chunk URL %q (found %v)", urlLazy, found)
	}
}

// TestDynamicImportUnresolvableLeftUntouched verifies that imports which don't
// resolve to a source file in the project are left as-is (no split attempt, no
// error).
func TestDynamicImportUnresolvableLeftUntouched(t *testing.T) {
	tmp := t.TempDir()
	pagesDir := filepath.Join(tmp, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	page := `
		export default function Page() {
			return <div onMount={() => import('some-remote-module')}>go</div>;
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
	if len(bundle.DynImportFiles) != 0 {
		t.Fatalf("expected no dynamic-import chunks for unresolvable specifier, got %v", bundle.DynImportFiles)
	}
}

func collectDynImportLiterals(stmt ast.Stmt, out map[string]bool) {
	switch s := stmt.(type) {
	case *ast.VarStmt:
		for _, d := range s.Decls {
			collectDynImportExprLiterals(d.Init, out)
		}
	case *ast.ExportStmt:
		if s.Declaration != nil {
			collectDynImportLiterals(s.Declaration, out)
		}
	case *ast.FnDecl:
		for _, body := range s.Body {
			collectDynImportLiterals(body, out)
		}
	case *ast.ReturnStmt:
		collectDynImportExprLiterals(s.Value, out)
	case *ast.ExprStmt:
		collectDynImportExprLiterals(s.Expression, out)
	}
}

func collectDynImportExprLiterals(expr ast.Expr, out map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Literal:
		if e.Kind == ast.StringLit {
			out[e.Value] = true
		}
	case *ast.DynamicImport:
		collectDynImportExprLiterals(e.Arg, out)
	case *ast.CallExpr:
		collectDynImportExprLiterals(e.Callee, out)
		for _, a := range e.Args {
			collectDynImportExprLiterals(a, out)
		}
	case *ast.AwaitExpr:
		collectDynImportExprLiterals(e.Arg, out)
	case *ast.ArrowFn:
		for _, s := range e.Body {
			collectDynImportLiterals(s, out)
		}
	case *ast.BinaryExpr:
		collectDynImportExprLiterals(e.Left, out)
		collectDynImportExprLiterals(e.Right, out)
	case *ast.MemberExpr:
		collectDynImportExprLiterals(e.Object, out)
		if e.Computed {
			collectDynImportExprLiterals(e.Property, out)
		}
	case *ast.JSXElement:
		if e.Opening != nil {
			for _, a := range e.Opening.Attributes {
				collectDynImportExprLiterals(a.Value, out)
			}
		}
	}
}
