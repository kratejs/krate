package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Build-level tests for React 19 behaviour that used to build clean and then
// lose content, ship dead markup, or throw in the browser: build-time tiers
// with client APIs, useId in children, useEffectEvent, Activity,
// ViewTransition, higher-order components under a tier decorator, tier
// conflicts and custom element props.

func buildAndRead(t *testing.T, files map[string]string) (html, js string) {
	t.Helper()
	b, root := scProject(t, files)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	_ = filepath.Walk(filepath.Join(root, "dist"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, ".js") {
			if b, e := os.ReadFile(p); e == nil {
				all.Write(b)
			}
		}
		return nil
	})
	return string(data), all.String()
}

const counterBody = `import { useState } from "react";
export default function Counter() {
  const [n, setN] = useState(0);
  return <button onClick={() => setN(n + 1)}>{n}</button>;
}`

const decoratedCounter = `import { useState } from "react";
@static
export default function Counter() {
  const [n, setN] = useState(0);
  return <button onClick={() => setN(n + 1)}>{n}</button>;
}`

// 1. Client APIs in build-time tiers fail the build.

func TestBuildTimeTierRejectsClientAPIs(t *testing.T) {
	cases := map[string]struct {
		files   map[string]string
		contain string
	}{
		"server comment": {map[string]string{
			"src/pages/index.tsx": "// @server\n" + counterBody,
		}, "useState() in Counter"},
		"server file name": {map[string]string{
			"src/pages/index.tsx": `// @server
import Inner from "../Inner.server";
export default function P() { return <main><Inner /></main>; }`,
			"src/Inner.server.tsx": counterBody,
		}, "useState() in Counter"},
		"static decorator": {map[string]string{
			"src/pages/index.tsx": `import Inner from "../Inner";
export default function P() { return <main><Inner /></main>; }`,
			"src/Inner.tsx": decoratedCounter,
		}, "a static component"},
		"server decorator on a helper": {map[string]string{
			"src/pages/index.tsx": `import { useState } from "react";
@server
function Live() { const [n, setN] = useState(0); return <button onClick={() => setN(n + 1)}>{n}</button>; }
export default function P() { return <main><Live /></main>; }`,
		}, "a server component"},
		"effect": {map[string]string{
			"src/pages/index.tsx": `// @server
import { useEffect } from "react";
export default function P() { useEffect(() => { document.title = "x"; }, []); return <main>hi</main>; }`,
		}, "useEffect() in P"},
		"handler only": {map[string]string{
			"src/pages/index.tsx": `// @server
export default function P() { return <button onClick={() => alert(1)}>go</button>; }`,
		}, "event handler onClick on <button>"},
		"class component": {map[string]string{
			"src/pages/index.tsx": `// @server
import { Component } from "react";
export default class P extends Component {
  state = { n: 0 };
  render() { return <button onClick={() => this.setState({ n: this.state.n + 1 })}>{this.state.n}</button>; }
}`,
		}, "a server component"},
		"class lifecycle": {map[string]string{
			"src/pages/index.tsx": `// @server
import { Component } from "react";
export default class P extends Component {
  componentDidMount() { document.title = "x"; }
  render() { return <p>hi</p>; }
}`,
		}, "componentDidMount()"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := buildErr(t, tc.files)
			wantCode(t, err, diag.CodeClientAPIInServer, tc.contain)
			if !strings.Contains(err.Error(), "client child component") {
				t.Errorf("the hint should explain the fix: %v", err)
			}
		})
	}
}

func TestBuildTimeTierAllowsClientChild(t *testing.T) {
	// The documented fix: the interactive part lives in a client child.
	html, js := buildAndRead(t, map[string]string{
		"src/pages/index.tsx": `// @server
import Counter from "../Counter";
export default function P() { return <main><h1>Static</h1><Counter /></main>; }`,
		"src/Counter.tsx": counterBody,
	})
	if !strings.Contains(html, "<h1>Static</h1>") || !strings.Contains(js, "createSignal") {
		t.Fatalf("client child should hydrate:\n%s\n%s", html, js)
	}
}

func TestClientOverrideInServerFileStillWorks(t *testing.T) {
	html, js := buildAndRead(t, map[string]string{
		"src/pages/index.tsx": `// @server
import { useState } from "react";
@client
function Counter() { const [n, setN] = useState(0); return <button onClick={() => setN(n + 1)}>{n}</button>; }
export default function P() { return <main><h1>Static</h1><Counter /></main>; }`,
	})
	if !strings.Contains(html, "<h1>Static</h1>") || !strings.Contains(js, "createSignal") {
		t.Fatalf("@client in a // @server file should hydrate:\n%s\n%s", html, js)
	}
}

// 2. useId in children.

var idRe = regexp.MustCompile(`(?:id|for)=("?)(krate[^ >"]+)`)

func idsIn(html string) map[string]int {
	out := map[string]int{}
	for _, m := range idRe.FindAllStringSubmatch(html, -1) {
		out[m[2]]++
	}
	return out
}

func TestUseIdInChildComponents(t *testing.T) {
	html, _ := buildAndRead(t, map[string]string{
		"src/pages/index.tsx": `import Field from "../Field";
import Group from "../Group";
const ITEMS = ["a", "b"];
export default function P() {
  return <form>
    <Field label="one" /><Field label="two" />
    <Group title="g"><Field label="nested" /></Group>
    {ITEMS.map((i) => <Field key={i} label={i} />)}
  </form>;
}`,
		"src/Field.tsx": `import { useId } from "react";
export default function Field({ label }: { label: string }) {
  const id = useId();
  return <div><label htmlFor={id}>{label}</label><input id={id} aria-describedby={id + "-hint"} /><span id={id + "-hint"}>h</span></div>;
}`,
		"src/Group.tsx": `import { useId } from "react";
export default function Group({ title, children }: { title: string; children?: any }) {
  const id = useId();
  return <fieldset aria-labelledby={id}><legend id={id}>{title}</legend>{children}</fieldset>;
}`,
	})
	if strings.Contains(html, "__krate_useId") {
		t.Fatalf("unresolved useId marker in HTML:\n%s", html)
	}
	ids := idsIn(html)
	if len(ids) < 6 {
		t.Fatalf("expected an id per instance, got %v in:\n%s", ids, html)
	}
	// Every instance has its own id; each id appears as for/id (or as the
	// referenced aria id) the same number of times on both sides.
	labels := regexp.MustCompile(`<label for=([^ >]+)>`).FindAllStringSubmatch(html, -1)
	if len(labels) != 5 {
		t.Fatalf("want 5 labels, got %d: %s", len(labels), html)
	}
	seen := map[string]bool{}
	for _, l := range labels {
		if seen[l[1]] {
			t.Errorf("label id %s used by two instances", l[1])
		}
		seen[l[1]] = true
		if !strings.Contains(html, "<input id="+l[1]+" ") {
			t.Errorf("label for=%s has no input", l[1])
		}
	}
}

func TestUseIdInHydratedChildMatchesHTML(t *testing.T) {
	html, js := buildAndRead(t, map[string]string{
		"src/pages/index.tsx": `import Field from "../Field";
export default function P() { return <div><Field /><Field /></div>; }`,
		"src/Field.tsx": `import { useId, useState } from "react";
export default function Field() {
  const id = useId();
  const [v, setV] = useState("");
  return <div><label htmlFor={id}>x</label><input id={id} value={v} onInput={(e) => setV(e.target.value)} /></div>;
}`,
	})
	for id := range idsIn(html) {
		if !strings.Contains(js, `"`+id+`"`) {
			t.Errorf("hydration code lacks the id %s used in the HTML", id)
		}
	}
	if strings.Contains(js, "__krate_useId") {
		t.Errorf("hydration code still calls __krate_useId:\n%s", js)
	}
}

func TestUseIdInDynamicListRowsIsNumberedPerRow(t *testing.T) {
	html, js := buildAndRead(t, map[string]string{
		"src/pages/index.tsx": `import { useState } from "react";
import Field from "../Field";
export default function P() {
  const [rows, setRows] = useState(["r1", "r2"]);
  return <div><button onClick={() => setRows([...rows, "r3"])}>add</button>{rows.map((r) => <Field key={r} label={r} />)}</div>;
}`,
		"src/Field.tsx": `import { useId } from "react";
export default function Field({ label }: { label: string }) {
  const id = useId();
  return <div><label htmlFor={id}>{label}</label><input id={id} /></div>;
}`,
	})
	for _, want := range []string{"krate-", ":$r1-u0", ":$r2-u0"} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML lacks row id %q:\n%s", want, html)
		}
	}
	// The row component is also built for the client, which numbers its
	// useId() calls per row with the runtime helper, not a literal.
	if !strings.Contains(js, "__krate_useId()") {
		t.Errorf("row component should call the runtime useId:\n%s", js)
	}
}

// 3. useEffectEvent.

func TestUseEffectEventIsLowered(t *testing.T) {
	_, js := buildAndRead(t, map[string]string{
		"src/pages/index.tsx": `import { useState, useEffect, useEffectEvent } from "react";
export default function P() {
  const [n, setN] = useState(0);
  const [log, setLog] = useState("");
  const onTick = useEffectEvent(() => { setLog("n=" + n); });
  useEffect(() => { const t = setTimeout(() => onTick(), 10); return () => clearTimeout(t); }, []);
  return <div><button onClick={() => setN(n + 1)}>{n}</button><p>{log}</p></div>;
}`,
	})
	if strings.Contains(js, "useEffectEvent(") {
		t.Errorf("a bare useEffectEvent call is left in client code:\n%s", js)
	}
	if !strings.Contains(js, "createEffectEvent(") {
		t.Errorf("useEffectEvent should lower to createEffectEvent:\n%s", js)
	}
}

// 4. Activity and ViewTransition.

func TestActivityRendersChildrenAndHidesThem(t *testing.T) {
	html, js := buildAndRead(t, map[string]string{
		"src/pages/index.tsx": `import { Activity, useState } from "react";
export default function P() {
  const [mode, setMode] = useState("hidden");
  return <div>
    <button onClick={() => setMode(mode === "visible" ? "hidden" : "visible")}>t</button>
    <Activity mode={mode}><p id="dyn">dyn child</p></Activity>
    <Activity mode="hidden"><p id="hid">hidden child</p></Activity>
    <Activity><p id="vis">default child</p></Activity>
  </div>;
}`,
	})
	for _, want := range []string{"dyn child", "hidden child", "default child"} {
		if !strings.Contains(html, want) {
			t.Errorf("Activity dropped %q from the HTML:\n%s", want, html)
		}
	}
	if got := strings.Count(html, `<krate-activity style=display:none>`); got != 2 {
		t.Errorf("want 2 hidden wrappers, got %d:\n%s", got, html)
	}
	if got := strings.Count(html, `<krate-activity style=display:contents>`); got != 1 {
		t.Errorf("want 1 visible wrapper, got %d:\n%s", got, html)
	}
	if !strings.Contains(js, "kbindActivity(") {
		t.Errorf("a reactive mode needs the activity binding:\n%s", js)
	}
}

func TestActivityUnsupportedPropWarns(t *testing.T) {
	_, _, logged := buildFiles(t, map[string]string{
		"src/pages/index.tsx": `import { Activity } from "react";
export default function P() { return <Activity mode="visible" name="x"><p>kept</p></Activity>; }`,
	}, nil)
	if !strings.Contains(logged, "KR8031") || !strings.Contains(logged, "no `name` prop") {
		t.Errorf("expected a KR8031 warning, got:\n%s", logged)
	}
}

func TestViewTransitionNamesItsChild(t *testing.T) {
	html, _, logged := func() (string, string, string) {
		root, out, logged := buildFiles(t, map[string]string{
			"src/pages/index.tsx": `import { ViewTransition } from "react";
export default function P() {
  return <main>
    <ViewTransition name="hero" default="slow-fade"><h1>Hero</h1></ViewTransition>
    <ViewTransition><p>auto</p></ViewTransition>
    <ViewTransition name="t" enter="slide" onEnter={() => {}}><span>x</span></ViewTransition>
    <ViewTransition name="w">text only</ViewTransition>
  </main>;
}`,
		}, nil)
		_ = root
		data, err := os.ReadFile(filepath.Join(out, "index.html"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data), out, logged
	}()
	for _, want := range []string{
		`<h1 data-view-transition=hero data-view-transition-class=slow-fade>Hero</h1>`,
		`<span data-view-transition=t>x</span>`,
		`<div data-view-transition=w>text only</div>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
	if !regexp.MustCompile(`<p data-view-transition=krate-vt-[a-z0-9]+-1>auto</p>`).MatchString(html) {
		t.Errorf("an unnamed ViewTransition should get a generated name:\n%s", html)
	}
	if strings.Count(logged, "KR8031") != 2 {
		t.Errorf("expected 2 KR8031 warnings (enter class, onEnter), got:\n%s", logged)
	}
}

// 5. A tier decorator on a higher-order component application.

const hocFile = `function withBox(C: any) {
  return function Box(props: any) { return <section id="box"><C {...props} /></section>; };
}
function Inner() { return <p id="inner">inner content</p>; }
`

func TestTierDecoratorOnHOCDefaultExport(t *testing.T) {
	for name, comp := range map[string]string{
		"default call":      hocFile + "@server\nexport default withBox(Inner);\n",
		"const then export": hocFile + "@server\nconst Wrapped = withBox(Inner);\nexport default Wrapped;\n",
		"no decorator":      hocFile + "export default withBox(Inner);\n",
		"memo of a name":    "import { memo } from \"react\";\nfunction Inner() { return <p id=\"inner\">inner content</p>; }\n@server\nexport default memo(Inner);\n",
	} {
		t.Run(name, func(t *testing.T) {
			html, _ := buildAndRead(t, map[string]string{
				"src/pages/index.tsx": "// @server\nimport Comp from \"../Comp\";\nexport default function P() { return <main><Comp /></main>; }",
				"src/Comp.tsx":        comp,
			})
			if !strings.Contains(html, "inner content") {
				t.Fatalf("the wrapped component's content is lost:\n%s", html)
			}
			if name != "memo of a name" && !strings.Contains(html, `<section id=box>`) {
				t.Errorf("the HOC was not applied:\n%s", html)
			}
		})
	}
}

func TestHOCFromAnotherModuleIsApplied(t *testing.T) {
	html, _ := buildAndRead(t, map[string]string{
		"src/pages/index.tsx": "// @server\nimport Comp from \"../Comp\";\nexport default function P() { return <main><Comp /></main>; }",
		"src/hoc.tsx":         "export function withBox(C: any) { return function Box(props: any) { return <section id=\"box\"><C {...props} /></section>; }; }",
		"src/Comp.tsx":        "import { withBox } from \"./hoc\";\nfunction Inner() { return <p>inner content</p>; }\n@server\nexport default withBox(Inner);\n",
	})
	if !strings.Contains(html, `<section id=box><p>inner content</p></section>`) {
		t.Fatalf("cross-module HOC not applied:\n%s", html)
	}
}

func TestUnresolvableHOCFailsTheBuild(t *testing.T) {
	err := buildErr(t, map[string]string{
		"src/pages/index.tsx": "// @server\nimport Comp from \"../Comp\";\nexport default function P() { return <main><Comp /></main>; }",
		"src/Comp.tsx":        "import { observer } from \"./lib\";\nfunction Inner() { return <p>inner content</p>; }\n@server\nexport default observer(Inner);\n",
		"src/lib.ts":          "export function observer(c: any) { const wrapped = c; return wrapped; }",
	})
	wantCode(t, err, diag.CodeHOCUnresolved, "observer(...)")
}

// 6. Conflicting tiers.

func TestConflictingTiersFail(t *testing.T) {
	page := "// @server\nimport Comp from \"../Comp\";\nexport default function P() { return <main><Comp /></main>; }"
	cases := map[string]struct {
		file, comp string
		cfg        func(*Builder)
		contain    string
	}{
		"comment and decorator":   {"Comp", "// @runtime\n@server\nexport default function Comp() { return <p>x</p>; }", nil, "`// @runtime` comment"},
		"file name and decorator": {"Comp.server", "@runtime\nexport default function Comp() { return <p>x</p>; }", nil, "`.server` file name"},
		"serverComponents list and decorator": {"Comp", "@runtime\nexport default function Comp() { return <p>x</p>; }",
			func(b *Builder) { b.Cfg.ServerComponents = []string{"Comp"} }, "`serverComponents` in the config"},
		"runtimeDirs and decorator": {"Comp", "@server\nexport default function Comp() { return <p>x</p>; }",
			func(b *Builder) { b.Cfg.RuntimeDirs = []string{"src"} }, "`runtimeDirs` in the config"},
		"body directive and decorator": {"Comp", "@runtime\nexport default function Comp() { \"@server\"; return <p>x</p>; }", nil, "\"@server\" directive"},
		"body directive and comment":   {"Comp", "// @server\nexport default function Comp() { \"@runtime\"; return <p>x</p>; }", nil, "`// @server` comment"},
		"comment and file name":        {"Comp.runtime", "// @server\nexport default function Comp() { return <p>x</p>; }", nil, "`.runtime` file name"},
		"two lists": {"Comp", "export default function Comp() { return <p>x</p>; }", func(b *Builder) {
			b.Cfg.ServerComponents = []string{"Comp"}
			b.Cfg.RuntimeComponents = []string{"Comp"}
		}, "`runtimeComponents` in the config"},
		"client decorator on default":  {"Comp", "// @server\n@client\nexport default function Comp() { return <p>x</p>; }", nil, "@client decorator"},
		"decorator and decorator body": {"Comp", "@static\nexport default function Comp() { \"@server\"; return <p>x</p>; }", nil, "conflicting tiers on Comp"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			b, _ := scProject(t, map[string]string{
				"src/pages/index.tsx":     strings.ReplaceAll(page, "../Comp", "../"+tc.file),
				"src/" + tc.file + ".tsx": tc.comp,
			})
			if tc.cfg != nil {
				tc.cfg(b)
			}
			err := b.BuildAll()
			wantCode(t, err, diag.CodeTierDecorator, tc.contain)
		})
	}
}

func TestAgreeingTiersBuild(t *testing.T) {
	for name, comp := range map[string]string{
		"comment and decorator":     "// @server\n@server\nexport default function Comp() { return <p>ok</p>; }",
		"decorator and body":        "@server\nexport default function Comp() { \"@server\"; return <p>ok</p>; }",
		"client override elsewhere": "// @server\n@client\nfunction Btn() { return <b>b</b>; }\nexport default function Comp() { return <p>ok<Btn /></p>; }",
	} {
		t.Run(name, func(t *testing.T) {
			html, _ := buildAndRead(t, map[string]string{
				"src/pages/index.tsx": "// @server\nimport Comp from \"../Comp\";\nexport default function P() { return <main><Comp /></main>; }",
				"src/Comp.tsx":        comp,
			})
			if !strings.Contains(html, "<p>ok") {
				t.Fatalf("lost content:\n%s", html)
			}
		})
	}
}

func TestServerDirsRelativeToProjectRoot(t *testing.T) {
	b, _ := scProject(t, map[string]string{
		"src/pages/index.tsx":            "// @server\nimport Comp from \"../components/server/Comp\";\nexport default function P() { return <main><Comp /></main>; }",
		"src/components/server/Comp.tsx": "export default function Comp() { return <button onClick={() => {}}>x</button>; }",
	})
	b.Cfg.ServerDirs = []string{"src/components/server"}
	wantCode(t, b.BuildAll(), diag.CodeClientAPIInServer, "event handler onClick")
}

// 7. Custom element props.

func TestCustomElementPropsFollowReact19(t *testing.T) {
	html, js := buildAndRead(t, map[string]string{
		"src/pages/index.tsx": `import Child from "../Child";
export default function P() {
  return <div>
    <my-el id="a" data={{ a: 1 }} list={[1, 2]} cb={() => 1} s="str" n={3} b={true} f={false} />
    <Child />
  </div>;
}`,
		"src/Child.tsx": `export default function Child() { return <my-el id="c" data={{ c: 1 }} s="x" />; }`,
	})
	for _, want := range []string{`s=str`, `n=3`, `b=""`} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
	for _, bad := range []string{"[object Object]", " data=", " list=", " f=", " cb="} {
		if strings.Contains(html, bad) {
			t.Errorf("custom element has %q in:\n%s", bad, html)
		}
	}
	for _, want := range []string{`"data"`, `"list"`, `"cb"`} {
		if !strings.Contains(js, want) {
			t.Errorf("hydration code does not set property %s:\n%s", want, js)
		}
	}
	// The static child component still needs hydration code for its property.
	if got := strings.Count(js, `"data"`); got < 2 {
		t.Errorf("both custom elements need a data binding, got %d:\n%s", got, js)
	}
}
