package build

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func buildPageJS(t *testing.T, files map[string]string) (html, js string) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		writeFileRel(t, root, rel, content)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	if out, err := captureBuild(New(root, cfg)); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	html = readOut(t, cfg.OutDir, "index.html")
	matches, _ := filepath.Glob(filepath.Join(cfg.OutDir, "index*.js"))
	if len(matches) > 0 {
		js = readOutput(t, cfg, "index*.js")
	}
	return html, js
}

func TestLocalFnReadingSignalHydratesAndStaticOneFolds(t *testing.T) {
	t.Parallel()
	html, js := buildPageJS(t, map[string]string{
		"src/components/panel.tsx": `export function Panel(props: { title: string; children?: any }) {
  return <section class="panel"><h2>{props.title}</h2>{props.children}</section>;
}`,
		"src/pages/index.tsx": `import { createSignal } from '@krate/runtime';
import { Panel } from '../components/panel';
const list = [{ label: "alpha" }, { label: "beta" }];
export default function Page() {
  const [n, setN] = createSignal(0);
  const [open, setOpen] = createSignal(false);
  const cur = () => list[n()];
  const cls = (i: number) => (n() === i ? "on" : "");
  const fixed = () => list[1].label;
  return <div>
    <p>{cur().label}</p><i>{fixed()}</i>
    {list.map((x, i) => <button class={cls(i)} onClick={() => setN(i)}>{x.label}</button>)}
    <Panel title="t">{open() ? "Open" : "Closed"}</Panel>
    <button onClick={() => setOpen(!open())}>toggle</button>
  </div>;
}`,
	})
	if !strings.Contains(html, "alpha") || !strings.Contains(html, "Closed") || !strings.Contains(html, "<i>beta</i>") {
		t.Fatalf("unexpected html:\n%s", html)
	}
	for _, want := range []string{"cur().label", "cls(", "'Open':'Closed'"} {
		if !strings.Contains(js, want) {
			t.Errorf("hydration JS missing %q:\n%s", want, js)
		}
	}
	if strings.Contains(js, "fixed(") {
		t.Errorf("non-reactive local function call must fold, but hydration JS references it:\n%s", js)
	}
}
