package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/syntaxhighlight"
)

const dynamicCodePageSrc = `import { createSignal } from "@krate/runtime";
import { Code } from "@krate/components";
export default function Page() {
	const [src, setSrc] = createSignal("const a: number = 1;\nfunction f(x) { return x + 1; }");
	return (
		<main>
			<span id="alive">alive</span>
			<button id="more" onClick={() => setSrc(src() + "\nlet z = 'x';")}>more</button>
			<button id="same" onClick={() => setSrc(src())}>same</button>
			<button id="big" onClick={() => setSrc("x ".repeat(40000))}>big</button>
			<button id="long" onClick={() => setSrc("a".repeat(3000))}>long</button>
			<button id="hostile1" onClick={() => setSrc(("(".repeat(1500) + "\n").repeat(40))}>h1</button>
			<button id="hostile2" onClick={() => setSrc(("'\"` + "`" + `/*{<$".repeat(250) + "\n").repeat(30))}>h2</button>
			<button id="reset" onClick={() => setSrc("const a: number = 1;\nfunction f(x) { return x + 1; }")}>reset</button>
			<Code lang="ts" title="live.ts">{src()}</Code>
		</main>
	);
}
`

func chromaInner(code, lang string) string {
	return syntaxhighlight.HighlightThemeLines(strings.TrimSpace(code), lang, syntaxhighlight.DefaultTheme, "")
}

// TestDynamicCodeBrowser drives a <Code> whose text is a signal in a real
// browser: it must be highlighted with the same markup the compiler writes,
// plain text first, debounced, left alone when unchanged, left plain when too
// big, and follow the light and dark theme.
func TestDynamicCodeBrowser(t *testing.T) {
	browser := requireBrowserTest(t)
	requireRuntimeDist(t)
	root := codeThemeProject(t, map[string]string{"index.tsx": dynamicCodePageSrc})
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = true
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	initial := "const a: number = 1;\nfunction f(x) { return x + 1; }"
	exp := map[string]string{
		"initial": chromaInner(initial, "ts"),
		"more":    chromaInner(initial+"\nlet z = 'x';", "ts"),
	}
	data, _ := json.Marshal(exp)
	expFile := filepath.Join(t.TempDir(), "expected.json")
	if err := os.WriteFile(expFile, data, 0o644); err != nil {
		t.Fatal(err)
	}
	runBrowserScript(t, "code_dynamic.mjs", cfg.OutDir, browser, expFile)
}

// requireRuntimeDist skips when @krate/runtime is not built (or built before the
// highlighter existed).
func requireRuntimeDist(t *testing.T) string {
	t.Helper()
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"highlight.js", "krate-f-highlight.js", "krate-features.json"} {
		if _, err := os.Stat(filepath.Join(repoRoot, "packages", "runtime", "dist", f)); err != nil {
			requireE2E(t, "the built runtime is missing %s (run pnpm --filter @krate/runtime build)", f)
		}
	}
	return repoRoot
}

var hlChunkRe = regexp.MustCompile(`chunks/hl[.-][^"']*\.js`)

// TestDynamicCodeShipsHighlighterOnlyWhereUsed checks what a build writes: the
// tokenizer and language chunks and the page script's map for a page with
// dynamic code, and nothing for pages with static or no code.
func TestDynamicCodeShipsHighlighterOnlyWhereUsed(t *testing.T) {
	requireRuntimeDist(t)
	pages := map[string]string{
		"index.tsx":   dynamicCodePageSrc,
		"static.tsx":  "import { Code } from \"@krate/components\";\nexport default function Page() { return <main><Code lang=\"ts\">{`const a = 1;`}</Code></main>; }\n",
		"plain.tsx":   "export default function Page() { return <main><h1>hi</h1></main>; }\n",
		"other.tsx":   "import { createSignal } from \"@krate/runtime\";\nexport default function Page() { const [n, setN] = createSignal(0); return <main><button onClick={() => setN(n() + 1)}>{n()}</button></main>; }\n",
		"unknown.tsx": "import { createSignal } from \"@krate/runtime\";\nimport { Code } from \"@krate/components\";\nexport default function Page() { const [s] = createSignal(\"x\"); return <main><Code lang=\"cobol\">{s()}</Code></main>; }\n",
	}
	root := codeThemeProject(t, pages)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(cfg.OutDir, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	pageJS := func(dir string) string {
		files, _ := filepath.Glob(filepath.Join(cfg.OutDir, dir, "index.*.js"))
		var out string
		for _, f := range files {
			b, _ := os.ReadFile(f)
			out += string(b)
		}
		return out
	}
	// The dynamic page maps its language to a chunk and starts the highlighter.
	js := pageJS(".")
	if !strings.Contains(js, "__KRATE_HL") || !strings.Contains(js, "krateHighlightInit()") {
		t.Errorf("dynamic page script does not start the highlighter:\n%.400s", js)
	}
	chunks, _ := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "hl*.js"))
	if len(chunks) != 2 {
		t.Errorf("want the tokenizer and one language chunk, got %v", chunks)
	}
	html := read("index.html")
	if !regexp.MustCompile(`data-krate-hl="?ts"?`).MatchString(html) || !strings.Contains(html, "chroma") {
		t.Errorf("dynamic code is not marked for the runtime")
	}
	// Pages without dynamic code ship none of it.
	for _, dir := range []string{"static", "plain", "other"} {
		if got := pageJS(dir); strings.Contains(got, "__KRATE_HL") || strings.Contains(got, "krateHighlight") {
			t.Errorf("%s ships highlighter code:\n%.300s", dir, got)
		}
		if h := read(filepath.Join(dir, "index.html")); hlChunkRe.MatchString(h) || strings.Contains(h, "data-krate-hl") {
			t.Errorf("%s references the highlighter", dir)
		}
	}
	// A language the tokenizer does not ship is left as plain text (KR7151).
	if h := read("unknown/index.html"); !regexp.MustCompile(`data-krate-hl="?cobol"?`).MatchString(h) {
		t.Errorf("unknown-language page lost its marker")
	}
	if got := pageJS("unknown"); strings.Contains(got, "__KRATE_HL") {
		t.Errorf("a language without a tokenizer must not get a chunk entry:\n%.300s", got)
	}
}
