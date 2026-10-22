package check

import (
	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"os"
	"path/filepath"
	"testing"
)

func TestCustomRule(t *testing.T) {
	root := t.TempDir()
	rulePath := filepath.Join(root, "rules", "no-todo.ts")
	if err := os.MkdirAll(filepath.Dir(rulePath), 0755); err != nil {
		t.Fatal(err)
	}
	src := `export default function check(page: any) {
  const out: any[] = [];
  if (page.route === "/todo") {
    out.push({ rule: "custom/no-todo", message: "route /todo is banned", severity: "error", line: 3, col: 1 });
  }
  return out;
}
`
	if err := os.WriteFile(rulePath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.Root = root
	cfg.Custom = []string{rulePath}

	fs, err := Run(cfg, []Page{
		{Route: "/todo", RelSource: "src/pages/todo.tsx", HTML: doc(`<h1>Todo</h1>`, "")},
		{Route: "/ok", RelSource: "src/pages/ok.tsx", HTML: doc(`<h1>Ok</h1>`, "")},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var found *Finding
	for i := range fs {
		if fs[i].Rule == "custom/no-todo" {
			found = &fs[i]
		}
	}
	if found == nil {
		t.Fatalf("expected custom/no-todo finding, got %+v", fs)
	}
	if found.Severity != Error {
		t.Errorf("severity = %v, want error", found.Severity)
	}
	if found.Route != "/todo" {
		t.Errorf("route = %q, want /todo", found.Route)
	}
	if found.Line != 3 {
		t.Errorf("line = %d, want 3", found.Line)
	}
}

func TestCustomRuleFailure(t *testing.T) {
	root := t.TempDir()
	rulePath := filepath.Join(root, "boom.ts")
	if err := os.WriteFile(rulePath, []byte(`export default function check() { return ["not-an-object"]; }`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Root = root
	cfg.Custom = []string{rulePath}
	_, err := Run(cfg, []Page{{Route: "/", HTML: doc("", "")}})
	if err == nil {
		t.Fatal("expected error from malformed custom finding")
	}
}

func TestCustomRuleReadsAST(t *testing.T) {
	root := t.TempDir()
	rulePath := filepath.Join(root, "rules", "ast.ts")
	if err := os.MkdirAll(filepath.Dir(rulePath), 0755); err != nil {
		t.Fatal(err)
	}
	src := `export default function check(page: any) {
  const keys = Object.keys(page).sort().join(",");
  const same = page.ast === page.ast;
  return [{ rule: "custom/ast", message: page.ast.kind + " " + page.ast.body.length + " " + same + " " + keys }];
}
`
	if err := os.WriteFile(rulePath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	prog, errs, _ := astprint.Parse("const a = 1;\nexport default function Page() { return <h1>{a}</h1>; }\n")
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	cfg := DefaultConfig()
	cfg.Root = root
	cfg.Custom = []string{rulePath}
	fs, err := Run(cfg, []Page{{Route: "/", RelSource: "src/pages/index.tsx", HTML: doc(`<h1>1</h1>`, ""), Program: prog}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, f := range fs {
		if f.Rule == "custom/ast" {
			if want := "Program 2 true ast,html,jsBytes,route,source"; f.Message != want {
				t.Fatalf("message = %q, want %q", f.Message, want)
			}
			return
		}
	}
	t.Fatalf("expected custom/ast finding, got %+v", fs)
}
