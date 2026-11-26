package irtree

import (
	"sync"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func parseDeterminismProg(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(src).Tokenize())
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}

func TestTypedModuleConstsDeterministic(t *testing.T) {
	t.Parallel()
	src := `
const alpha = (() => { const ignored = beta; return 10; })();
const beta = alpha + 5;
const gamma = beta * 2;
`
	prog := parseDeterminismProg(t, src)
	inits := collectModuleConstInits(prog)

	for i := 0; i < 200; i++ {
		got := typedModuleConsts(inits, nil, nil)
		if v, ok := got["alpha"]; !ok || v.ToNumber() != 10 {
			t.Fatalf("iter %d: alpha = (%v, %v), want 10", i, v, ok)
		}
		if v, ok := got["beta"]; !ok || v.ToNumber() != 15 {
			t.Fatalf("iter %d: beta = (%v, %v), want 15 (poisoned by map order)", i, v, ok)
		}
		if v, ok := got["gamma"]; !ok || v.ToNumber() != 30 {
			t.Fatalf("iter %d: gamma = (%v, %v), want 30", i, v, ok)
		}
	}
}

func TestFoldLocalConstsDeterministicChainedHelpers(t *testing.T) {
	t.Parallel()
	src := `
function formatSlug(raw) {
  return raw ? "/" + raw : "/";
}
function buildNavGraph(entries, active) {
  const out = [];
  for (const e of entries) {
    out.push({ label: e.label, active: e.href === active });
  }
  return out;
}
function NavList(props) {
  return <ul>{props.graph.map((n) => <li>{n.label}</li>)}</ul>;
}
function DrawerPanel(props) {
  const toggle = createCSSToggle(false);
  onMount(() => {});
  return (
    <div>
      <button {...toggle.trigger()}>Open</button>
      <div {...toggle.panel(true)}>
        <NavList graph={props.graph} />
      </div>
    </div>
  );
}
function HandbookShell(props) {
  const opts = props.options || {};
  const entries = opts.entries || [];
  const norm = formatSlug(props.rawSlug);
  const navGraph = buildNavGraph(entries, norm);
  return (
    <section>
      <DrawerPanel graph={navGraph} />
    </section>
  );
}
export default function Page() {
  const cfg = {
    rawSlug: "intro",
    options: { entries: [{ label: "Intro", href: "/intro" }, { label: "Guide", href: "/guide" }] },
  };
  return <HandbookShell {...cfg} />;
}
`
	prog := parseDeterminismProg(t, src)
	fns := map[string]*ast.FnDecl{}
	for _, stmt := range prog.Body {
		switch s := stmt.(type) {
		case *ast.FnDecl:
			fns[s.Name] = s
		case *ast.ExportStmt:
			if fn, ok := s.Declaration.(*ast.FnDecl); ok {
				fns[fn.Name] = fn
			}
		}
	}
	ann := &Annotations{
		EntryPoint: "Page",
		Functions:  fns,
		ComponentTiers: map[string]ComponentTier{
			"Page":          TierClient,
			"HandbookShell": TierClient,
			"DrawerPanel":   TierClient,
			"NavList":       TierClient,
		},
		UsedComponents: map[string]bool{
			"Page":          true,
			"HandbookShell": true,
			"DrawerPanel":   true,
			"NavList":       true,
		},
		ComponentSources: map[string]string{},
		Signals:          map[string]ast.Expr{},
	}

	var findShell func(slots []SlotNode) *ComponentNode
	findShell = func(slots []SlotNode) *ComponentNode {
		for _, s := range slots {
			cs, ok := s.(*ComponentSlot)
			if !ok || cs.Component == nil {
				continue
			}
			if cs.Component.Name == "HandbookShell" {
				return cs.Component
			}
			if got := findShell(cs.Component.Children); got != nil {
				return got
			}
		}
		return nil
	}

	for i := 0; i < 100; i++ {
		tree := Build(prog, ann)
		shell := findShell(tree.Root.Children)
		if shell == nil {
			t.Fatalf("iter %d: HandbookShell not found", i)
		}
		v, ok := shell.LocalVars["navGraph"]
		if !ok || v.ContainsUnknown() {
			t.Fatalf("iter %d: HandbookShell.LocalVars[navGraph] = (%v, %v), want folded array", i, v, ok)
		}
	}
}

func TestLocalShadowsModuleConst(t *testing.T) {
	t.Parallel()
	src := `
const navGraph = "module-fallback";
function Leaf(props) {
  return <span>{props.val}</span>;
}
export default function Page() {
  const navGraph = ["one", "two"];
  return <Leaf val={navGraph.join(",")} />;
}
`
	prog := parseDeterminismProg(t, src)
	fns := map[string]*ast.FnDecl{}
	for _, stmt := range prog.Body {
		switch s := stmt.(type) {
		case *ast.FnDecl:
			fns[s.Name] = s
		case *ast.ExportStmt:
			if fn, ok := s.Declaration.(*ast.FnDecl); ok {
				fns[fn.Name] = fn
			}
		}
	}
	ann := &Annotations{
		EntryPoint: "Page",
		Functions:  fns,
		ComponentTiers: map[string]ComponentTier{
			"Page": TierClient,
			"Leaf": TierClient,
		},
		UsedComponents:   map[string]bool{"Page": true, "Leaf": true},
		ComponentSources: map[string]string{},
		Signals:          map[string]ast.Expr{},
	}
	tree := Build(prog, ann)
	var leaf *ComponentNode
	for _, s := range tree.Root.Children {
		if cs, ok := s.(*ComponentSlot); ok && cs.Component != nil && cs.Component.Name == "Leaf" {
			leaf = cs.Component
		}
	}
	if leaf == nil {
		t.Fatal("Leaf component slot not found")
	}
	got := leaf.SSREvalBindings["val"]
	if got.StringValue() != "one,two" {
		t.Fatalf("Leaf val = %q (kind %v), want \"one,two\" (module const shadowed local)", got.StringValue(), got.Kind())
	}
}

func TestBridgeIslandExprDoesNotMutateSharedAST(t *testing.T) {
	t.Parallel()
	sharedSrc := `
export function Wrapper(props) {
  const label = props.label;
  return <ExternalCard title={label} />;
}
`
	sharedProg := parseDeterminismProg(t, sharedSrc)
	var wrapperFn *ast.FnDecl
	for _, stmt := range sharedProg.Body {
		if exp, ok := stmt.(*ast.ExportStmt); ok {
			if fn, ok := exp.Declaration.(*ast.FnDecl); ok {
				wrapperFn = fn
			}
		}
	}
	if wrapperFn == nil {
		t.Fatal("Wrapper not found")
	}

	buildPage := func(label string) string {
		pageSrc := `export default function Page() { return <Wrapper label="` + label + `" />; }`
		pageProg := parseDeterminismProg(t, pageSrc)
		var pageFn *ast.FnDecl
		for _, stmt := range pageProg.Body {
			if exp, ok := stmt.(*ast.ExportStmt); ok {
				if fn, ok := exp.Declaration.(*ast.FnDecl); ok {
					pageFn = fn
				}
			}
		}
		ann := &Annotations{
			EntryPoint: "Page",
			Functions:  map[string]*ast.FnDecl{"Page": pageFn, "Wrapper": wrapperFn},
			ComponentTiers: map[string]ComponentTier{
				"Page":    TierClient,
				"Wrapper": TierClient,
			},
			UsedComponents:   map[string]bool{"Page": true, "Wrapper": true},
			ComponentSources: map[string]string{"Page": "page.tsx", "Wrapper": "wrapper.tsx"},
			Signals:          map[string]ast.Expr{},
		}
		tree := BuildWithOptions(pageProg, ann, BuildOptions{
			ExternalImports: map[string]ExternalImport{
				"ExternalCard": {Local: "ExternalCard", Source: "external-ui"},
			},
		})
		if len(tree.ExternalComponents) != 1 {
			t.Fatalf("expected 1 external component, got %d", len(tree.ExternalComponents))
		}
		return tree.ExternalComponents[0].Subtree
	}

	first := buildPage("Alpha")
	second := buildPage("Beta")
	if first == second {
		t.Fatalf("second page reused mutated AST from first page: both got %q", first)
	}

	// Concurrent builds sharing wrapperFn must not race.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			lbl := "First"
			if n%2 == 1 {
				lbl = "Second"
			}
			sub := buildPage(lbl)
			if sub == "" {
				t.Errorf("empty subtree for %s", lbl)
			}
		}(i)
	}
	wg.Wait()
}

func TestTypedModuleConstsKeepFieldsAroundJSX(t *testing.T) {
	t.Parallel()
	prog := parseDeterminismProg(t, `
export const items = [
  { id: "a", name: "Alpha", icon: <svg viewBox="0 0 1 1" /> },
  { id: "b", name: "Beta", icon: <svg viewBox="0 0 1 1" /> },
];
export const lone = <b>x</b>;
`)
	got := typedModuleConsts(collectModuleConstInits(prog), nil, nil)
	items, ok := got["items"]
	if !ok || !items.IsArray() || len(items.Elems()) != 2 {
		t.Fatalf("items = (%v, %v), want a two element array", items, ok)
	}
	name, _ := items.Elems()[1].Get("name")
	if name.ToString() != "Beta" {
		t.Errorf("items[1].name = %q, want Beta", name.ToString())
	}
	icon, _ := items.Elems()[1].Get("icon")
	if _, isSrc := icon.JSXSource(); !isSrc {
		t.Errorf("items[1].icon should keep its JSX source, got %v", icon)
	}
	if _, kept := got["lone"]; kept {
		t.Error("a constant that is only JSX stays out of the value table")
	}
}
