package build

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// A dynamic-route page that renders a component with a signal builds, and
// the component hydrates. The page used to be evaluated whole at build time
// (to bind its route params), which ran the child's createSignal there:
// "cannot evaluate `n()` at build time: unresolved identifier createSignal".
func TestDynamicRoutePageWithHydratingChild(t *testing.T) {
	counter := "import { createSignal } from '@krate/runtime';\n" +
		"export default function Counter() { const [n, setN] = createSignal(0); return <button id=\"c\" onClick={() => setN(n() + 1)}>count {n()}</button>; }\n"
	cases := map[string]string{
		"array destructuring":  "import Counter from '../c';\nexport default function P({ params }) { const [first] = [1]; return <main><h1>{params.id}-{first}</h1><Counter /></main>; }\n",
		"plain":                "import Counter from '../c';\nexport default function P({ params }) { return <main><h1>{params.id}</h1><Counter /></main>; }\n",
		"nested in an element": "import Counter from '../c';\nexport default function P({ params }) { return <main><section><h1>{params.id}</h1><div><Counter /></div></section></main>; }\n",
	}
	for name, page := range cases {
		t.Run(name, func(t *testing.T) {
			_, out, logged := buildFiles(t, map[string]string{
				"src/c.tsx":          counter,
				"src/pages/[id].tsx": page,
			}, func(c *config.Config) { c.Tailwind.Enabled = false; c.Tailwind.Strict = false })
			html := readBuilt(t, filepath.Join(out, "[id]", "index.html"))
			if !strings.Contains(html, "<button data-k=") || !strings.Contains(html, "count <!--k:") {
				t.Errorf("the counter was not emitted with its hydration markers:\n%s", html)
			}
			if strings.Contains(logged, "cannot evaluate") {
				t.Errorf("unexpected build-time evaluation error:\n%s", logged)
			}
		})
	}
}

// A compound widget in the shape of @krate/components' Dialog: a CSS group
// scope provided through context, with Trigger and Content parts that hand
// their `value` prop to the scope.
const compoundWidgetSrc = `import { createScope, createCSSGroup } from '@krate/runtime';
const Ctx = createScope<string | null>();
export function Dialog(props) {
  const open = createCSSGroup<string>(null, { as: 'dialog' });
  return <Ctx.Provider value={open}><div class="dlg">{props.children}</div></Ctx.Provider>;
}
export function DialogTrigger(props) {
  const open = Ctx.use();
  return <button type="button" {...open.trigger(props.value)}>{props.children}</button>;
}
export function DialogContent(props) {
  const open = Ctx.use();
  return <div role="dialog" {...open.panel(props.value)}>{props.children}</div>;
}
Dialog.Trigger = DialogTrigger;
Dialog.Content = DialogContent;
`

func buildWidgetPage(t *testing.T, body string) (html, logged string) {
	t.Helper()
	return buildPlain(t, map[string]string{
		"src/ui/dialog.tsx":   compoundWidgetSrc,
		"src/pages/index.tsx": "import { Dialog } from '../ui/dialog';\nexport default function P() { return <main>" + body + "</main>; }\n",
	})
}

// A trigger and a content written without `value` open each other. Before,
// the trigger pointed at option "on" and the content at the empty (closed)
// option, so nothing ever opened and nothing said so.
func TestScopePartsWithoutValueArePaired(t *testing.T) {
	html, logged := buildWidgetPage(t, `<Dialog><Dialog.Trigger>Open</Dialog.Trigger><Dialog.Content>Body</Dialog.Content></Dialog>`)
	trigger := regexp.MustCompile(`<label[^>]*for=([^ >]+)[^>]*>Open</label>`).FindStringSubmatch(html)
	if trigger == nil {
		t.Fatalf("no trigger label in:\n%s", rootOf(t, html))
	}
	if !strings.Contains(html, `id=`+trigger[1]) || !strings.HasSuffix(trigger[1], "-on") {
		t.Errorf("the trigger does not point at an option input of its own: for=%s\n%s", trigger[1], rootOf(t, html))
	}
	if !regexp.MustCompile(`class=krc\d+-p-on><div role=dialog`).MatchString(html) {
		t.Errorf("the content is not wrapped for option on:\n%s", rootOf(t, html))
	}
	if strings.Contains(logged, "KR8351") {
		t.Errorf("a matched pair must not warn:\n%s", logged)
	}
}

// Parts with explicit values and a controlled content with no trigger keep
// working without a warning; a trigger nothing answers is reported.
func TestScopePartPairingDiagnostics(t *testing.T) {
	cases := []struct {
		name, body string
		wantWarn   bool
	}{
		{"explicit values", `<Dialog><Dialog.Trigger value="a">Open</Dialog.Trigger><Dialog.Content value="a">Body</Dialog.Content></Dialog>`, false},
		{"content only (opened from code)", `<Dialog><Dialog.Content>Body</Dialog.Content></Dialog>`, false},
		{"trigger without value, content with value", `<Dialog><Dialog.Trigger>Open</Dialog.Trigger><Dialog.Content value="c">Body</Dialog.Content></Dialog>`, true},
		{"content without value, trigger with value", `<Dialog><Dialog.Trigger value="c">Open</Dialog.Trigger><Dialog.Content>Body</Dialog.Content></Dialog>`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, logged := buildWidgetPage(t, c.body)
			if got := strings.Contains(logged, "KR8351"); got != c.wantWarn {
				t.Errorf("KR8351 reported = %v, want %v:\n%s", got, c.wantWarn, logged)
			}
			if c.wantWarn && !strings.Contains(logged, "give the trigger and its content the same value") {
				t.Errorf("the warning has no fix hint:\n%s", logged)
			}
		})
	}
}

// A value a signal-less component needs at build time but cannot get fails
// the build with KR8352 and a hint naming the identifier.
func TestBuildTimeUnknownValueHasCodeAndHint(t *testing.T) {
	cases := map[string]string{
		"identifier": "function C() { return <p>{missingVar}</p>; }\nexport default function P() { return <main><C /></main>; }\n",
		"call":       "function C() { return <p>{missingFn(1)}</p>; }\nexport default function P() { return <main><C /></main>; }\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			got := buildErrText(t, map[string]string{"src/pages/index.tsx": src})
			if !strings.Contains(got, "[KR8352]") || !strings.Contains(got, "unresolved identifier missing") {
				t.Fatalf("want a KR8352 error naming the identifier, got %q", got)
			}
			if !strings.Contains(got, "is not a prop, a local or a module constant") || !strings.Contains(got, "pass it as a prop") {
				t.Errorf("the hint does not say how to fix it: %q", got)
			}
		})
	}
}
