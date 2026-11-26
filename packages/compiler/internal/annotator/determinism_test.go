package annotator

import (
	"sync"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestLowerContextUsesDoesNotMutateSharedAST(t *testing.T) {
	t.Parallel()
	ctxSrc := `export const ThemeCtx = createContext("light");`
	compSrc := `
import { ThemeCtx } from "./ctx";
export function Banner() {
  const mode = __krateUse(() => __krateRead(ThemeCtx));
  return <div>{mode()}</div>;
}
`
	pageSrc := `
import { Banner } from "./banner";
export default function Page() {
  return <Banner />;
}
`
	ctxProg := parseProg(t, ctxSrc)
	compProg := parseProg(t, compSrc)
	pageProg := parseProg(t, pageSrc)

	// Record the original Init pointer on the shared component AST.
	var origDecl *ast.VarDecl
	for _, stmt := range compProg.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok {
			continue
		}
		fn, ok := exp.Declaration.(*ast.FnDecl)
		if !ok || fn.Name != "Banner" {
			continue
		}
		for _, bs := range fn.Body {
			if vs, ok := bs.(*ast.VarStmt); ok && len(vs.Decls) > 0 {
				origDecl = vs.Decls[0]
			}
		}
	}
	if origDecl == nil {
		t.Fatal("failed to locate shared VarDecl in Banner")
	}
	origInit := origDecl.Init

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ann := Annotate(pageProg, &config.Config{}, "src/pages/index.tsx", pageSrc)
			extra := []ModuleSource{
				{Program: ctxProg, Path: "src/ctx.tsx", RawSource: ctxSrc},
				{Program: compProg, Path: "src/banner.tsx", RawSource: compSrc},
			}
			MergeModuleFunctions(ann, extra)
			MergeImportAliases(ann, extra, ModuleSource{Program: pageProg, Path: "src/pages/index.tsx", RawSource: pageSrc})

			fn := ann.Functions["Banner"]
			if fn == nil {
				t.Errorf("Banner missing from ann.Functions")
				return
			}
			var lowered ast.Expr
			for _, bs := range fn.Body {
				if vs, ok := bs.(*ast.VarStmt); ok && len(vs.Decls) > 0 {
					lowered = vs.Decls[0].Init
				}
			}
			call, ok := lowered.(*ast.CallExpr)
			if !ok {
				t.Errorf("expected lowered CallExpr, got %T", lowered)
				return
			}
			id, ok := call.Callee.(*ast.Identifier)
			if !ok || id.Name != "createMemo" {
				t.Errorf("expected createMemo callee, got %#v", call.Callee)
			}
		}()
	}
	wg.Wait()

	if origDecl.Init != origInit {
		t.Fatalf("lowerContextUses mutated shared *ast.Program VarDecl.Init in place")
	}
}
