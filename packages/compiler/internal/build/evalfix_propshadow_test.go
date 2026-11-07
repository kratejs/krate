package build

import (
	"strings"
	"testing"
)

// A prop the caller omitted is undefined (or its default), even when a module
// constant, a local or a signal shares its name.
func TestOmittedPropIsNotShadowedByModuleValue(t *testing.T) {
	const imp = "import { createSignal } from '@krate/runtime';\n"
	const page = "export default function P() { const [n, setN] = createSignal(1); return <div><Card count={n()} /></div>; }\n"
	cases := map[string]struct{ src, want string }{
		"destructured default": {"const label = 'module';\nfunction Card({ label = 'dflt', count }) { return <p>{label}-{count}</p>; }\n", "<p>dflt-"},
		"body destructuring":   {"const label = 'module';\nfunction Card(props) { const { label = 'dflt', count } = props; return <p>{label}-{count}</p>; }\n", "<p>dflt-"},
		"props member default": {"const label = 'module';\nfunction Card(props) { return <p>{props.label ?? 'dflt'}-{props.count}</p>; }\n", "<p>dflt-"},
		"renamed default":      {"const label = 'module';\nfunction Card({ label: lbl = 'dflt', count }) { return <p>{lbl}-{count}</p>; }\n", "<p>dflt-"},
		"declared after":       {"function Card({ label = 'dflt', count }) { return <p>{label}-{count}</p>; }\nconst label = 'late';\n", "<p>dflt-"},
		"no default":           {"const label = 'module';\nfunction Card(props) { const a = props.label; return <p>[{a}]-{props.count}</p>; }\n", "<p>[]-"},
		"local named alike":    {"const label = 'module';\nfunction Card(props) { const label = props.label ?? 'local'; return <p>{label}|{props.label}</p>; }\n", "<p>local|"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			html, _ := buildPlain(t, map[string]string{"src/pages/index.tsx": imp + c.src + page})
			if !strings.Contains(html, c.want) || strings.Contains(html, "module") || strings.Contains(html, "late") {
				t.Errorf("want %q and no module value in:\n%s", c.want, rootOf(t, html))
			}
		})
	}
	// A prop that is passed still wins over the constant.
	html, _ := buildPlain(t, map[string]string{"src/pages/index.tsx": imp + "const label = 'module';\nfunction Card({ label = 'dflt', count }) { return <p>{label}-{count}</p>; }\n" +
		"export default function P() { const [n, setN] = createSignal(1); return <div><Card label=\"given\" count={n()} /></div>; }\n"})
	if !strings.Contains(html, "<p>given-") {
		t.Errorf("a passed prop must render:\n%s", rootOf(t, html))
	}
}

func TestOmittedPropIsNotShadowedBrowser(t *testing.T) {
	page := `import { createSignal } from "@krate/runtime";
const label = "module";
function Card({ label = "dflt", count }) { return <p class="out" onClick={() => {}}>{label}-{count}</p>; }
export default function Page() { const [n, setN] = createSignal(1); return <main><Card count={n()} /><button class="inc" onClick={() => setN(n() + 1)}>+</button></main>; }
`
	evalFixBrowser(t, map[string]string{"src/pages/index.tsx": page}, []evalFixCheck{
		{Path: "/", Selector: "p.out", Hydrated: "dflt-1", Click: "button.inc", AfterClick: strp("dflt-2")},
	})
}
