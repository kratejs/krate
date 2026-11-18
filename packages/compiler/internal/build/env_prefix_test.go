package build

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// envPrefixProject builds a one-page project with the given config tweak and
// .env values, returning the page HTML, hydration script and build output.
func envPrefixProject(t *testing.T, env map[string]string, prefixes []string, page string) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", page)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	cfg.EnvPrefix = prefixes
	b := New(root, cfg)
	b.Env = env
	out, err := captureOutput(func() error { return b.BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	html := readOut(t, cfg.OutDir, "index.html")
	js := ""
	if files, _ := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js")); len(files) > 0 {
		js = readOut(t, cfg.OutDir, filepath.Base(files[0]))
	}
	return html, js, out
}

const envReadPage = `
import { createSignal } from "@krate/runtime";
export default function Page() {
  const [n, setN] = createSignal(0);
  return (
    <div>
      <p id="a">{process.env.%s}</p>
      <button onClick={() => setN(n() + 1)} data-x={import.meta.env.%s}>{n()}</button>
    </div>
  );
}`

func envReadPageFor(name string) string {
	return strings.NewReplacer("%s", name).Replace(envReadPage)
}

func TestEnvPrefixInlinesConfiguredPrefixes(t *testing.T) {
	html, js, out := envPrefixProject(t,
		map[string]string{"NEXT_PUBLIC_SITE": "https://site.example.test", "SECRET": "nope-1234"},
		[]string{"KRATE_PUBLIC_", "NEXT_PUBLIC_"}, envReadPageFor("NEXT_PUBLIC_SITE"))
	if !strings.Contains(html, `<p id="a">https://site.example.test</p>`) {
		t.Errorf("NEXT_PUBLIC_SITE not inlined in html:\n%s\n%s", html, out)
	}
	if !strings.Contains(html+js, `data-x="https://site.example.test"`) {
		t.Errorf("import.meta.env.NEXT_PUBLIC_SITE not inlined:\n%s", html+js)
	}
	if strings.Contains(out, "KR8310") {
		t.Errorf("an inlined read warned:\n%s", out)
	}
}

func TestEnvPrefixDefaultLeavesNextPublicAlone(t *testing.T) {
	html, _, out := envPrefixProject(t, map[string]string{"NEXT_PUBLIC_SITE": "https://site.example.test"},
		nil, envReadPageFor("NEXT_PUBLIC_SITE"))
	if strings.Contains(html, "site.example.test") {
		t.Errorf("NEXT_PUBLIC_SITE was inlined without envPrefix:\n%s", html)
	}
	want := `NEXT_PUBLIC_ is not a public prefix in this project; add "NEXT_PUBLIC_" to envPrefix in krate.config.ts, or rename the variable to KRATE_PUBLIC_SITE`
	if !strings.Contains(out, want) {
		t.Errorf("want hint (d) %q in:\n%s", want, out)
	}
}

func TestEnvNotExposedHintPrefixedButUnset(t *testing.T) {
	_, _, out := envPrefixProject(t, map[string]string{}, nil, envReadPageFor("KRATE_PUBLIC_UMAMI_URL"))
	want := "KRATE_PUBLIC_UMAMI_URL is not set when krate build runs. Define it in .env (Krate loads .env, .env.production, .env.local, .env.production.local from .) or export it in the build environment"
	if !strings.Contains(out, want) {
		t.Errorf("want hint (a) %q in:\n%s", want, out)
	}
}

func TestEnvNotExposedHintTwinUnderAnotherPrefix(t *testing.T) {
	_, _, out := envPrefixProject(t, map[string]string{"NEXT_PUBLIC_UMAMI_URL": "https://u.example.test"},
		nil, envReadPageFor("KRATE_PUBLIC_UMAMI_URL"))
	want := `NEXT_PUBLIC_UMAMI_URL is set but the code reads KRATE_PUBLIC_UMAMI_URL. Rename one of them, or add "NEXT_PUBLIC_" to envPrefix in krate.config.ts.`
	if !strings.Contains(strings.ReplaceAll(out, "\n", " "), strings.TrimSuffix(want, ".")) {
		t.Errorf("want hint (b) %q in:\n%s", want, out)
	}
}

func TestEnvNotExposedHintUnprefixed(t *testing.T) {
	t.Setenv("KRATE_PUBLIC_API_URL", "")
	_, _, out := envPrefixProject(t, map[string]string{}, nil, envReadPageFor("API_URL"))
	if !strings.Contains(out, "(KRATE_PUBLIC_API_URL is set: use that name)") {
		t.Errorf("want the set twin named (an empty value counts as set):\n%s", out)
	}
	if !strings.Contains(out, "name the variable with the KRATE_PUBLIC_ prefix") {
		t.Errorf("want the prefix hint:\n%s", out)
	}
}

func TestEnvPrefixRejectsUnsafeEntries(t *testing.T) {
	for _, bad := range []string{`""`, `"NEXT_PUBLIC"`, `["KRATE_PUBLIC_", ""]`} {
		root := t.TempDir()
		writeFileRel(t, root, "krate.config.json", `{"envPrefix": `+bad+`}`)
		if _, err := config.Load(root); err == nil || !strings.Contains(err.Error(), "envPrefix") {
			t.Errorf("envPrefix %s: want a config error, got %v", bad, err)
		}
	}
	root := t.TempDir()
	writeFileRel(t, root, "krate.config.json", `{"envPrefix": "NEXT_PUBLIC_"}`)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.PublicEnvPrefixes(); len(got) != 1 || got[0] != "NEXT_PUBLIC_" {
		t.Errorf("prefixes = %v", got)
	}
}
