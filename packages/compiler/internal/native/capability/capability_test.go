package capability

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
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

// TestIDsMatchTypeScriptWrappers checks every id row against the @krate/app
// module that wraps it: the module file calls the id (lowerCamel, as the SDK
// registers it) and exports every name the row lists, so a UI import maps to
// the permission of the ids behind it.
func TestIDsMatchTypeScriptWrappers(t *testing.T) {
	src := map[string]string{}
	for _, r := range Rows() {
		if r.Module == "" {
			continue
		}
		text, ok := src[r.Module]
		if !ok {
			file := r.Module
			if file == "app" {
				file = "lifecycle" // @krate/app/app is src/lifecycle.ts
			}
			b, err := os.ReadFile(filepath.Join(repoRoot(t), "packages", "app", "src", file+".ts"))
			if err != nil {
				t.Errorf("%s: module %q has no packages/app/src/%s.ts: %v", r.ID, r.Module, r.Module, err)
				src[r.Module] = ""
				continue
			}
			text = string(b)
			src[r.Module] = text
		}
		if text == "" {
			continue
		}
		dyn := r.ID[:strings.Index(r.ID, ".")+1] + "${" // `krate/window.${name}`
		if !strings.Contains(text, `"`+r.ID+`"`) && !strings.Contains(text, "`"+dyn) {
			t.Errorf("%s is not called by packages/app/src/%s.ts", r.ID, r.Module)
		}
		for _, name := range r.Exports {
			re := regexp.MustCompile(`(?m)^export\s+(?:async\s+)?(?:function\*?|const|class)\s+` + regexp.QuoteMeta(name) + `\b|^export\s*\{[^}]*\b` + regexp.QuoteMeta(name) + `\b`)
			if !re.MatchString(text) {
				t.Errorf("%s: packages/app/src/%s.ts does not export %q", r.ID, r.Module, name)
			}
		}
	}
}

var testPlugins = []PluginInfo{
	{Name: "sys", Ops: []PluginOp{
		{Name: "info", Permission: "plugin:sys.read"},
		{Name: "memory", Permission: "plugin:sys.read"},
		{Name: "watch", Permission: "plugin:sys.monitor"},
		{Name: "free"},
	}},
	{Name: "other", Ops: []PluginOp{{Name: "run", Permission: "plugin:other.run"}}},
}

func analyzePlugins(t *testing.T, files map[string]string) *Result {
	t.Helper()
	root, cfg, p := project(t, files)
	res, err := Analyze(root, cfg, p, Options{Plugins: testPlugins})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestPluginNamedImports(t *testing.T) {
	res := analyzePlugins(t, map[string]string{"src/pages/index.tsx": `import { info, watch as w } from "krate/plugin/sys";
export default function P() { return <b onClick={() => { info(); w(); }}>x</b>; }
`})
	if got := strings.Join(res.Permissions, ","); got != "plugin:sys.monitor,plugin:sys.read" {
		t.Errorf("permissions = %s", got)
	}
	if got := strings.Join(res.PluginOps["sys"], ","); got != "info,watch" {
		t.Errorf("ops = %v", res.PluginOps)
	}
	if _, ok := res.PluginOps["other"]; ok {
		t.Error("an unused plugin is reported as used")
	}
	site := res.Needs["plugin:sys.read"][0]
	if site.File != "src/pages/index.tsx" || !strings.Contains(site.Symbol, "info from krate/plugin/sys") {
		t.Errorf("site = %+v", site)
	}
	// Plugin ops are not SDK areas.
	for _, a := range res.UIAreas {
		if strings.HasPrefix(a, "plugin:") {
			t.Errorf("area %q", a)
		}
	}
}

func TestPluginOpWithoutPermissionStillCounts(t *testing.T) {
	res := analyzePlugins(t, map[string]string{"src/pages/index.tsx": `import { free } from "krate/plugin/sys";
export default function P() { return <b onClick={() => free()}>x</b>; }
`})
	if len(res.Permissions) != 0 {
		t.Errorf("permissions = %v", res.Permissions)
	}
	if got := strings.Join(res.PluginOps["sys"], ","); got != "free" {
		t.Errorf("ops = %v", res.PluginOps)
	}
}

func TestPluginNamespaceAndDynamicUse(t *testing.T) {
	res := analyzePlugins(t, map[string]string{
		"src/pages/index.tsx": `import * as sys from "krate/plugin/sys";
export default function P() { return <b onClick={() => sys.memory()}>x</b>; }
`,
		"src/pages/two.tsx": `export default function P() { return <b onClick={async () => (await import("krate/plugin/other")).run()}>x</b>; }
`,
	})
	if got := strings.Join(res.PluginOps["sys"], ","); got != "memory" {
		t.Errorf("namespace member use: %v", res.PluginOps)
	}
	// A dynamic import cannot be narrowed: every op of the plugin.
	if got := strings.Join(res.PluginOps["other"], ","); got != "run" {
		t.Errorf("dynamic import: %v", res.PluginOps)
	}
	if !strings.Contains(strings.Join(res.Permissions, ","), "plugin:other.run") {
		t.Errorf("permissions = %v", res.Permissions)
	}
}

func TestPluginNamespaceEscapeUsesWholePlugin(t *testing.T) {
	res := analyzePlugins(t, map[string]string{"src/pages/index.tsx": `import * as sys from "krate/plugin/sys";
const api = sys;
export default function P() { return <b>{String(api)}</b>; }
`})
	if got := strings.Join(res.PluginOps["sys"], ","); got != "free,info,memory,watch" {
		t.Errorf("whole plugin: %v", res.PluginOps)
	}
}

func TestPluginIDLiterals(t *testing.T) {
	res := analyzePlugins(t, map[string]string{"src/pages/index.tsx": `const a = "plugin/sys.watch";
const b = "plugin/other.unknown";
export default function P() { return <b>{a}{b}</b>; }
`})
	if got := strings.Join(res.PluginOps["sys"], ","); got != "watch" {
		t.Errorf("literal id: %v", res.PluginOps)
	}
	// An unknown op of a known plugin counts as the whole plugin.
	if got := strings.Join(res.PluginOps["other"], ","); got != "run" {
		t.Errorf("unknown op: %v", res.PluginOps)
	}
}

func TestAddGoSites(t *testing.T) {
	r := &Result{GoAreas: []string{"kv"}}
	r.AddGoSites([]Site{{File: "p/p.go", Symbol: "sdk/tray", Kind: "go"}, {File: "p/q.go", Symbol: "sdk/kv", Kind: "go"}})
	if strings.Join(r.GoAreas, ",") != "kv,tray" || len(r.GoSites) != 2 {
		t.Errorf("GoAreas = %v sites %v", r.GoAreas, r.GoSites)
	}
	// A plugin that imports sdk/tray keeps the tray area, and the menu it needs.
	if got := strings.Join(r.Linked(), ","); !strings.Contains(got, "tray") || !strings.Contains(got, "menu") {
		t.Errorf("Linked = %s", got)
	}
}
