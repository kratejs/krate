package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestEventDelegationBubbles verifies the generated delegated listener walks
// every ancestor (bubbling) instead of stopping at the first handler.
func TestEventDelegationBubbles(t *testing.T) {
	page := `
		import { createSignal } from '@krate/runtime';
		export default function Page() {
			const [n, setN] = createSignal(0);
			return <div onClick={() => setN(n() + 1)}><button onClick={() => setN(n() + 2)}>x</button></div>;
		}
	`
	_, js := buildReactPage(t, page)
	if strings.Contains(js, "if(_found)break") {
		t.Errorf("delegated listener still stops at the first handler:\n%s", js)
	}
	if !strings.Contains(js, "e.cancelBubble") {
		t.Errorf("delegated listener should honour stopPropagation:\n%s", js)
	}
}

// TestNonBubblingEventUsesDirectListener verifies mouseenter (non-bubbling) is
// attached directly with kbindEvent rather than delegated.
func TestNonBubblingEventUsesDirectListener(t *testing.T) {
	page := `
		import { createSignal } from '@krate/runtime';
		export default function Page() {
			const [n, setN] = createSignal(0);
			return <div onMouseEnter={() => setN(n() + 1)}>{n()}</div>;
		}
	`
	_, js := buildReactPage(t, page)
	if !strings.Contains(js, `kbindEvent(`) || !strings.Contains(js, `"mouseenter"`) {
		t.Errorf("onMouseEnter should use a direct listener:\n%s", js)
	}
}

// TestCaptureEventUsesCaptureListener verifies onClickCapture becomes a
// capture-phase direct listener (event name without the Capture suffix).
func TestCaptureEventUsesCaptureListener(t *testing.T) {
	page := `
		import { createSignal } from '@krate/runtime';
		export default function Page() {
			const [n, setN] = createSignal(0);
			return <button onClickCapture={() => setN(n() + 1)}>{n()}</button>;
		}
	`
	_, js := buildReactPage(t, page)
	if !strings.Contains(js, `"click"`) || !strings.Contains(js, ",true)") {
		t.Errorf("onClickCapture should use a capture direct listener:\n%s", js)
	}
	if strings.Contains(js, "clickcapture") {
		t.Errorf("capture suffix leaked into the DOM event name:\n%s", js)
	}
}

// TestFocusMapsToFocusin verifies onFocus uses the bubbling focusin event.
func TestFocusMapsToFocusin(t *testing.T) {
	page := `
		import { createSignal } from '@krate/runtime';
		export default function Page() {
			const [n, setN] = createSignal(0);
			return <input onFocus={() => setN(n() + 1)} />;
		}
	`
	_, js := buildReactPage(t, page)
	if !strings.Contains(js, "focusin") {
		t.Errorf("onFocus should map to focusin:\n%s", js)
	}
}

// TestFormValueUsesPropertyBinding verifies a reactive value uses kbindProp.
func TestFormValueUsesPropertyBinding(t *testing.T) {
	page := `
		import { createSignal } from '@krate/runtime';
		export default function Page() {
			const [v, setV] = createSignal('x');
			return <input value={v()} onInput={(e) => setV(e.target.value)} />;
		}
	`
	_, js := buildReactPage(t, page)
	if !strings.Contains(js, "kbindProp(") {
		t.Errorf("reactive value should use kbindProp:\n%s", js)
	}
}

// TestClassNameAttrBindingUsesClass verifies a reactive className binds the
// `class` attribute, not `className`.
func TestClassNameAttrBindingUsesClass(t *testing.T) {
	page := `
		import { createSignal } from '@krate/runtime';
		export default function Page() {
			const [on, setOn] = createSignal(false);
			return <div className={on() ? 'a' : 'b'} onClick={() => setOn(true)}>x</div>;
		}
	`
	_, js := buildReactPage(t, page)
	if !strings.Contains(js, `"class"`) || strings.Contains(js, `"className"`) {
		t.Errorf("className should bind the class attribute:\n%s", js)
	}
}

// TestModuloByZeroDoesNotPanic verifies a statically-evaluated modulo by zero
// renders NaN instead of crashing the build.
func TestModuloByZeroDoesNotPanic(t *testing.T) {
	page := `
		function F(props: any) { return <b>{props.n % 0}</b>; }
		export default function Page() { return <F n={5} />; }
	`
	html, _ := buildReactPage(t, page)
	if !strings.Contains(html, "NaN") {
		t.Errorf("modulo by zero should render NaN:\n%s", html)
	}
}

// TestSignalStringWithSpacesQuoted verifies a resolved string initial with
// spaces is emitted as a quoted literal.
func TestSignalStringWithSpacesQuoted(t *testing.T) {
	page := `
		import { createSignal } from '@krate/runtime';
		function C(props: any) {
			const [x, setX] = createSignal(props.label);
			return <b onClick={() => setX(x())}>{x()}</b>;
		}
		export default function Page() { return <C label="Hello world" />; }
	`
	_, js := buildReactPage(t, page)
	if !strings.Contains(js, `createSignal('Hello world')`) {
		t.Errorf("string initial with spaces should be quoted:\n%s", js)
	}
}

// TestNumericZeroLocalEmittedAsNumber verifies a folded numeric 0 local is a
// number, not the string "0".
func TestNumericZeroLocalEmittedAsNumber(t *testing.T) {
	page := `
		import { createSignal } from '@krate/runtime';
		export default function Page() {
			const [n, setN] = createSignal(1);
			const zero = 0;
			return <button onClick={() => setN(n() + zero)}>{n()}</button>;
		}
	`
	_, js := buildReactPage(t, page)
	if strings.Contains(js, "zero='0'") || strings.Contains(js, `zero="0"`) {
		t.Errorf("numeric 0 local should not be stringified:\n%s", js)
	}
}

// TestDurableStateScriptEscaped verifies a krate.state.json containing a
// </script> sequence cannot break out of the injected script.
func TestDurableStateScriptEscaped(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", `export default function Page() { return <h1>Hi</h1>; }`)
	writeFileRel(t, root, "krate.state.json", `{"note":"</script><script>alert(1)</script>"}`)

	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	html, _ := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if strings.Contains(string(html), "</script><script>alert(1)") {
		t.Errorf("durable state allows script breakout:\n%s", html)
	}
	if !strings.Contains(string(html), "\\u003c/script\\u003e") {
		t.Errorf("durable state < should be escaped as \\u003c:\n%s", html)
	}
}

// TestUnresolvableLocalStillEmitted verifies a local whose initializer can't be
// folded (references an unresolvable member chain) is still declared in the
// hydration scope, so bindings referencing it do not throw "X is not defined".
func TestUnresolvableLocalStillEmitted(t *testing.T) {
	page := `
		import { createSignal } from '@krate/runtime';
		function C(props: any) {
			const items = props.items || [];
			const show = !props.hidden && items.length > 0;
			const [n, setN] = createSignal(0);
			return <div onClick={() => setN(n() + 1)}>{show ? <b>{items.length}</b> : null}{n()}</div>;
		}
		export default function Page() { return <C items={[1, 2]} />; }
	`
	_, js := buildReactPage(t, page)
	if !strings.Contains(js, "var show=") {
		t.Errorf("unresolvable local `show` was not emitted into hydration:\n%s", js)
	}
}

// TestStringLocalConstantStaysQuoted verifies a client component's plain string
// constant is emitted as a quoted literal, not a bare identifier. A folded value
// like "menu" cannot be distinguished from an identifier reference by shape, so
// emitting it bare produced `var label=menu` (ReferenceError at hydration).
func TestStringLocalConstantStaysQuoted(t *testing.T) {
	page := `
		import { createSignal } from '@krate/runtime';
		function C(props: any) {
			const label = "menu";
			const [n, setN] = createSignal(0);
			return <button onClick={() => setN(n() + 1)}>{label}{n()}</button>;
		}
		export default function Page() { return <C />; }
	`
	_, js := buildReactPage(t, page)
	if !strings.Contains(js, "var label='menu'") {
		t.Errorf("string local should be emitted quoted:\n%s", js)
	}
	if strings.Contains(js, "var label=menu") {
		t.Errorf("string local emitted as a bare identifier:\n%s", js)
	}
}

// TestNullStringLiteralRenders verifies the string "null" is kept while the
// null keyword is omitted, even though both fold to the same text.
func TestNullStringLiteralRenders(t *testing.T) {
	page := `
		import { createSignal } from '@krate/runtime';
		function C(props: any) {
			const [n, setN] = createSignal(0);
			return <div title={"null"}>{"null"}{null}{n()}</div>;
		}
		export default function Page() { return <C />; }
	`
	html, _ := buildReactPage(t, page)
	if !strings.Contains(html, `title="null"`) {
		t.Errorf("string \"null\" attribute should render:\n%s", html)
	}
	// Exactly two "null"s: the attribute value and the rendered string. The
	// null keyword must not add a third.
	if got := strings.Count(html, "null"); got != 2 {
		t.Errorf("expected 2 occurrences of \"null\", got %d:\n%s", got, html)
	}
}

// TestBasePathPrefixesAssets verifies Emitted asset URLs are prefixed with the
// configured base path so the site works when hosted under a sub-path.
func TestBasePathPrefixesAssets(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", `
		import { createSignal } from '@krate/runtime';
		export default function Page() {
			const [n, setN] = createSignal(0);
			return <button onClick={() => setN(n() + 1)}>{n()}</button>;
		}
	`)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	cfg.BasePath = "/docs"
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	html, _ := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if !strings.Contains(string(html), `src="/docs/index`) {
		t.Errorf("expected base-path-prefixed hydration script:\n%s", html)
	}
	// The page script must not also be referenced at the site root.
	if strings.Contains(string(html), `src="/index.`) {
		t.Errorf("hydration script not base-path-prefixed:\n%s", html)
	}
}

// TestBareBooleanPropForwarded verifies a bare boolean prop reaches the child.
func TestBareBooleanPropForwarded(t *testing.T) {
	page := `
		function Child(props: any) { return <button disabled={props.disabled}>x</button>; }
		export default function Page() { return <Child disabled />; }
	`
	html, _ := buildReactPage(t, page)
	if !strings.Contains(html, "disabled") {
		t.Errorf("bare boolean prop should reach the child:\n%s", html)
	}
}
