package hostts

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func nativeCfg(rt string, islands map[string]string, scriptc bool) func(*config.Config) {
	return func(c *config.Config) {
		c.Native = &config.NativeConfig{
			Host:         config.NativeHost{Runtime: rt, Islands: islands},
			Experimental: config.NativeExperimental{Scriptc: scriptc},
		}
	}
}

const imgPkgFiles = `{"name":"fakeimg","version":"1.0.0","main":"index.js"}`

// islandProject has a plain host module, one that needs the fakeimg package
// (which reads files with node:fs) and a page that uses both.
func islandProject() map[string]string {
	return map[string]string{
		"node_modules/fakeimg/package.json": imgPkgFiles,
		"node_modules/fakeimg/index.js":     `const fs = require("fs"); module.exports.size = (p) => fs.statSync(p).size;`,
		"src/lib/plain.host.ts":             `export function hello(n: string) { return "hi " + n; }`,
		"src/lib/img.host.ts": `import { size } from "fakeimg";
export function imageSize(p: string): number { return size(p); }
`,
		"src/native/gpu.host.ts": `export function gpu() { return "gpu"; }`,
		"src/pages/index.tsx": `import { hello } from "../lib/plain.host";
import { imageSize } from "../lib/img.host";
import { gpu } from "../native/gpu.host";
export default function P() { return <p onClick={() => { hello("x"); imageSize("a"); gpu(); }}>x</p>; }
`,
	}
}

func groupOf(t *testing.T, res *Result, rt string) Group {
	t.Helper()
	for _, g := range res.Groups {
		if g.Runtime == rt {
			return g
		}
	}
	t.Fatalf("no %s group in %+v", rt, res.Groups)
	return Group{}
}

func idsOf(g Group) string {
	var out []string
	for _, r := range g.Registered {
		out = append(out, r.ID)
	}
	return strings.Join(out, ",")
}

func TestQuickJSNodeBuiltinIsAnError(t *testing.T) {
	_, res, ds := buildProject(t, map[string]string{
		"src/lib/files.host.ts": `import { readFileSync } from "node:fs";
export function read(p: string) { return readFileSync(p, "utf8"); }
`,
		"src/pages/index.tsx": `import { read } from "../lib/files.host";
export default function P() { return <p onClick={() => read("a")}>x</p>; }
`,
	}, nil)
	if res.HostBundle != "" {
		t.Error("a bundle was written despite the error")
	}
	errs := errorsOf(ds)
	if len(errs) != 1 || errs[0].Code != diag.CodeHostNodeBuiltin {
		t.Fatalf("diagnostics %+v", ds)
	}
	d := errs[0]
	if d.File != "src/lib/files.host.ts" || d.Line != 1 || d.Col != 30 {
		t.Errorf("position %s:%d:%d", d.File, d.Line, d.Col)
	}
	if !strings.Contains(d.Hint, `"src/lib/files.host.ts": "node"`) || !strings.Contains(d.Hint, "native.host.runtime") {
		t.Errorf("hint %q", d.Hint)
	}
}

func TestQuickJSPackageUsingBuiltin(t *testing.T) {
	files := islandProject()
	_, _, ds := buildProject(t, files, nil)
	var got *diag.Diagnostic
	for i, d := range ds {
		if d.Code == diag.CodeHostNodeBuiltin {
			got = &ds[i]
		}
	}
	if got == nil {
		t.Fatalf("no KR7230 in %+v", ds)
	}
	if !strings.Contains(got.Message, "fakeimg") || !strings.Contains(got.Hint, `{ "fakeimg": "node" }`) {
		t.Errorf("diagnostic %+v", got)
	}
}

func TestIslandPackageToNode(t *testing.T) {
	_, res, ds := buildProject(t, islandProject(), nativeCfg("quickjs", map[string]string{"fakeimg": "node"}, false))
	mustNoErrors(t, ds)
	qjs, node := groupOf(t, res, "quickjs"), groupOf(t, res, "node")
	if got := idsOf(qjs); got != "src/lib/plain.host.hello,src/native/gpu.host.gpu" {
		t.Errorf("quickjs ids %s", got)
	}
	if got := idsOf(node); got != "src/lib/img.host.imageSize" {
		t.Errorf("node ids %s", got)
	}
	if got := strings.Join(node.Funcs, ","); got != "src/lib/img.host.imageSize" {
		t.Errorf("node funcs %s", got)
	}
	if code := read(t, qjs.Bundle); strings.Contains(code, "fakeimg") || strings.Contains(code, "statSync") {
		t.Errorf("the island's code leaked into the QuickJS bundle")
	}
	nodeCode := read(t, node.Bundle)
	if !strings.Contains(nodeCode, "statSync") || strings.Contains(nodeCode, `"hi "`) {
		t.Errorf("node bundle content wrong")
	}
	if res.HostBundle != qjs.Bundle {
		t.Errorf("HostBundle %s", res.HostBundle)
	}
	for _, r := range res.Registered {
		want := ""
		if strings.Contains(r.ID, "imageSize") {
			want = "node"
		}
		if r.Runtime != want {
			t.Errorf("%s runtime %q, want %q", r.ID, r.Runtime, want)
		}
	}
}

func TestIslandPathGlobAndRuntimeKinds(t *testing.T) {
	_, res, ds := buildProject(t, islandProject(), nativeCfg("quickjs", map[string]string{
		"fakeimg":        "node",
		"src/native/**":  "bun",
		"src/lib/*.nope": "deno",
	}, false))
	errs := errorsOf(ds)
	if len(errs) != 0 {
		t.Fatalf("errors %+v", errs)
	}
	if got := idsOf(groupOf(t, res, "bun")); got != "src/native/gpu.host.gpu" {
		t.Errorf("bun ids %s", got)
	}
	var unused []string
	for _, d := range ds {
		if d.Code == diag.CodeHostIslandUnused {
			if !d.IsWarning() {
				t.Errorf("KR7232 must be a warning")
			}
			unused = append(unused, d.Message)
		}
	}
	if len(unused) != 1 || !strings.Contains(unused[0], "src/lib/*.nope") {
		t.Errorf("unused entries %v", unused)
	}
}

func TestIslandDefaultNodeWithQuickJSIsland(t *testing.T) {
	_, res, ds := buildProject(t, islandProject(), nativeCfg("node", map[string]string{"src/lib/plain.host.ts": "quickjs"}, false))
	mustNoErrors(t, ds)
	if got := idsOf(groupOf(t, res, "quickjs")); got != "src/lib/plain.host.hello" {
		t.Errorf("quickjs ids %s", got)
	}
	if got := idsOf(groupOf(t, res, "node")); got != "src/lib/img.host.imageSize,src/native/gpu.host.gpu" {
		t.Errorf("node ids %s", got)
	}
}

func TestIslandConflict(t *testing.T) {
	files := islandProject()
	files["src/lib/both.host.ts"] = `import { size } from "fakeimg";
import { gpu } from "../native/gpu.host";
export function both(p: string) { return size(p) + gpu().length; }
`
	files["src/pages/index.tsx"] = `import { both } from "../lib/both.host";
export default function P() { return <p onClick={() => both("a")}>x</p>; }
`
	_, _, ds := buildProject(t, files, nativeCfg("quickjs", map[string]string{"fakeimg": "node", "src/native/**": "bun"}, false))
	var found bool
	for _, d := range ds {
		if d.Code == diag.CodeHostIslandConflict {
			found = true
			if d.File != "src/lib/both.host.ts" || !strings.Contains(d.Message, "node") || !strings.Contains(d.Message, "bun") {
				t.Errorf("diagnostic %+v", d)
			}
		}
	}
	if !found {
		t.Fatalf("no KR7231 in %+v", ds)
	}
}

func TestScriptcDefaultWritesBothVariants(t *testing.T) {
	_, res, ds := buildProject(t, map[string]string{
		"src/lib/plain.host.ts": `export function hello(n: string) { return "hi " + n; }`,
		"src/pages/index.tsx": `import { hello } from "../lib/plain.host";
export default function P() { return <p onClick={() => hello("x")}>x</p>; }
`,
	}, nativeCfg("scriptc", nil, true))
	mustNoErrors(t, ds)
	q, sc := groupOf(t, res, "quickjs"), groupOf(t, res, "scriptc")
	if idsOf(q) != idsOf(sc) || idsOf(q) == "" {
		t.Errorf("ids %q vs %q", idsOf(q), idsOf(sc))
	}
	if !strings.HasSuffix(strings.ReplaceAll(sc.Bundle, "\\", "/"), "host/scriptc/host.mjs") {
		t.Errorf("scriptc bundle %s", sc.Bundle)
	}
	if res.Registered[0].Runtime != "" {
		t.Errorf("a scriptc project runs on QuickJS in development, runtime %q", res.Registered[0].Runtime)
	}
}

func TestIslandKeyParsing(t *testing.T) {
	for k, pkg := range map[string]bool{
		"sharp": true, "@scope/name": true, "src/lib/x.ts": false, "src/native": false,
		"./vendor": false, "src/**": false, "lodash/fp": false, "a*": false,
	} {
		if isPackageKey(k) != pkg {
			t.Errorf("isPackageKey(%q) = %v", k, !pkg)
		}
	}
	re := globRegexp("src/native/**")
	for p, want := range map[string]bool{"src/native/a.ts": true, "src/native/x/y.ts": true, "src/nativex/a.ts": false} {
		if re.MatchString(p) != want {
			t.Errorf("src/native/** vs %s = %v", p, !want)
		}
	}
	re = globRegexp("src/lib/*.host.ts")
	if !re.MatchString("src/lib/a.host.ts") || re.MatchString("src/lib/sub/a.host.ts") {
		t.Error("single star must stay in one segment")
	}
	re = globRegexp("src/lib")
	if !re.MatchString("src/lib/a.ts") || !re.MatchString("src/lib") || re.MatchString("src/library/a.ts") {
		t.Error("a plain path matches the directory tree")
	}
	if nodeModulesPackage("node_modules/.pnpm/a@1/node_modules/@s/p/lib/x.js") != "@s/p" || nodeModulesPackage("src/a.ts") != "" {
		t.Error("nodeModulesPackage")
	}
}
