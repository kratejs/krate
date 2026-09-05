package irtree_test

import (
	"strings"
	"testing"

	"krate-compiler/internal/annotator"
	"krate-compiler/internal/ast"
	"krate-compiler/internal/config"
	"krate-compiler/internal/irtree"
)

func annotateWith(prog *ast.Program, cfg *config.Config, path, raw string) *irtree.Annotations {
	return annotator.Annotate(prog, cfg, path, raw)
}

func configWithRuntime(names ...string) *config.Config {
	return &config.Config{RuntimeComponents: names}
}

func configWithServer(names ...string) *config.Config {
	return &config.Config{ServerComponents: names}
}

// ─── Fragment children ───────────────────────────────────────────────────────

func TestBuildFragmentFlattensMixedChildren(t *testing.T) {
	tree := annotateAndBuild(t, `export default function App() {
	return <><span>one</span>{` + "`two`" + `}<span>three</span></>;
}`)
	if len(tree.Root.Children) < 3 {
		t.Fatalf("expected >=3 children from flattened fragment, got %d", len(tree.Root.Children))
	}
	var textCount, elementCount int
	for _, child := range tree.Root.Children {
		if sh, ok := child.(*irtree.StaticHTML); ok {
			switch {
			case strings.Contains(sh.HTML, "two"):
				textCount++
			case strings.Contains(sh.HTML, "one") || strings.Contains(sh.HTML, "three"):
				elementCount++
			}
		}
	}
	if textCount == 0 {
		t.Error("expected template-literal text slot in fragment")
	}
	if elementCount == 0 {
		t.Error("expected element children in fragment")
	}
}

func TestBuildFragmentEscapesTextExpression(t *testing.T) {
	tree := annotateAndBuild(t, "export default function App() {\n\treturn <>{`a < b`}</>;\n}")
	if len(tree.Root.Children) == 0 {
		t.Fatal("expected children")
	}
	sh, ok := tree.Root.Children[0].(*irtree.StaticHTML)
	if !ok {
		t.Fatalf("expected StaticHTML, got %T", tree.Root.Children[0])
	}
	if !strings.Contains(sh.HTML, "a &lt; b") {
		t.Errorf("expected escaped text 'a &lt; b', got %q", sh.HTML)
	}
}

// ─── Keyed list slots ────────────────────────────────────────────────────────

func TestBuildKeyedListWithLiteralArray(t *testing.T) {
	tree := annotateAndBuild(t, `export default function App() {
	return <ul>{[1,2,3].map(i => <li key={i}>{i}</li>)}</ul>;
}`)
	var list *irtree.ListSlot
	for _, child := range tree.Root.Children {
		if ls, ok := child.(*irtree.ListSlot); ok {
			list = ls
			break
		}
	}
	if list == nil {
		t.Fatal("expected ListSlot for .map() call")
	}
	if len(list.Items) != 3 {
		t.Fatalf("expected 3 resolved items, got %d", len(list.Items))
	}
	// Current implementation resolves literal-array items synchronously and
	// falls back to index-based keys (data-derived key reconciliation is not
	// yet implemented).
	if list.Items[0].Key != "0" || list.Items[1].Key != "1" || list.Items[2].Key != "2" {
		t.Errorf("expected index keys, got %q,%q,%q",
			list.Items[0].Key, list.Items[1].Key, list.Items[2].Key)
	}
	for i, item := range list.Items {
		if len(item.Contents) != 1 {
			t.Errorf("item %d: expected 1 content slot, got %d", i, len(item.Contents))
		}
	}
}

func TestBuildListWithoutKeyFallsBackToIndex(t *testing.T) {
	tree := annotateAndBuild(t, `export default function App() {
	return <ul>{["a","b"].map(x => <li>{x}</li>)}</ul>;
}`)
	var list *irtree.ListSlot
	for _, child := range tree.Root.Children {
		if ls, ok := child.(*irtree.ListSlot); ok {
			list = ls
			break
		}
	}
	if list == nil {
		t.Fatal("expected ListSlot")
	}
	if len(list.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(list.Items))
	}
	if list.Items[0].Key != "0" || list.Items[1].Key != "1" {
		t.Errorf("expected index fallback keys, got %q,%q", list.Items[0].Key, list.Items[1].Key)
	}
}

// ─── Nested components ───────────────────────────────────────────────────────

func TestBuildNestedComponentSlot(t *testing.T) {
	src := `function Header() { return <h1>hi</h1>; }
export default function App() { return <div><Header /></div>; }`
	tree := annotateAndBuild(t, src)
	var comp *irtree.ComponentSlot
	for _, child := range tree.Root.Children {
		if cs, ok := child.(*irtree.ComponentSlot); ok {
			comp = cs
			break
		}
	}
	if comp == nil {
		t.Fatal("expected ComponentSlot for nested <Header />")
	}
	if comp.Component == nil {
		t.Fatal("expected non-nil Component on slot")
	}
	if comp.Component.Name != "Header" {
		t.Errorf("expected Header component, got %q", comp.Component.Name)
	}
	if !comp.Component.IsSSREval {
		t.Error("expected signal-less nested component to be SSR-evaluated")
	}
}

func TestBuildClientNestedComponentSlot(t *testing.T) {
	src := `function Counter() {
	const [c, setC] = createSignal(0);
	return <button onClick={() => setC(c + 1)}>{c()}</button>;
}
export default function App() { return <div><Counter /></div>; }`
	tree := annotateAndBuild(t, src)
	var comp *irtree.ComponentSlot
	for _, child := range tree.Root.Children {
		if cs, ok := child.(*irtree.ComponentSlot); ok {
			comp = cs
			break
		}
	}
	if comp == nil {
		t.Fatal("expected ComponentSlot for <Counter />")
	}
	if comp.Component.Tier != irtree.TierClient {
		t.Errorf("expected client tier, got %v", comp.Component.Tier)
	}
	if len(comp.Component.Signals) == 0 {
		t.Error("expected signal declarations on client component")
	}
}

// ─── Runtime / server tier components ────────────────────────────────────────

func TestBuildRuntimeComponentSlotIsPlaceholder(t *testing.T) {
	src := `function RuntimeWidget(props) { return <div>{props.label}</div>; }
export default function App() { return <div><RuntimeWidget label="hi" /></div>; }`
	prog := parseProg(t, src)
	cfg := configWithRuntime("RuntimeWidget")
	ann := annotateWith(prog, cfg, "page.tsx", src)
	tree := irtree.Build(prog, ann)
	var comp *irtree.ComponentSlot
	for _, child := range tree.Root.Children {
		if cs, ok := child.(*irtree.ComponentSlot); ok {
			comp = cs
			break
		}
	}
	if comp == nil {
		t.Fatal("expected ComponentSlot for runtime widget")
	}
	if comp.Component.Tier != irtree.TierRuntime {
		t.Errorf("expected runtime tier, got %v", comp.Component.Tier)
	}
	if comp.Component.IsSSREval {
		t.Error("runtime components must not be SSR-evaluated at build time")
	}
	if comp.Component.RuntimeProps == nil {
		t.Fatal("expected resolved RuntimeProps on runtime component")
	}
	if comp.Component.RuntimeProps["label"] != "hi" {
		t.Errorf("expected prop label='hi', got %v", comp.Component.RuntimeProps["label"])
	}
}

func TestBuildServerComponentNoSignals(t *testing.T) {
	src := `function ServerWidget(props) { return <div>{props.title}</div>; }
export default function App() { return <div><ServerWidget title="t" /></div>; }`
	prog := parseProg(t, src)
	cfg := configWithServer("ServerWidget")
	ann := annotateWith(prog, cfg, "page.tsx", src)
	tree := irtree.Build(prog, ann)
	var comp *irtree.ComponentSlot
	for _, child := range tree.Root.Children {
		if cs, ok := child.(*irtree.ComponentSlot); ok {
			comp = cs
			break
		}
	}
	if comp == nil || comp.Component == nil {
		t.Fatal("expected ComponentSlot for server widget")
	}
	if !comp.Component.IsSSREval {
		t.Error("expected server component to be SSR-evaluated at build time")
	}
	if comp.Component.Tier == irtree.TierRuntime {
		t.Error("server component must not be routed to the runtime placeholder path")
	}
}

// ─── Text escaping in static value slots ────────────────────────────────────

func TestBuildEscapesTemplateLiteralInTextPosition(t *testing.T) {
	// Mirrors the <Code>{`function App() { return <h1>Hello</h1>; }`}</Code> case:
	// a template literal rendered as JSX text must be HTML-escaped.
	tree := annotateAndBuild(t, "export default function App() {\n\treturn <code>{`return <h1>Hello</h1>;`}</code>;\n}")
	var found bool
	for _, child := range tree.Root.Children {
		if sh, ok := child.(*irtree.StaticHTML); ok {
			if strings.Contains(sh.HTML, "&lt;h1&gt;") {
				found = true
			}
			if strings.Contains(sh.HTML, "<h1>Hello") {
				t.Error("template literal leaked raw <h1> into static HTML")
			}
		}
	}
	if !found {
		t.Error("expected escaped template literal (&lt;h1&gt;) in static HTML")
	}
}

func TestBuildEscapesStringBinaryConcat(t *testing.T) {
	tree := annotateAndBuild(t, "export default function App() {\n\treturn <p>{`a ` + `&` + ` b`}</p>;\n}")
	var found bool
	for _, child := range tree.Root.Children {
		if sh, ok := child.(*irtree.StaticHTML); ok {
			if strings.Contains(sh.HTML, "a &amp; b") {
				found = true
			}
			if strings.Contains(sh.HTML, "a & b") {
				t.Errorf("binary string concat leaked raw & into static HTML: %q", sh.HTML)
			}
		}
	}
	if !found {
		t.Error("expected escaped binary string concat in static HTML")
	}
}

func TestBuildHandlerIdentifierReferences(t *testing.T) {
	// onClick={fn} should work the same as onClick={() => fn()} for local
	// function declarations, const arrow functions, and module-level functions.
	tests := []struct {
		name       string
		src        string
		wantBody   string
		wantVar    string // expected extra var hoisting the function into scope
	}{
		{
			name: "local function declaration",
			src: `function Counter() {
  const [c, setC] = createSignal(0);
  function toggle() { setC(c() + 1); }
  return <button onClick={toggle}>{c()}</button>;
}
export default function App() { return <div><Counter/></div>; }`,
			wantBody: "function(){setC((c() + 1));}",
		},
		{
			name: "local const arrow",
			src: `function Counter() {
  const [c, setC] = createSignal(0);
  const toggle = () => setC(c() + 1);
  return <button onClick={toggle}>{c()}</button>;
}
export default function App() { return <div><Counter/></div>; }`,
			wantBody: "()=>setC((c() + 1))",
		},
		{
			name: "module-level function",
			src: `function toggle() { return 1; }
function Counter() {
  const [c, setC] = createSignal(0);
  return <button onClick={toggle}>{c()}</button>;
}
export default function App() { return <div><Counter/></div>; }`,
			wantBody: "toggle",
			wantVar:  "function toggle(){return 1;}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := annotateAndBuild(t, tt.src)
			var comp *irtree.ComponentSlot
			for _, child := range tree.Root.Children {
				if cs, ok := child.(*irtree.ComponentSlot); ok {
					comp = cs
					break
				}
			}
			if comp == nil {
				t.Fatal("expected ComponentSlot")
			}
			if len(comp.Component.Handlers) != 1 {
				t.Fatalf("expected 1 handler, got %d", len(comp.Component.Handlers))
			}
			if comp.Component.Handlers[0].Event != "click" {
				t.Errorf("expected click handler, got %q", comp.Component.Handlers[0].Event)
			}
			if comp.Component.Handlers[0].Body != tt.wantBody {
				t.Errorf("handler body = %q, want %q", comp.Component.Handlers[0].Body, tt.wantBody)
			}
			if tt.wantVar != "" {
				found := false
				for _, ev := range comp.Component.ExtraVars {
					if ev == tt.wantVar {
						found = true
					}
				}
				if !found {
					t.Errorf("expected extra var %q, got %v", tt.wantVar, comp.Component.ExtraVars)
				}
			}
		})
	}
}

func TestBuildMetaSlotKeepsScriptRaw(t *testing.T) {
	// Inline <Script>{`var x = a < b;`}</Script> must stay raw (not escaped).
	tree := annotateAndBuild(t, "export default function App() {\n\treturn <Script>{`var x = a < b;`}</Script>;\n}")
	var found bool
	for _, child := range tree.Root.Children {
		if ms, ok := child.(*irtree.MetaSlot); ok {
			for _, c := range ms.Children {
				if sh, ok := c.(*irtree.StaticHTML); ok && strings.Contains(sh.HTML, "a < b") {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("expected raw (unescaped) script content in MetaSlot")
	}
}

// ─── <SyntaxHighlight> compile-time chroma through {children} ────────────────

func findStaticHTML(children []irtree.SlotNode, pred func(string) bool) bool {
	for _, child := range children {
		switch n := child.(type) {
		case *irtree.StaticHTML:
			if pred(n.HTML) {
				return true
			}
		case *irtree.ComponentSlot:
			if n.Component != nil {
				if findStaticHTML(n.Component.Children, pred) ||
					findStaticHTML(n.Component.ReturnSlots, pred) {
					return true
				}
			}
		case *irtree.ConditionalSlot:
			if findStaticHTML(n.Consequent, pred) || findStaticHTML(n.Alternate, pred) {
				return true
			}
		}
	}
	return false
}

func TestSyntaxHighlightHighlightsCallSiteChildren(t *testing.T) {
	// Mirrors <Code lang="tsx">{`...code...`}</Code>: a client component
	// (signal + handler) wrapping <SyntaxHighlight>{children}</SyntaxHighlight>
	// must run the Go chroma highlighter over the call-site code at build time.
	src := `function Code(props) {
	const [copied, setCopied] = createSignal(false);
	return (
		<div>
			<button onClick={() => setCopied(true)}>{copied() ? "y" : "n"}</button>
			<SyntaxHighlight lang={props.lang}>{props.children}</SyntaxHighlight>
		</div>
	);
}
export default function Page() {
	return <Code lang="tsx">{` + "`function App() { return <h1>Hi</h1>; }`" + `}</Code>;
}`
	tree := annotateAndBuild(t, src)
	found := findStaticHTML(tree.Root.Children, func(html string) bool {
		return strings.Contains(html, "language-tsx") && strings.Contains(html, "<span")
	})
	if !found {
		t.Error("expected chroma-highlighted HTML for static call-site children of <SyntaxHighlight>")
	}
}

// ─── useRef object refs ──────────────────────────────────────────────────────

func TestRefBindingUseRefObjectAssignsCurrent(t *testing.T) {
	src := `export default function App() {
	const inputRef = { current: null };
	return <input ref={inputRef} />;
}`
	tree := annotateAndBuild(t, src)
	refs := tree.Root.RefBindings
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref binding, got %d: %+v", len(refs), refs)
	}
	if refs[0].Target != "inputRef.current" {
		t.Errorf("expected target inputRef.current for ref={inputRef}, got %q", refs[0].Target)
	}
}

func TestRefBindingPlainVarAssignsVariable(t *testing.T) {
	src := `export default function App() {
	let el;
	return <input ref={el} />;
}`
	tree := annotateAndBuild(t, src)
	refs := tree.Root.RefBindings
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref binding, got %d: %+v", len(refs), refs)
	}
	if refs[0].Target != "el" {
		t.Errorf("expected target el for plain var ref, got %q", refs[0].Target)
	}
}

func TestRefBindingUseRefCallAssignsCurrent(t *testing.T) {
	src := `export default function App() {
	const inputRef = useRef(null);
	return <input ref={inputRef} />;
}`
	tree := annotateAndBuild(t, src)
	refs := tree.Root.RefBindings
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref binding, got %d: %+v", len(refs), refs)
	}
	if refs[0].Target != "inputRef.current" {
		t.Errorf("expected target inputRef.current for ref={inputRef} from useRef(null), got %q", refs[0].Target)
	}
}
