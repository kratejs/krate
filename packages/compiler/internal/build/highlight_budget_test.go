package build

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/syntaxhighlight"
)

func gz(b []byte) int {
	var buf bytes.Buffer
	w, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = w.Write(b)
	_ = w.Close()
	return buf.Len()
}

// highlightBudgets are the most each piece of browser highlighting may weigh
// (gzip bytes). They carry about 12% headroom over what ships; raising one
// needs a reason, since this is what a visitor downloads for dynamic code.
var highlightBudgets = map[string]int{
	"tokenizer":  2650,
	"feature":    1400,
	"typescript": 1800, "javascript": 1450, "react": 1600, "json": 500, "css": 8200,
	"html": 9800, "bash": 1300, "go": 1650, "python": 3300, "rust": 1850, "yaml": 820,
	"markdown": 4500, "toml": 460, "xml": 470, "c": 1500, "plaintext": 220,
}

const apiPageSrc = `import { krateHighlight } from "@krate/runtime";
export default function Page() {
	return <button onClick={() => krateHighlight("a", "ts").then((h) => console.log(h))}>go</button>;
}
`

// TestHighlightSizeBudget builds a site that loads every shipped language and
// checks what the browser would download for it against the budgets above, and
// that a site with only static code ships none of the highlighter.
func TestHighlightSizeBudget(t *testing.T) {
	repoRoot := requireRuntimeDist(t)
	root := codeThemeProject(t, map[string]string{"index.tsx": apiPageSrc})
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Highlight.Languages = syntaxhighlight.ShippedLanguages
	cfg.Minify = true
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	chunks, _ := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "hl*.js"))
	sizes := map[string]int{}
	re := regexp.MustCompile(`^hl(?:-(.+))?\.[a-z0-9]+\.js$`)
	for _, f := range chunks {
		data, _ := os.ReadFile(f)
		m := re.FindStringSubmatch(filepath.Base(f))
		if m == nil {
			t.Errorf("unexpected chunk %s", f)
			continue
		}
		name := m[1]
		if name == "" {
			name = "tokenizer"
		}
		sizes[name] = gz(data)
	}
	sizes["feature"] = featureGzip(t, repoRoot)
	if runtimeFiles, _ := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "runtime.*.js")); len(runtimeFiles) == 1 {
		data, _ := os.ReadFile(runtimeFiles[0])
		if !strings.Contains(string(data), "__KRATE_HL") {
			t.Errorf("the runtime chunk of a page that uses the highlighter lacks the highlight feature")
		}
	}
	var names []string
	for n := range sizes {
		names = append(names, n)
	}
	sort.Strings(names)
	var report []string
	for _, n := range names {
		report = append(report, fmt.Sprintf("%s=%d", n, sizes[n]))
		budget, ok := highlightBudgets[n]
		switch {
		case !ok:
			t.Errorf("%s has no budget (%d B gzip)", n, sizes[n])
		case sizes[n] > budget:
			t.Errorf("%s is %d B gzip, over its budget of %d", n, sizes[n], budget)
		}
	}
	t.Logf("gzip bytes: %s", strings.Join(report, " "))
	for lang := range highlightBudgets {
		if _, ok := sizes[lang]; !ok && lang != "feature" {
			t.Errorf("no chunk written for %s", lang)
		}
	}
}

// featureGzip returns the gzip size of the highlight runtime feature, the code
// that loads the chunks and highlights changed text.
func featureGzip(t *testing.T, repoRoot string) int {
	t.Helper()
	feat, err := os.ReadFile(filepath.Join(repoRoot, "packages", "runtime", "dist", "krate-f-highlight.js"))
	if err != nil {
		t.Fatal(err)
	}
	return gz([]byte(minifyJSBase(string(feat))))
}

// TestStaticCodeShipsNoHighlighter: a site whose code is all known at build
// time, and pages with no code, ship no highlighter chunk, map or runtime
// feature.
func TestStaticCodeShipsNoHighlighter(t *testing.T) {
	requireRuntimeDist(t)
	root := codeThemeProject(t, map[string]string{
		"index.tsx": "import { Code } from \"@krate/components\";\nexport default function Page() { return <main><Code lang=\"ts\" title=\"a.ts\">{`const a = 1;`}</Code></main>; }\n",
		"other.tsx": "import { createSignal } from \"@krate/runtime\";\nexport default function Page() { const [n, setN] = createSignal(0); return <main><button onClick={() => setN(n() + 1)}>{n()}</button></main>; }\n",
	})
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	var all []string
	_ = filepath.WalkDir(cfg.OutDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".html")) {
			b, _ := os.ReadFile(p)
			s := string(b)
			for _, marker := range []string{"__KRATE_HL", "krateHighlight", "data-krate-hl", "krate.highlight.registry"} {
				if strings.Contains(s, marker) {
					all = append(all, fmt.Sprintf("%s contains %s", p, marker))
				}
			}
			if strings.Contains(p, "hl.") || strings.Contains(filepath.Base(p), "hl-") {
				all = append(all, p+" is a highlighter chunk")
			}
		}
		return nil
	})
	if len(all) > 0 {
		t.Errorf("static code must ship no highlighter:\n%s", strings.Join(all, "\n"))
	}
}
