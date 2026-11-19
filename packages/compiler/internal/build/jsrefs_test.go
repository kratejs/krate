package build

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/renderer"
)

func TestJSRefs(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		has     []string
		hasNot  []string
		dynamic bool
	}{
		{name: "call", src: `cn("a", b)`, has: []string{"cn", "b"}},
		{name: "string contents", src: `kbindRef("cn", () => x); var t = 'requestFormReset';`, has: []string{"kbindRef", "x"}, hasNot: []string{"cn", "requestFormReset"}},
		{name: "object keys and members", src: `var p = {title: "Children", redirect: 1, x}; p.navigate(); a?.Slot;`, has: []string{"p", "x"}, hasNot: []string{"title", "redirect", "navigate", "Slot", "Children"}},
		{name: "global members", src: `var f = window.__krateOwn || g; globalThis.cva(); self["twMerge"](); window[k]`, has: []string{"__krateOwn", "cva", "twMerge", "k", "g"}},
		{name: "template", src: "var s = `useParams ${cn(a)} and ${`${notFound()}`} redirect`;", has: []string{"cn", "a", "notFound"}, hasNot: []string{"useParams", "redirect"}},
		{name: "comments", src: "// navigate()\n/* cn() */ go()", has: []string{"go"}, hasNot: []string{"navigate", "cn"}},
		{name: "regex", src: `var r = /cn"'/g.test(s); if (a) x = b / c / d;`, has: []string{"r", "s", "a", "b", "c", "d"}, hasNot: []string{"cn"}},
		{name: "regex after return", src: `function f(){return /redirect'/.test(u)}notFound()`, has: []string{"u", "notFound"}, hasNot: []string{"redirect"}},
		{name: "division after call", src: `var h = width() / 2; var q = '/'; useParams()`, has: []string{"width", "useParams"}},
		{name: "spread", src: `f(...cn)`, has: []string{"f", "cn"}},
		{name: "ternary keeps values", src: `x ? cn : clsx`, has: []string{"cn", "clsx"}},
		{name: "dynamic import", src: `import("./x.js").then(m => m.run())`, has: []string{"m"}, hasNot: []string{"run"}, dynamic: true},
		{name: "import meta", src: `var u = import.meta.url`, hasNot: []string{dynamicImportRef}},
		{name: "unterminated string falls back", src: `cn("a); navigate()`, has: []string{"cn", "a", "navigate"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			refs := jsRefs(c.src)
			for _, n := range c.has {
				if !refs[n] {
					t.Errorf("missing %s in %v", n, sortedKeys(refs))
				}
			}
			for _, n := range c.hasNot {
				if refs[n] {
					t.Errorf("unexpected %s in %v", n, sortedKeys(refs))
				}
			}
			if refs[dynamicImportRef] != c.dynamic {
				t.Errorf("dynamic import = %v, want %v", refs[dynamicImportRef], c.dynamic)
			}
		})
	}
}

// A name counts only when it survives minification too: a local the author
// named like a runtime export is renamed, a global reference is not.
func TestRecordRuntimeRefsDropsRenamedLocals(t *testing.T) {
	raw := "(function(){function handleError(e){console.log(e)}handleError(1);cn('a')})();"
	min := minifyJS(raw)
	if strings.Contains(min, "handleError") {
		t.Skipf("minifier kept the local name: %s", min)
	}
	got := strings.Join(recordRuntimeRefs(raw, min, []renderer.LazyChunk{{JS: "useParams()"}}), ",")
	if strings.Contains(got, "handleError") || !strings.Contains(got, "cn") || !strings.Contains(got, "useParams") {
		t.Errorf("recordRuntimeRefs = %s", got)
	}
}

func TestScanPageMarkup(t *testing.T) {
	cases := []struct {
		name            string
		html            string
		island, actions bool
		scripts         int
	}{
		{name: "code sample text", html: `<pre><code>&lt;form data-krate-action&gt; &lt;krate-island&gt; /__krate/action/a1</code></pre>`},
		{name: "attribute value mention", html: `<a title="data-krate-action" href="/docs/__krate/action/">x</a>`},
		{name: "action form", html: `<form action="/__krate/action/a1" method="post"></form>`, actions: true},
		{name: "action attribute", html: `<form data-krate-action><button formaction="/x">b</button></form>`, actions: true},
		{name: "formaction", html: `<button formaction="/base/__krate/action/a2">b</button>`, actions: true},
		{name: "template form", html: `<template><form data-krate-action></form></template>`, actions: true},
		{name: "island", html: `<krate-island data-id="x"></krate-island>`, island: true},
		{name: "preact island", html: `<div><!--krate-island:{"m":"a","e":"default","s":"/chunks/a.js","p":{}}--><p>x</p><!--/krate-island--></div>`, island: true},
		{name: "other comment", html: `<!-- krate-island: a note -->`},
		{name: "scripts", html: `<script>cn()</script><script type="module">x()</script><script type="application/ld+json">{"a":"cn"}</script><script src="/a.js"></script><textarea><script>no()</script></textarea>`, scripts: 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			scripts, island, actions := scanPageMarkup(c.html)
			if island != c.island || actions != c.actions || len(scripts) != c.scripts {
				t.Errorf("island=%v actions=%v scripts=%q, want %v %v %d", island, actions, scripts, c.island, c.actions, c.scripts)
			}
		})
	}
}

// Text that names runtime APIs (headings in props, slot ids, code samples)
// must not decide the runtime; code and real markup must.
func TestRuntimeUsageIgnoresText(t *testing.T) {
	text := &PageResult{
		HydrationJS: `__krate_props["a"]={toc:[{title:"requestFormReset"},{title:"Children, keys, and refs"},{title:"krateHighlight"}]};kbindRef("cn",()=>t,e=>{t=e})`,
		HTML:        `<pre><code>&lt;form data-krate-action&gt;</code></pre><script type="application/ld+json">{"headline":"useParams"}</script>`,
	}
	u := runtimeUsageOf([]*PageResult{text})
	for _, name := range []string{"requestFormReset", "Children", "krateHighlight", "cn", "useParams"} {
		if u.Refs[name] {
			t.Errorf("%s counted as a reference", name)
		}
	}
	if u.ActionForms || u.Unknown || runtimeNeedsCompat(u) {
		t.Errorf("usage = %+v", u)
	}

	code := &PageResult{
		RuntimeRefs: []string{"cn", "kbindRef"},
		ScriptHTML:  `<script>navigate("/")</script>`,
		HTML:        `<form action="/__krate/action/a1"></form><krate-island></krate-island>`,
	}
	u = runtimeUsageOf([]*PageResult{code})
	if !u.Refs["cn"] || !u.Refs["navigate"] || !u.ActionForms || !u.Unknown {
		t.Errorf("usage = %+v", u)
	}

	// A page result without recorded references is scanned as shipped; its
	// lazy chunks are unseen, so every feature and the compat layer ship.
	lazy := &PageResult{HydrationJS: `__krateLazy("/a/lazy-x.js",0)`}
	u = runtimeUsageOf([]*PageResult{lazy})
	if !u.Unknown || !runtimeNeedsCompat(u) {
		t.Errorf("usage = %+v", u)
	}
	if !runtimeNeedsSuspense(u) {
		t.Error("a lazy component needs the Suspense runtime")
	}
}
