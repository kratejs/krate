package build

import (
	"strings"
	"testing"
)

func TestModuleJSXConstants(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  []string
	}{
		"report": {
			files: map[string]string{"src/pages/index.tsx": `
interface LegalSection { id: string; title: string; body: React.ReactNode }
const sections: LegalSection[] = [
  { id: 'terms', title: 'Terms', body: (<><p>Be nice.</p><p>Pay up.</p></>) },
  { id: 'privacy', title: 'Privacy', body: (<p>We <b>keep</b> data.</p>) },
];
export default function Page() {
  return <main>{sections.map(s => <section id={s.id}><h2>{s.title}</h2>{s.body}</section>)}</main>;
}`},
			want: []string{`<section id="terms"><h2>Terms</h2><p>Be nice.</p><p>Pay up.</p></section>`, `<p>We <b>keep</b> data.</p>`},
		},
		"report in a stateless component": {
			files: map[string]string{
				"src/legal.tsx": `
interface LegalSection { id: string; title: string; body: React.ReactNode }
const sections: LegalSection[] = [
  { id: 'terms', title: 'Terms', body: (<><p>Be nice.</p><p>Pay up.</p></>) },
];
export function Legal() {
  return <main>{sections.map(s => <section key={s.id} id={s.id}><h2>{s.title}</h2>{s.body}</section>)}</main>;
}`,
				"src/pages/index.tsx": `
import { Legal } from '../legal';
export default function Page() { return <Legal />; }`,
			},
			want: []string{`<section id="terms"><h2>Terms</h2><p>Be nice.</p><p>Pay up.</p></section>`},
		},
		"local derived in stateless component": {
			files: map[string]string{
				"src/legal.tsx": `
const sections = [
  { id: 'terms', title: 'Terms', body: (<><p>Be nice.</p></>) },
];
export function Legal(props: { heading: string }) {
  const shown = sections.filter(s => s.id);
  return <main><h1>{props.heading}</h1>{shown.map(s => <section key={s.id} id={s.id}><h2>{s.title}</h2>{s.body}</section>)}</main>;
}`,
				"src/pages/index.tsx": `
import { Legal } from '../legal';
export default function Page() { return <Legal heading="H" />; }`,
			},
			want: []string{`<section id="terms"><h2>Terms</h2><p>Be nice.</p></section>`},
		},
		"imported constant read in a stateless component": {
			files: map[string]string{
				"src/data.tsx": `
import { Badge } from './badge';
export const rows = [{ id: 'x', cell: <Badge text="B" /> }];`,
				"src/badge.tsx": `export function Badge(props: { text: string }) { return <b class="badge">{props.text}</b>; }`,
				"src/list.tsx": `
import { rows } from './data';
export function List(props: { n: number }) {
  const shown = rows.slice(0, props.n);
  return <ul>{shown.map(r => <li key={r.id}>{r.cell}</li>)}</ul>;
}`,
				"src/pages/index.tsx": `
import { List } from '../list';
export default function Page() { return <List n={1} />; }`,
			},
			want: []string{`<li><b class="badge">B</b></li>`},
		},
		"interactive page": {
			files: map[string]string{"src/pages/index.tsx": `
import { createSignal } from '@krate/runtime';
const sections = [{ id: 'a', body: <p>Alpha</p> }, { id: 'b', body: <p>Beta</p> }];
const footer = <footer>Foot</footer>;
export default function Page() {
  const [open, setOpen] = createSignal('a');
  return <main><button onClick={() => setOpen('b')}>{open()}</button>{sections.map(s => <div key={s.id} class={open() === s.id ? 'on' : 'off'}>{s.body}</div>)}{footer}</main>;
}`},
			want: []string{`<p>Alpha</p>`, `<p>Beta</p>`, `<footer>Foot</footer>`, `class="on"`},
		},
		"declared after, as const, refs other const, let, map": {
			files: map[string]string{"src/pages/index.tsx": `
export default function Page() {
  return <main>{items.map(i => <li key={i.id}>{i.icon}{i.label}</li>)}{all.map(a => <i>{a}</i>)}{cfg.node}</main>;
}
const brand = 'Acme';
const items = [{ id: 1, label: brand, icon: <span class="ic">*</span> }] as const;
let cfg = { node: <em>{brand}</em> };
const all = ['a', 'b'].map(x => x + brand);`},
			want: []string{`<span class="ic">*</span>Acme`, `<i>aAcme</i><i>bAcme</i>`, `<em>Acme</em>`},
		},
		"imported component and constant": {
			files: map[string]string{
				"src/data.tsx": `
import { Badge } from './badge';
export const prefix = 'P-';
export const rows = [{ id: 'x', cell: <Badge text={prefix + 'x'} /> }];`,
				"src/badge.tsx": `export function Badge(props: { text: string }) { return <b class="badge">{props.text}</b>; }`,
				"src/pages/index.tsx": `
import { rows } from '../data';
export default function Page() { return <ul>{rows.map(r => <li>{r.cell}</li>)}</ul>; }`,
			},
			want: []string{`<li><b class="badge">P-x</b></li>`},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			html, js, err := buildKrateProject(t, tc.files)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			for _, w := range tc.want {
				if !strings.Contains(html, w) {
					t.Errorf("html misses %s:\n%s", w, html)
				}
			}
			if name == "interactive page" && !strings.Contains(js, "kbindAttr") {
				t.Errorf("interactive page lost its hydration bindings:\n%s", js)
			}
		})
	}
}
