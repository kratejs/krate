package build

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Many async pages, each in its own directory, render the same lucide-react
// icons, directly and through a module that re-exports them. Every page has
// the same package boundary (same specifier, same exports) but a different
// importer directory, and the batched chunk checks (#37) are shared between
// them. Every chunk a server bundle or page names must be written.
func TestPackageBoundaryChunkSharedAcrossDirectories(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "packages", "runtime", "dist", "krate-runtime.js")); err != nil {
		requireE2E(t, "the runtime package is not built: %v", err)
	}
	icons := []string{"Activity", "BadgeCheck", "Bot", "Car", "LogOut", "Settings", "Users", "Wallet"}
	files := map[string]string{
		"src/components/nav-icons.ts": "export { " + strings.Join(icons, ", ") + " } from \"lucide-react\";\n",
		"src/components/nav.tsx": `import { ` + strings.Join(icons, ", ") + ` } from "./nav-icons";
export function Nav() {
  return <nav>` + iconTags(icons) + `</nav>;
}`,
	}
	const pages = 12
	for i := range pages {
		dir := fmt.Sprintf("src/pages/area%d/section%d", i%3, i)
		// A stateful client component of the page's own, so the page's chunk
		// checks go through a batch with the icons' boundary.
		files[fmt.Sprintf("src/components/w%d.tsx", i)] = fmt.Sprintf(`"use client";
import { useState } from "react";
export function W%d() { const [n, setN] = useState(0); return <button onClick={() => setN(n + 1)}>w%d {n}</button>; }`, i, i)
		body := "<main>" + iconTags(icons) + fmt.Sprintf("<W%d /></main>", i)
		imp := "import { " + strings.Join(icons, ", ") + " } from \"lucide-react\";\n"
		if i%4 == 3 {
			// A page that only reaches the icons through the re-export module.
			body = fmt.Sprintf("<main><Nav /><W%d /></main>", i)
			imp = `import { Nav } from "../../../components/nav";` + "\n"
		}
		files[dir+"/index.tsx"] = imp + fmt.Sprintf(`import { W%d } from "../../../components/w%d";
export default async function Page() {
  await Promise.resolve();
  return `, i, i) + body + `;
}`
	}
	b, root := scProject(t, files)
	withPreact(t, root)
	linkReactPackages(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	b.Cfg.Minify = true
	before := esbuildStatsSnapshot()
	if out, err := captureOutput(b.BuildAll); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	calls := esbuildStatsSince(before)
	if calls["batch:(*Builder).runClientChunkBatchWith"].n == 0 {
		t.Error("no client chunk check batch ran")
	}
	missing, err := findMissingChunkRefs(b.Cfg.OutDir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range missing {
		t.Errorf("%s is referenced from %v but was not written", m.URL, m.Files)
	}
	// The pages do reference the icons' boundary chunk.
	refs := map[string]bool{}
	cbRe := regexp.MustCompile(`__krateCB\("[^"]+", "Wallet", "(/chunks/island_[0-9a-f]+\.js)"`)
	_ = filepath.Walk(filepath.Join(b.Cfg.OutDir, ".krate", "server-bundles"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".mjs") {
			return nil
		}
		data, _ := os.ReadFile(p)
		for _, m := range cbRe.FindAllStringSubmatch(string(data), -1) {
			refs[m[1]] = true
		}
		return nil
	})
	if len(refs) == 0 {
		t.Fatal("no server bundle references the icons' boundary chunk")
	}
	t.Logf("boundary chunks of the icons: %v", refs)
}

func iconTags(names []string) string {
	var sb strings.Builder
	for _, n := range names {
		sb.WriteString("<" + n + " />")
	}
	return sb.String()
}

func TestFindMissingChunkRefs(t *testing.T) {
	out := t.TempDir()
	write := func(rel, s string) {
		p := filepath.Join(out, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("chunks/island_aaa.js", `import "./isl-1.js";`)
	write("chunks/isl-1.js", ``)
	write("index.html", `<script type="module" src="/base/chunks/island_aaa.js"></script><link rel="modulepreload" href='/base/chunks/island_bbb.js'>`)
	write(".krate/server-bundles/p.server.mjs", "__krateCB(\"m\", \"X\", \"/base/chunks/island_ccc.js\", a);\nimport(`/base/chunks/island_aaa.js`);")
	// Page content that mentions a chunk URL, escaped in a string or without
	// quotes, and URLs without the base path or with an interpolation.
	write("docs/index.html", "<p>/base/chunks/island_ddd.js</p><script>const s = \"say \\\"/base/chunks/island_eee.js\\\"\";</script>")
	write("other.js", `fetch("/chunks/island_fff.js"); import("/base/chunks/" + n + ".js"); x("/base/chunks/${n}.js")`)
	write("public-file.js", `import("/base/chunks/island_ggg.js")`)
	write("manifest.json", `{"a": "/base/chunks/island_ccc.js", "b": "/base/chunks/island_aaa.js"}`)
	write("style.css", `url("/base/chunks/island_hhh.js")`)

	got, err := findMissingChunkRefs(out, "/base", func(rel string) bool { return rel == "public-file.js" })
	if err != nil {
		t.Fatal(err)
	}
	want := []missingChunkRef{
		{URL: "/base/chunks/island_bbb.js", Files: []string{"index.html"}},
		{URL: "/base/chunks/island_ccc.js", Files: []string{".krate/server-bundles/p.server.mjs", "manifest.json"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("missing refs:\n got %+v\nwant %+v", got, want)
	}

	gotNoBase, err := findMissingChunkRefs(out, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotNoBase) != 1 || gotNoBase[0].URL != "/chunks/island_fff.js" {
		t.Fatalf("without a base path: %+v", gotNoBase)
	}
}

func TestCheckChunkRefsFailsBuild(t *testing.T) {
	b, _ := scProject(t, map[string]string{"src/pages/index.tsx": `export default function Home() { return <p>hi</p>; }`})
	dist := b.Cfg.OutDir
	if err := os.MkdirAll(filepath.Join(dist, "chunks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte(`<script type="module" src="/chunks/island_0123456789ab.js"></script>`), 0644); err != nil {
		t.Fatal(err)
	}
	err := b.checkChunkRefs()
	var d diag.Diagnostic
	if !errors.As(err, &d) || d.Code != diag.CodeChunkMissing {
		t.Fatalf("checkChunkRefs = %v, want a %s diagnostic", err, diag.CodeChunkMissing)
	}
	if !strings.Contains(d.Message, "/chunks/island_0123456789ab.js (referenced from index.html)") {
		t.Errorf("message does not name the chunk and its referrer: %s", d.Message)
	}
}
