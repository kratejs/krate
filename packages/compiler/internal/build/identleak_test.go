package build

import (
	"strings"
	"testing"
)

// Markup rendered at build time never contains the spelling of a variable. A
// value the build cannot resolve renders empty (the browser fills it in) and a
// value it can resolve renders as that value.
func TestIdentifierNamesNeverReachTheMarkup(t *testing.T) {
	const imp = "import { createSignal, createMemo } from '@krate/runtime';\n"
	sources := map[string]struct{ mod, local, arg string }{
		"module const":        {"const ZQinit = 5;", "", "ZQinit"},
		"module unknown":      {"const ZQinit = window.innerWidth;", "", "ZQinit"},
		"module random":       {"const ZQinit = Math.random();", "", "ZQinit"},
		"module object":       {"const ZQcfg = { start: 5 };", "", "ZQcfg.start"},
		"module computed":     {"const ZQa = 2;\nconst ZQinit = ZQa + 3;", "", "ZQinit"},
		"module typed":        {"const ZQinit: number = 5 as number;", "", "ZQinit"},
		"local const":         {"", "const ZQinit = 5;", "ZQinit"},
		"local unknown":       {"", "const ZQinit = localStorage.getItem('x');", "ZQinit"},
		"local shadows const": {"const ZQinit = 1;", "const ZQinit = 5;", "ZQinit"},
		"template":            {"const ZQa = 5;", "", "`v${ZQa}`"},
		"unresolved global":   {"", "", "ZQglobal"},
	}
	uses := map[string]string{
		"text":      "<p>{n()}</p>",
		"attr":      "<p title={n()} data-a={`a${n()}`}>x</p>",
		"expr":      "<p>{n() + 1}{String(n())}{'x' + n()}</p>",
		"cond":      "<p>{n() ? <b>y</b> : <i>z</i>}</p>",
		"handler":   "<button onClick={() => setN(n() + 1)}>{n()}</button>",
		"memo":      "<p>{double()}</p>",
		"list":      "<ul>{[1, 2].map((i) => <li key={i}>{n() + i}</li>)}</ul>",
		"derived":   "<p>{label}</p>",
		"two reads": "<p>{n()}-{n()}</p>",
		"branches":  "<div>{n() ? <b>{n()}-{label}</b> : <i>{n()}{label}</i>}</div>",
		"row text":  "<ul>{[1, 2].map((i) => <li key={i} title={n()}>{i}{n()}{label}</li>)}</ul>",
	}
	wrap := map[string]func(mod, local, arg, use string) string{
		"page": func(mod, local, arg, use string) string {
			return imp + mod + "\nexport default function P() { " + local + " " + body(arg, use) + " }\n"
		},
		"child": func(mod, local, arg, use string) string {
			return imp + mod + "\nfunction C() { " + local + " " + body(arg, use) + " }\nexport default function P() { return <main><C /></main>; }\n"
		},
		"grandchild": func(mod, local, arg, use string) string {
			return imp + mod + "\nfunction D() { " + local + " " + body(arg, use) + " }\nfunction C() { return <section><D /></section>; }\nexport default function P() { return <main><C /></main>; }\n"
		},
	}
	for wn, w := range wrap {
		for sn, s := range sources {
			for un, use := range uses {
				t.Run(wn+"/"+sn+"/"+un, func(t *testing.T) {
					html, logged := buildPlain(t, map[string]string{"src/pages/index.tsx": w(s.mod, s.local, s.arg, use)})
					if root := rootOf(t, html); strings.Contains(root, "ZQ") {
						t.Errorf("a variable name reached the markup:\n%s\nlog:\n%s", root, logged)
					}
				})
			}
		}
	}
}

func body(arg, use string) string {
	return "const [n, setN] = createSignal(" + arg + "); const double = createMemo(() => n() * 2); const label = n() ? 'on' : 'off'; return " + use + ";"
}

// A module constant seeds a signal in a child component the same way it does
// in the page: the first paint shows its value.
func TestModuleConstSeedsChildSignal(t *testing.T) {
	cases := map[string]string{
		"const": "const initial = 5;",
		"typed": "const initial: number = 5;",
		"sum":   "const base = 2;\nconst initial = base + 3;",
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			html, _ := buildPlain(t, map[string]string{"src/pages/index.tsx": "import { createSignal } from '@krate/runtime';\n" + mod +
				"\nfunction C() { const [n, setN] = createSignal(initial); return <button onClick={() => setN(n() + 1)}>{n()}</button>; }\nexport default function P() { return <main><C /></main>; }\n"})
			if want := "5<!--/k:3--></button>"; !strings.Contains(html, want) {
				t.Errorf("want %q in:\n%s", want, rootOf(t, html))
			}
		})
	}
}

// In a browser the child signal starts from its constant, or, when the build
// cannot know the value, from what the page computes on load.
func TestChildSignalSeedBrowser(t *testing.T) {
	page := `import { createSignal } from "@krate/runtime";
const known = 5;
const unknown = window.innerWidth > 0 ? 7 : 8;
function Known() { const [n, setN] = createSignal(known); return <button class="known" onClick={() => setN(n() + 1)}>{n()}</button>; }
function Unknown() { const [n, setN] = createSignal(unknown); return <button class="unknown" onClick={() => setN(n() + 1)}>{n()}</button>; }
export default function Page() { return <main><Known /><Unknown /></main>; }
`
	evalFixBrowser(t, map[string]string{"src/pages/index.tsx": page}, []evalFixCheck{
		{Path: "/", Selector: "button.known", Hydrated: "5", Click: "button.known", AfterClick: strp("6")},
		{Path: "/", Selector: "button.unknown", Static: strp(""), Hydrated: "7", Click: "button.unknown", AfterClick: strp("8")},
	})
}
