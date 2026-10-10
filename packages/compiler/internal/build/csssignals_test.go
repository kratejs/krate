package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// buildPageSrc writes a one-page project and builds it.
func buildPageSrc(t *testing.T, src string) (string, string, error) {
	t.Helper()
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	err := New(root, cfg).BuildAll()
	html := ""
	if data, rerr := os.ReadFile(filepath.Join(cfg.OutDir, "index.html")); rerr == nil {
		html = string(data)
	}
	return html, cfg.OutDir, err
}

// TestBuildCSSSignalsZeroJS verifies the createCSSChoice transform end-to-end:
// radios, labels, and panels are emitted with no client hydration JS.
func TestBuildCSSSignalsZeroJS(t *testing.T) {
	src := `export default function Tabs() {
	const [tab, setTab] = createCSSChoice('overview');
	return (
		<div class="tabs">
			<button class="tab" onClick={() => setTab('overview')}>Overview</button>
			<button class="tab" onClick={() => setTab('features')}>Features</button>
			<div class="panel" showIf={tab() === 'overview'}>Overview content</div>
			<div class="panel" showIf={tab() === 'features'}>Features content</div>
		</div>
	);
}`
	html, outDir, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if strings.Contains(html, "createCSSChoice") || strings.Contains(html, "showIf") {
		t.Errorf("compiler sugar leaked:\n%.900s", html)
	}
	if !strings.Contains(html, "type=radio") && !strings.Contains(html, `type="radio"`) {
		t.Errorf("expected radio controllers:\n%.900s", html)
	}
	if !strings.Contains(html, "<label") || !strings.Contains(html, "for=") {
		t.Errorf("expected label triggers:\n%.900s", html)
	}
	if hasHydrationScript(html) {
		t.Errorf("CSS signal page should be zero-JS:\n%.900s", html)
	}
	css := readAllCSS(t, outDir)
	if !strings.Contains(css, ".krc0-r-overview:checked") {
		t.Errorf("expected compact radio rule:\n%s", css)
	}
	// Regression: base rules must not be joined to the next rule.
	if strings.Contains(css, "display:none},") {
		t.Errorf("dangling comma joining rules:\n%s", css)
	}
	if !strings.Contains(css, "display:contents") {
		t.Errorf("panels should toggle via display:contents wrapper:\n%s", css)
	}
	// Byte-savings check: no long legacy class prefix (the --krate-css-* theme
	// custom properties are intentional and excluded).
	if regexp.MustCompile(`\.krate-css-`).MatchString(css) {
		t.Errorf("legacy long class prefix present:\n%s", css)
	}
}

// TestBuildCSSToggle verifies createCSSToggle compiles to a checkbox.
func TestBuildCSSToggle(t *testing.T) {
	src := `export default function Theme() {
	const [on, setOn] = createCSSToggle(false);
	return (
		<div>
			<button onClick={() => setOn(!on())}>Toggle</button>
			<div showIf={on()}>On</div>
			<div showIf={!on()}>Off</div>
		</div>
	);
}`
	html, outDir, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if !strings.Contains(html, "type=checkbox") && !strings.Contains(html, `type="checkbox"`) {
		t.Errorf("expected checkbox controller:\n%.900s", html)
	}
	if hasHydrationScript(html) {
		t.Errorf("toggle should be zero-JS:\n%.900s", html)
	}
	css := readAllCSS(t, outDir)
	if !strings.Contains(css, ".krc0-c:checked") {
		t.Errorf("expected checkbox rule:\n%s", css)
	}
}

// TestBuildCSSFlags verifies createCSSFlags compiles to independent checkboxes.
func TestBuildCSSFlags(t *testing.T) {
	src := `export default function Options() {
	const [flags, setFlag] = createCSSFlags(['bold', 'italic']);
	return (
		<div>
			<button onClick={() => setFlag('bold', !flags.bold())}>Bold</button>
			<button onClick={() => setFlag('italic', !flags.italic())}>Italic</button>
			<div showIf={flags.bold()}>Bold on</div>
			<div showIf={flags.italic()}>Italic on</div>
		</div>
	);
}`
	html, outDir, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if !strings.Contains(html, "type=checkbox") && !strings.Contains(html, `type="checkbox"`) {
		t.Errorf("expected checkbox controllers:\n%.900s", html)
	}
	if hasHydrationScript(html) {
		t.Errorf("flags should be zero-JS:\n%.900s", html)
	}
	css := readAllCSS(t, outDir)
	if !strings.Contains(css, ".krc0-c-bold:checked") {
		t.Errorf("expected bold flag rule:\n%s", css)
	}
}

// TestBuildCSSSignalsCompound verifies compound showIf conditions compile to
// zero-JS :has() selector chains with a compound wrapper class.
func TestBuildCSSSignalsCompound(t *testing.T) {
	src := `export default function App() {
	const [plat, setPlat] = createCSSChoice('mac', ['mac', 'win']);
	const [licensed, setLicensed] = createCSSToggle(false);
	return (
		<div>
			<button onClick={() => setPlat('mac')}>Mac</button>
			<button onClick={() => setLicensed(!licensed())}>License</button>
			<div class="appr" showIf={plat() === 'mac' && licensed()}>Approve</div>
			<div class="upg" showIf={plat() === 'win' || !licensed()}>Upgrade</div>
		</div>
	);
}`
	html, outDir, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if strings.Contains(html, "createCSSChoice") || strings.Contains(html, "showIf") ||
		strings.Contains(html, "&&") || strings.Contains(html, "||") {
		t.Errorf("compiler sugar leaked:\n%.900s", html)
	}
	if hasHydrationScript(html) {
		t.Errorf("compound panels should be zero-JS:\n%.900s", html)
	}
	css := readAllCSS(t, outDir)
	// AND: both atoms stacked on one anchor chain (cross-scope: krc0 + krc1).
	if !strings.Contains(css, ".krc0:has(.krc0-r-mac:checked):has(.krc1-c:checked) .krc0-x-0") {
		t.Errorf("expected AND compound rule:\n%s", css)
	}
	// OR: a selector list with one entry per AND-term, sharing the wrapper.
	if !strings.Contains(css, ".krc0 .krc0-x-1{display:none}") {
		t.Errorf("expected compound wrapper hide rule:\n%s", css)
	}
	if !strings.Contains(css, ".krc0:has(.krc0-r-win:checked) .krc0-x-1,") ||
		!strings.Contains(css, ".krc0:not(:has(.krc1-c:checked)) .krc0-x-1") {
		t.Errorf("expected OR selector list:\n%s", css)
	}
}

// TestBuildCSSSignalsGroup verifies createCSSGroup compiles to a radio group
// with a null sentinel and emits accordion ARIA + the tiny runtime.
func TestBuildCSSSignalsGroup(t *testing.T) {
	src := `export default function Faq() {
	const [open, setOpen] = createCSSGroup(null, { as: 'accordion' });
	return (
		<div>
			<button onClick={() => setOpen('q1')}>Question 1</button>
			<div showIf={open() === 'q1'}>Answer 1</div>
			<button onClick={() => setOpen(null)}>Close</button>
		</div>
	);
}`
	html, outDir, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if strings.Contains(html, "createCSSGroup") || strings.Contains(html, "showIf") {
		t.Errorf("compiler sugar leaked:\n%.900s", html)
	}
	if !strings.Contains(html, `role=button`) && !strings.Contains(html, `role="button"`) {
		t.Errorf("expected accordion trigger role:\n%.900s", html)
	}
	if !strings.Contains(html, "aria-expanded") {
		t.Errorf("expected aria-expanded on trigger:\n%.900s", html)
	}
	// Accordion needs synthesized aria-expanded -> tiny runtime is injected, but
	// not the page hydration bundle.
	if !strings.Contains(html, "addEventListener") {
		t.Errorf("expected the ARIA synchroniser script:\n%.900s", html)
	}
	if hasHydrationScript(html) {
		t.Errorf("group should not hydrate a page bundle:\n%.900s", html)
	}
	_ = outDir
}

// TestBuildCSSSignalsRange verifies createCSSRange compiles to a radio chain
// with a proportional fill rule and bounded stepper labels.
func TestBuildCSSSignalsRange(t *testing.T) {
	src := `export default function Steps() {
	const [n, setN] = createCSSRange(0, { min: 0, max: 3, step: 1 });
	return (
		<div>
			<button onClick={() => setN(n() + 1)}>Next</button>
			<button onClick={() => setN(n() - 1)}>Prev</button>
			<div showIf={n() === 1}>One</div>
		</div>
	);
}`
	html, outDir, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if strings.Contains(html, "createCSSRange") || strings.Contains(html, "showIf") {
		t.Errorf("compiler sugar leaked:\n%.900s", html)
	}
	if hasHydrationScript(html) {
		t.Errorf("range should be zero-JS:\n%.900s", html)
	}
	css := readAllCSS(t, outDir)
	if !strings.Contains(css, "width:100%") && !strings.Contains(css, "width:66.7%") {
		t.Errorf("expected a proportional fill rule:\n%s", css)
	}
	if !strings.Contains(css, "-st-inc") || !strings.Contains(css, "-st-dec") {
		t.Errorf("expected stepper groups:\n%s", css)
	}
}

// TestBuildCSSSignalsStack verifies createCSSStack compiles to nested panels
// with push/pop labels, zero-JS.
func TestBuildCSSSignalsStack(t *testing.T) {
	src := `export default function Nav() {
	const [stack, { push, pop, clear }] = createCSSStack(['root']);
	return (
		<div>
			<button onClick={() => push('settings')}>Settings</button>
			<div showIf={stack.top() === 'settings'}>
				<button onClick={() => pop()}>Back</button>
			</div>
		</div>
	);
}`
	html, _, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if strings.Contains(html, "createCSSStack") || strings.Contains(html, "showIf") || strings.Contains(html, ".top()") {
		t.Errorf("compiler sugar leaked:\n%.900s", html)
	}
	if hasHydrationScript(html) {
		t.Errorf("stack should be zero-JS:\n%.900s", html)
	}
	if !strings.Contains(html, "<label") || !strings.Contains(html, "for=") {
		t.Errorf("expected label triggers:\n%.900s", html)
	}
}

// TestBuildCSSSignalsTabsARIA verifies a tabs role emits tablist ARIA and the
// tiny runtime, but no page hydration bundle.
func TestBuildCSSSignalsTabsARIA(t *testing.T) {
	src := `export default function Tabs() {
	const [tab, setTab] = createCSSChoice('a', { as: 'tabs' });
	return (
		<div>
			<button onClick={() => setTab('a')}>A</button>
			<button onClick={() => setTab('b')}>B</button>
			<div showIf={tab() === 'a'}>A panel</div>
			<div showIf={tab() === 'b'}>B panel</div>
		</div>
	);
}`
	html, _, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if !strings.Contains(html, `role=tab`) && !strings.Contains(html, `role="tab"`) {
		t.Errorf("expected tab roles:\n%.900s", html)
	}
	if !strings.Contains(html, "aria-selected") {
		t.Errorf("expected aria-selected on tabs:\n%.900s", html)
	}
	if !strings.Contains(html, "addEventListener") {
		t.Errorf("expected the ARIA synchroniser script:\n%.900s", html)
	}
	if hasHydrationScript(html) {
		t.Errorf("tabs should not hydrate a page bundle:\n%.900s", html)
	}
}

// TestBuildCSSSignalsLiveText verifies a bare `{getter()}` read compiles to a
// `.krc-live` element backed by `--krate-current`, still zero-JS, and that
// literal `vars` become custom properties.
func TestBuildCSSSignalsLiveText(t *testing.T) {
	src := `export default function T() {
	const [tier, setTier] = createCSSChoice('solo', {
		options: ['solo', 'pro'],
		vars: { '--price': { solo: '"$9"', pro: '"$29"' } },
	});
	return (
		<div>
			<button onClick={() => setTier('pro')}>Pro</button>
			<p>Plan: {tier()}</p>
			<div showIf={tier() === 'pro'}>Pro features</div>
		</div>
	);
}`
	html, outDir, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if strings.Contains(html, "createCSSChoice") || strings.Contains(html, "showIf") {
		t.Errorf("compiler sugar leaked:\n%.900s", html)
	}
	if !strings.Contains(html, "krc-live") {
		t.Errorf("expected a live-text element:\n%.900s", html)
	}
	if hasHydrationScript(html) {
		t.Errorf("live text should be zero-JS:\n%.900s", html)
	}
	css := readAllCSS(t, outDir)
	if !strings.Contains(css, `.krc-live::after{content:var(--krate-current,"")}`) {
		t.Errorf("missing live-text rule:\n%s", css)
	}
	if !strings.Contains(css, `.krc0:has(.krc0-r-pro:checked){--krate-current:"pro";--price:"$29"}`) {
		t.Errorf("missing vars rule:\n%s", css)
	}
}

// TestBuildCSSSignalsToggleLiveText verifies a toggle publishes on/off values.
func TestBuildCSSSignalsToggleLiveText(t *testing.T) {
	src := `export default function T() {
	const [on, setOn] = createCSSToggle(false);
	return (
		<div>
			<button onClick={() => setOn(!on())}>Toggle</button>
			<p>{on()}</p>
		</div>
	);
}`
	_, outDir, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	css := readAllCSS(t, outDir)
	if !strings.Contains(css, `.krc0:has(.krc0-c:checked){--krate-current:"on"}`) {
		t.Errorf("missing toggle on rule:\n%s", css)
	}
	if !strings.Contains(css, `.krc0:not(:has(.krc0-c:checked)){--krate-current:"off"}`) {
		t.Errorf("missing toggle off rule:\n%s", css)
	}
}

// TestBuildCSSSignalsMultiInstanceUnique verifies two instances get distinct
// controller groups over one shared stylesheet.
func TestBuildCSSSignalsMultiInstanceUnique(t *testing.T) {
	src := `function Tabs(props: { label: string }) {
	const [tab, setTab] = createCSSChoice('a');
	return (
		<div class="tabs">
			<button onClick={() => setTab('a')}>{props.label} A</button>
			<button onClick={() => setTab('b')}>{props.label} B</button>
			<div showIf={tab() === 'a'}>A</div>
			<div showIf={tab() === 'b'}>B</div>
		</div>
	);
}
export default function Page() { return <main><Tabs label="one" /><Tabs label="two" /></main>; }`
	html, outDir, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if hasHydrationScript(html) {
		t.Errorf("multi-instance should be zero-JS:\n%.900s", html)
	}
	names := map[string]bool{}
	for _, m := range regexp.MustCompile(`class="[^"]*krc-h[^"]*"\s+name=([A-Za-z0-9_-]+)`).FindAllStringSubmatch(html, -1) {
		names[m[1]] = true
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 distinct control groups, got %d: %v\n%.900s", len(names), names, html)
	}
	css := readAllCSS(t, outDir)
	// A single scope produces exactly one base-hide rule for its panels.
	if strings.Count(css, "display:none}") != 1 {
		t.Errorf("expected one shared scope stylesheet, got:\n%s", css)
	}
}

// TestBuildCSSSignalsFragmentWrapped verifies a fragment root is wrapped.
func TestBuildCSSSignalsFragmentWrapped(t *testing.T) {
	src := `export default function Tabs() {
	const [tab, setTab] = createCSSChoice('a');
	return (
		<>
			<button onClick={() => setTab('b')}>B</button>
			<div showIf={tab() === 'b'}>B panel</div>
		</>
	);
}`
	html, _, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if hasHydrationScript(html) {
		t.Errorf("fragment should be zero-JS:\n%.900s", html)
	}
	if !strings.Contains(html, "display:contents") {
		t.Errorf("expected display:contents scope wrapper:\n%.900s", html)
	}
}

// TestBuildCSSSignalsPanelChildrenRender verifies panel bodies render through the
// normal pipeline (nested components + lists).
func TestBuildCSSSignalsPanelChildrenRender(t *testing.T) {
	src := `function Badge(props: { label: string }) {
	const [n, setN] = createSignal(0);
	return <button onClick={() => setN(n() + 1)}>{props.label}:{n()}</button>;
}
export default function Tabs() {
	const [tab, setTab] = createCSSChoice('a');
	return (
		<div>
			<button onClick={() => setTab('b')}>B</button>
			<div showIf={tab() === 'a'}>
				<Badge label="count" />
				<ul>{['x', 'y'].map((v) => <li>{v}</li>)}</ul>
			</div>
		</div>
	);
}`
	html, _, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if !strings.Contains(html, "data-k=") {
		t.Errorf("expected nested interactive component inside panel:\n%.900s", html)
	}
	if !strings.Contains(html, "<li") || !strings.Contains(html, ">x<") {
		t.Errorf("expected mapped list items inside panel:\n%.900s", html)
	}
}

// Hard-error cases

func TestBuildCSSSignalsErrorsAreFatal(t *testing.T) {
	cases := map[string]string{
		"stray text read": `export default function T() {
	const [tab, setTab] = createCSSChoice('a');
	return <div><p>{tab() + '!'}</p><div showIf={tab() === 'a'}>A</div></div>;
}`,
		"non-labelable trigger": `export default function T() {
	const [tab, setTab] = createCSSChoice('a');
	return <div><section onClick={() => setTab('b')}>B</section><div showIf={tab() === 'b'}>B</div></div>;
}`,
		"dynamic trigger class": `export default function T() {
	const [tab, setTab] = createCSSChoice('a');
	return <div><button class={x} onClick={() => setTab('b')}>B</button><div showIf={tab() === 'b'}>B</div></div>;
}`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := buildPageSrc(t, src)
			if err == nil {
				t.Fatal("expected the build to fail")
			}
			if !strings.Contains(err.Error(), "createSignal") {
				t.Errorf("error should tell the user to use createSignal: %v", err)
			}
		})
	}
}

// hasHydrationScript reports whether the page references a page hydration script.
func hasHydrationScript(html string) bool {
	return regexp.MustCompile(`<script src="/index\.[A-Za-z0-9]+\.js"`).MatchString(html)
}

// readAllCSS concatenates every emitted stylesheet.
func readAllCSS(t *testing.T, outDir string) string {
	t.Helper()
	var sb strings.Builder
	_ = filepath.Walk(outDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".css") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err == nil {
			sb.Write(data)
		}
		return nil
	})
	return sb.String()
}
