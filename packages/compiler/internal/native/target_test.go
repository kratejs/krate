package native

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func targetDiags(t *testing.T, files map[string]string, mut func(c *config.Config)) []diag.Diagnostic {
	t.Helper()
	root := project(t, files)
	cfg := config.Default()
	if mut != nil {
		mut(cfg)
	}
	cfg.Resolve(root)
	ds, err := CheckTarget(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func hasCode(ds []diag.Diagnostic, code diag.Code, substr string) bool {
	for _, d := range ds {
		if d.Code == code && strings.Contains(d.File+" "+d.Message, substr) {
			return d.Hint != ""
		}
	}
	return false
}

const page = "export default function P() { return <div/>; }\n"

func TestTargetCleanProject(t *testing.T) {
	ds := targetDiags(t, map[string]string{
		"src/pages/index.tsx": page,
		"src/host/db.host.ts": dbBody,
		"src/pages/a.tsx":     "export const dynamic = \"auto\";\nexport const revalidate = false;\n" + page,
	}, nil)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %v", ds)
	}
}

func TestTargetServerRenderingConfig(t *testing.T) {
	ds := targetDiags(t, nil, func(c *config.Config) { c.SSR.Streaming = true; c.PPR = true })
	if !hasCode(ds, diag.CodeNativeServerRendering, "ssr.streaming") || !hasCode(ds, diag.CodeNativeServerRendering, "ppr") {
		t.Errorf("diagnostics: %v", ds)
	}
}

func TestTargetServerRenderingPageExports(t *testing.T) {
	cases := map[string]string{
		"dynamic":        "export const dynamic = \"force-dynamic\";\n",
		"revalidate":     "export const revalidate = 60;\n",
		"revalidate 0":   "export const revalidate = 0;\n",
		"config ssr":     "export const config = { ssr: true };\n",
		"config isr":     "export const config = { isr: true, revalidate: 30 };\n",
		"config stream":  "export const config = { streaming: true };\n",
		"config reval":   "export const config = { revalidate: 30 };\n",
		"revalidate var": "export const revalidate = SOME;\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			ds := targetDiags(t, map[string]string{"src/pages/index.tsx": src + page}, nil)
			if !hasCode(ds, diag.CodeNativeServerRendering, "index.tsx") {
				t.Errorf("diagnostics: %v", ds)
			}
		})
	}
	// Outside the pages dir the exports mean nothing.
	ds := targetDiags(t, map[string]string{"src/lib.ts": "export const dynamic = \"force-dynamic\";\n"}, nil)
	if len(ds) != 0 {
		t.Errorf("diagnostics: %v", ds)
	}
}

func TestTargetAPIRoutes(t *testing.T) {
	ds := targetDiags(t, map[string]string{
		"src/api/users.ts":  "export function GET() {}\n",
		"src/api/health.go": "package api\n\nfunc GET() {}\n",
		"src/api/_util.ts":  "export const x = 1;\n",
	}, nil)
	if !hasCode(ds, diag.CodeNativeAPIRoute, "src/api/users.ts") || !hasCode(ds, diag.CodeNativeAPIRoute, "src/api/health.go") {
		t.Errorf("diagnostics: %v", ds)
	}
	if hasCode(ds, diag.CodeNativeAPIRoute, "_util") {
		t.Errorf("underscore files are helpers: %v", ds)
	}
}

func TestTargetMiddleware(t *testing.T) {
	for _, f := range []string{"middleware.ts", "src/middleware.ts"} {
		ds := targetDiags(t, map[string]string{f: "export function middleware() {}\n"}, nil)
		if !hasCode(ds, diag.CodeNativeMiddleware, f) {
			t.Errorf("%s: diagnostics: %v", f, ds)
		}
	}
}

func TestTargetRuntimeComponents(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		mut   func(c *config.Config)
		file  string
	}{
		"file name":     {map[string]string{"src/Stats.runtime.tsx": page}, nil, "Stats.runtime.tsx"},
		"directive":     {map[string]string{"src/Stats.tsx": "// @runtime\n" + page}, nil, "Stats.tsx"},
		"decorator":     {map[string]string{"src/Stats.tsx": "@runtime\nexport default function Stats() { return <div/>; }\n"}, nil, "Stats.tsx"},
		"body string":   {map[string]string{"src/Stats.tsx": "export function Stats() { \"@runtime\"; return <div/>; }\nexport function Other() { return <p/>; }\n"}, nil, "Stats.tsx"},
		"async":         {map[string]string{"src/Stats.tsx": "export default async function Stats() { return <div/>; }\n"}, nil, "Stats.tsx"},
		"config list":   {map[string]string{"src/Stats.tsx": page}, func(c *config.Config) { c.RuntimeComponents = []string{"Stats"} }, "Stats.tsx"},
		"config dirs":   {map[string]string{"src/rt/Stats.tsx": page}, func(c *config.Config) { c.RuntimeDirs = []string{"src/rt"} }, "Stats.tsx"},
		"config only":   {nil, func(c *config.Config) { c.RuntimeComponents = []string{"X"} }, "krate.config"},
		"config dirs 2": {nil, func(c *config.Config) { c.RuntimeDirs = []string{"x"} }, "krate.config"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ds := targetDiags(t, tc.files, tc.mut)
			if !hasCode(ds, diag.CodeNativeRuntimeComponent, tc.file) {
				t.Errorf("diagnostics: %v", ds)
			}
		})
	}
	// An async non-component helper is fine.
	ds := targetDiags(t, map[string]string{"src/lib.ts": "export async function load() { return 1; }\n"}, nil)
	if len(ds) != 0 {
		t.Errorf("diagnostics: %v", ds)
	}
}

func TestTargetServerActions(t *testing.T) {
	ds := targetDiags(t, map[string]string{
		"src/actions.ts": "\"use server\";\nexport async function save() {}\n",
		"src/Form.tsx":   "export function Form() { async function go() { \"use server\"; } return <form/>; }\n",
	}, nil)
	if !hasCode(ds, diag.CodeNativeServerAction, "actions.ts") || !hasCode(ds, diag.CodeNativeServerAction, "Form.tsx") {
		t.Errorf("diagnostics: %v", ds)
	}
}

func webDiags(t *testing.T, files map[string]string, mut func(c *config.Config)) []diag.Diagnostic {
	t.Helper()
	root := project(t, files)
	cfg := config.Default()
	if mut != nil {
		mut(cfg)
	}
	cfg.Resolve(root)
	ds, err := CheckWebBuild(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func TestWebBuildRejectsHostMarkers(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		mut   func(c *config.Config)
		want  string
	}{
		"decorator":     {map[string]string{"src/a.tsx": "@host\nexport async function a() {}\n" + page}, nil, "a.tsx"},
		"body string":   {map[string]string{"src/a.tsx": "export async function a() { \"@host\"; }\n" + page}, nil, "a.tsx"},
		"directive":     {map[string]string{"src/a.ts": "// @host\n" + dbBody}, nil, "a.ts"},
		"file name":     {map[string]string{"src/a.host.ts": dbBody}, nil, "a.host.ts"},
		"hostFunctions": {nil, func(c *config.Config) { c.HostFunctions = []string{"a"} }, "hostFunctions"},
		"hostDirs":      {nil, func(c *config.Config) { c.HostDirs = []string{"src/host"} }, "hostDirs"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ds := webDiags(t, tc.files, tc.mut)
			if !hasCode(ds, diag.CodeHostInWebBuild, tc.want) {
				t.Errorf("diagnostics: %v", ds)
			}
			for _, d := range ds {
				if !strings.Contains(d.Hint, "only runs in a native build") {
					t.Errorf("hint = %q", d.Hint)
				}
			}
		})
	}
	if ds := webDiags(t, map[string]string{"src/a.tsx": page, "src/b.go": "package b\n"}, nil); len(ds) != 0 {
		t.Errorf("clean web project: %v", ds)
	}
}
