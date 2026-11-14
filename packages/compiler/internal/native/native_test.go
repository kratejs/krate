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

const dbBody = "export async function addTask(title: string) { return title; }\nexport function listTasks() { return []; }\n"

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
