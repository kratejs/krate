package build

import (
	"fmt"
	"strings"
	"testing"
)

func collidingModule(tag string) string {
	return fmt.Sprintf(`function up() { return "helper-%[1]s"; }
const LABEL = "const-%[1]s";
let count = %[2]d;
function Row() { return <li>row-%[1]s</li>; }
export function Box%[1]s() { return <ul><Row /><li>{up()}</li><li>{LABEL}</li><li>{++count}</li></ul>; }
export default function Card() { return <p>card-%[1]s</p>; }
`, tag, map[string]int{"a": 10, "b": 20}[tag])
}

// Static HTML used to show one module's helper, constant, state and component
// for both modules.
func TestModulesKeepTheirPrivateNames(t *testing.T) {
	root, err := buildTierProject(t, map[string]string{
		"src/lib/a.tsx": collidingModule("a"),
		"src/lib/b.tsx": collidingModule("b"),
		"src/pages/index.tsx": `import CardA, { Boxa } from "../lib/a";
import CardB, { Boxb } from "../lib/b";
export default function Page() { return <main><Boxa /><Boxb /><CardA /><CardB /></main>; }
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	html := readTierDist(t, root, "index.html")
	for _, want := range []string{"row-a", "row-b", "helper-a", "helper-b", "const-a", "const-b", "<li>11</li>", "<li>21</li>", "card-a", "card-b"} {
		if !strings.Contains(html, want) {
			t.Errorf("page lacks %q:\n%s", want, html)
		}
	}
}

// Server bundles are built from source, where a renamed component still has its
// written name.
func TestRenamedAsyncComponentKeepsItsServerExport(t *testing.T) {
	module := func(tag string) string {
		return fmt.Sprintf("async function Data() { await 0; return <em>data-%[1]s</em>; }\nexport function Box%[1]s() { return <section><Data /></section>; }\n", tag)
	}
	_, err := buildTierProject(t, map[string]string{
		"src/lib/a.tsx": module("a"),
		"src/lib/b.tsx": module("b"),
		"src/pages/index.tsx": `import { Boxa } from "../lib/a";
import { Boxb } from "../lib/b";
export default function Page() { return <main><Boxa /><Boxb /></main>; }
`,
	})
	if err != nil {
		t.Fatal(err)
	}
}
