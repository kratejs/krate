package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// pageJS returns the concatenated page hydration scripts of a build.
func pageJS(t *testing.T, outDir string) string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(outDir, "index.*.js"))
	var sb strings.Builder
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		sb.Write(b)
	}
	return sb.String()
}

var tagRe = regexp.MustCompile(`<!--/?k:[^>]*-->| data-k(attr-[a-z-]+)?=[^ >]+`)

// visibleHTML drops hydration markers so assertions read like the DOM.
func visibleHTML(html string) string { return tagRe.ReplaceAllString(html, "") }

func TestContextDefaultObjectWithoutProvider(t *testing.T) {
	html, _ := buildCompatSrc(t, `import { createContext, useContext } from "react";
const Currency = createContext({ code: "USD", symbol: "$" });
function Price(props: { amount: number }) {
  const c = useContext(Currency);
  return <span className="price">{c.symbol}{props.amount.toFixed(2)} {c.code}</span>;
}
export default function Page() {
  return <div><Price amount={9.5} /><Currency.Provider value={{ code: "EUR", symbol: "E" }}><Price amount={1} /></Currency.Provider></div>;
}`, nil)
	got := visibleHTML(html)
	for _, want := range []string{`<span class=price>$9.50 USD</span>`, `<span class=price>E1.00 EUR</span>`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestContextSelfNestingProviders(t *testing.T) {
	html, _ := buildCompatSrc(t, `import { createContext, useContext } from "react";
const Depth = createContext(0);
const User = createContext("anonymous");
function Level(props: { children?: any }) {
  const depth = useContext(Depth);
  const user = useContext(User);
  return (
    <Depth.Provider value={depth + 1}>
      <div className="level" data-depth={depth + 1}>{user}{props.children}</div>
    </Depth.Provider>
  );
}
export default function Page() {
  return (
    <User.Provider value="ada">
      <Level><Level><User.Provider value="guest"><Level /></User.Provider></Level></Level>
    </User.Provider>
  );
}`, nil)
	got := visibleHTML(html)
	want := `<div class=level data-depth=1>ada<div class=level data-depth=2>ada<div class=level data-depth=3>guest</div></div></div>`
	if !strings.Contains(got, want) {
		t.Errorf("self-nesting providers: want %s in:\n%s", want, got)
	}
}

func TestContextReact19ProviderAndConsumer(t *testing.T) {
	html, outDir := buildCompatSrc(t, `import { createContext, useContext, useState } from "react";
const Density = createContext("comfortable");
const Auth = createContext({ user: null as string | null });
function Row(props: { label: string }) {
  const density = useContext(Density);
  return <li className={density}>{props.label}</li>;
}
export default function Page() {
  const [density, setDensity] = useState("compact");
  return (
    <Density value={density}>
      <ul><Row label="one" /></ul>
      <Auth.Consumer>{(auth) => <p id="who">{auth.user ? "in" : "out"}</p>}</Auth.Consumer>
      <Density.Consumer>{(d) => <p id="d">{d}</p>}</Density.Consumer>
      <button onClick={() => setDensity("comfortable")}>toggle</button>
    </Density>
  );
}`, nil)
	got := visibleHTML(html)
	for _, want := range []string{`<li class=compact>one</li>`, `<p id=who>out</p>`, `<p id=d>compact</p>`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	js := pageJS(t, outDir)
	if !strings.Contains(js, "__krate_ctx_Density") {
		t.Errorf("dynamic provider value should reach Row through its context prop:\n%s", js)
	}
	if strings.Contains(js, "Density.useContext") || strings.Contains(js, "Auth.useContext") {
		t.Errorf("context reads must not reach the hydration code:\n%s", js)
	}
}

func TestContextChildrenSeeComponentProvider(t *testing.T) {
	html, outDir := buildCompatSrc(t, `import { createContext, useContext, useState } from "react";
const Theme = createContext("light");
function Label(props: { text: string }) {
  const theme = useContext(Theme);
  return <span>{props.text} {theme}</span>;
}
function ThemeBox(props: { children?: any }) {
  const [dark, setDark] = useState(false);
  return (
    <Theme.Provider value={dark ? "dark" : "light"}>
      <button onClick={() => setDark(!dark)}>flip</button>
      <div className="box">{props.children}</div>
    </Theme.Provider>
  );
}
export default function Page() {
  return <main><ThemeBox><Label text="in" /></ThemeBox><Label text="out" /></main>;
}`, nil)
	got := visibleHTML(html)
	if !strings.Contains(got, `<span>in light</span>`) || !strings.Contains(got, `<span>out light</span>`) {
		t.Errorf("unexpected SSR output:\n%s", got)
	}
	js := pageJS(t, outDir)
	// ThemeBox publishes its provider value; the call-site child reads it.
	if !strings.Contains(js, `#ctx:Theme"]=function(){return`) {
		t.Errorf("component provider value should be published for its children:\n%s", js)
	}
	if !regexp.MustCompile(`__krate_ctx_Theme:\(\)=>__krate_props\["[^"]+#ctx:Theme"\]\(\)`).MatchString(js) {
		t.Errorf("call-site child should read the published provider value:\n%s", js)
	}
}
