package renderer

import (
	"testing"

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
