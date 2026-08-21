package irtree_test

import (
	"strings"
	"testing"

	"krate-compiler/internal/annotator"
	"krate-compiler/internal/ast"
	"krate-compiler/internal/config"
	"krate-compiler/internal/irtree"
	"krate-compiler/internal/lexer"
	"krate-compiler/internal/parser"
)

func parseProg(t *testing.T, src string) *ast.Program {
	t.Helper()
	l := lexer.New(src)
	tokens := l.Tokenize()
	p := parser.New(tokens)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}

func annotateAndBuild(t *testing.T, src string) *irtree.ComponentTree {
	t.Helper()
	prog := parseProg(t, src)
	ann := annotator.Annotate(prog, &config.Config{}, "test.tsx", src)
	return irtree.Build(prog, ann)
}

func getEntryTier(t *testing.T, ann *irtree.Annotations) irtree.ComponentTier {
	t.Helper()
	if fn, ok := ann.Functions[ann.EntryPoint]; ok {
		_ = fn
	}
	// Get the tier from ComponentTiers for the entry point
	if tier, ok := ann.ComponentTiers[ann.EntryPoint]; ok {
		return tier
	}
	return irtree.TierUnknown
}

// ─── Annotator Tests ───────────────────────────────────────────────────────

func TestAnnotateDefaultIsClient(t *testing.T) {
	prog := parseProg(t, `export default function App() { return <div>hi</div>; }`)
	ann := annotator.Annotate(prog, &config.Config{}, "page.tsx", "")
	tier := getEntryTier(t, ann)
	if tier != irtree.TierClient {
		t.Errorf("expected client tier, got %v", tier)
	}
}

func TestAnnotateServerDirective(t *testing.T) {
	src := `// @server
export default function App() { return <div>hi</div>; }`
	prog := parseProg(t, src)
	ann := annotator.Annotate(prog, &config.Config{}, "page.tsx", src)
	tier := getEntryTier(t, ann)
	if tier != irtree.TierServer {
		t.Errorf("expected server tier, got %v", tier)
	}
}

func TestAnnotateStaticDirective(t *testing.T) {
	src := `// @static
export default function App() { return <div>hi</div>; }`
	prog := parseProg(t, src)
	ann := annotator.Annotate(prog, &config.Config{}, "page.tsx", src)
	tier := getEntryTier(t, ann)
	if tier != irtree.TierStatic {
		t.Errorf("expected static tier, got %v", tier)
	}
}

func TestAnnotateRuntimeDirective(t *testing.T) {
	src := `// @runtime
export default function App() { return <div>hi</div>; }`
	prog := parseProg(t, src)
	ann := annotator.Annotate(prog, &config.Config{}, "page.tsx", src)
	tier := getEntryTier(t, ann)
	if tier != irtree.TierRuntime {
		t.Errorf("expected runtime tier, got %v", tier)
	}
}

func TestAnnotateFileConvention(t *testing.T) {
	prog := parseProg(t, `export default function App() { return <div>hi</div>; }`)
	ann := annotator.Annotate(prog, &config.Config{}, "app.server.tsx", "")
	tier := getEntryTier(t, ann)
	if tier != irtree.TierServer {
		t.Errorf("expected server tier from .server.tsx, got %v", tier)
	}
}

func TestAnnotateConfigNameList(t *testing.T) {
	prog := parseProg(t, `export default function DataTable() { return <div>hi</div>; }`)
	cfg := &config.Config{
		ServerComponents: []string{"DataTable"},
	}
	ann := annotator.Annotate(prog, cfg, "page.tsx", "")
	tier := getEntryTier(t, ann)
	if tier != irtree.TierServer {
		t.Errorf("expected server tier from config, got %v", tier)
	}
}

func TestAnnotateDetectsSignals(t *testing.T) {
	prog := parseProg(t, `export default function App() {
	const [count, setCount] = createSignal(0);
	return <div>{count()}</div>;
}`)
	ann := annotator.Annotate(prog, &config.Config{}, "page.tsx", "")
	if len(ann.Signals) == 0 {
		t.Error("expected signals to be detected")
	}
	found := false
	for name := range ann.Signals {
		if name == "count" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected signal 'count' in signals map")
	}
}

func TestAnnotateDetectsStreaming(t *testing.T) {
	prog := parseProg(t, `export const config = { streaming: true };
export default function App() { return <div>hi</div>; }`)
	ann := annotator.Annotate(prog, &config.Config{}, "page.tsx", "")
	if !ann.HasStreaming {
		t.Error("expected streaming to be detected")
	}
}

// ─── IR Tree Builder Tests ─────────────────────────────────────────────────

func TestBuildSimpleElement(t *testing.T) {
	tree := annotateAndBuild(t, `export default function App() { return <div>hello</div>; }`)
	if len(tree.Root.Children) == 0 {
		t.Fatal("expected children in root")
	}
	slot, ok := tree.Root.Children[0].(*irtree.StaticHTML)
	if !ok {
		t.Fatalf("expected StaticHTML, got %T", tree.Root.Children[0])
	}
	if !strings.Contains(slot.HTML, "hello") {
		t.Errorf("expected 'hello' in static HTML, got %q", slot.HTML)
	}
}

func TestBuildTextContent(t *testing.T) {
	tree := annotateAndBuild(t, `export default function App() {
	const [name, setName] = createSignal("world");
	return <div>{name()}</div>;
}`)
	if len(tree.Root.Children) == 0 {
		t.Fatal("expected children")
	}
	// Dynamic children are now separate SlotNodes, not embedded in StaticHTML.
	// The div produces: StaticHTML(opening) + TextSlot + StaticHTML(closing)
	static, ok := tree.Root.Children[0].(*irtree.StaticHTML)
	if !ok {
		t.Fatalf("expected StaticHTML, got %T", tree.Root.Children[0])
	}
	if !strings.Contains(static.HTML, "<div>") {
		t.Errorf("expected opening div tag, got %q", static.HTML)
	}
	// The TextSlot should be a separate child
	if len(tree.Root.Children) < 3 {
		t.Fatalf("expected at least 3 children (open, dynamic, close), got %d", len(tree.Root.Children))
	}
	_, isText := tree.Root.Children[1].(*irtree.TextSlot)
	if !isText {
		t.Errorf("expected TextSlot at position 1, got %T", tree.Root.Children[1])
	}
	// Should also have signal declarations
	if len(tree.Root.Signals) == 0 {
		t.Error("expected signal declarations on root")
	}
}

func TestBuildConditional(t *testing.T) {
	tree := annotateAndBuild(t, `export default function App() {
	const [show, setShow] = createSignal(false);
	return <div>{show() ? <span>yes</span> : <span>no</span>}</div>;
}`)
	if len(tree.Root.Children) == 0 {
		t.Fatal("expected children")
	}
	// Dynamic conditional is now a separate SlotNode
	static, ok := tree.Root.Children[0].(*irtree.StaticHTML)
	if !ok {
		t.Fatalf("expected StaticHTML, got %T", tree.Root.Children[0])
	}
	if !strings.Contains(static.HTML, "<div>") {
		t.Errorf("expected opening div tag, got %q", static.HTML)
	}
	// Conditional with a signal-backed test stays reactive as a
	// ConditionalSlot with both branches built (render-both). The test is
	// not baked statically so toggling show() swaps the visible branch.
	foundCond := false
	for _, child := range tree.Root.Children {
		if cond, ok := child.(*irtree.ConditionalSlot); ok {
			foundCond = true
			if cond.TestJS == "" {
				t.Error("expected TestJS on conditional slot")
			}
			foundYes := false
			for _, c := range cond.Consequent {
				if sh, ok := c.(*irtree.StaticHTML); ok && strings.Contains(sh.HTML, "<span>yes</span>") {
					foundYes = true
					break
				}
			}
			if !foundYes {
				t.Error("expected consequent branch <span>yes</span> in conditional slot")
			}
			foundNo := false
			for _, c := range cond.Alternate {
				if sh, ok := c.(*irtree.StaticHTML); ok && strings.Contains(sh.HTML, "<span>no</span>") {
					foundNo = true
					break
				}
			}
			if !foundNo {
				t.Error("expected alternate branch <span>no</span> in conditional slot")
			}
			break
		}
	}
	if !foundCond {
		t.Error("expected ConditionalSlot for signal-backed ternary")
	}
}

func TestBuildList(t *testing.T) {
	tree := annotateAndBuild(t, `export default function App() {
	const [items, setItems] = createSignal(["a","b","c"]);
	return <ul>{items().map(item => <li>{item}</li>)}</ul>;
}`)
	if len(tree.Root.Children) == 0 {
		t.Fatal("expected children")
	}
	// Dynamic list is now a separate SlotNode
	static, ok := tree.Root.Children[0].(*irtree.StaticHTML)
	if !ok {
		t.Fatalf("expected StaticHTML, got %T", tree.Root.Children[0])
	}
	if !strings.Contains(static.HTML, "<ul>") {
		t.Errorf("expected opening ul tag, got %q", static.HTML)
	}
	// Should have a ListSlot as a separate child
	found := false
	for _, child := range tree.Root.Children {
		if _, ok := child.(*irtree.ListSlot); ok {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected ListSlot among children")
	}
}

func TestBuildSignalDecls(t *testing.T) {
	tree := annotateAndBuild(t, `export default function App() {
	const [count, setCount] = createSignal(0);
	return <div>{count()}</div>;
}`)
	if len(tree.Root.Signals) == 0 {
		t.Error("expected signal declarations")
	}
	if tree.Root.Signals[0].Name != "count" {
		t.Errorf("expected signal name 'count', got %q", tree.Root.Signals[0].Name)
	}
}

func TestBuildNestedElement(t *testing.T) {
	tree := annotateAndBuild(t, `export default function App() {
	return <div><span>inner</span></div>;
}`)
	if len(tree.Root.Children) == 0 {
		t.Fatal("expected children")
	}
	outer, ok := tree.Root.Children[0].(*irtree.StaticHTML)
	if !ok {
		t.Fatalf("expected StaticHTML, got %T", tree.Root.Children[0])
	}
	if !strings.Contains(outer.HTML, "inner") {
		t.Errorf("expected 'inner' in nested HTML, got %q", outer.HTML)
	}
}

func TestBuildFragment(t *testing.T) {
	tree := annotateAndBuild(t, `export default function App() {
	return <><span>one</span><span>two</span></>;
}`)
	if len(tree.Root.Children) < 2 {
		t.Errorf("expected at least 2 children from fragment, got %d", len(tree.Root.Children))
	}
}

func TestBuildStaticTier(t *testing.T) {
	tree := annotateAndBuild(t, `// @static
export default function App() { return <div>static content</div>; }`)
	if tree.Root.Tier != irtree.TierStatic {
		t.Errorf("expected static tier, got %v", tree.Root.Tier)
	}
}

func TestBuildDoesNotPanic(t *testing.T) {
	prog := parseProg(t, `export default function App() { return <div>hello</div>; }`)
	ann := annotator.Annotate(prog, &config.Config{}, "page.tsx", "")
	tree := irtree.Build(prog, ann)
	if tree == nil {
		t.Fatal("tree is nil")
	}
}
