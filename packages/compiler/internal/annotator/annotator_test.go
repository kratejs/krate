package annotator

import (
	"testing"

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

func entryTier(t *testing.T, ann *irtree.Annotations) irtree.ComponentTier {
	t.Helper()
	return ann.ComponentTiers[ann.EntryPoint]
}

// ─── Tier classification priority ───────────────────────────────────────────

func TestClassifyDirectivePriority(t *testing.T) {
	src := `// @server
export default function App() { return <div>hi</div>; }`
	ann := Annotate(parseProg(t, src), &config.Config{}, "whatever.runtime.tsx", src)
	if got := entryTier(t, ann); got != irtree.TierServer {
		t.Errorf("directive must outrank file convention; got %v", got)
	}
}

func TestClassifyFileConvention(t *testing.T) {
	ann := Annotate(parseProg(t, `export default function App() { return <div>hi</div>; }`), &config.Config{}, "x.runtime.tsx", "")
	if got := entryTier(t, ann); got != irtree.TierRuntime {
		t.Errorf("expected runtime from .runtime.tsx, got %v", got)
	}
	ann = Annotate(parseProg(t, `export default function App() { return <div>hi</div>; }`), &config.Config{}, "x.server.ts", "")
	if got := entryTier(t, ann); got != irtree.TierServer {
		t.Errorf("expected server from .server.ts, got %v", got)
	}
	ann = Annotate(parseProg(t, `export default function App() { return <div>hi</div>; }`), &config.Config{}, "x.static.tsx", "")
	if got := entryTier(t, ann); got != irtree.TierStatic {
		t.Errorf("expected static from .static.tsx, got %v", got)
	}
}

func TestClassifyConfigDirs(t *testing.T) {
	ann := Annotate(parseProg(t, `export default function App() { return <div>hi</div>; }`), &config.Config{
		ServerDirs: []string{"src/components/server"},
	}, "src/components/server/Table.tsx", "")
	if got := entryTier(t, ann); got != irtree.TierServer {
		t.Errorf("expected server from dir membership, got %v", got)
	}
}

func TestClassifyDefaultClient(t *testing.T) {
	ann := Annotate(parseProg(t, `export default function App() { return <div>hi</div>; }`), &config.Config{}, "App.tsx", "")
	if got := entryTier(t, ann); got != irtree.TierClient {
		t.Errorf("expected default client, got %v", got)
	}
}

func TestClassifyComponentVarArrow(t *testing.T) {
	src := `const Button = (props) => <button>{props.label}</button>;
export default function App() { return <Button label="x" />; }`
	ann := Annotate(parseProg(t, src), &config.Config{}, "App.tsx", src)
	if _, ok := ann.Functions["Button"]; !ok {
		t.Fatal("expected arrow-function component to be collected")
	}
	if ann.ComponentTiers["Button"] != irtree.TierClient {
		t.Errorf("expected Button client tier, got %v", ann.ComponentTiers["Button"])
	}
}

// ─── Per-module source classification ───────────────────────────────────────

func TestImportedRuntimeComponentUsesOwnFileConvention(t *testing.T) {
	// The page is @server, but the imported component lives in a *.runtime.tsx
	// file. Its tier must come from ITS OWN file, not the page directive.
	pageSrc := `// @server
import RuntimeWidget from '../ui/RuntimeWidget.runtime';
export default function Page() {
  return <div><RuntimeWidget label="x" /></div>;
}`
	pageProg := parseProg(t, pageSrc)
	ann := Annotate(pageProg, &config.Config{}, "src/pages/demo.tsx", pageSrc)

	runtimeSrc := `export default function RuntimeWidget(props: { label: string }) {
  return <div>{props.label}</div>;
}`
	MergeModuleFunctions(ann, []ModuleSource{{
		Program:   parseProg(t, runtimeSrc),
		Path:      "src/components/ui/RuntimeWidget.runtime.tsx",
		RawSource: runtimeSrc,
	}})
	ReclassifyTiers(ann, &config.Config{})

	if got := ann.ComponentTiers["Page"]; got != irtree.TierServer {
		t.Errorf("page should be server (own directive), got %v", got)
	}
	if got := ann.ComponentTiers["RuntimeWidget"]; got != irtree.TierRuntime {
		t.Errorf("imported component should be runtime from its own file, got %v", got)
	}
}

func TestImportedServerComponentUsesOwnFile(t *testing.T) {
	pageSrc := `export default function Page() {
  return <div><ServerTime /></div>;
}`
	pageProg := parseProg(t, pageSrc)
	ann := Annotate(pageProg, &config.Config{}, "src/pages/demo.tsx", pageSrc)

	serverSrc := `// @server
export default function ServerTime() { return <div>t</div>; }`
	MergeModuleFunctions(ann, []ModuleSource{{
		Program:   parseProg(t, serverSrc),
		Path:      "src/components/ui/ServerTime.server.tsx",
		RawSource: serverSrc,
	}})
	ReclassifyTiers(ann, &config.Config{})

	if got := ann.ComponentTiers["ServerTime"]; got != irtree.TierServer {
		t.Errorf("expected server tier for .server.tsx import, got %v", got)
	}
}

func TestMergeModuleFunctionsCollectsSignals(t *testing.T) {
	pageSrc := `export default function Page() {
  return <div><Counter /></div>;
}`
	pageProg := parseProg(t, pageSrc)
	ann := Annotate(pageProg, &config.Config{}, "src/pages/demo.tsx", pageSrc)

	compSrc := `export default function Counter() {
  const [count, setCount] = createSignal(0);
  return <button>{count()}</button>;
}`
	MergeModuleFunctions(ann, []ModuleSource{{
		Program:   parseProg(t, compSrc),
		Path:      "src/components/Counter.tsx",
		RawSource: compSrc,
	}})

	if _, ok := ann.Functions["Counter"]; !ok {
		t.Fatal("expected Counter function merged into annotations")
	}
	if !ann.UsedComponents["Counter"] {
		t.Error("expected Counter marked as used (reachable from entry)")
	}
	if len(ann.Signals) == 0 {
		t.Error("expected signals detected from imported component")
	}
}

// ─── Streaming / Suspense detection ─────────────────────────────────────────

func TestDetectSuspenseUsage(t *testing.T) {
	src := `import { Suspense } from '@krate/runtime/server';
export default function Page() {
  return <Suspense fallback={<div>loading</div>}><Slow /></Suspense>;
}`
	ann := Annotate(parseProg(t, src), &config.Config{}, "src/pages/demo.tsx", src)
	if !ann.HasSuspense {
		t.Error("expected <Suspense> to be detected")
	}
}

func TestDetectStreamingConfigVariants(t *testing.T) {
	cases := []struct{ src string }{
		{`export const config = { streaming: true }; export default function A(){return <div/>}`},
		{`const config = { streaming: true }; export default function A(){return <div/>}`},
	}
	for _, c := range cases {
		ann := Annotate(parseProg(t, c.src), &config.Config{}, "p.tsx", c.src)
		if !ann.HasStreaming {
			t.Errorf("expected streaming detection for %q", c.src)
		}
	}
}

func TestSignalDetectionAcrossNestedScopes(t *testing.T) {
	src := `export default function App() {
  if (true) {
    const [a, setA] = createSignal(1);
  }
  {
    const [b, setB] = createSignal("x");
  }
  return <div>{a}{b}</div>;
}`
	ann := Annotate(parseProg(t, src), &config.Config{}, "p.tsx", src)
	for _, name := range []string{"a", "b"} {
		if _, ok := ann.Signals[name]; !ok {
			t.Errorf("expected signal %q detected in nested scope", name)
		}
	}
}

func TestEntryPointDetection(t *testing.T) {
	src := `export default function Home() { return <div/>; }
function Other() { return <div/>; }`
	ann := Annotate(parseProg(t, src), &config.Config{}, "p.tsx", src)
	if ann.EntryPoint != "Home" {
		t.Errorf("expected entry 'Home', got %q", ann.EntryPoint)
	}
	if !ann.UsedComponents["Home"] {
		t.Error("expected entry marked used")
	}
	if ann.UsedComponents["Other"] {
		t.Error("unused component should not be marked used")
	}
}
