package build

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestHelperCallWithSignalArgIsNotDropped(t *testing.T) {
	files := map[string]string{
		"src/pages/index.tsx": `
import { createSignal } from "@krate/runtime";
const fmt = (n: number) => "v" + n;
export default function Page() {
  const [n, setN] = createSignal(1);
  return <div><p>{fmt(n())}</p><button onClick={() => setN(n() + 1)}>go</button></div>;
}`,
	}
	html, js, err := buildKrateProject(t, files)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(html, "v1") || !strings.Contains(js, "fmt(n())") {
		t.Errorf("fmt(n()) must render and stay reactive:\nhtml=%s\njs=%s", html, js)
	}
}

func envProject(t *testing.T, env map[string]string, page string) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", page)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
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

func TestPublicEnvInlinedInSSRAndClient(t *testing.T) {
	t.Setenv("KRATE_PUBLIC_FROM_SHELL", "shell-value")
	html, js, out := envProject(t, map[string]string{
		"KRATE_PUBLIC_API":  "https://api.example.test",
		"SECRET_TOKEN":      "do-not-leak-1234",
		"KRATE_PUBLIC_MODE": "x",
	}, `
import { createSignal } from "@krate/runtime";
export default function Page() {
  const [n, setN] = createSignal(0);
  return (
    <div>
      <p id="a">{import.meta.env.KRATE_PUBLIC_API}</p>
      <p id="b">{process.env.KRATE_PUBLIC_FROM_SHELL}</p>
      <p id="c">{import.meta.env.MODE}</p>
      <p id="d">{String(import.meta.env.PROD)}</p>
      <p id="e">{import.meta.env.BASE_URL}</p>
      <button onClick={() => setN(n() + 1)} data-api={import.meta.env.KRATE_PUBLIC_API}>{n()}</button>
    </div>
  );
}`)
	for _, want := range []string{`<p id="a">https://api.example.test</p>`, `<p id="b">shell-value</p>`, `<p id="c">production</p>`, `<p id="d">true</p>`, `<p id="e">/</p>`} {
		if !strings.Contains(html, want) {
			t.Errorf("html misses %s:\n%s\n%s", want, html, out)
		}
	}
	if strings.Contains(html+js, "do-not-leak-1234") || strings.Contains(html+js, "SECRET_TOKEN") {
		t.Errorf("a non-public variable reached client output")
	}
	if strings.Contains(js, "import.meta") {
		t.Errorf("import.meta survived into the classic hydration script:\n%s", js)
	}
}

func TestNonPublicEnvReadIsDiagnosedAndSafe(t *testing.T) {
	html, js, out := envProject(t, map[string]string{"SECRET_TOKEN": "do-not-leak-1234"}, `
import { createSignal } from "@krate/runtime";
export default function Page() {
  const [n, setN] = createSignal(0);
  return (
    <div>
      <p id="a">{process.env.SECRET_TOKEN}</p>
      <button onClick={() => setN(n() + (import.meta.env.OTHER ? 1 : 2))}>{n()}</button>
    </div>
  );
}`)
	if !strings.Contains(out, "KR8310") || !strings.Contains(out, "process.env.SECRET_TOKEN") || !strings.Contains(out, "import.meta.env.OTHER") || !strings.Contains(out, "KRATE_PUBLIC_") {
		t.Errorf("expected KR8310 warnings with the prefix hint:\n%s", out)
	}
	if strings.Contains(html+js, "do-not-leak-1234") {
		t.Errorf("secret reached output")
	}
	if strings.Contains(js, "import.meta") || strings.Contains(js, "process.env") {
		t.Errorf("unexposed environment read left in the hydration script:\n%s", js)
	}
}
