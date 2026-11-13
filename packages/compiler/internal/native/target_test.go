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

func TestTargetServerRenderingConfig(t *testing.T) {
	ds := targetDiags(t, nil, func(c *config.Config) { c.SSR.Streaming = true; c.PPR = true })
	if !hasCode(ds, diag.CodeNativeServerRendering, "ssr.streaming") || !hasCode(ds, diag.CodeNativeServerRendering, "ppr") {
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
