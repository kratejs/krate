package css

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestImportLayerWrapped(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"layered.css": ".x{color:red}"})
	out := InlineImports(`@import "./layered.css" layer(base);`, dir)
	if !strings.Contains(out, "@layer base{.x{color:red}") {
		t.Fatalf("layer not applied: %s", out)
	}
	out = InlineImports(`@import "./layered.css" layer;`, dir)
	if !strings.Contains(out, "@layer{.x{color:red}") {
		t.Fatalf("anonymous layer not applied: %s", out)
	}
}

func TestImportSupportsMediaLayerNesting(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"sup.css": ".s{a:b}"})
	out := InlineImports(`@import "./sup.css" supports(display: grid) screen;`, dir)
	if !strings.Contains(out, "@supports (display: grid){@media screen{.s{a:b}") {
		t.Fatalf("bad nesting: %s", out)
	}
	out = InlineImports(`@import "./sup.css" layer(l) supports(not (display: grid)) print and (min-width:1px);`, dir)
	if !strings.Contains(out, "@supports not (display: grid){@media print and (min-width:1px){@layer l{.s{a:b}") {
		t.Fatalf("bad nesting: %s", out)
	}
}

func TestImportIgnoresCommentsAndKeepsRepeats(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"print.css": ".p{a:b}"})
	src := `/* @import "./print.css"; */
@import "./print.css" print;
@import "./print.css" screen;
.k{c:d}`
	out := InlineImports(src, dir)
	if !strings.Contains(out, "@media print{.p{a:b}") || !strings.Contains(out, "@media screen{.p{a:b}") {
		t.Fatalf("repeated import with different media dropped: %s", out)
	}
	if !strings.Contains(out, `/* @import "./print.css"; */`) {
		t.Fatalf("comment lost: %s", out)
	}
	// An @import in a string or comment alone must not be inlined.
	out = InlineImports(`/* @import "./print.css"; */ .a{content:"@import './print.css';"}`, dir)
	if strings.Contains(out, ".p{") {
		t.Fatalf("import inside comment/string inlined: %s", out)
	}
}

func TestImportCycleOnlyAlongAncestors(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"a.css": `@import "./b.css"; .a{}`,
		"b.css": `@import "./a.css"; .b{}`,
	})
	var warns []string
	out := InlineImportsWith(`@import "./a.css";`, dir, ImportOptions{Root: dir, Warn: func(m string) { warns = append(warns, m) }})
	if !strings.Contains(out, ".a{}") || !strings.Contains(out, ".b{}") {
		t.Fatalf("cycle broke output: %s", out)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "circular") {
		t.Fatalf("want one circular warning, got %v", warns)
	}
}

func TestImportExternalHoisted(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"reset.css": `.reset{margin:0}`,
		"fonts.css": `@import url("https://fonts.googleapis.com/css2?family=Inter");
.f{a:b}`,
	})
	out := InlineImports(`@import "./reset.css";
@import url("https://fonts.googleapis.com/css2?family=Inter");
.m{c:d}`, dir)
	if strings.Index(out, "@import") > strings.Index(out, ".reset") {
		t.Fatalf("external @import below rules: %s", out)
	}
	out = InlineImports(`@import "./fonts.css";`, dir)
	if strings.Index(out, "@import") > strings.Index(out, ".f{") {
		t.Fatalf("nested external @import below rules: %s", out)
	}
}

func TestHoistImportsConcatenatedSheets(t *testing.T) {
	in := `@charset "utf-8";
.a{x:y}
@import url(https://e.com/a.css);
.b{x:y}
@charset "utf-8";`
	out := HoistImports(in)
	if !strings.HasPrefix(out, `@charset "utf-8";`) {
		t.Fatalf("charset not first: %s", out)
	}
	if strings.Index(out, "@import") > strings.Index(out, ".a{") {
		t.Fatalf("import not hoisted: %s", out)
	}
	if strings.Count(out, "@charset") != 1 {
		t.Fatalf("duplicate charset kept: %s", out)
	}
}

func TestHoistImportsKeepsLeadingLayerStatement(t *testing.T) {
	out := HoistImports("@layer a,b;\n.x{y:z}\n@import url(https://e.com/a.css);")
	i := strings.Index(out, "@layer a,b;")
	j := strings.Index(out, "@import")
	k := strings.Index(out, ".x{")
	if !(i >= 0 && i < j && j < k) {
		t.Fatalf("bad order: %s", out)
	}
}

func TestImportSiblingDirAndPackageAndURLRebase(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"src/styles/tokens.css":               `:root{--c:red}`,
		"src/styles/sub/app.css":              `@import "../tokens.css"; @import url(./unq.css); .app{a:b}`,
		"src/styles/sub/unq.css":              `.unq{a:b}`,
		"node_modules/fakepkg/dist/style.css": `.pkg{a:url(./p.png)}`,
		"node_modules/fakepkg/dist/p.png":     "x",
		"src/styles/sub/deep/deep.css":        `.d{background:url(./d.png)}`,
		"src/styles/sub/deep/d.png":           "y",
		"src/styles/sub/importsdeep.css":      `@import "./deep/deep.css";`,
	})
	dir := filepath.Join(root, "src/styles/sub")
	var reads []string
	opts := ImportOptions{
		Root: root,
		Resolve: func(spec, from string) string {
			p := filepath.Join(root, "node_modules", filepath.FromSlash(spec))
			if fileExists(p) {
				return p
			}
			return ""
		},
		RewriteURLs: func(c, d string) string {
			return RewriteURLs(c, func(ref string) (string, bool) {
				if strings.HasPrefix(ref, "./") {
					return "/assets/" + filepath.Base(d) + "-" + ref[2:], true
				}
				return "", false
			})
		},
		OnRead: func(p string) { reads = append(reads, p) },
	}
	app, _ := os.ReadFile(filepath.Join(dir, "app.css"))
	out := InlineImportsWith(string(app)+`@import "fakepkg/dist/style.css"; @import "./importsdeep.css";`, dir, opts)
	for _, want := range []string{"--c:red", ".unq{", ".pkg{a:url(/assets/dist-p.png)}", "url(/assets/deep-d.png)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "@import") {
		t.Errorf("import left behind:\n%s", out)
	}
	if len(reads) < 5 {
		t.Errorf("OnRead missed files: %v", reads)
	}
}

func TestImportUnresolvedWarns(t *testing.T) {
	dir := t.TempDir()
	var warns []string
	out := InlineImportsWith(`@import "./nope.css"; @import "../../../etc/passwd";`, dir, ImportOptions{Root: dir, Warn: func(m string) { warns = append(warns, m) }})
	if len(warns) != 2 {
		t.Fatalf("want 2 warnings, got %v", warns)
	}
	if !strings.Contains(out, "nope.css") {
		t.Fatalf("statement dropped: %s", out)
	}
}

func TestRewriteURLsForms(t *testing.T) {
	in := `a{b:url(x.png)} c{d:url( 'y.png' )} e{f:URL("z.png")} /* url(skip.png) */ g{content:"url(no.png)"}
@import url(foo.css); h{i:url(/root.png)}`
	var seen []string
	out := RewriteURLs(in, func(ref string) (string, bool) {
		seen = append(seen, ref)
		if strings.HasPrefix(ref, "/") {
			return "", false
		}
		return "/a/" + ref, true
	})
	if strings.Join(seen, ",") != "x.png,y.png,z.png,/root.png" {
		t.Fatalf("seen=%v", seen)
	}
	for _, want := range []string{"url(/a/x.png)", "url('/a/y.png')", `url("/a/z.png")`, "url(skip.png)", "url(/root.png)", "@import url(foo.css)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
}
