package build

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var devChunkURL = regexp.MustCompile(`/chunks/[A-Za-z0-9_.\-]+\.js`)

// treeSnapshot records every file under dir with its size and mod time.
func treeSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out[rel] = fmt.Sprintf("%d/%d", info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// `krate build` run while `krate dev` serves the same app must not touch what
// dev serves: dev writes to its own directory, the build to outDir, and a dev
// rebuild afterwards leaves the production build alone.
func TestDevAndProductionBuildShareNoOutput(t *testing.T) {
	d := startDevRun(t, map[string]string{
		"src/pages/index.tsx": `import Toggle from "../components/Toggle";
export default async function Page() { return <main><h1>home one</h1><Toggle /></main>; }
`,
		"src/components/Toggle.tsx": devToggleIsland,
	})
	devOut := DevOutDir(d.root)
	if d.b.Cfg.OutDir != devOut {
		t.Errorf("dev builds into %s, want %s", d.b.Cfg.OutDir, devOut)
	}
	body := d.waitBody(t, "/", "home one")
	chunks := devChunkURL.FindAllString(body, -1)
	hasVendor, hasIsland := false, false
	for _, c := range chunks {
		hasVendor = hasVendor || strings.Contains(c, "/chunks/preact-")
		hasIsland = hasIsland || strings.Contains(c, "/chunks/island_")
	}
	if !hasVendor || !hasIsland {
		t.Fatalf("dev page lacks vendor (%v) or island (%v) chunks:\n%s", hasVendor, hasIsland, body)
	}

	// A production build into the project's outDir while dev runs.
	prodCfg := *d.b.Cfg
	prodCfg.OutDir = filepath.Join(d.root, "dist")
	prod := New(d.root, &prodCfg)
	if err := prod.BuildAll(); err != nil {
		t.Fatalf("production build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(prodCfg.OutDir, "index.html")); err != nil {
		t.Fatalf("production build wrote no index.html: %v", err)
	}

	// The dev page still imports chunks that exist.
	_, body = getText(t, d.base+"/")
	for _, c := range devChunkURL.FindAllString(body, -1) {
		d.waitStatus(t, c, 200)
	}
	for _, c := range chunks {
		d.waitStatus(t, c, 200)
	}

	// A dev edit still rebuilds, and leaves the production output untouched.
	before := treeSnapshot(t, prodCfg.OutDir)
	d.write(t, "src/pages/index.tsx", `import Toggle from "../components/Toggle";
export default async function Page() { return <main><h1>home two</h1><Toggle /></main>; }
`)
	body = d.waitBody(t, "/", "home two")
	for _, c := range devChunkURL.FindAllString(body, -1) {
		d.waitStatus(t, c, 200)
	}
	after := treeSnapshot(t, prodCfg.OutDir)
	if len(before) != len(after) {
		t.Errorf("dev rebuild changed the file count of outDir: %d -> %d", len(before), len(after))
	}
	for p, v := range before {
		if after[p] != v {
			t.Errorf("dev rebuild touched production output %s", p)
		}
	}
}
