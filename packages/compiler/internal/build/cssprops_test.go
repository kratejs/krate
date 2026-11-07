package build

import (
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const cssPropsImport = "import { createCSSChoice, createSignal } from '@krate/runtime';\n"

func cssPropsPage(parts string) map[string]string {
	return map[string]string{"src/pages/index.tsx": cssPropsImport + parts}
}

// A CSS scope handle passed as a prop works at any depth, to components with
// and without client state. The receiving components only use the helpers, so
// the handle has no run-time value and none is registered for hydration.
func TestCSSScopeHandlePassedAsProp(t *testing.T) {
	const leaf = `function Leaf(props) { return <div><button {...props.scope.trigger('b')}>B</button><div {...props.scope.panel('b')}>pb</div></div>; }` + "\n"
	cases := map[string]string{
		"hydrating child": `function Child(props) { const [c, setC] = createSignal(0); return <div><button class="count" onClick={() => setC(c() + 1)}>{c()}</button><button {...props.scope.trigger('b')}>B</button><div {...props.scope.panel('b')}>pb</div></div>; }
export default function P() { const tab = createCSSChoice('a', ['a', 'b']); return <main><Child scope={tab} /></main>; }`,
		"destructured": `function Child({ scope }) { const [c, setC] = createSignal(0); return <div><button class="count" onClick={() => setC(c() + 1)}>{c()}</button><button {...scope.trigger('b')}>B</button><div {...scope.panel('b')}>pb</div></div>; }
export default function P() { const tab = createCSSChoice('a', ['a', 'b']); return <main><Child scope={tab} /></main>; }`,
		"renamed": `function Child({ scope: s }) { const [c, setC] = createSignal(0); return <div><button class="count" onClick={() => setC(c() + 1)}>{c()}</button><button {...s.trigger('b')}>B</button><div {...s.panel('b')}>pb</div></div>; }
export default function P() { const tab = createCSSChoice('a', ['a', 'b']); return <main><Child scope={tab} /></main>; }`,
		"no client state": leaf + `export default function P() { const tab = createCSSChoice('a', ['a', 'b']); return <main><Leaf scope={tab} /></main>; }`,
		"two levels":      leaf + `function Mid(props) { return <section><Leaf scope={props.scope} /></section>; }` + "\n" + `export default function P() { const tab = createCSSChoice('a', ['a', 'b']); return <main><Mid scope={tab} /></main>; }`,
		"spread through":  leaf + `function Mid(props) { return <section><Leaf {...props} /></section>; }` + "\n" + `export default function P() { const tab = createCSSChoice('a', ['a', 'b']); return <main><Mid scope={tab} /></main>; }`,
		"three levels":    `function Leaf({ scope }) { return <div><button {...scope.trigger('b')}>B</button><div {...scope.panel('b')}>pb</div></div>; }` + "\n" + `function Mid({ scope }) { return <section><Leaf scope={scope} /></section>; }` + "\n" + `function Top({ s }) { return <Mid scope={s} />; }` + "\n" + `export default function P() { const tab = createCSSChoice('a', ['a', 'b']); return <main><Top s={tab} /></main>; }`,
		"triggers in a list": `function Leaf(props) { return <ul>{props.items.map((i) => <li {...props.scope.trigger('b')}>{i}</li>)}</ul>; }
export default function P() { const tab = createCSSChoice('a', ['a', 'b']); return <main><Leaf scope={tab} items={['x', 'y']} /></main>; }`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, out, logged := buildFiles(t, cssPropsPage(src), func(c *config.Config) { c.Tailwind.Enabled = false; c.Tailwind.Strict = false })
			htmlBytes, err := os.ReadFile(filepath.Join(out, "index.html"))
			if err != nil {
				t.Fatal(err)
			}
			html := string(htmlBytes)
			if !regexp.MustCompile(`<label class="?krc0-t-b"? for="?p-c0-r0-b`).MatchString(html) {
				t.Errorf("the trigger was not compiled to a label:\n%s\nlog:\n%s", rootOf(t, html), logged)
			}
			js, _ := filepath.Glob(filepath.Join(out, "index.*.js"))
			for _, f := range js {
				b, _ := os.ReadFile(f)
				if regexp.MustCompile(`\bscope\s*:|\btab\b|\bs\b\s*[,}]`).Match(b) {
					t.Errorf("the hydration code mentions the build-time handle:\n%s", b)
				}
			}
		})
	}
}

// A handle read by code that runs in the browser, or handed to an island, is
// a build error naming the fix, never a ReferenceError after the page loads.
func TestCSSScopeHandleReadAtRuntimeIsAnError(t *testing.T) {
	cases := map[string]string{
		"event handler": `function Child(props) { const [c, setC] = createSignal(0); return <button onClick={() => { setC(c() + 1); console.log(props.scope.current()); }}>{c()}</button>; }
export default function P() { const tab = createCSSChoice('a', ['a', 'b']); return <main><Child scope={tab} /></main>; }`,
		"destructured local": `function Child({ scope }) { const [c, setC] = createSignal(0); const label = scope.current(); return <button onClick={() => setC(c() + 1)}>{c()}{label}</button>; }
export default function P() { const tab = createCSSChoice('a', ['a', 'b']); return <main><Child scope={tab} /></main>; }`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			got := buildErrText(t, cssPropsPage(src))
			if !strings.Contains(got, "KR8330") || !strings.Contains(got, "hint:") {
				t.Errorf("want a KR8330 error with a hint, got %q", got)
			}
		})
	}
}

// A CSS signal's getter or setter has no value at build time, so a component
// rendered at build time that reads it is a build error.
func TestCSSSignalGetterInBuildTimeComponentIsAnError(t *testing.T) {
	got := buildErrText(t, cssPropsPage(`function Child(props) { return <p>{props.tab()}</p>; }
export default function P() { const [tab, setTab] = createCSSChoice('a', ['a', 'b']); return <main><Child tab={tab} /></main>; }`))
	if !strings.Contains(got, "KR8331") || !strings.Contains(got, "scope={tab}") {
		t.Errorf("want a KR8331 error naming the scope handle fix, got %q", got)
	}
	// A child that does not read the prop is fine.
	buildPlain(t, cssPropsPage(`function Child(props) { return <p>static</p>; }
export default function P() { const [tab, setTab] = createCSSChoice('a', ['a', 'b']); return <main><Child tab={tab} /></main>; }`))
}

// Getters and setters passed to a component with client state are read in the
// browser and keep working.
func TestCSSSignalGetterInHydratingChildBrowser(t *testing.T) {
	page := cssPropsImport + `function Child(props) { const [c, setC] = createSignal(0); return <p class="out" onClick={() => { setC(c() + 1); props.set('b'); }}>{props.tab()}{c()}</p>; }
export default function P() { const [tab, setTab] = createCSSChoice('a', ['a', 'b']); return <main><Child tab={tab} set={setTab} /></main>; }
`
	evalFixBrowser(t, map[string]string{"src/pages/index.tsx": page}, []evalFixCheck{
		{Path: "/", Selector: "p.out", Static: strp("0"), Hydrated: "a0", Click: "p.out", AfterClick: strp("b1")},
	})
}

func TestCSSScopeHandleInHydratingChildBrowser(t *testing.T) {
	page := cssPropsImport + `function Child(props) { const [c, setC] = createSignal(0); return <div><button class="count" onClick={() => setC(c() + 1)}>{c()}</button><label {...props.scope.trigger('b')} class="go">go</label></div>; }
export default function P() { const tab = createCSSChoice('a', ['a', 'b']); return <main><Child scope={tab} /></main>; }
`
	evalFixBrowser(t, map[string]string{"src/pages/index.tsx": page}, []evalFixCheck{
		{Path: "/", Selector: "button.count", Hydrated: "0", Click: "button.count", AfterClick: strp("1")},
	})
}

// A scope handle has no value in the browser, so handing it to an island from
// an npm package fails the build instead of throwing when the island mounts.
func TestCSSScopeHandleToExternalIslandIsAnError(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	page := `import { createCSSChoice } from '@krate/runtime';
import { Badge } from 'fake-lib';
export default function Page() {
	const tab = createCSSChoice('a', ['a', 'b']);
	return <div><Badge label="x" scope={tab} /><button {...tab.trigger('b')}>b</button><div {...tab.panel('b')}>pb</div></div>;
}
`
	if err := os.WriteFile(filepath.Join(cfg.PagesDir, "index.tsx"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	err := New(root, cfg).BuildAll()
	if err == nil || !strings.Contains(err.Error(), "KR8330") || !strings.Contains(err.Error(), "external island") {
		t.Fatalf("want a KR8330 error about the island, got %v", err)
	}
}
