package build

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// A dynamic-route page that renders a component with a signal builds, and
// the component hydrates. The page used to be evaluated whole at build time
// (to bind its route params), which ran the child's createSignal there:
// "cannot evaluate `n()` at build time: unresolved identifier createSignal".
func TestDynamicRoutePageWithHydratingChild(t *testing.T) {
	counter := "import { createSignal } from '@krate/runtime';\n" +
		"export default function Counter() { const [n, setN] = createSignal(0); return <button id=\"c\" onClick={() => setN(n() + 1)}>count {n()}</button>; }\n"
	cases := map[string]string{
		"array destructuring":  "import Counter from '../c';\nexport default function P({ params }) { const [first] = [1]; return <main><h1>{params.id}-{first}</h1><Counter /></main>; }\n",
		"plain":                "import Counter from '../c';\nexport default function P({ params }) { return <main><h1>{params.id}</h1><Counter /></main>; }\n",
		"nested in an element": "import Counter from '../c';\nexport default function P({ params }) { return <main><section><h1>{params.id}</h1><div><Counter /></div></section></main>; }\n",
	}
	for name, page := range cases {
		t.Run(name, func(t *testing.T) {
			_, out, logged := buildFiles(t, map[string]string{
				"src/c.tsx":          counter,
				"src/pages/[id].tsx": page,
			}, func(c *config.Config) { c.Tailwind.Enabled = false; c.Tailwind.Strict = false })
			html := readBuilt(t, filepath.Join(out, "[id]", "index.html"))
			if !strings.Contains(html, "<button data-k=") || !strings.Contains(html, "count <!--k:") {
				t.Errorf("the counter was not emitted with its hydration markers:\n%s", html)
			}
			if strings.Contains(logged, "cannot evaluate") {
				t.Errorf("unexpected build-time evaluation error:\n%s", logged)
			}
		})
	}
}
