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
		"module const":      {"const ZQinit = 5;", "", "ZQinit"},
		"module unknown":    {"const ZQinit = window.innerWidth;", "", "ZQinit"},
		"local const":       {"", "const ZQinit = 5;", "ZQinit"},
		"local unknown":     {"", "const ZQinit = localStorage.getItem('x');", "ZQinit"},
		"unresolved global": {"", "", "ZQglobal"},
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
