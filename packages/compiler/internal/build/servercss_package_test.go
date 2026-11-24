package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSSRPageImportsPackageStylesheet: a bare import of a package whose entry
// is a stylesheet (font packages such as @fontsource/inter) must not stay an
// external import in the server bundle, where Node cannot load a .css file.
func TestSSRPageImportsPackageStylesheet(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/_layout.tsx": `import "@fake/font-main";
export default function Layout({ children }: { children: any }) { return <div class="shell">{children}</div>; }`,
		"src/pages/index.tsx": `import "@fake/font-exports";
import "@fake/font-exports/400.css";
export const config = { ssr: true };
export default async function Home() { return <h1>home ok</h1>; }`,
		"node_modules/@fake/font-main/package.json":    `{"name":"@fake/font-main","version":"1.0.0","main":"index.css","style":"index.css"}`,
		"node_modules/@fake/font-main/index.css":       `@font-face { font-family: "Main"; src: url(./files/main.woff2) format("woff2"); }`,
		"node_modules/@fake/font-exports/package.json": `{"name":"@fake/font-exports","version":"1.0.0","exports":{".":"./index.css","./*.css":"./*.css","./package.json":"./package.json"}}`,
		"node_modules/@fake/font-exports/index.css":    `@font-face { font-family: "Exp"; src: url(./files/exp.woff2) format("woff2"); }`,
		"node_modules/@fake/font-exports/400.css":      `@font-face { font-family: "Exp"; font-weight: 400; src: url(./files/exp-400.woff2) format("woff2"); }`,
	})
	e2eEnv(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	status, _, html := getWith(t, base+"/", nil)
	if status != 200 || !strings.Contains(html, "<h1>home ok</h1>") || !strings.Contains(html, "shell") {
		t.Fatalf("status %d:\n%s", status, html)
	}
	// The stylesheets still reach the page through its linked CSS.
	css := readDistCSS(t, b.Cfg.OutDir)
	for _, family := range []string{`"Main"`, `"Exp"`} {
		if !strings.Contains(css, family) {
			t.Errorf("page CSS is missing the %s font-face:\n%s", family, css)
		}
	}
}

func readDistCSS(t *testing.T, outDir string) string {
	t.Helper()
	var sb strings.Builder
	_ = filepath.WalkDir(outDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".css") {
			data, _ := os.ReadFile(p)
			sb.Write(data)
		}
		return nil
	})
	return sb.String()
}
