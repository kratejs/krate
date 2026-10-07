package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// buildExprProject builds a single-page project with the given page source
// (and optional extra files) and returns the rendered index.html.
func buildExprProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, src := range files {
		writeTestFile(t, root, rel, src)
	}
	cfg := config.Default()
	cfg.Resolve(root)
	cfg.Minify = false
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestBuildArrayJoinFolds verifies `arr.join(sep)` is folded at build time —
// including when the array arrives as a component prop (the docs contributors
// case). Before the fix the join returned the raw array-literal source (or ""
// for a client component) instead of the joined text.
func TestBuildArrayJoinFolds(t *testing.T) {
	// Signal-less component: array prop passed directly.
	out := buildExprProject(t, map[string]string{
		"src/pages/index.tsx": `
function Child(props: { authors: string[] }) {
  return <p>{props.authors.join(", ")}|{props.authors.length}</p>;
}
export default function Page() {
  return <Child authors={["A", "B", "C"]} />;
}`,
	})
	if !strings.Contains(out, "A, B, C|3") {
		t.Fatalf("join of an array prop should fold to \"A, B, C|3\"; got:\n%s", rootDiv(out))
	}
}

// TestBuildArrayJoinFoldsClient verifies the same fold happens for a CLIENT
// component (signals), rendered into static HTML at build — the exact path the
// docs theme layout takes.
func TestBuildArrayJoinFoldsClient(t *testing.T) {
	out := buildExprProject(t, map[string]string{
		"src/components/child.tsx": `
import { createSignal } from "@krate/runtime";
export default function Child(props: { authors: string[] }) {
  const [n] = createSignal(0);
  return <p>{props.authors.join(", ")}:{props.authors.length}</p>;
}
`,
		"src/pages/index.tsx": `
import Child from "../components/child";
export default function Page() {
  const p = { authors: ["Ada", "Linus"] };
  return <Child {...p} />;
}`,
	})
	if !strings.Contains(out, "Ada, Linus:2") {
		t.Fatalf("client component join should fold to \"Ada, Linus:2\"; got:\n%s", rootDiv(out))
	}
}

// TestBuildArrayMapStringUnquoted verifies `.map` over a prop array of strings
// renders the elements unquoted (a serialized literal element like `'one'` must
// become `one`, not the quoted source).
func TestBuildArrayMapStringUnquoted(t *testing.T) {
	out := buildExprProject(t, map[string]string{
		"src/pages/index.tsx": `
function List(props: { items: string[] }) {
  return <ul>{props.items.map((x) => <li>{x}</li>)}</ul>;
}
export default function Page() {
  return <List items={["one", "two"]} />;
}`,
	})
	if !strings.Contains(out, "<li>one</li><li>two</li>") {
		t.Fatalf("mapped string items should be unquoted; got:\n%s", rootDiv(out))
	}
	if strings.Contains(out, "&#39;one&#39;") {
		t.Fatalf("mapped string items leaked their quotes; got:\n%s", rootDiv(out))
	}
}

func rootDiv(out string) string {
	if i := strings.Index(out, `<div id="root">`); i >= 0 {
		return out[i:]
	}
	return out
}
