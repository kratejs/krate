package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/css"
)

// workspaceWithSharedFiles is a pnpm workspace whose static-site package
// imports files out of the web app's src/app tree.
func workspaceWithSharedFiles(t *testing.T) (ws, web string) {
	t.Helper()
	ws = t.TempDir()
	web = filepath.Join(ws, "packages", "web")
	writeNextFile(t, ws, "pnpm-workspace.yaml", "packages:\n  - packages/*\n")
	writeNextFile(t, ws, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	writeNextFile(t, ws, "packages/static-site/src/styles.css", "@import '../../web/src/app/tokens.css';\nbody { color: var(--brand); background: url(../../web/src/app/hero.png); }\n")
	writeNextFile(t, ws, "packages/static-site/src/theme.ts", "import theme from '../../web/src/app/theme';\nexport const brand = theme.brand;\n")
	writeNextFile(t, web, "package.json", `{"dependencies":{"next":"16.0.0","react":"19.0.0"}}`)
	writeNextFile(t, web, "src/app/tokens.css", ":root { --brand: #123456; }\n")
	writeNextFile(t, web, "src/app/theme.ts", "export const extra = 1;\nexport default { brand: 'blue' };\n")
	writeNextFile(t, web, "src/app/hero.png", "PNG")
	writeNextFile(t, web, "src/app/layout.tsx", "import './tokens.css';\nexport default function L({ children }) { return <html><body>{children}</body></html>; }\n")
	writeNextFile(t, web, "src/app/page.tsx", "import theme from './theme';\nexport default function P() { return <p>{theme.brand}</p>; }\n")
	return ws, web
}

func TestOutsideReferencesKeepStubsAtTheOldPaths(t *testing.T) {
	ws, web := workspaceWithSharedFiles(t)
	rep, err := ScanNext(web)
	if err != nil {
		t.Fatal(err)
	}
	cssStub := findPlan(rep, "src/app/tokens.css")
	if cssStub == nil || cssStub.Kind != KindStub {
		t.Fatalf("no CSS stub: %+v", rep.Files)
	}
	if !strings.Contains(string(cssStub.Content), "@import '../migrated/app/tokens.css';") {
		t.Errorf("css stub:\n%s", cssStub.Content)
	}
	tsStub := findPlan(rep, "src/app/theme.ts")
	if tsStub == nil || tsStub.Kind != KindStub {
		t.Fatalf("no TS stub: %+v", rep.Files)
	}
	for _, want := range []string{"export * from '../migrated/app/theme';", "export { default } from '../migrated/app/theme';"} {
		if !strings.Contains(string(tsStub.Content), want) {
			t.Errorf("ts stub lacks %q:\n%s", want, tsStub.Content)
		}
	}
	if !hasInfo(rep, "kept src/app/tokens.css as a stub that imports the moved file, because packages/static-site/src/styles.css:1 references it") {
		t.Errorf("css info missing: %v", rep.Info)
	}
	if !hasInfo(rep, "kept src/app/theme.ts as a stub that re-exports the moved file, because packages/static-site/src/theme.ts:1 references it") {
		t.Errorf("ts info missing: %v", rep.Info)
	}
	// The image has no module to re-export: a copy stays and the item names it.
	if png := findPlan(rep, "src/app/hero.png"); png == nil || png.Kind != KindStub || string(png.Content) != "PNG" {
		t.Errorf("image copy: %+v", png)
	}
	if !hasManual(rep, "src/app/hero.png", "packages/static-site/src/styles.css:2") {
		t.Errorf("no manual item for the image: %+v", rep.Manual)
	}
	for _, m := range rep.Manual {
		if m.Source == "src/app/tokens.css" || m.Source == "src/app/theme.ts" {
			t.Errorf("stubbed file still has a manual item: %+v", m)
		}
	}

	res, err := Apply(rep, ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"src/app/tokens.css", "src/app/theme.ts", "src/app/hero.png"} {
		if _, err := os.Stat(filepath.Join(web, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s was not written after the tree moved: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(web, "src", "app", "page.tsx")); !os.IsNotExist(err) {
		t.Errorf("the route tree should have moved aside, stat err = %v", err)
	}
	if !containsString(res.Written, "src/app/tokens.css") {
		t.Errorf("stub not recorded as written: %v", res.Written)
	}

	// The sibling stylesheet builds through Krate's CSS pipeline with the
	// tokens inlined from the moved file.
	sheet, err := os.ReadFile(filepath.Join(ws, "packages", "static-site", "src", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	built := css.InlineImportsWith(string(sheet), filepath.Join(ws, "packages", "static-site", "src"), css.ImportOptions{Root: ws})
	if !strings.Contains(built, "--brand: #123456") || strings.Contains(built, "@import") {
		t.Errorf("tokens not inlined through the stub:\n%s", built)
	}

	if err := Rollback(res.BackupDir); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]string{
		"src/app/tokens.css": ":root { --brand: #123456; }\n",
		"src/app/theme.ts":   "export const extra = 1;\nexport default { brand: 'blue' };\n",
		"src/app/hero.png":   "PNG",
	} {
		got, err := os.ReadFile(filepath.Join(web, filepath.FromSlash(rel)))
		if err != nil || string(got) != want {
			t.Errorf("after rollback %s = %q, %v", rel, got, err)
		}
	}
	_ = filepath.WalkDir(filepath.Join(web, "src", "migrated"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			t.Errorf("rollback left %s", p)
		}
		return nil
	})
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
