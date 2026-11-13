package native

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// project writes files (path -> content) into a temp dir and returns the root.
func project(t *testing.T, files map[string]string) string {
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
	return root
}

func analyze(t *testing.T, files map[string]string, mut func(c *config.Config)) *Placement {
	t.Helper()
	root := project(t, files)
	cfg := config.Default()
	if mut != nil {
		mut(cfg)
	}
	cfg.Resolve(root)
	p, err := Analyze(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func funcNames(m *Module) string {
	var n []string
	for _, f := range m.Funcs {
		n = append(n, f.Name)
	}
	return strings.Join(n, ",")
}

func wantDiag(t *testing.T, p *Placement, code diag.Code, substr string) {
	t.Helper()
	for _, d := range p.Diagnostics {
		if d.Code == code && strings.Contains(d.Message+" "+d.Hint, substr) {
			if d.Hint == "" {
				t.Errorf("diagnostic has no hint: %v", d)
			}
			return
		}
	}
	t.Fatalf("no %s diagnostic containing %q in %v", code, substr, p.Diagnostics)
}

func noDiags(t *testing.T, p *Placement) {
	t.Helper()
	if len(p.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", p.Diagnostics)
	}
}

const dbBody = "export async function addTask(title: string) { return title; }\nexport function listTasks() { return []; }\n"

func TestSourceDecorator(t *testing.T) {
	p := analyze(t, map[string]string{
		"src/pages/index.tsx": "@host\nexport async function addTask(title: string) {\n  return title;\n}\n\nexport default function Page() { return <div/>; }\n",
	}, nil)
	noDiags(t, p)
	m := p.Module("src/pages/index.tsx")
	if m == nil || m.Whole || funcNames(m) != "addTask" {
		t.Fatalf("module = %+v", m)
	}
	f := m.Funcs[0]
	if f.Source != SourceDecorator || !f.Inline || !f.Exported {
		t.Errorf("func = %+v", f)
	}
}

func TestSourceBodyStringTopLevelAndArrow(t *testing.T) {
	p := analyze(t, map[string]string{
		"src/pages/index.tsx": "export async function a() { \"@host\"; return 1; }\nexport const b = async () => { \"@host\"; return 2; };\nexport default function Page() { return <div/>; }\n",
	}, nil)
	noDiags(t, p)
	m := p.Module("src/pages/index.tsx")
	if m == nil || funcNames(m) != "a,b" {
		t.Fatalf("module = %+v", m)
	}
	if m.Funcs[0].Source != SourceBodyString {
		t.Errorf("source = %v", m.Funcs[0].Source)
	}
}

func TestInlineRange(t *testing.T) {
	src := "import x from \"y\";\n@host\nexport async function addTask(t: string) {\n  const s = \"}\";\n  return `${t}}`;\n}\nexport const z = 1;\n"
	p := analyze(t, map[string]string{"src/a.tsx": src}, nil)
	noDiags(t, p)
	f := p.Module("src/a.tsx").Funcs[0]
	got := src[f.Start:f.End]
	if !strings.HasPrefix(got, "@host\nexport async function addTask") || !strings.HasSuffix(got, "}") || strings.Contains(got, "export const z") {
		t.Errorf("range = %q", got)
	}
	if f.Line != 3 && f.Line != 2 {
		t.Errorf("line = %d", f.Line)
	}
}

func TestSourceDirective(t *testing.T) {
	p := analyze(t, map[string]string{"src/db.ts": "// @host\n" + dbBody}, nil)
	noDiags(t, p)
	m := p.Module("src/db.ts")
	if m == nil || !m.Whole || m.Source != SourceDirective || funcNames(m) != "addTask,listTasks" {
		t.Fatalf("module = %+v", m)
	}
	if m.Funcs[0].Inline {
		t.Error("whole-module functions are not inline")
	}
}

func TestSourceFileName(t *testing.T) {
	for _, name := range []string{"src/db.host.ts", "src/db.host.tsx"} {
		p := analyze(t, map[string]string{name: dbBody}, nil)
		noDiags(t, p)
		if m := p.Module(name); m == nil || !m.Whole || m.Source != SourceFileName {
			t.Fatalf("%s: module = %+v", name, m)
		}
	}
}

func TestSourceConfigFunctionName(t *testing.T) {
	p := analyze(t, map[string]string{"src/lib/tasks.ts": dbBody + "export function other() {}\n"}, func(c *config.Config) {
		c.HostFunctions = []string{"addTask", "missing"}
	})
	noDiags(t, p)
	m := p.Module("src/lib/tasks.ts")
	if m == nil || m.Whole || funcNames(m) != "addTask" || m.Funcs[0].Source != SourceConfigFunc {
		t.Fatalf("module = %+v", m)
	}
	if len(p.Warnings) != 1 || !strings.Contains(p.Warnings[0], `"missing"`) {
		t.Errorf("warnings = %v", p.Warnings)
	}
}

func TestSourceConfigFilePath(t *testing.T) {
	for _, entry := range []string{"src/lib/tasks.ts", "./src/lib/tasks.ts", "src/lib/tasks"} {
		p := analyze(t, map[string]string{"src/lib/tasks.ts": dbBody}, func(c *config.Config) { c.HostFunctions = []string{entry} })
		noDiags(t, p)
		if m := p.Module("src/lib/tasks.ts"); m == nil || !m.Whole || m.Source != SourceConfigFunc {
			t.Fatalf("%q: module = %+v", entry, m)
		}
	}
}

func TestSourceHostDirs(t *testing.T) {
	p := analyze(t, map[string]string{"src/host/db.ts": dbBody, "src/ui/a.ts": dbBody}, func(c *config.Config) {
		c.HostDirs = []string{"src/host"}
	})
	noDiags(t, p)
	if m := p.Module("src/host/db.ts"); m == nil || !m.Whole || m.Source != SourceConfigDir {
		t.Fatalf("module = %+v", m)
	}
	if p.Module("src/ui/a.ts") != nil {
		t.Error("src/ui/a.ts is not host code")
	}
}

func TestGoFilesAlwaysHost(t *testing.T) {
	p := analyze(t, map[string]string{
		"src/native/store.go":  "package native\n\nfunc ListTasks() []string { return nil }\n\nfunc helper() {}\n\nfunc (s *S) Method() {}\n",
		"src/native/x_test.go": "package native\n\nfunc TestX() {}\n",
	}, nil)
	noDiags(t, p)
	m := p.Module("src/native/store.go")
	if m == nil || !m.Whole || m.Source != SourceGo || funcNames(m) != "ListTasks" {
		t.Fatalf("module = %+v", m)
	}
	if p.Module("src/native/x_test.go") != nil {
		t.Error("test files are not host code")
	}
}

func TestPrecedenceReportsHighestSource(t *testing.T) {
	p := analyze(t, map[string]string{"src/host/db.host.ts": "// @host\n" + dbBody}, func(c *config.Config) {
		c.HostDirs = []string{"src/host"}
		c.HostFunctions = []string{"src/host/db.host.ts"}
	})
	noDiags(t, p)
	if m := p.Module("src/host/db.host.ts"); m.Source != SourceDirective {
		t.Errorf("source = %v, want directive", m.Source)
	}
	p = analyze(t, map[string]string{"src/host/db.host.ts": dbBody}, func(c *config.Config) {
		c.HostDirs = []string{"src/host"}
		c.HostFunctions = []string{"src/host/db.host.ts"}
	})
	if m := p.Module("src/host/db.host.ts"); m.Source != SourceFileName {
		t.Errorf("source = %v, want file name", m.Source)
	}
	p = analyze(t, map[string]string{"src/host/db.ts": "@host\nexport function a() {}\n\nexport function b() {}\n"}, func(c *config.Config) {
		c.HostDirs = []string{"src/host"}
	})
	m := p.Module("src/host/db.ts")
	if m.Funcs[0].Source != SourceDecorator || m.Funcs[1].Source != SourceConfigDir {
		t.Errorf("funcs = %+v", m.Funcs)
	}
}

func TestFunctionMarkerOverridesFile(t *testing.T) {
	p := analyze(t, map[string]string{
		"src/db.ts": "// @host\nexport function a() {}\n@client\nexport function b() {}\nexport function c() { \"@client\"; }\n",
	}, nil)
	noDiags(t, p)
	if m := p.Module("src/db.ts"); funcNames(m) != "a" {
		t.Errorf("funcs = %s", funcNames(m))
	}
	// And a host function in a server file.
	p = analyze(t, map[string]string{
		"src/page.server.tsx": "@host\nexport function a() {}\nexport default function Page() { return <div/>; }\n",
	}, nil)
	noDiags(t, p)
	if m := p.Module("src/page.server.tsx"); m == nil || m.Whole || funcNames(m) != "a" {
		t.Errorf("module = %+v", m)
	}
}

func TestConflictFilePairs(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		mut   func(c *config.Config)
		want  string
	}{
		{"directive vs server comment", map[string]string{"src/a.ts": "// @host\n// @server\n" + dbBody}, nil, "@server"},
		{"directive vs runtime comment", map[string]string{"src/a.ts": "// @host\n// @runtime\n" + dbBody}, nil, "@runtime"},
		{"file name vs server comment", map[string]string{"src/a.host.ts": "// @server\n" + dbBody}, nil, "@server"},
		{"file name vs serverComponents", map[string]string{"src/a.host.ts": dbBody}, func(c *config.Config) { c.ServerComponents = []string{"a.host"} }, "serverComponents"},
		{"hostDirs vs serverDirs", map[string]string{"src/host/a.ts": dbBody}, func(c *config.Config) {
			c.HostDirs = []string{"src/host"}
			c.ServerDirs = []string{"src/host"}
		}, "serverDirs"},
		{"hostDirs vs runtimeDirs", map[string]string{"src/host/a.ts": dbBody}, func(c *config.Config) {
			c.HostDirs = []string{"src/host"}
			c.RuntimeDirs = []string{"src/host"}
		}, "runtimeDirs"},
		{"hostFunctions path vs runtimeComponents", map[string]string{"src/a.ts": dbBody}, func(c *config.Config) {
			c.HostFunctions = []string{"src/a.ts"}
			c.RuntimeComponents = []string{"a"}
		}, "runtimeComponents"},
		{"directive vs default-export decorator", map[string]string{"src/a.ts": "// @host\n@server\nexport default function A() { return 1; }\n"}, nil, "default export"},
		{"go file vs serverDirs", map[string]string{"src/g/a.go": "package g\n\nfunc F() {}\n"}, func(c *config.Config) { c.ServerDirs = []string{"src/g"} }, "serverDirs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantDiag(t, analyze(t, tc.files, tc.mut), diag.CodeHostConflict, tc.want)
		})
	}
}

func TestConflictFunctionLevel(t *testing.T) {
	p := analyze(t, map[string]string{"src/a.tsx": "@host\nexport function a() { \"@server\"; }\n"}, nil)
	wantDiag(t, p, diag.CodeHostConflict, "decorator and the \"@server\"")
	p = analyze(t, map[string]string{"src/a.tsx": "@server\nexport function a() { \"@host\"; }\n"}, nil)
	wantDiag(t, p, diag.CodeHostConflict, "decorator and the \"@host\"")
	p = analyze(t, map[string]string{"src/a.ts": "@client\nexport function addTask() {}\n"}, func(c *config.Config) { c.HostFunctions = []string{"addTask"} })
	wantDiag(t, p, diag.CodeHostConflict, "hostFunctions")
}

func TestComponentRejected(t *testing.T) {
	cases := map[string]string{
		"decorator":  "@host\nexport function Card() { return <div/>; }\n",
		"body":       "export function Card() { \"@host\"; return <div/>; }\n",
		"lowercase":  "@host\nexport function card() { return <div/>; }\n",
		"arrow":      "@host\nexport const Card = () => <div/>;\n",
		"file-level": "// @host\nexport function Card() { return <div/>; }\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			p := analyze(t, map[string]string{"src/a.tsx": src}, nil)
			wantDiag(t, p, diag.CodeHostComponent, "@server")
		})
	}
	p := analyze(t, map[string]string{"src/a.tsx": "// @host\n@client\nexport function Card() { return <div/>; }\n@client\nexport function Other() { return <p/>; }\nexport function f() {}\n"}, nil)
	noDiags(t, p)
}

func TestInlineCapture(t *testing.T) {
	src := `import { createSignal } from "@krate/runtime";
import { save } from "./db";
const LIMIT = 3;
export default function Todo(props) {
  const [count, setCount] = createSignal(0);
  let local = 1;
  async function add(title) {
    "@host";
    return save(title, count());
  }
  const bad2 = async (t) => { "@host"; local = t; };
  return <button onClick={() => add("x")}>{count()}</button>;
}
`
	p := analyze(t, map[string]string{"src/a.tsx": src}, nil)
	wantDiag(t, p, diag.CodeHostCapture, "count")
	wantDiag(t, p, diag.CodeHostCapture, "local")
	for _, d := range p.Diagnostics {
		if d.Code == diag.CodeHostCapture && !strings.Contains(d.Hint, "pass the value as an argument") {
			t.Errorf("hint = %q", d.Hint)
		}
	}
	if len(p.Diagnostics) != 2 {
		t.Errorf("diagnostics = %v", p.Diagnostics)
	}
}

func TestInlineAllowedReferences(t *testing.T) {
	src := `import { save } from "./db";
const LIMIT = 3;
@host
export async function topLevel(x) { return x; }
export default function Todo(props) {
  const [count, setCount] = createSignal(0);
  async function helper(a) { "@host"; return a + 1; }
  async function add(title, n) {
    "@host";
    const rows = [title, n].map((r) => r + 1);
    for (const r of rows) { console.log(r); }
    try { await save(title); } catch (err) { throw err; }
    const { a, b = 2 } = n;
    return [topLevel(a), helper(b), LIMIT, Math.max(1, 2), JSON.stringify(rows)];
  }
  return <button onClick={() => add("x", count())}>{count()}</button>;
}
`
	p := analyze(t, map[string]string{"src/a.tsx": src}, nil)
	noDiags(t, p)
	m := p.Module("src/a.tsx")
	if funcNames(m) != "topLevel,helper,add" {
		t.Errorf("funcs = %s", funcNames(m))
	}
	for _, f := range m.Funcs {
		if !f.Inline || f.End <= f.Start {
			t.Errorf("func %+v", f)
		}
	}
}

func TestNoMarkersNoModule(t *testing.T) {
	p := analyze(t, map[string]string{"src/a.tsx": "export default function A() { return <div/>; }\n"}, nil)
	noDiags(t, p)
	if len(p.Modules) != 0 {
		t.Errorf("modules = %+v", p.Modules)
	}
}

func TestAtHostInStringOrTemplateIsNotAMarker(t *testing.T) {
	src := "export default function A() { return <pre>{`\n@host\n`}</pre>; }\n"
	p := analyze(t, map[string]string{"src/a.tsx": src}, nil)
	noDiags(t, p)
	if len(p.Modules) != 0 {
		t.Errorf("modules = %+v", p.Modules)
	}
}

func TestHostDecoratorDoesNotChangeModuleTier(t *testing.T) {
	// Parse must accept @host on an async function and leave the tier alone.
	p := analyze(t, map[string]string{"src/a.tsx": "@host\nexport async function a() {}\nexport default function P() { return <div/>; }\n"}, nil)
	noDiags(t, p)
}

func TestInlineRangeArrowAndNested(t *testing.T) {
	src := "export const add = async (t: string) => {\n  \"@host\";\n  return t;\n};\nexport default function C() {\n  async function inner(a: number) {\n    \"@host\";\n    return { a };\n  }\n  return <div/>;\n}\n"
	p := analyze(t, map[string]string{"src/a.tsx": src}, nil)
	noDiags(t, p)
	m := p.Module("src/a.tsx")
	if funcNames(m) != "add,inner" {
		t.Fatalf("funcs = %s", funcNames(m))
	}
	if got := src[m.Funcs[0].Start:m.Funcs[0].End]; !strings.HasPrefix(got, "export const add") || !strings.HasSuffix(got, "};") {
		t.Errorf("arrow range = %q", got)
	}
	if got := src[m.Funcs[1].Start:m.Funcs[1].End]; !strings.HasPrefix(got, "async function inner") || !strings.HasSuffix(got, "}") || strings.Contains(got, "<div/>") {
		t.Errorf("nested range = %q", got)
	}
}
