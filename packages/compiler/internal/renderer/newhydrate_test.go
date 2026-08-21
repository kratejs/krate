package renderer

import (
	"strings"
	"testing"
)

// ─── Slot bindings in hydration JS ──────────────────────────────────────────

func TestHydrationTextSlotBinding(t *testing.T) {
	src := `export default function Page() {
  const [count, setCount] = createSignal(0);
  return <div>{count()}</div>;
}`
	_, js := fullPipeline(t, src)
	if !strings.Contains(js, "kbindText(") {
		t.Errorf("expected kbindText binding for text slot, got:\n%s", js)
	}
	if !strings.Contains(js, "refreshSlots(") {
		t.Errorf("expected refreshSlots() call to rebuild the slot cache, got:\n%s", js)
	}
}

func TestHydrationExprSlotBinding(t *testing.T) {
	src := `export default function Page() {
  const [x, setX] = createSignal(5);
  return <div>{x() > 3 ? "big" : "small"}</div>;
}`
	_, js := fullPipeline(t, src)
	if !strings.Contains(js, "kbindContent(") {
		t.Errorf("expected kbindContent binding for expr slot, got:\n%s", js)
	}
}

func TestHydrationListSlotBinding(t *testing.T) {
	src := `export default function Page() {
  const [items, setItems] = createSignal(["a","b"]);
  return <ul>{items().map(i => <li>{i}</li>)}</ul>;
}`
	_, js := fullPipeline(t, src)
	if !strings.Contains(js, "kbindContent(") {
		t.Errorf("expected kbindContent binding for list slot, got:\n%s", js)
	}
}

func TestHydrationConditionalSlotBinding(t *testing.T) {
	src := `export default function Page() {
  const [show, setShow] = createSignal(false);
  return <div>{show() ? <span>yes</span> : <span>no</span>}</div>;
}`
	_, js := fullPipeline(t, src)
	if !strings.Contains(js, "kbindCond(") {
		t.Errorf("expected kbindCond binding for conditional slot, got:\n%s", js)
	}
}

// ─── Attribute bindings ─────────────────────────────────────────────────────

func TestHydrationAttrBindings(t *testing.T) {
	src := `export default function Page() {
  const [open, setOpen] = createSignal(false);
  return <div data-state={open() ? "open" : "closed"} class={open() ? "a" : "b"}></div>;
}`
	_, js := fullPipeline(t, src)
	if !strings.Contains(js, "kbindAttr(") {
		t.Errorf("expected kbindAttr binding, got:\n%s", js)
	}
	if !strings.Contains(js, `"data-state"`) {
		t.Errorf("expected data-state attribute binding, got:\n%s", js)
	}
	if !strings.Contains(js, `"class"`) {
		t.Errorf("expected class attribute binding, got:\n%s", js)
	}
	if strings.Count(js, "kbindAttr(") != 2 {
		t.Errorf("expected 2 attribute bindings, got %d:\n%s", strings.Count(js, "kbindAttr("), js)
	}
}

// ─── Handler properties & delegation ────────────────────────────────────────

func TestHydrationHandlerPropNaming(t *testing.T) {
	src := `export default function Page() {
  const [n, setN] = createSignal(0);
  return <button id="inc" onClick={() => setN(n + 1)}>+</button>;
}`
	_, js := fullPipeline(t, src)
	if !strings.Contains(js, "__krate_click_") {
		t.Errorf("expected __krate_click_ handler property, got:\n%s", js)
	}
}

func TestHydrationEventDelegationDistinctness(t *testing.T) {
	// Two elements, both onClick, plus one onInput: one delegated listener per
	// distinct event type, and a handler property for each element.
	src := `export default function Page() {
  const [a, setA] = createSignal(0);
  const [b, setB] = createSignal(0);
  return <div>
    <button id="x" onClick={() => setA(a + 1)}>x</button>
    <button id="y" onClick={() => setB(b + 1)}>y</button>
    <input id="z" onInput={() => {}} />
  </div>;
}`
	_, js := fullPipeline(t, src)
	if strings.Count(js, "__krate_del_add('click'") != 1 {
		t.Errorf("expected exactly ONE delegated click listener, got %d:\n%s",
			strings.Count(js, "__krate_del_add('click'"), js)
	}
	if strings.Count(js, "__krate_del_add('input'") != 1 {
		t.Errorf("expected exactly ONE delegated input listener, got %d:\n%s",
			strings.Count(js, "__krate_del_add('input'"), js)
	}
	// Two click handler properties (one per button), passed to kbindHandler
	// as quoted prop names.
	if got := strings.Count(js, `"__krate_click_`); got != 2 {
		t.Errorf("expected 2 click handler properties, got %d:\n%s", got, js)
	}
}

// ─── XSS sanitizer presence ─────────────────────────────────────────────────

func TestHydrationIncludesEscSanitizer(t *testing.T) {
	src := `export default function Page() {
  const [msg, setMsg] = createSignal("hi");
  return <div>{msg()}</div>;
}`
	_, js := fullPipeline(t, src)
	if strings.Contains(js, "$esc") {
		t.Errorf("$esc should live in the shared chunk bootstrap, not page JS:\n%s", js)
	}
	if !strings.Contains(HydrationBootstrapJS, "$esc") {
		t.Error("expected $esc sanitizer in HydrationBootstrapJS")
	}
}

// ─── Per-component scoped IIFEs ─────────────────────────────────────────────

func TestHydrationPerComponentScopes(t *testing.T) {
	src := `function Counter() {
  const [c, setC] = createSignal(0);
  return <button onClick={() => setC(c + 1)}>{c()}</button>;
}
export default function Page() {
  return <div><Counter /><Counter /></div>;
}`
	_, js := fullPipeline(t, src)
	// Two Counter instances must not collide: each declares its own
	// createSignal(0) inside its own IIFE scope (no signal mangling).
	if strings.Count(js, "=createSignal(0);") != 2 {
		t.Errorf("expected 2 scoped createSignal declarations, got %d:\n%s",
			strings.Count(js, "=createSignal(0);"), js)
	}
	if strings.Contains(js, "_c0") && strings.Contains(js, "count_c0") {
		t.Errorf("signal mangling (_c0) should not appear in scoped IIFEs:\n%s", js)
	}
}

// ─── Router / reinit ────────────────────────────────────────────────────────

func TestHydrationRouterInit(t *testing.T) {
	src := `export default function Page() {
  return <div><Link href="/about">About</Link></div>;
}`
	_, js := fullPipeline(t, src)
	if !strings.Contains(js, "reinitRouter") && !strings.Contains(js, "initRouter") {
		t.Errorf("expected router init in hydration JS, got:\n%s", js)
	}
}

// ─── Unit: handler property sanitization ────────────────────────────────────

func TestSanitizeHandlerProp(t *testing.T) {
	cases := map[string]string{
		"page.counter.btn":      "page_counter_btn",
		"page.items:0.del":      "page_items_0_del",
		"page.content-slot.x":   "page_content_slot_x",
		"already_underscored":   "already_underscored",
	}
	for in, want := range cases {
		if got := sanitizeHandlerProp(in); got != want {
			t.Errorf("sanitizeHandlerProp(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsBareStringToken(t *testing.T) {
	for _, lit := range []string{"true", "false", "null", "undefined", "NaN", "Infinity", ""} {
		if isBareStringToken(lit) {
			t.Errorf("isBareStringToken(%q) = true, want false", lit)
		}
	}
	for _, ident := range []string{"count", "_x", "$y", "helloWorld1"} {
		if !isBareStringToken(ident) {
			t.Errorf("isBareStringToken(%q) = false, want true", ident)
		}
	}
	if isBareStringToken("not an ident") {
		t.Error("spaced string should not be a bare token")
	}
}
