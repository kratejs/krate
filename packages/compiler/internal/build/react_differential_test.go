package build

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// Differential test against react-dom/server (React 19). Each fixture in
// testdata/reactdiff/*.tsx is rendered by React (renderToStaticMarkup, via
// packages/react-oracle/render.mjs) and by the real Krate build pipeline. Both
// outputs are parsed into DOM trees, normalized, and compared.
//
// Fixtures known to diverge are listed in testdata/reactdiff/divergences.json
// (fixture -> reason). The test fails for an unlisted divergence and logs a
// listed fixture that now matches. KRATE_REACTDIFF_UPDATE=1 rewrites the list
// (existing reasons are preserved).
//
// Normalization strips only these Krate artifacts, so real divergences stay
// visible:
//   - all comment nodes (Krate <!--k:..-->, <!--/k:..--> and suspense/region
//     markers; React's <!-- --> text separators)
//   - attributes: data-k, data-kattr-*, data-krate-*
//   - the <div id="root"> page wrapper (only its children are compared)
//   - adjacent text nodes are merged; attribute order is irrelevant.

const reactDiffDir = "testdata/reactdiff"

func reactOracleDir(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	// .../packages/compiler/internal/build/<file> -> .../packages/react-oracle
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "react-oracle")
}

// renderWithReact transforms each fixture with esbuild (automatic JSX runtime)
// and renders it in Node with react-dom/server.
func renderWithReact(t *testing.T, names []string) map[string]struct{ HTML, Error string } {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		requireE2E(t, "node not available: %v", err)
		return nil
	}
	oracle := reactOracleDir(t)
	if _, err := os.Stat(filepath.Join(oracle, "node_modules", "react-dom", "package.json")); err != nil {
		requireE2E(t, "react-dom not installed in %s (run pnpm install): %v", oracle, err)
		return nil
	}

	// The transformed modules must live under the oracle package so that
	// bare imports of react resolve from its node_modules.
	tmp, err := os.MkdirTemp(oracle, ".tmp-fixtures-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })

	for _, name := range names {
		src, err := os.ReadFile(filepath.Join(reactDiffDir, name+".tsx"))
		if err != nil {
			t.Fatal(err)
		}
		res := api.Transform(string(src), api.TransformOptions{
			Loader:          api.LoaderTSX,
			Format:          api.FormatESModule,
			JSX:             api.JSXAutomatic,
			JSXImportSource: "react",
			Sourcefile:      name + ".tsx",
		})
		if len(res.Errors) > 0 {
			t.Fatalf("esbuild %s: %s", name, res.Errors[0].Text)
		}
		if err := os.WriteFile(filepath.Join(tmp, name+".mjs"), res.Code, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cmd := exec.Command(node, filepath.Join(oracle, "render.mjs"), tmp)
	cmd.Dir = oracle
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("node render.mjs: %v\n%s", err, stderr.String())
	}
	var raw map[string]struct {
		HTML  string `json:"html"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatalf("decode oracle output: %v", err)
	}
	res := map[string]struct{ HTML, Error string }{}
	for k, v := range raw {
		res[k] = struct{ HTML, Error string }{v.HTML, v.Error}
	}
	return res
}

// renderWithKrate builds the fixture as the index page and returns the
// children of the page root element as an HTML string.
func renderWithKrate(src string) (string, error) {
	root, err := os.MkdirTemp("", "reactdiff-krate-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(root)
	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(src), 0o644); err != nil {
		return "", err
	}
	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	if err := New(root, cfg).BuildAll(); err != nil {
		return "", fmt.Errorf("krate build: %w", err)
	}
	b, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		return "", err
	}
	doc, err := html.Parse(strings.NewReader(string(b)))
	if err != nil {
		return "", err
	}
	rootNode := findByID(doc, "root")
	if rootNode == nil {
		return "", fmt.Errorf("no #root element in output")
	}
	var sb strings.Builder
	for c := rootNode.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&sb, c); err != nil {
			return "", err
		}
	}
	return sb.String(), nil
}

func findByID(n *html.Node, id string) *html.Node {
	if n.Type == html.ElementNode {
		for _, a := range n.Attr {
			if a.Key == "id" && a.Val == id {
				return n
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if r := findByID(c, id); r != nil {
			return r
		}
	}
	return nil
}

func isKrateAttr(k string) bool {
	return k == "data-k" || strings.HasPrefix(k, "data-kattr-") || strings.HasPrefix(k, "data-krate-")
}

// canonical parses an HTML fragment (body context) and returns a normalized,
// line-per-node rendering of its DOM.
func canonical(frag string) []string {
	ctx := &html.Node{Type: html.ElementNode, DataAtom: atom.Body, Data: "body"}
	nodes, err := html.ParseFragment(strings.NewReader(frag), ctx)
	if err != nil {
		return []string{"PARSE ERROR: " + err.Error()}
	}
	var lines []string
	walkCanonical(nodes, 0, &lines)
	return lines
}

func walkCanonical(nodes []*html.Node, depth int, out *[]string) {
	ind := strings.Repeat("  ", depth)
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			*out = append(*out, ind+"#text "+strconv.Quote(text.String()))
			text.Reset()
		}
	}
	for _, n := range nodes {
		switch n.Type {
		case html.CommentNode:
			continue
		case html.TextNode:
			text.WriteString(n.Data)
		case html.ElementNode:
			flush()
			var attrs []string
			for _, a := range n.Attr {
				if isKrateAttr(a.Key) {
					continue
				}
				attrs = append(attrs, a.Key+"="+strconv.Quote(a.Val))
			}
			sort.Strings(attrs)
			line := ind + "<" + n.Data
			if len(attrs) > 0 {
				line += " " + strings.Join(attrs, " ")
			}
			*out = append(*out, line+">")
			var kids []*html.Node
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				kids = append(kids, c)
			}
			walkCanonical(kids, depth+1, out)
		}
	}
	flush()
}

// lineDiff renders a small side-by-side style diff of two line lists.
func lineDiff(react, krate []string) string {
	var sb strings.Builder
	n := len(react)
	if len(krate) > n {
		n = len(krate)
	}
	for i := 0; i < n; i++ {
		var r, k string
		if i < len(react) {
			r = react[i]
		}
		if i < len(krate) {
			k = krate[i]
		}
		if r == k {
			fmt.Fprintf(&sb, "    %s\n", r)
			continue
		}
		if i < len(react) {
			fmt.Fprintf(&sb, "  - %s   (react)\n", r)
		}
		if i < len(krate) {
			fmt.Fprintf(&sb, "  + %s   (krate)\n", k)
		}
	}
	return sb.String()
}

func TestReactDifferential(t *testing.T) {
	entries, err := filepath.Glob(filepath.Join(reactDiffDir, "*.tsx"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("no fixtures in %s: %v", reactDiffDir, err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(filepath.Base(e), ".tsx"))
	}
	sort.Strings(names)

	reactOut := renderWithReact(t, names)
	if reactOut == nil {
		return
	}

	divPath := filepath.Join(reactDiffDir, "divergences.json")
	known := map[string]string{}
	if b, err := os.ReadFile(divPath); err == nil {
		if err := json.Unmarshal(b, &known); err != nil {
			t.Fatalf("parse %s: %v", divPath, err)
		}
	}
	update := os.Getenv("KRATE_REACTDIFF_UPDATE") != ""

	diverged := map[string]string{} // fixture -> diff text
	for _, name := range names {
		r, ok := reactOut[name]
		if !ok || r.Error != "" {
			t.Errorf("%s: react oracle failed (fixture bug): %q", name, r.Error)
			continue
		}
		src, err := os.ReadFile(filepath.Join(reactDiffDir, name+".tsx"))
		if err != nil {
			t.Fatal(err)
		}
		kHTML, err := renderWithKrate(string(src))
		if err != nil {
			diverged[name] = "  krate error: " + strings.ReplaceAll(err.Error(), "\n", " ")
			continue
		}
		rc, kc := canonical(r.HTML), canonical(kHTML)
		if strings.Join(rc, "\n") != strings.Join(kc, "\n") {
			diverged[name] = lineDiff(rc, kc) + "  react raw: " + r.HTML + "\n  krate raw: " + kHTML + "\n"
		}
	}

	if update {
		next := map[string]string{}
		for name := range diverged {
			reason := known[name]
			if reason == "" {
				reason = "TODO: classify"
			}
			next[name] = reason
		}
		b, _ := json.MarshalIndent(next, "", "  ")
		if err := os.WriteFile(divPath, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s: %d of %d fixtures diverge", divPath, len(next), len(names))
		return
	}

	for _, name := range names {
		diff, bad := diverged[name]
		_, listed := known[name]
		switch {
		case bad && !listed:
			t.Errorf("%s diverges from React and is not in divergences.json:\n%s", name, diff)
		case !bad && listed:
			t.Logf("FIXED: %s now matches React; remove it from divergences.json (or run with KRATE_REACTDIFF_UPDATE=1)", name)
		}
	}
	for name := range known {
		if _, err := os.Stat(filepath.Join(reactDiffDir, name+".tsx")); err != nil {
			t.Errorf("divergences.json lists unknown fixture %q", name)
		}
	}
	t.Logf("react differential: %d fixtures, %d match, %d diverge (all expected: %v)",
		len(names), len(names)-len(diverged), len(diverged), !t.Failed())
}
