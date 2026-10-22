package renderer

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

func findList(slots []irtree.SlotNode) *irtree.ListSlot {
	for _, s := range slots {
		switch v := s.(type) {
		case *irtree.ListSlot:
			return v
		case *irtree.ConditionalSlot:
			if l := findList(v.Consequent); l != nil {
				return l
			}
		}
	}
	return nil
}

func listWarningCodes(tree *irtree.ComponentTree) []string {
	var out []string
	for _, w := range tree.Warnings {
		out = append(out, string(w.Code))
	}
	return out
}

func TestKeyedListKeyExtraction(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		markers []string
		keyJS   string
	}{
		{"member", "t.id", []string{"<!--k:2:$1-->", "<!--k:2:$2-->"}, "(t)=>t.id"},
		{"template", "`row-${t.id}`", []string{"<!--k:2:$row-1-->", "<!--k:2:$row-2-->"}, "`row-${t.id}`"},
		{"number literal on item", "t.id * 10", []string{"<!--k:2:$10-->", "<!--k:2:$20-->"}, "(t.id * 10)"},
		{"string needing comment encoding", "t.name", []string{"<!--k:2:$a%3Eb-->", "<!--k:2:$c-->"}, "(t)=>t.name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := `import { useState } from 'react';
export default function Page() {
  const [items, setItems] = useState([{ id: 1, name: 'a>b' }, { id: 2, name: 'c' }]);
  return <ul>{items.map((t) => <li key={` + tc.key + `}>{t.name}</li>)}</ul>;
}`
			tree, html, js := buildTreeReact(t, src)
			list := findList(tree.Root.Children)
			if list == nil || !list.Keyed || list.Bind == nil {
				t.Fatalf("expected a keyed list binding, got %+v", list)
			}
			mustContain(t, "html", html, tc.markers...)
			mustContain(t, "js", js, "kbindList(", tc.keyJS, "()=>t().name")
			mustNotContain(t, "js", js, "kbindContent(")
			if len(listWarningCodes(tree)) != 0 {
				t.Errorf("unexpected warnings: %v", tree.Warnings)
			}
		})
	}
}

func TestKeyedFragmentAndNestedLists(t *testing.T) {
	src := `import { Fragment, useState } from 'react';
export default function Page() {
  const [groups, setGroups] = useState([{ id: 'g', items: ['x', 'y'] }]);
  return <dl>{groups.map((g) => (
    <Fragment key={g.id}>
      <dt>{g.id}</dt>
      <dd>{g.items.map((s) => <b key={s}>{s}</b>)}</dd>
    </Fragment>
  ))}</dl>;
}`
	_, html, js := buildTreeReact(t, src)
	mustContain(t, "html", html, "<!--k:2:$g--><dt>g</dt><dd>", "<!--k:", ":$x--><b>x</b>")
	mustContain(t, "js", js, "(g)=>g.id", "krateList(()=>(g().items),(s)=>s,")
}

func TestListKeyDiagnostics(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"missing key on a dynamic list", `{items.map((t) => <li>{t}</li>)}`, []string{string(diag.CodeListMissingKey)}},
		{"keyed list", `{items.map((t) => <li key={t}>{t}</li>)}`, nil},
		{"text rows need no key", `{items.map((t) => t + ',')}`, nil},
		{"static list is not reported", `{['a', 'b'].map((t) => <li>{t}</li>)}`, nil},
		{"duplicate keys", `{items.map((t) => <li key={t.length}>{t}</li>)}`, []string{string(diag.CodeListDuplicateKey)}},
		{"partial keys", `{items.map((t) => (t === 'a' ? <li key={t}>{t}</li> : <i>{t}</i>))}`, []string{string(diag.CodeListPartialKey)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := `import { useState } from 'react';
export default function Page() {
  const [items, setItems] = useState(['a', 'b']);
  return <ul>` + tc.body + `</ul>;
}`
			tree, _, _ := buildTreeReact(t, src)
			got := listWarningCodes(tree)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("warning codes = %v, want %v (%v)", got, tc.want, tree.Warnings)
			}
			for _, w := range tree.Warnings {
				if w.Hint == "" {
					t.Errorf("%s has no hint", w.Code)
				}
			}
		})
	}
}

func TestDuplicateServerKeysGetOccurrenceSuffix(t *testing.T) {
	src := `import { useState } from 'react';
export default function Page() {
  const [items, setItems] = useState(['a', 'a']);
  return <ul>{items.map((t) => <li key={t}>{t}</li>)}</ul>;
}`
	_, html, _ := buildTreeReact(t, src)
	mustContain(t, "html", html, "<!--k:2:$a-->", "<!--k:2:$a%001-->")
}

func TestKeyedRemount(t *testing.T) {
	src := `import { useState } from 'react';
function Profile(props) {
  const [note, setNote] = useState('');
  return <input value={note} data-user={props.user} onChange={(e) => setNote(e.target.value)} />;
}
export default function Page() {
  const [user, setUser] = useState('ada');
  return <div><button onClick={() => setUser('grace')}>x</button><Profile key={user} user={user} /></div>;
}`
	tree, html, js := buildTreeReact(t, src)
	list := findList(tree.Root.Children)
	if list == nil || list.Bind == nil || list.Bind.Source != "[user()]" {
		t.Fatalf("expected a one-row keyed list for the keyed element, got %+v", list)
	}
	mustContain(t, "html", html, "$ada-->", `data-user="ada"`)
	mustContain(t, "js", js, "kbindList(", "()=>([user()]),(k)=>k,", "get user() {")
	// The row component is rendered by the list runtime, not hydrated on its own.
	mustNotContain(t, "js", js, "kbindProp(")
}

func TestListRowsAreNotHydratedTwice(t *testing.T) {
	src := `import { useState } from 'react';
function Row({ item, onPick = () => {} }) {
  const [n, setN] = useState(0);
  const label = item.title.toUpperCase();
  return <li onClick={() => { setN(n + 1); onPick(item.id); }}>{label} {n}</li>;
}
export default function Page() {
  const [items, setItems] = useState([{ id: 1, title: 'a' }]);
  return <ul>{items.map((it) => <Row key={it.id} item={it} />)}</ul>;
}`
	_, _, js := buildTreeReact(t, src)
	mustContain(t, "js", js,
		// Destructured props read the live props object; the derived local is
		// inlined so it updates with the item.
		"function Row(__kp)",
		"__kp.item.title.toUpperCase()",
		"get item() {",
	)
	mustNotContain(t, "js", js, "kbindText(", "kbindHandler(")
}

func TestUnkeyedListUsesIndexRows(t *testing.T) {
	src := `import { useState } from 'react';
export default function Page() {
  const [items, setItems] = useState(['a', 'b']);
  return <ul>{items.map((t, i) => <li key={i}>{i}:{t}</li>)}</ul>;
}`
	_, html, js := buildTreeReact(t, src)
	mustContain(t, "html", html, "<!--k:2:$0-->", "<!--k:2:$1-->")
	mustContain(t, "js", js, "()=>i()", "()=>t()")
}
