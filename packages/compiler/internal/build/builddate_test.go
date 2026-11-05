package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const dateClientPage = `import { createSignal } from "@krate/runtime";
export default function P() {
  const [n, setN] = createSignal(0);
  return <main>
    <p id="year">{new Date().getUTCFullYear()}</p>
    <p id="now">{Date.now()}</p>
    <p id="rnd">{Math.random()}</p>
    <p id="iso">{new Date("2020-01-01").toISOString()}</p>
    <button onClick={() => setN(n() + 1)}>{n()}</button>
  </main>;
}`

const dateServerPage = `// @server
export default function P() {
  const now = new Date();
  const year = now.getUTCFullYear();
  return <main><p id="year">{new Date().getUTCFullYear()}</p><p id="local">{year}-{now.toISOString().slice(5, 7)}</p><p id="rnd">{Math.random()}</p></main>;
}`

func readDateOut(t *testing.T, outDir string, parts ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{outDir}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// new Date(), Date.now() and Math.random() in a page are baked into the HTML
// at build time, SOURCE_DATE_EPOCH names the instant, a client page keeps a
// binding that computes them again in the browser, and nothing is reported as
// having no build-time value.
func TestDateAndRandomAreBakedAtBuildTime(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
	t.Setenv("KRATE_NO_BUILD_CACHE", "1")
	_, out, logged := buildFiles(t, map[string]string{
		"src/pages/index.tsx":  dateClientPage,
		"src/pages/server.tsx": dateServerPage,
	}, nil)
	if strings.Contains(logged, "KR8011") {
		t.Errorf("a build-time value was reported as missing:\n%s", logged)
	}
	html := readDateOut(t, out, "index.html")
	for _, want := range []string{">2023<", ">1700000000000<", ">2020-01-01T00:00:00.000Z<"} {
		if !strings.Contains(html, want) {
			t.Errorf("client page HTML lacks %q:\n%s", want, html)
		}
	}
	if !regexp.MustCompile(`id=rnd>(?:<!--[^>]*-->)?0\.\d+`).MatchString(html) {
		t.Errorf("Math.random() was not baked:\n%s", html)
	}
	js := ""
	files, _ := filepath.Glob(filepath.Join(out, "*.js"))
	for _, f := range files {
		js += readDateOut(t, f)
	}
	for _, want := range []string{"new Date().getUTCFullYear()", "Date.now()", "Math.random()"} {
		if !strings.Contains(js, want) {
			t.Errorf("the hydration script does not compute %q again:\n%s", want, js)
		}
	}
	// The deterministic expression is folded, with no binding.
	if strings.Contains(js, `"2020-01-01"`) {
		t.Errorf("a deterministic Date was left to the browser:\n%s", js)
	}
	server := readDateOut(t, out, "server", "index.html")
	if !strings.Contains(server, ">2023<") || !strings.Contains(server, ">2023-11<") {
		t.Errorf("server page HTML lacks the build year or a local derived from the clock:\n%s", server)
	}
}

// With SOURCE_DATE_EPOCH set two builds of the same sources are byte-identical
// even though they read the clock and the random number generator.
func TestSourceDateEpochMakesBuildsReproducible(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
	t.Setenv("KRATE_NO_BUILD_CACHE", "1")
	files := map[string]string{"src/pages/index.tsx": dateClientPage, "src/pages/server.tsx": dateServerPage}
	_, out1, _ := buildFiles(t, files, nil)
	_, out2, _ := buildFiles(t, files, nil)
	for _, name := range []string{"index.html", filepath.Join("server", "index.html")} {
		if a, b := readDateOut(t, out1, name), readDateOut(t, out2, name); a != b {
			t.Errorf("%s differs between two builds:\n%s\n%s", name, a, b)
		}
	}
}
