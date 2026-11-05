package build

import (
	"strings"
	"testing"
)

// A member tag of a namespace import resolves to the exported component,
// through barrel files, whatever the case of the namespace name.
func TestNamespaceMemberTagResolves(t *testing.T) {
	btn := "export function Button(p) { return <button>{p.children}</button>; }\n"
	counter := dynTagSig + "export function Counter() { const [n, setN] = createSignal(0); return <button onClick={() => setN(n() + 1)}>{n()}</button>; }\n"
	cases := map[string]struct {
		files map[string]string
		want  string
	}{
		"direct module": {map[string]string{
			"src/ui/index.tsx":    btn,
			"src/pages/index.tsx": "import * as ui from '../ui';\nexport default function P() { return <main><ui.Button>go</ui.Button></main>; }\n",
		}, "<main><button>go</button></main>"},
		"named re-export barrel": {map[string]string{
			"src/ui/index.ts":     "export { Button } from './Button';\n",
			"src/ui/Button.tsx":   btn,
			"src/pages/index.tsx": "import * as ui from '../ui';\nexport default function P() { return <main><ui.Button>go</ui.Button></main>; }\n",
		}, "<main><button>go</button></main>"},
		"star re-export barrel": {map[string]string{
			"src/ui/index.ts":     "export * from './Button';\n",
			"src/ui/Button.tsx":   btn,
			"src/pages/index.tsx": "import * as ui from '../ui';\nexport default function P() { return <main><ui.Button>go</ui.Button></main>; }\n",
		}, "<main><button>go</button></main>"},
		"renamed default re-export": {map[string]string{
			"src/ui/index.ts":     "export { default as Button } from './Button';\n",
			"src/ui/Button.tsx":   "export default function B(p) { return <button>{p.children}</button>; }\n",
			"src/pages/index.tsx": "import * as ui from '../ui';\nexport default function P() { return <main><ui.Button>go</ui.Button></main>; }\n",
		}, "<main><button>go</button></main>"},
		"renamed export": {map[string]string{
			"src/ui/index.ts":     "export { Button as Btn } from './Button';\n",
			"src/ui/Button.tsx":   btn,
			"src/pages/index.tsx": "import * as ui from '../ui';\nexport default function P() { return <main><ui.Btn>go</ui.Btn></main>; }\n",
		}, "<main><button>go</button></main>"},
		"nested namespace": {map[string]string{
			"src/ui/index.ts":     "export * as forms from './forms';\n",
			"src/ui/forms.ts":     "export { Input } from './Input';\n",
			"src/ui/Input.tsx":    "export function Input() { return <input />; }\n",
			"src/pages/index.tsx": "import * as ui from '../ui';\nexport default function P() { return <main><ui.forms.Input /></main>; }\n",
		}, "<main><input/></main>"},
		"capitalized namespace over a barrel": {map[string]string{
			"src/ui/index.ts":     "export { Button } from './Button';\n",
			"src/ui/Button.tsx":   btn,
			"src/pages/index.tsx": "import * as UI from '../ui';\nexport default function P() { return <main><UI.Button>go</UI.Button></main>; }\n",
		}, "<main><button>go</button></main>"},
		"namespace re-export imported by name": {map[string]string{
			"src/lib/index.ts":    "export * as ui from '../ui';\n",
			"src/ui/index.ts":     "export { Button } from './Button';\n",
			"src/ui/Button.tsx":   btn,
			"src/pages/index.tsx": "import { ui } from '../lib';\nexport default function P() { return <main><ui.Button>go</ui.Button></main>; }\n",
		}, "<main><button>go</button></main>"},
		"hydrating component": {map[string]string{
			"src/ui/index.ts":     "export { Counter } from './Counter';\n",
			"src/ui/Counter.tsx":  counter,
			"src/pages/index.tsx": "import * as ui from '../ui';\nexport default function P() { return <main><ui.Counter /></main>; }\n",
		}, "<main><button data-k=k:2><!--k:3-->0<!--/k:3--></button></main>"},
		"inside a static component": {map[string]string{
			"src/ui/index.ts":     "export { Button } from './Button';\n",
			"src/ui/Button.tsx":   btn,
			"src/pages/index.tsx": "import * as ui from '../ui';\nfunction Bar({ children }) { return <nav><ui.Button>{children}</ui.Button></nav>; }\nexport default function P() { return <main><Bar>go</Bar></main>; }\n",
		}, "<main><nav><button>go</button></nav></main>"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			html, logged := buildPlain(t, c.files)
			if !strings.Contains(html, c.want) {
				t.Errorf("want %s in:\n%s", c.want, rootOf(t, html))
			}
			if strings.Contains(html, "<ui.") || strings.Contains(html, "$ns") || strings.Contains(logged, "could not be resolved") {
				t.Errorf("member tag left unresolved:\n%s\n%s", rootOf(t, html), logged)
			}
		})
	}
}

// A member that names no component fails the build with KR8321.
func TestNamespaceMemberUnresolvedIsAnError(t *testing.T) {
	ui := map[string]string{
		"src/ui/index.ts":   "export const label = 'x';\nexport { Button } from './Button';\n",
		"src/ui/Button.tsx": "export function Button(p) { return <button>{p.children}</button>; }\n",
	}
	with := func(page string) map[string]string {
		files := map[string]string{"src/pages/index.tsx": page}
		for k, v := range ui {
			files[k] = v
		}
		return files
	}
	cases := map[string]struct{ page, shown string }{
		"missing export":        {"import * as ui from '../ui';\nexport default function P() { return <main><ui.Nope>go</ui.Nope></main>; }\n", "ui.Nope"},
		"not a component":       {"import * as ui from '../ui';\nexport default function P() { return <main><ui.label>go</ui.label></main>; }\n", "ui.label"},
		"in a static component": {"import * as ui from '../ui';\nfunction Bar() { return <nav><ui.Nope /></nav>; }\nexport default function P() { return <main><Bar /></main>; }\n", "ui.Nope"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := buildErrText(t, with(c.page))
			if !strings.Contains(got, "KR8321") || !strings.Contains(got, "<"+c.shown+">") {
				t.Errorf("want a KR8321 error naming <%s>, got %q", c.shown, got)
			}
			if strings.Contains(got, "$ns") {
				t.Errorf("the internal binding name leaked into the message: %q", got)
			}
		})
	}
}

// A member tag of a React library namespace mounts the component as an island,
// as <Badge> imported by name does.
func TestNamespaceMemberTagOfReactLibrary(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	writeProjectFile(t, root, "src/pages/index.tsx", `
		import * as lib from 'fake-lib';
		export default function Page() { return <main><lib.Badge label="one" /></main>; }
	`)
	if _, err := captureOutput(func() error { return New(root, cfg).BuildAll() }); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	html := readBuilt(t, cfg.OutDir+"/index.html")
	if !strings.Contains(html, "data-krate-island") || !strings.Contains(html, `<span class="badge">one</span>`) {
		t.Errorf("namespace member of a React library was not rendered as an island:\n%s", html)
	}
	if strings.Contains(html, "<lib.Badge") {
		t.Errorf("literal member tag in the output:\n%s", html)
	}
}

func TestNamespaceMemberTagBrowser(t *testing.T) {
	files := map[string]string{
		"src/ui/index.ts":     "export { Counter } from './Counter';\nexport { Label } from './Label';\n",
		"src/ui/Counter.tsx":  dynTagSig + "export function Counter() { const [n, setN] = createSignal(0); return <button class=\"ctr\" onClick={() => setN(n() + 1)}>n={n()}</button>; }\n",
		"src/ui/Label.tsx":    "export function Label(p) { return <em class=\"lbl\">{p.children}</em>; }\n",
		"src/pages/index.tsx": "import * as ui from '../ui';\nexport default function P() { return <main><ui.Label>hi</ui.Label><ui.Counter /></main>; }\n",
	}
	evalFixBrowser(t, files, []evalFixCheck{
		{Path: "/", Selector: "em.lbl", Hydrated: "hi"},
		{Path: "/", Selector: "button.ctr", Hydrated: "n=0", Click: "button.ctr", AfterClick: strp("n=1")},
	})
}
