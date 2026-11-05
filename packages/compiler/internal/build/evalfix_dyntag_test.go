package build

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const dynTagSig = "import { createSignal } from '@krate/runtime';\n"

func rootOf(t *testing.T, html string) string {
	t.Helper()
	i := strings.Index(html, "<div id=root>")
	j := strings.Index(html, "</div> ")
	if i < 0 || j < i {
		t.Fatalf("no root in:\n%s", html)
	}
	return html[i : j+6]
}

// A tag the build can fold renders the real element with its children, for
// module constants, props, prop defaults and conditions on them.
func TestDynamicTagResolvesAtBuildTime(t *testing.T) {
	cases := map[string]struct{ src, want string }{
		"module-const": {
			"const Tag = 'section';\nexport default function P() { return <main><Tag>kids</Tag></main>; }\n",
			"<main><section>kids</section></main>",
		},
		"module-const-hydrating": {
			dynTagSig + "const Tag = 'section';\nexport default function P() { const [n, setN] = createSignal(0); return <main onClick={() => setN(n() + 1)}><Tag>kids{n()}</Tag></main>; }\n",
			"<section>kids<!--k:3-->0<!--/k:3--></section>",
		},
		"renamed-prop-default": {
			"function Box({ as: Tag = 'div', children }) { return <Tag>{children}</Tag>; }\nexport default function P() { return <main><Box>x</Box><Box as='h2'>y</Box></main>; }\n",
			"<main><div>x</div><h2>y</h2></main>",
		},
		"same-name-prop-default": {
			"function Box({ Tag = 'div', children }) { return <Tag>{children}</Tag>; }\nexport default function P() { return <main><Box>x</Box><Box Tag='h2'>y</Box></main>; }\n",
			"<main><div>x</div><h2>y</h2></main>",
		},
		"prop-local": {
			"function Box({ as, children }) { const Tag = as || 'div'; return <Tag>{children}</Tag>; }\nexport default function P() { return <main><Box>x</Box><Box as='h2'>y</Box></main>; }\n",
			"<main><div>x</div><h2>y</h2></main>",
		},
		"condition": {
			"function Box({ href, children }) { const Tag = href ? 'a' : 'span'; return <Tag href={href}>{children}</Tag>; }\nexport default function P() { return <main><Box href='/x'>x</Box><Box>y</Box></main>; }\n",
			"<main><a href=/x>x</a><span>y</span></main>",
		},
		"module-const-in-static-component": {
			"const Tag = 'section';\nfunction Box({ children }) { return <Tag>{children}</Tag>; }\nexport default function P() { return <main><Box>x</Box></main>; }\n",
			"<main><section>x</section></main>",
		},
		"hydrating-component-with-prop-tag": {
			dynTagSig + "function Box({ as: Tag = 'div', children }) { const [n, setN] = createSignal(0); return <Tag onClick={() => setN(n() + 1)}>{children}{n()}</Tag>; }\nexport default function P() { return <main><Box>x</Box><Box as='h2'>y</Box></main>; }\n",
			"<main><div data-k=k:2>x<!--k:3-->0<!--/k:3--></div><h2 data-k=k:4>y<!--k:5-->0<!--/k:5--></h2></main>",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			html, logged := buildPlain(t, map[string]string{"src/pages/index.tsx": c.src})
			if !strings.Contains(html, c.want) {
				t.Errorf("want %s in:\n%s", c.want, rootOf(t, html))
			}
			if strings.Contains(html, "<span></span>") || strings.Contains(logged, "could not be resolved") {
				t.Errorf("placeholder or warning for a resolvable tag:\n%s\n%s", rootOf(t, html), logged)
			}
		})
	}
}

// A tag the build cannot fold fails the build with KR8320 instead of
// shipping an empty placeholder.
func TestDynamicTagUnresolvableIsAnError(t *testing.T) {
	cases := map[string]string{
		"static component, unknown value": "function Box({ children }) { const Tag = globalThis.foo; return <Tag>{children}</Tag>; }\nexport default function P() { return <main><Box>x</Box></main>; }\n",
		"static component, omitted prop":  "function Box({ as: Tag, children }) { return <Tag>{children}</Tag>; }\nexport default function P() { return <main><Box>x</Box></main>; }\n",
		"page, unknown value":             "export default function P() { const Tag = globalThis.foo; return <main><Tag>kids</Tag></main>; }\n",
		"hydrating, tag read from signal": dynTagSig + "export default function P() { const [n, setN] = createSignal(0); const Tag = n() > 0 ? 'b' : 'i'; return <main onClick={() => setN(n() + 1)}><Tag>kids</Tag></main>; }\n",
		"hydrating, signal-derived prop":  dynTagSig + "function Box({ as: Tag = 'div', children }) { const [m] = createSignal(0); return <Tag>{children}{m()}</Tag>; }\nexport default function P() { const [t] = createSignal('h2'); return <main><Box as={t()}>y</Box></main>; }\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			got := buildErrText(t, map[string]string{"src/pages/index.tsx": src})
			if !strings.Contains(got, "KR8320") || !strings.Contains(got, "<Tag>") {
				t.Errorf("want a KR8320 error naming <Tag>, got %q", got)
			}
		})
	}
}

// Runtime components are compiled to request-time code, where a tag held in a
// variable is ordinary JavaScript, so the build accepts them.
func TestDynamicTagInRuntimePageBuilds(t *testing.T) {
	b, _ := scProject(t, map[string]string{
		"src/pages/index.tsx": "// @runtime\nexport default function Home({ query }) { const Tag = globalThis.foo || 'section'; return <main><Tag>runtime</Tag></main>; }",
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
}

// The request-time renderer evaluates the tag like any other JavaScript value.
func TestDynamicTagInRuntimePageServes(t *testing.T) {
	e2eEnv(t)
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx": "// @runtime\nexport default function Home() { const Tag = Date.now() > 0 ? 'section' : 'div'; return <main><Tag>runtime</Tag></main>; }",
	})
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	// The region arrives in a streamed script, with markup JSON-escaped.
	if !strings.Contains(string(body), `\u003csection\u003eruntime\u003c/section\u003e`) {
		t.Errorf("runtime page did not render the dynamic tag:\n%s", body)
	}
}

func TestDynamicTagBrowser(t *testing.T) {
	page := dynTagSig + `function Box({ as: Tag = "div", children }) {
	const [n, setN] = createSignal(0);
	return <Tag class="box" onClick={() => setN(n() + 1)}>{children}:{n()}</Tag>;
}
const Wrap = "section";
export default function Page() {
	return <main><Box>a</Box><Box as="h2">b</Box><Wrap class="wrap">w</Wrap></main>;
}
`
	evalFixBrowser(t, map[string]string{"src/pages/index.tsx": page}, []evalFixCheck{
		{Path: "/", Selector: "div.box", Hydrated: "a:0", Click: "div.box", AfterClick: strp("a:1")},
		{Path: "/", Selector: "h2.box", Hydrated: "b:0", Click: "h2.box", AfterClick: strp("b:1")},
		{Path: "/", Selector: "section.wrap", Hydrated: "w"},
	})
}
