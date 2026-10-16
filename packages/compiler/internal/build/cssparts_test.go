package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// buildProject writes a multi-file project and builds it, returning outDir.
func buildProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	return cfg.OutDir
}

// assertForResolves fails when any for="..." has no matching id="..." in html.
func assertForResolves(t *testing.T, html string) {
	t.Helper()
	ids := map[string]bool{}
	for _, m := range regexp.MustCompile(`id=("?)([^"\s>]+)`).FindAllStringSubmatch(html, -1) {
		ids[m[2]] = true
	}
	fors := regexp.MustCompile(`for=("?)([^"\s>]+)`).FindAllStringSubmatch(html, -1)
	if len(fors) == 0 {
		t.Fatalf("expected trigger labels with for=:\n%.900s", html)
	}
	for _, m := range fors {
		if !ids[m[2]] {
			t.Errorf("label for=%q has no matching controller id:\n%.1200s", m[2], html)
		}
	}
}

// TestBuildCSSScopeHandlesSameComponent verifies `{...x.trigger('a')}` /
// `{...x.panel('a')}` lower to zero-JS markup in the declaring component.
func TestBuildCSSScopeHandlesSameComponent(t *testing.T) {
	src := `export default function Page() {
	const tab = createCSSChoice('a', ['a', 'b']);
	return (
		<div class="w">
			<button {...tab.trigger('a')}>A</button>
			<button {...tab.trigger('b')}>B</button>
			<div {...tab.panel('a')}>Panel A</div>
			<div {...tab.panel('b')}>Panel B</div>
		</div>
	);
}`
	html, outDir, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if hasHydrationScript(html) {
		t.Errorf("handle API should be zero-JS:\n%.1200s", html)
	}
	if strings.Contains(html, "data-krate-scope") {
		t.Errorf("scope-helper spread leaked to output:\n%.1200s", html)
	}
	if !strings.Contains(html, "<label") {
		t.Errorf("expected label triggers:\n%.1200s", html)
	}
	assertForResolves(t, html)
	if css := readAllCSS(t, outDir); !strings.Contains(css, "display:contents") {
		t.Errorf("expected panel wrappers:\n%s", css)
	}
}

// TestBuildCSSScopeHandlesCrossComponent verifies a scope handle passed as a
// prop drives triggers/panels in a child component.
func TestBuildCSSScopeHandlesCrossComponent(t *testing.T) {
	src := `function TabTrigger(props) {
	return <button class="tab" {...props.scope.trigger(props.value)}>{props.children}</button>;
}
function TabPanel(props) {
	return <div class="panel" {...props.scope.panel(props.value)}>{props.children}</div>;
}
export default function Page() {
	const tab = createCSSChoice('a', ['a', 'b']);
	return (
		<div class="tabs">
			<TabTrigger scope={tab} value="a">A</TabTrigger>
			<TabTrigger scope={tab} value="b">B</TabTrigger>
			<TabPanel scope={tab} value="a">Panel A</TabPanel>
			<TabPanel scope={tab} value="b">Panel B</TabPanel>
		</div>
	);
}`
	html, outDir, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if hasHydrationScript(html) {
		t.Errorf("cross-component handle should be zero-JS:\n%.1200s", html)
	}
	if !strings.Contains(html, "<label") {
		t.Errorf("expected child-authored labels:\n%.1200s", html)
	}
	assertForResolves(t, html)
	if css := readAllCSS(t, outDir); !strings.Contains(css, "display:contents") {
		t.Errorf("expected panel wrappers:\n%s", css)
	}
}

// TestBuildCSSScopeContextFlow verifies a scope provided via context
// (`<Ctx.Provider value={scope}>`) resolves in descendant parts that read
// `Ctx.useContext()` — the ergonomic compound-component model.
func TestBuildCSSScopeContextFlow(t *testing.T) {
	src := `const TabsCtx = createContext(null);
function Tabs(props) {
	const tab = createCSSChoice('a');
	return <TabsCtx.Provider value={tab}><div class="tabs">{props.children}</div></TabsCtx.Provider>;
}
function TabTrigger(props) {
	const tab = TabsCtx.useContext();
	return <button class="tab" {...tab.trigger(props.value)}>{props.children}</button>;
}
function TabPanel(props) {
	const tab = TabsCtx.useContext();
	return <div class="panel" {...tab.panel(props.value)}>{props.children}</div>;
}
export default function Page() {
	return (
		<Tabs defaultValue="a">
			<TabTrigger value="a">A</TabTrigger>
			<TabTrigger value="b">B</TabTrigger>
			<TabPanel value="a">Panel A</TabPanel>
			<TabPanel value="b">Panel B</TabPanel>
		</Tabs>
	);
}`
	html, outDir, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if hasHydrationScript(html) {
		t.Errorf("context flow should be zero-JS:\n%.1200s", html)
	}
	if strings.Contains(html, "data-krate-scope") {
		t.Errorf("scope-helper spread leaked:\n%.1200s", html)
	}
	if !strings.Contains(html, "<label") || !strings.Contains(html, ">A</label>") {
		t.Errorf("expected context-resolved labels:\n%.1200s", html)
	}
	assertForResolves(t, html)
	if css := readAllCSS(t, outDir); !strings.Contains(css, "display:contents") {
		t.Errorf("expected panel wrappers:\n%s", css)
	}
}

// TestBuildTemplateLiteralCooksEscapes verifies template-literal escapes are
// cooked when a component evaluates them to text (SSR), while a literal
// backslash-n in the source is preserved.
func TestBuildTemplateLiteralCooksEscapes(t *testing.T) {
	// First template: `a\nb` -> a real newline. Second: `c\\nd` -> literal "\n".
	src := "export default function Page() {\n\treturn <pre>{`a\\nb`}{`c\\\\nd`}</pre>;\n}"
	html, _, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if !strings.Contains(html, "a\nb") {
		t.Errorf("expected the \\n escape to cook to a newline:\n%q", html)
	}
	if strings.Contains(html, "a\\nb") {
		t.Errorf("literal backslash-n leaked for `a\\nb`:\n%q", html)
	}
	if !strings.Contains(html, "c\\nd") {
		t.Errorf("expected a literal backslash-n to be preserved:\n%q", html)
	}
}

// TestBuildCSSScopeLiveText verifies `{scope.current()}` compiles to a
// `--krate-current`-driven live text node with zero JS.
func TestBuildCSSScopeLiveText(t *testing.T) {
	src := `export default function Page() {
	const tab = createCSSChoice('a', ['a', 'b']);
	return (
		<div>
			<span>{tab.current()}</span>
			<button {...tab.trigger('a')}>A</button>
			<button {...tab.trigger('b')}>B</button>
		</div>
	);
}`
	html, outDir, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if hasHydrationScript(html) {
		t.Errorf("live text should be zero-JS:\n%.1200s", html)
	}
	if !strings.Contains(html, "krc-live") {
		t.Errorf("expected a krc-live node:\n%.1200s", html)
	}
	if css := readAllCSS(t, outDir); !strings.Contains(css, "krc-live") {
		t.Errorf("expected the krc-live rule:\n%s", css)
	}
}

// TestBuildCSSScopeUnboundErrors verifies a scope helper whose handle can't be
// resolved is a hard build error, not silently dropped.
func TestBuildCSSScopeUnboundErrors(t *testing.T) {
	src := `const Ctx = createContext(null);
function Trigger(props) {
	const tab = Ctx.use();
	return <button {...tab.trigger('a')}>A</button>;
}
export default function Page() {
	return <Trigger />;
}`
	_, _, err := buildPageSrc(t, src)
	if err == nil {
		t.Fatalf("expected a build error for an unbound scope handle")
	}
	if !strings.Contains(err.Error(), "not bound to a scope") {
		t.Errorf("expected a clear unbound-scope error, got: %v", err)
	}
}

// TestBuildCSSScopeHandleCompoundConditions verifies the handle API supports
// compound `showIf` conditions via scope.is()/on()/off(), with options inferred
// from the trigger literals.
func TestBuildCSSScopeHandleCompoundConditions(t *testing.T) {
	src := `export default function P() {
	const plat = createCSSChoice('mac');
	const licensed = createCSSToggle(false);
	return (
		<div>
			<button {...plat.trigger('mac')}>Mac</button>
			<button {...plat.trigger('win')}>Win</button>
			<button {...licensed.trigger()}>Licensed</button>
			<div showIf={plat.is('mac') && licensed.on()}>Both</div>
			<div showIf={!plat.is('win') || licensed.off()}>Either</div>
		</div>
	);
}`
	html, outDir, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if hasHydrationScript(html) {
		t.Errorf("handle compound conditions should be zero-JS:\n%.1200s", html)
	}
	if !strings.Contains(html, "Both") || !strings.Contains(html, "Either") {
		t.Errorf("expected both panels:\n%.1200s", html)
	}
	// Options inferred from trigger literals: mac + win radios.
	if n := strings.Count(html, "type=radio") + strings.Count(html, `type="radio"`); n != 2 {
		t.Errorf("expected 2 radios from trigger literals, got %d:\n%.1200s", n, html)
	}
	assertForResolves(t, html)
	if css := readAllCSS(t, outDir); !strings.Contains(css, "-x-") {
		t.Errorf("expected a compound condition wrapper class:\n%s", css)
	}
}

// TestBuildCSSScopeFormNameAndDisabled verifies the `name` option submits the
// value under a friendly key and `disabled` options disable their controller.
func TestBuildCSSScopeFormNameAndDisabled(t *testing.T) {
	src := `export default function P() {
	const v = createCSSChoice('a', { options: ['a', 'b', 'c'], name: 'plan', disabled: ['c'] });
	return (
		<div>
			<button {...v.trigger('a')}>A</button>
			<button {...v.trigger('b')}>B</button>
			<button {...v.trigger('c')}>C</button>
			<div {...v.panel('a')}>A</div>
		</div>
	);
}`
	html, outDir, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if !strings.Contains(html, `name=plan`) && !strings.Contains(html, `name="plan"`) {
		t.Errorf("expected controllers to submit under name=plan:\n%.1200s", html)
	}
	if !strings.Contains(html, "disabled") {
		t.Errorf("expected the disabled option's controller to be disabled:\n%.1200s", html)
	}
	if !strings.Contains(html, `aria-disabled="true"`) && !strings.Contains(html, "aria-disabled=true") {
		t.Errorf("expected aria-disabled on the disabled trigger:\n%.1200s", html)
	}
	assertForResolves(t, html)
	if css := readAllCSS(t, outDir); !strings.Contains(css, "pointer-events:none") {
		t.Errorf("expected a disabled-trigger rule:\n%s", css)
	}
}

// TestBuildCSSScopeAnchorAndTransition verifies anchor positioning and discrete
// transition CSS are emitted for an overlay scope.
func TestBuildCSSScopeAnchorAndTransition(t *testing.T) {
	src := `export default function P() {
	const v = createCSSGroup(null, { as: 'popover', anchor: 'bottom-end', transition: true });
	return (
		<div>
			<button {...v.trigger('p')}>Open</button>
			<div {...v.panel('p')}>Body</div>
		</div>
	);
}`
	_, outDir, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	css := readAllCSS(t, outDir)
	for _, want := range []string{"anchor-name", "position-anchor", "anchor(bottom)", "@starting-style", "transition:opacity"} {
		if !strings.Contains(css, want) {
			t.Errorf("expected %q in generated CSS:\n%s", want, css)
		}
	}
}

// TestBuildCSSScopeNoCrossPageLeak verifies a scope component imported by
// several pages renders exactly one scope anchor per page (no class
// accumulation through the shared library AST).
func TestBuildCSSScopeNoCrossPageLeak(t *testing.T) {
	component := `import { createContext, createCSSChoice } from '@krate/runtime';
const Ctx = createContext(null);
export function Tabs(props) {
	const tab = createCSSChoice('a');
	return <Ctx.Provider value={tab}><div class="tabs">{props.children}</div></Ctx.Provider>;
}
export function TabsTrigger(props) { const tab = Ctx.useContext(); return <button class="tab" {...tab.trigger(props.value)}>{props.children}</button>; }
export function TabsPanel(props) { const tab = Ctx.useContext(); return <div class="panel" {...tab.panel(props.value)}>{props.children}</div>; }`
	page := `import { Tabs, TabsTrigger, TabsPanel } from '../components/tabs';
export default function Page() {
	return <Tabs defaultValue="a"><TabsTrigger value="a">A</TabsTrigger><TabsTrigger value="b">B</TabsTrigger><TabsPanel value="a">PA</TabsPanel><TabsPanel value="b">PB</TabsPanel></Tabs>;
}`
	outDir := buildProject(t, map[string]string{
		"src/components/tabs.tsx": component,
		"src/pages/a.tsx":         page,
		"src/pages/b.tsx":         page,
	})
	for _, name := range []string{"a", "b"} {
		data, err := os.ReadFile(filepath.Join(outDir, name, "index.html"))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		html := string(data)
		if n := len(regexp.MustCompile(`class=krc[0-9a-z]+ `).FindAllString(html, -1)); n != 1 {
			t.Errorf("page %s: expected exactly 1 scope anchor, got %d:\n%.900s", name, n, html)
		}
		assertForResolves(t, html)
	}
}

// TestBuildCSSGroupTriggerCloses verifies a group scope (accordion/popover/…)
// emits a second "close" label so re-clicking the open trigger closes it, and
// starts closed (the empty sentinel is selected).
func TestBuildCSSGroupTriggerCloses(t *testing.T) {
	src := `const PopCtx = createContext(null);
function Pop(props) {
	const open = createCSSGroup(null, { as: 'popover' });
	return <PopCtx.Provider value={open}><div class="pop">{props.children}</div></PopCtx.Provider>;
}
function PopTrigger(props) { const open = PopCtx.useContext(); return <button class="pop-trigger" {...open.trigger(props.value)}>{props.children}</button>; }
function PopContent(props) { const open = PopCtx.useContext(); return <div class="pop-content" {...open.panel(props.value)}>{props.children}</div>; }
export default function Page() {
	return <Pop><PopTrigger value="p">Open</PopTrigger><PopContent value="p">Body</PopContent></Pop>;
}`
	html, outDir, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if hasHydrationScript(html) {
		t.Errorf("group scope should be zero-JS:\n%.1200s", html)
	}
	assertForResolves(t, html)
	if got := strings.Count(html, "pop-trigger"); got != 2 {
		t.Errorf("expected a select + close trigger, got %d pop-trigger occurrences:\n%.1200s", got, html)
	}
	// Regression: the close label must keep the trigger's children.
	if !regexp.MustCompile(`-ta-p"[^>]*>[^<]*Open`).MatchString(html) {
		t.Errorf("close label lost its children:\n%.1200s", html)
	}
	if !strings.Contains(html, `value="" checked`) && !strings.Contains(html, `value="" checked=true`) {
		t.Errorf("expected the closed sentinel selected initially:\n%.1200s", html)
	}
	css := readAllCSS(t, outDir)
	if !strings.Contains(css, "-ta-p{display:none}") && !strings.Contains(css, "-ta-p {display:none}") {
		t.Errorf("expected the close label hidden by default:\n%s", css)
	}
	if !regexp.MustCompile(`-ta-p\{display:inline-flex`).MatchString(css) {
		t.Errorf("expected the close label shown when selected:\n%s", css)
	}
}
