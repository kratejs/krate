package renderer

import "testing"

// Elements inside rows of a runtime-bound list are bound by the row render;
// the enclosing component must not also bind their handlers or attributes,
// and module constants the list reads are declared in its scope.
func TestListRowConditionalAndBindings(t *testing.T) {
	src := `import { useState } from 'react';
export default function Page() {
  const [open, setOpen] = useState([0]);
  const [items, setItems] = useState([{ q: 'a', a: 'A' }, { q: 'b', a: 'B' }]);
  return <div>{items().map((f, i) => {
    const isOpen = open.includes(i);
    return (
      <div key={f.q}>
        <button aria-expanded={isOpen} onClick={() => setOpen([i])}>{f.q}</button>
        {isOpen && <p>{f.a}</p>}
      </div>
    );
  })}</div>;
}`
	_, html, js := buildTreeReact(t, src)
	mustContain(t, "html", html, "<p>A</p>")
	mustContain(t, "js", js,
		"kbindList(",
		"'aria-expanded':()=>(open().includes(i()))",
		"()=>((open().includes(i())) && h('p'",
	)
	mustNotContain(t, "js", js, "kbindHandler(", "kbindAttr(")
}
