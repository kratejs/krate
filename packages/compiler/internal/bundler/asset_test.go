package bundler

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"krate-compiler/internal/ast"
)

// TestAssetImportRewrite verifies that importing a non-code file registers it
// in AssetFiles and rewrites bare references to the imported binding into the
// hashed /assets/… URL literal inside the AST.
func TestAssetImportRewrite(t *testing.T) {
	tmp := t.TempDir()
	pagesDir := filepath.Join(tmp, "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	imgPath := filepath.Join(pagesDir, "logo.png")
	if err := os.WriteFile(imgPath, []byte("fake-png-bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	pagePath := filepath.Join(pagesDir, "asset-page.tsx")
	pageSrc := `
		import logo from './logo.png';
		export default function AssetPage() {
			return (
				<div>
					<img src={logo} alt={logo + '-alt'} />
				</div>
			);
		}
	`
	if err := os.WriteFile(pagePath, []byte(pageSrc), 0644); err != nil {
		t.Fatal(err)
	}

	b := New(tmp)
	bundle, err := b.Bundle(pagePath)
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}

	// The asset must be registered in AssetFiles.
	imgAbs := filepath.Clean(imgPath)
	url, ok := bundle.AssetFiles[imgAbs]
	if !ok {
		t.Fatalf("expected asset %s in AssetFiles, got %v", imgAbs, bundle.AssetFiles)
	}
	re := regexp.MustCompile(`^/assets/logo-[a-z0-9]{6}\.png$`)
	if !re.MatchString(url) {
		t.Fatalf("asset URL %q does not match expected pattern", url)
	}

	// The page module's AST must reference the URL literal, not the binding.
	if len(bundle.Modules) == 0 {
		t.Fatal("no modules in bundle")
	}
	var pageMod *Module
	for _, m := range bundle.Modules {
		if filepath.Clean(m.Path) == imgAbs {
			continue
		}
		pageMod = m
	}
	if pageMod == nil {
		t.Fatal("page module not found in bundle")
	}

	refs := []string{}
	collectIdentRefs(pageMod.Program.Body, &refs)
	for _, ref := range refs {
		if ref == "logo" {
			t.Fatalf("bare reference to asset binding 'logo' survived rewrite")
		}
	}

	foundLiteral := false
	collectStringLits(pageMod.Program.Body, &foundLiteral, url)
	if !foundLiteral {
		t.Fatalf("expected rewritten literal %q in page AST", url)
	}
}

func collectIdentRefs(stmts []ast.Stmt, out *[]string) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.ExportStmt:
			if s.Declaration != nil {
				if fn, ok := s.Declaration.(*ast.FnDecl); ok {
					for _, st := range fn.Body {
						switch bst := st.(type) {
						case *ast.ReturnStmt:
							walkExprForIdents(bst.Value, out)
						case *ast.ExprStmt:
							walkExprForIdents(bst.Expression, out)
						}
					}
				}
			}
		}
	}
}

func walkExprForIdents(expr ast.Expr, out *[]string) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		*out = append(*out, e.Name)
	case *ast.BinaryExpr:
		walkExprForIdents(e.Left, out)
		walkExprForIdents(e.Right, out)
	case *ast.JSXElement:
		if e.Opening != nil {
			for _, attr := range e.Opening.Attributes {
				walkExprForIdents(attr.Value, out)
			}
		}
		for _, child := range e.Children {
			walkChildForIdents(child, out)
		}
	case *ast.Literal:
	}
}

func walkChildForIdents(child ast.JSXChild, out *[]string) {
	switch c := child.(type) {
	case *ast.JSXExprContainer:
		walkExprForIdents(c.Expression, out)
	case *ast.JSXElementChild:
		walkExprForIdents(c.Element, out)
	case *ast.JSXFragmentChild:
		walkExprForIdents(c.Fragment, out)
	}
}

func collectStringLits(stmts []ast.Stmt, out *bool, want string) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.ExportStmt:
			if s.Declaration != nil {
				if fn, ok := s.Declaration.(*ast.FnDecl); ok {
					for _, st := range fn.Body {
						switch bst := st.(type) {
						case *ast.ReturnStmt:
							walkForStringLit(bst.Value, out, want)
						case *ast.ExprStmt:
							walkForStringLit(bst.Expression, out, want)
						}
					}
				}
			}
		}
	}
}

func walkForStringLit(expr ast.Expr, out *bool, want string) {
	if expr == nil || *out {
		return
	}
	switch e := expr.(type) {
	case *ast.Literal:
		if e.Kind == ast.StringLit && e.Value == want {
			*out = true
		}
	case *ast.BinaryExpr:
		walkForStringLit(e.Left, out, want)
		walkForStringLit(e.Right, out, want)
	case *ast.JSXElement:
		if e.Opening != nil {
			for _, attr := range e.Opening.Attributes {
				walkForStringLit(attr.Value, out, want)
			}
		}
		for _, child := range e.Children {
			walkChildForStringLit(child, out, want)
		}
	}
}

func walkChildForStringLit(child ast.JSXChild, out *bool, want string) {
	switch c := child.(type) {
	case *ast.JSXExprContainer:
		walkForStringLit(c.Expression, out, want)
	case *ast.JSXElementChild:
		walkForStringLit(c.Element, out, want)
	case *ast.JSXFragmentChild:
		walkForStringLit(c.Fragment, out, want)
	}
}