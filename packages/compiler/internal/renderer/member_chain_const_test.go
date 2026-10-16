package renderer

import (
	"strings"
	"testing"
)

// The docs theme aliases `const accent = props.options?.accent || "#0f766e"` (and
// the variant `(props.options && props.options.accent) || "#0f766e"`) from a
// call-site object option. The const must fold through the nested member chain
// in both SSR and hydration, never leak the identifier name, and
// `props.options?.dark` must select the createSignal initial at both tiers.
func TestNestedMemberConstFold(t *testing.T) {
	src := `
import { createSignal, onMount } from "@krate/runtime";
interface ThemeOptions { accent?: string; dark?: boolean }
function NightLayout(props: { options?: ThemeOptions }) {
  const accent = (props.options && props.options.accent) || "#0f766e";
  const [theme, setTheme] = createSignal(props.options?.dark ? "dark" : "light");
  onMount(function () {});
  return (
    <div>
      <header class="docs-navbar" style={"background: " + accent}>nav</header>
      <span class="accent-chip">accent: {accent}</span>
      <span class="theme-init">{theme()}</span>
    </div>
  );
}
export default function Page() {
  return <NightLayout options={{ accent: "#0f766e", dark: true }} />;
}
`
	result, hjs := fullPipeline(t, src)
	t.Logf("HTML:\n%s", result.HTML)
	t.Logf("HYDRATION:\n%s", hjs)
	for _, want := range []string{`background: #0f766e`, `accent: #0f766e`} {
		if !strings.Contains(result.HTML, want) {
			t.Errorf("SSR missing %q:\n%s", want, result.HTML)
		}
	}
	if strings.Contains(result.HTML, "background: accent") || strings.Contains(result.HTML, ">accent<") {
		t.Errorf("SSR leaked identifier name:\n%s", result.HTML)
	}
	if !strings.Contains(hjs, "createSignal('dark')") {
		t.Errorf("hydration signal initial did not fold to 'dark':\n%s", hjs)
	}
	if !strings.Contains(hjs, "props.options") {
		t.Logf("no props.options leak in hydration (good)")
	}
}

// A member chain that must NOT be const-folded: props.root is not a definite
// object in localProps, so props.root.missing stays a leak and the local is
// skipped (falls back to runtime), never emitting a literal "" in place of a
// value that could be non-empty at runtime.
func TestNestedMemberLeakStaysClean(t *testing.T) {
	src := `
import { onMount } from "@krate/runtime";
function Card(props: { root?: any }) {
  let badge = props.root && props.root.badge;
  onMount(function () {});
  return <div>{badge}</div>;
}
export default function Page() {
  return <Card root={{ badge: "NEW" }} />;
}
`
	result, hjs := fullPipeline(t, src)
	t.Logf("HTML:\n%s", result.HTML)
	t.Logf("HYDRATION:\n%s", hjs)
	// root is a definite object so the chain SHOULD fold to NEW.
	if !strings.Contains(result.HTML, "NEW") {
		t.Errorf("SSR did not fold resolvable chain to NEW:\n%s", result.HTML)
	}
	for _, bad := range []string{`()=>props`, `props.root`, `variable badge`} {
		if strings.Contains(hjs, bad) {
			t.Logf("hydration contains %q: %s", bad, hjs)
		}
	}
}

// An absent nested prop chain (props.meta?.title with no `meta` prop) must
// render as nothing, never the literal text "undefined". Guard for the
// props-chain fold, which must stay scoped so it can't leak JS `undefined`
// into SSR output for ordinary components.
func TestNestedMemberAbsentPropRendersNothing(t *testing.T) {
	src := `
function Card(props: { meta?: { title?: string } }) {
  return <div>{props.meta?.title}</div>;
}
export default function Page() {
  return <Card />;
}
`
	result, _ := fullPipeline(t, src)
	if strings.Contains(result.HTML, "undefined") {
		t.Errorf("absent nested prop leaked 'undefined' into SSR output:\n%s", result.HTML)
	}
}
