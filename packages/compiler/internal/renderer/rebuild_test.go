package renderer

import (
	"strings"
	"testing"
)

// TestRebuildInOneProcessIsIdentical builds the same tree twice in one
// process, with sibling components that install early-return, provider and
// local-folding state between plain siblings, and requires byte-identical
// output: no builder state may survive one build or leak between siblings.
func TestRebuildInOneProcessIsIdentical(t *testing.T) {
	src := `
const Theme = createContext("light");
function Early(props) {
  const [n, setN] = createSignal(0);
  if (!props.show) return <p class="early">hidden</p>;
  return <p class="early" onClick={() => setN(n() + 1)}>{n()}</p>;
}
function ThemeBox() {
  const [t, setT] = createSignal("dark");
  return <Theme.Provider value={t()}><section onClick={() => setT("x")}><Plain k="c" /></section></Theme.Provider>;
}
function Plain(props) {
  const [c, setC] = createSignal(1);
  const items = [props.k];
  items.push("z");
  return <button title={items.join("-")} onClick={() => setC(c() + 1)}>{c()}</button>;
}
export default function Page() {
  return <main>
    <Plain k="a" />
    <Early show={false} />
    <Plain k="b" />
    <ThemeBox />
    <Plain k="d" />
  </main>;
}
`
	res1, js1 := fullPipeline(t, src)
	res2, js2 := fullPipeline(t, src)
	if res1.HTML != res2.HTML {
		t.Fatalf("HTML differs between builds:\n%s\n---\n%s", res1.HTML, res2.HTML)
	}
	if js1 != js2 {
		t.Fatalf("hydration JS differs between builds:\n%s\n---\n%s", js1, js2)
	}
	for _, want := range []string{`title="a-z"`, `title="b-z"`, `title="c-z"`, `title="d-z"`, "hidden"} {
		if !strings.Contains(res1.HTML, want) {
			t.Errorf("missing %s in:\n%s", want, res1.HTML)
		}
	}
}
