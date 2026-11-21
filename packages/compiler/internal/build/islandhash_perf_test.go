package build

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// namingSite writes a production output tree shaped like a large app's: entries
// island entries (each importing a shared chunk, with a source map), as many
// shared chunks, pages documents (the build id placeholder, a page script, an
// entry preload), page scripts importing two entries each (with source maps),
// and bundles server bundles naming three entries each, padded to bundleKB.
func namingSite(t testing.TB, dir string, entries, pages, bundles, bundleKB int) {
	t.Helper()
	write := func(rel, s string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0644); err != nil {
			t.Fatal(err)
		}
	}
	entry := func(i int) string { return fmt.Sprintf("island_%012x.js", i+1) }
	pad := strings.Repeat("var a=b.c.d(e,f.g);", 400)
	for i := 0; i < entries; i++ {
		write("chunks/"+entry(i), fmt.Sprintf("import{a}from\"./isl-%08d.js\";export function mount(){return a(%d)}%s\n//# sourceMappingURL=%s.map", i, i, pad, entry(i)))
		write("chunks/"+entry(i)+".map", fmt.Sprintf(`{"version":3,"file":"%s","sources":["x.tsx"]}`, entry(i)))
		write(fmt.Sprintf("chunks/isl-%08d.js", i), "export const a=x=>x;"+pad)
	}
	for i := 0; i < pages; i++ {
		script := fmt.Sprintf("p%d/index.%s.js", i, hashContent([]byte(fmt.Sprint("page", i))))
		write(script, fmt.Sprintf("import(\"/chunks/%s\").then(m=>m.mount());import(\"/chunks/%s\");%s\n//# sourceMappingURL=%s.map", entry(i%entries), entry((i+1)%entries), pad, filepath.Base(script)))
		write(script+".map", fmt.Sprintf(`{"version":3,"file":"%s","sources":["p.tsx"]}`, filepath.Base(script)))
		write(fmt.Sprintf("p%d/index.html", i), fmt.Sprintf(`<!doctype html><meta name="krate-build" content="%s"><link rel=modulepreload href=/chunks/%s><script type=module src=/%s></script>%s`, buildIDPlaceholder, entry(i%entries), script, strings.Repeat("<p>text</p>", 300)))
	}
	big := strings.Repeat("function f(a){return a.b.c}", bundleKB*1024/28)
	for i := 0; i < bundles; i++ {
		write(fmt.Sprintf(".krate/server-bundles/b%d.server.mjs", i), fmt.Sprintf(`var A=__krateCB("m","default","/chunks/%s");var B=__krateCB("m","default","/chunks/%s");var C=__krateCB("m","default","/chunks/%s");var H='<meta name="krate-build" content="%s">';%s`, entry(i%entries), entry((i+7)%entries), entry((i+13)%entries), buildIDPlaceholder, big))
	}
}

// TestNameOutputsByContentOnePass checks the naming on a small site: no file
// keeps a source name, every renamed file's hash covers its final content
// (the final names it references, its own masked), source maps move with their
// files, the build id replaces the placeholder, each text output is read once
// and only the files that change are written.
func TestNameOutputsByContentOnePass(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	namingSite(t, dir, 20, 30, 10, 4)
	n, err := nameOutputsByContent(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	texts := 0
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && renameTextExts[filepath.Ext(p)] {
			texts++
		}
		return nil
	})
	// 20 entries, 20 entry maps, 30 page scripts, 30 script maps, 30
	// documents and 10 server bundles change; the 20 shared chunks do not.
	if n.read != texts || n.written != 140 {
		t.Errorf("read %d of %d text files, wrote %d, want 140", n.read, texts, n.written)
	}
	if len(n.renames) != 50 {
		t.Errorf("want 20 entries and 30 page scripts renamed, got %d", len(n.renames))
	}
	stable := regexp.MustCompile(`island_[0-9a-f]{12}\.js|` + buildIDPlaceholder)
	hashedScript := regexp.MustCompile(`^index\.([0-9a-z]{6})\.js$`)
	scripts := 0
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		if m := stable.Find(data); m != nil {
			t.Errorf("%s still holds %s", rel, m)
		}
		base := filepath.Base(p)
		if m := hashedScript.FindStringSubmatch(base); m != nil {
			scripts++
			if hashContent([]byte(strings.ReplaceAll(string(data), base, selfName))) != m[1] {
				t.Errorf("%s: its name does not hash its final content", rel)
			}
			if _, err := os.Stat(p + ".map"); err != nil {
				t.Errorf("%s: its source map did not move with it", rel)
			}
		}
		if strings.HasPrefix(base, "island_") && strings.HasSuffix(base, ".js") {
			if !hashedEntryRe.MatchString("chunks/" + base) {
				t.Errorf("entry %s is not named by its content", rel)
			}
			if !strings.Contains(string(data), "sourceMappingURL="+base+".map") {
				t.Errorf("entry %s does not name its moved source map", rel)
			}
		}
		return nil
	})
	if scripts != 30 {
		t.Errorf("want 30 renamed page scripts, found %d", scripts)
	}
	if len(n.buildID) != 16 || !strings.Contains(readBuilt(t, filepath.Join(dir, "p0", "index.html")), n.buildID) {
		t.Errorf("the build id %q is not in the documents", n.buildID)
	}
	for _, h := range n.hashed {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(h))); err != nil {
			t.Errorf("hashed asset %s does not exist", h)
		}
	}
}

// TestNameOutputsByContentCycle: content-hashed files that name each other
// cannot be named from their content; the build says so.
func TestNameOutputsByContentCycle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeProjectFile(t, dir, "chunks/island_000000000001.js", "export const x=1;")
	writeProjectFile(t, dir, "a.aaaaaa.js", `import("/chunks/island_000000000001.js");import("./b.bbbbbb.js")`)
	writeProjectFile(t, dir, "b.bbbbbb.js", `import("./a.aaaaaa.js")`)
	_, err := nameOutputsByContent(dir, nil)
	if err == nil || !strings.Contains(err.Error(), "cycle") || !strings.Contains(err.Error(), "a.aaaaaa.js") {
		t.Fatalf("want a cycle error naming the files, got %v", err)
	}
}

// TestNameOutputsByContentLargeSite names a site the size of the Genesis app
// (520 island entries, 1300 server bundles, about 40 MB of text) in well under
// a second. Renaming that repeated a scan of every output for every name took
// a minute there.
func TestNameOutputsByContentLargeSite(t *testing.T) {
	if testing.Short() {
		t.Skip("writes a large output tree")
	}
	dir := t.TempDir()
	namingSite(t, dir, 520, 120, 1300, 24)
	start := time.Now()
	n, err := nameOutputsByContent(dir, nil)
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("named %d files, read %d, wrote %d in %v", len(n.renames), n.read, n.written, took)
	limit := 2 * time.Second
	if raceEnabled {
		limit = 20 * time.Second
	}
	if took > limit {
		t.Errorf("naming took %v, want under %v", took, limit)
	}
}

func BenchmarkNameOutputsByContent(b *testing.B) {
	root := b.TempDir()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		dir := filepath.Join(root, fmt.Sprint(i))
		namingSite(b, dir, 520, 120, 1300, 24)
		b.StartTimer()
		if _, err := nameOutputsByContent(dir, nil); err != nil {
			b.Fatal(err)
		}
	}
}
