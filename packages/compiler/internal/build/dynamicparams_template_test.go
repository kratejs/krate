package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const itemGSP = `export function generateStaticParams() { return [{ item: 'a' }, { item: 'b' }]; }
`

func distHas(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, "dist", filepath.FromSlash(rel)))
	return err == nil
}

// A prerendered dynamic route with generateStaticParams and no request-time
// consumer writes only the concrete pages, not the [param] template.
func TestDynamicRouteTemplateDroppedWithoutConsumer(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, map[string]string{
		"src/pages/compare/[item].tsx": itemGSP + `export default function Compare() { return <h1>Compare</h1>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"compare/a/index.html", "compare/b/index.html"} {
		if !distHas(root, rel) {
			t.Errorf("%s is missing", rel)
		}
	}
	if distHas(root, "compare/[item]/index.html") {
		t.Error("the [item] template was written")
	}
	m := readManifest(t, root)
	found := false
	for _, r := range m.StaticOnlyRoutes {
		if strings.Contains(r, "[item]") {
			found = true
		}
	}
	if !found {
		t.Errorf("manifest staticOnlyRoutes lacks the route: %v", m.StaticOnlyRoutes)
	}
}

// An explicit dynamicParams = true keeps the fallback shell.
func TestDynamicRouteTemplateKeptWithExplicitDynamicParams(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, map[string]string{
		"src/pages/compare/[item].tsx": itemGSP + `export const dynamicParams = true;
export default function Compare() { return <h1>Compare</h1>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	if !distHas(root, "compare/[item]/index.html") {
		t.Error("the [item] template should be written with dynamicParams = true")
	}
}

// A request-time page (ISR) keeps its fallback shell.
func TestDynamicRouteTemplateKeptForISR(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, map[string]string{
		"src/pages/compare/[item].tsx": itemGSP + `export const revalidate = 60;
export default function Compare() { return <h1>Compare</h1>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	if !distHas(root, "compare/[item]/index.html") {
		t.Error("the [item] template should be written for an ISR page")
	}
}

// A dynamic route without generateStaticParams keeps its template.
func TestDynamicRouteTemplateKeptWithoutGSP(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, map[string]string{
		"src/pages/compare/[item].tsx": `export default function Compare() { return <h1>Compare</h1>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	if !distHas(root, "compare/[item]/index.html") {
		t.Error("the [item] template should be written without generateStaticParams")
	}
}

func readPageJS(t *testing.T, root, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "dist", dir))
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".js") {
			data, err := os.ReadFile(filepath.Join(root, "dist", dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			sb.Write(data)
		}
	}
	return sb.String()
}

const panelModule = `function Frame({ children }: { children?: any }) { return <section class="frame">{children}</section>; }
export function FirstPanel() { return <Frame><p>first panel</p></Frame>; }
export function SecondPanel() { return <Frame><p>second panel</p></Frame>; }
export const panels = { first: FirstPanel, second: SecondPanel };
export const views = [{ id: 1, view: FirstPanel }, { id: 2, view: SecondPanel }];
`

// A component read from a table of components (`panels[key]`) renders by
// lowering to a conditional over the table's members, and the members' helper
// components ship with the page script.
func TestComponentTableLookupShipsHelpers(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, map[string]string{
		"src/components/panels.tsx": panelModule,
		"src/pages/lookup.tsx": `import { useState } from 'react';
import { panels } from '../components/panels';
export default function Page() {
  const [key, setKey] = useState<'first' | 'second'>('first');
  const P = panels[key];
  return <main><button onClick={() => setKey('second')}>go</button><P /></main>;
}`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	js := readPageJS(t, root, "lookup")
	for _, want := range []string{"first panel", "second panel", `class:"frame"`} {
		if !strings.Contains(js, want) {
			t.Errorf("page script lacks %q:\n%s", want, js)
		}
	}
}

// A member tag and a tag held in a row-local variable render each row's
// component, with the components' helpers in the page script.
func TestComponentFieldTagInListRows(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, map[string]string{
		"src/components/panels.tsx": panelModule,
		"src/pages/rows.tsx": `import { useState } from 'react';
import { views } from '../components/panels';
export default function Page() {
  const [n, setN] = useState(0);
  return <main><button onClick={() => setN(n + 1)}>n={n}</button>
    {views.map((it) => <it.view key={it.id} />)}
    {views.map((item) => { const View = item.view; return <View key={item.id} />; })}
  </main>;
}`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	js := readPageJS(t, root, "rows")
	if strings.Contains(js, `"it.view"`) || strings.Contains(js, `'it.view'`) {
		t.Errorf("a member tag is emitted as a string:\n%s", js)
	}
	if !strings.Contains(js, `class:"frame"`) {
		t.Errorf("the helper component is missing from the page script:\n%s", js)
	}
	if !strings.Contains(js, "().view") {
		t.Errorf("rows do not read the component through the row accessor:\n%s", js)
	}
}
