package capability

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/native"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", ".."))
}

// TestIDsJSONMatchesSDK keeps the compiler's copy of the id table in step with
// packages/native/sdk/ids.json.
func TestIDsJSONMatchesSDK(t *testing.T) {
	want, err := os.ReadFile(filepath.Join(repoRoot(t), "packages", "native", "sdk", "ids.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, idsJSON) {
		t.Fatal("ids.json is stale; run: cp packages/native/sdk/ids.json packages/compiler/internal/native/capability/ids.json")
	}
	if len(Rows()) < 70 {
		t.Fatalf("only %d rows", len(Rows()))
	}
}

func project(t *testing.T, files map[string]string) (string, *config.Config, *native.Placement) {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.Resolve(root)
	p, err := native.Analyze(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return root, cfg, p
}

func analyze(t *testing.T, files map[string]string) *Result {
	t.Helper()
	root, cfg, p := project(t, files)
	res, err := Analyze(root, cfg, p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func sitesOf(r *Result, perm string) []Site { return r.Needs[perm] }

func TestUIImports(t *testing.T) {
	res := analyze(t, map[string]string{
		"src/pages/index.tsx": `import { readText } from "@krate/app/fs";
import { writeText } from "@krate/app/clipboard";
import { get } from "@krate/app/kv";
import type { WindowOptions } from "@krate/app/window";
export default function Page() { return <div/>; }
`,
		"src/lib/other.ts": `import * as os from "@krate/app/os";
export const d = () => os.cacheDir();
`,
	})
	want := []string{"clipboard", "fs:read", "kv", "os:info"}
	if !reflect.DeepEqual(res.Permissions, want) {
		t.Fatalf("permissions = %v, want %v", res.Permissions, want)
	}
	s := sitesOf(res, "fs:read")
	if len(s) != 1 || s[0].File != "src/pages/index.tsx" || s[0].Line != 1 || s[0].Kind != "ui" || !strings.HasPrefix(s[0].Symbol, "readText") {
		t.Fatalf("fs:read sites = %+v", s)
	}
	if got := sitesOf(res, "os:info"); len(got) == 0 || got[0].File != "src/lib/other.ts" || !strings.HasPrefix(got[0].Symbol, "os.cacheDir") {
		t.Fatalf("namespace member site = %+v", got)
	}
	// A type-only import reaches nothing; clipboard:read is not implied by write.
	for _, p := range res.Permissions {
		if p == "window" || p == "clipboard:read" {
			t.Fatalf("unexpected %s", p)
		}
	}
	if !reflect.DeepEqual(res.UIAreas, []string{"clipboard", "fs", "kv", "os"}) {
		t.Fatalf("areas = %v", res.UIAreas)
	}
}

func TestNamespaceEscapesCountAsWholeModule(t *testing.T) {
	res := analyze(t, map[string]string{
		"src/pages/index.tsx": `import * as clip from "@krate/app/clipboard";
use(clip);
export default function Page() { return <div/>; }
`,
	})
	if !reflect.DeepEqual(res.Permissions, []string{"clipboard", "clipboard:read"}) {
		t.Fatalf("permissions = %v", res.Permissions)
	}
}

func TestHostModuleImportsAndLiteralIDs(t *testing.T) {
	res := analyze(t, map[string]string{
		"src/pages/index.tsx": `export default function Page() { return <div/>; }`,
		"src/lib/notes.host.ts": `import { secretGet } from "@krate/app/os";
export async function token() { return secretGet("t"); }
`,
		"src/lib/raw.ts": `import { call } from "@krate/app";
export const x = () => call("krate/tray.Set", []);
export const y = (op: string) => call("krate/shortcut." + op, []);
`,
	})
	want := []string{"os:secrets.read", "shortcut", "tray"}
	if !reflect.DeepEqual(res.Permissions, want) {
		t.Fatalf("permissions = %v, want %v", res.Permissions, want)
	}
	if s := sitesOf(res, "os:secrets.read"); len(s) != 1 || s[0].Kind != "host" || s[0].File != "src/lib/notes.host.ts" {
		t.Fatalf("host site = %+v", s)
	}
	if s := sitesOf(res, "tray"); len(s) != 1 || s[0].Line != 2 || s[0].Symbol != "krate/tray.Set" {
		t.Fatalf("literal site = %+v", s)
	}
}

func TestFsImpliesFsRead(t *testing.T) {
	res := analyze(t, map[string]string{
		"src/pages/index.tsx": `import { readText, writeText } from "@krate/app/fs";
export default function Page() { return <div/>; }`,
	})
	if !reflect.DeepEqual(res.Permissions, []string{"fs"}) {
		t.Fatalf("permissions = %v", res.Permissions)
	}
	if !Covers([]string{"fs"}, "fs:read") || Covers([]string{"fs:read"}, "fs") || Covers(nil, "kv") {
		t.Fatal("Covers")
	}
}

func TestLinkedAreasAndTags(t *testing.T) {
	r := &Result{UIAreas: []string{"kv", "os"}, GoAreas: []string{"tray"}}
	linked := strings.Join(r.Linked(), ",")
	if linked != "db,kv,menu,tray" {
		t.Fatalf("linked = %s", linked)
	}
	tags := strings.Join(r.Tags(false), ",")
	for _, w := range []string{"krate_no_fs", "krate_no_shortcut", "krate_no_notify", "krate_no_hostjs", "krate_no_window"} {
		if !strings.Contains(tags, w) {
			t.Errorf("tags %s lack %s", tags, w)
		}
	}
	for _, w := range []string{"krate_no_db", "krate_no_kv", "krate_no_menu", "krate_no_tray"} {
		if strings.Contains(tags, w) {
			t.Errorf("tags %s has %s", tags, w)
		}
	}
	if strings.Contains(strings.Join(r.Tags(true), ","), "hostjs") {
		t.Error("hostjs tag with host code")
	}
	// Shortcut pulls app.
	r = &Result{UIAreas: []string{"shortcut"}}
	if got := strings.Join(r.Linked(), ","); got != "app,shortcut" {
		t.Fatalf("linked = %s", got)
	}
}

func TestGoSDKImports(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go")
	}
	native := filepath.Join(repoRoot(t), "packages", "native")
	mod, _ := os.ReadFile(filepath.Join(native, "go.mod"))
	sum, _ := os.ReadFile(filepath.Join(native, "go.sum"))
	gomod := strings.Replace(string(mod), "module "+NativeModule, "module example.com/goapp\n\nrequire "+NativeModule+" v0.0.0\n\nreplace "+NativeModule+" => "+filepath.ToSlash(native), 1)
	root, cfg, p := project(t, map[string]string{
		"go.mod":        gomod,
		"go.sum":        string(sum),
		"src/lib/a.go":  "package lib\n\nimport (\n\t\"context\"\n\n\t\"example.com/goapp/src/util\"\n)\n\nfunc Hi(ctx context.Context) string { return util.Hi() }\n",
		"src/util/u.go": "package util\n\nimport \"" + NativeModule + "/sdk/tray\"\n\nfunc Hi() string { _ = tray.Remove; return \"hi\" }\n",
		"src/pages/index.tsx": `import { Hi } from "../lib/a.go";
export default function Page() { return <div>{Hi}</div>; }`,
	})
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	res, err := Analyze(root, cfg, p, Options{GoFiles: []string{filepath.Join(root, "src", "lib", "a.go")}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.GoAreas, []string{"tray"}) {
		t.Fatalf("GoAreas = %v (sites %+v)", res.GoAreas, res.GoSites)
	}
	if len(res.GoSites) != 1 || res.GoSites[0].File != "src/util/u.go" || res.GoSites[0].Line != 3 || res.GoSites[0].Kind != "go" {
		t.Fatalf("sites = %+v", res.GoSites)
	}
	// Go imports are not permissions: Go host code is trusted.
	if len(res.Permissions) != 0 {
		t.Fatalf("permissions = %v", res.Permissions)
	}
}
