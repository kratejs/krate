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
