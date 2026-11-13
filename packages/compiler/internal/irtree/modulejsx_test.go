package irtree_test

import "testing"

func TestModuleJSXConstantsRenderStatically(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "array of objects with JSX bodies",
			src: `const sections: Section[] = [{ id: 'a', body: (<><p>x</p></>) }, { id: 'b', body: <i>y</i> }] as const;
export default function App() {
	return <div>{sections.map(s => <section key={s.id}>{s.body}</section>)}</div>;
}`,
			want: `<div><section><p>x</p></section><section><i>y</i></section></div>`,
		},
		{
			name: "declared after use, member path and index",
			src: `export default function App() {
	return <div>{cfg.node}{rows[1]}</div>;
}
const cfg = { node: <em>n</em> };
const rows = [<a>0</a>, <b>1</b>];`,
			want: `<div><em>n</em><b>1</b></div>`,
		},
		{
			name: "local shadows the module constant",
			src: `const items = [{ v: <b>mod</b> }];
export default function App() {
	const items = [{ v: 'local' }];
	return <div>{items.map(x => <p>{x.v}</p>)}</div>;
}`,
			want: `<div><p>local</p></div>`,
		},
		{
			name: "local shadows a name the constant uses",
			src: `const year = 2026;
const footer = <p>{year}</p>;
export default function P() {
	const year = 1999;
	return <main>{footer}</main>;
}`,
			want: `<main></main>`,
		},
		{
			name: "map callback parameter shadows a name the constant uses",
			src: `const n = 'mod';
const tag = { v: <b>{n}</b> };
export default function P() {
	return <main>{['cb'].map(n => <i>{tag.v}</i>)}</main>;
}`,
			want: `<main><i><b>mod</b></i></main>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := buildStaticTreeWithJS(t, tc.src)
			if got := renderStaticHTML(tree.Root.Children); got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}
