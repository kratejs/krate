package renderer

import (
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// buildTreeReact runs parse, React rewrite, annotate and IR build, returning
// the tree (for errors/warnings) plus the emitted HTML and hydration JS.
func buildTreeReact(t *testing.T, src string) (*irtree.ComponentTree, string, string) {
	t.Helper()
	prog := parseProg(t, src)
	bundler.RewriteReact(prog)
	ann := annotator.Annotate(prog, &config.Config{}, "page.tsx", src)
	tree := irtree.Build(prog, ann)
	result := NewEmitter().Emit(tree)
	EmitMeta(tree, result)
	return tree, result.HTML, GenerateNewHydrationJS(result)
}

func mustContain(t *testing.T, label, haystack string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(haystack, w) {
			t.Errorf("%s: missing %q in:\n%s", label, w, haystack)
		}
	}
}

func mustNotContain(t *testing.T, label, haystack string, bad ...string) {
	t.Helper()
	for _, w := range bad {
		if strings.Contains(haystack, w) {
			t.Errorf("%s: unexpected %q in:\n%s", label, w, haystack)
		}
	}
}

// Determinism: the same tree built repeatedly (and under different
// GOMAXPROCS) yields byte-identical HTML and hydration JS. Child props
// registrations used to follow map iteration order.
func TestBuildOutputDeterministic(t *testing.T) {
	src := `import { createSignal } from "@krate/runtime";
function Card(props) {
  const [open, setOpen] = createSignal(false);
  return <div class={props.tone} title={props.title} data-a={props.a} data-b={props.b} onClick={() => setOpen(!open())}>{props.label}{open() ? "x" : "y"}</div>;
}
export default function App() {
  const [n, setN] = createSignal(0);
  return <section>
    <Card label="one" tone="red" title="t1" a="1" b="2" />
    <Card label="two" tone="blue" title="t2" a="3" b={String(n())} />
    <button onClick={() => setN(n() + 1)}>+</button>
  </section>;
}`
	prev := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(prev)
	var wantHTML, wantJS string
	for i, procs := range []int{1, 2, 8, 1, 4, 16, 1, 8} {
		runtime.GOMAXPROCS(procs)
		for j := 0; j < 4; j++ {
			_, html, js := buildTreeReact(t, src)
			if i == 0 && j == 0 {
				wantHTML, wantJS = html, js
				if !strings.Contains(js, `__krate_props[`) {
					t.Fatalf("fixture must register child props:\n%s", js)
				}
				continue
			}
			if html != wantHTML || js != wantJS {
				t.Fatalf("build %d/%d (GOMAXPROCS=%d) differs:\n--- first\n%s\n--- now\n%s", i, j, procs, wantJS, js)
			}
		}
	}
}
