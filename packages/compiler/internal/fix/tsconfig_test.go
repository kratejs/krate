package fix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// The tsconfig shipped by the create-krate-app template up to 0.4.0.
const tsconfig040 = `{
  "compilerOptions": {
    "target": "ES2022",
    // 0.4 ignored baseUrl for aliases
    "baseUrl": "./src",
    "paths": {
      "@/*": ["./src/*"]
    }
  },
  "include": ["src"]
}
`

func writeProject(t *testing.T, tsconfig string, dirs ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(tsconfig), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestFixTSConfigBaseURLFor040Template(t *testing.T) {
	root := writeProject(t, tsconfig040, "src/components")
	out, changes, ok := fixTSConfig(root)
	if !ok || len(changes) != 1 {
		t.Fatalf("ok=%v changes=%+v", ok, changes)
	}
	want := strings.Replace(tsconfig040, `"baseUrl": "./src"`, `"baseUrl": "."`, 1)
	if out != want {
		t.Fatalf("output differs (comments and layout must survive):\n%s", out)
	}
	c := changes[0]
	if c.Code != diag.CodeTSConfigBaseURL || c.File != "tsconfig.json" || c.Line != 5 || c.From != `"./src"` || c.To != `"."` {
		t.Fatalf("change = %+v", c)
	}
}

// The rewritten file must resolve the alias the way the compiler does.
func TestFixTSConfigBaseURLResolvesAliasAfterwards(t *testing.T) {
	root := writeProject(t, tsconfig040, "src/components")
	cfg := &config.Config{}
	cfg.LoadTSConfigPaths(root)
	// Before the fix the compiler reads the target from the project root.
	if got := cfg.PathAliases[0].Targets[0]; got != filepath.ToSlash(filepath.Join(root, "src"))+"/*" || filepath.ToSlash(cfg.TSBaseDir) != filepath.ToSlash(filepath.Join(root, "src")) {
		t.Fatalf("precondition: baseDir=%s target=%s", cfg.TSBaseDir, got)
	}
	if _, err := Run(root, true); err != nil {
		t.Fatal(err)
	}
	cfg = &config.Config{}
	cfg.LoadTSConfigPaths(root)
	if filepath.Clean(cfg.TSBaseDir) != filepath.Clean(root) {
		t.Fatalf("TSBaseDir = %s, want %s", cfg.TSBaseDir, root)
	}
	if got := cfg.PathAliases[0].Targets[0]; got != "./src/*" {
		t.Fatalf("target after the fix = %s, want it untouched", got)
	}
}

func TestFixTSConfigBaseURLVariants(t *testing.T) {
	cases := []struct {
		name, src string
		dirs      []string
		fixed     bool
	}{
		{"no dot slash", `{"compilerOptions":{"baseUrl":"src","paths":{"@/*":["src/*"]}}}`, []string{"src"}, true},
		{"trailing slash", `{"compilerOptions":{"baseUrl":"./src/","paths":{"@/*":["./src/*"]}}}`, []string{"src"}, true},
		{"backslashes", `{"compilerOptions":{"baseUrl":".\\src","paths":{"@/*":[".\\src\\*"]}}}`, []string{"src"}, true},
		{"several aliases", `{"compilerOptions":{"baseUrl":"./src","paths":{"@/*":["./src/*"],"~lib/*":["./src/lib/*"]}}}`, []string{"src/lib"}, true},
		{"other directory", `{"compilerOptions":{"baseUrl":"./app","paths":{"@/*":["./app/*"]}}}`, []string{"app"}, true},
		{"already root", `{"compilerOptions":{"baseUrl":".","paths":{"@/*":["./src/*"]}}}`, []string{"src"}, false},
		{"no baseUrl", `{"compilerOptions":{"paths":{"@/*":["./src/*"]}}}`, []string{"src"}, false},
		{"no paths", `{"compilerOptions":{"baseUrl":"./src"}}`, []string{"src"}, false},
		// A target relative to baseUrl is a valid, different config.
		{"targets relative to baseUrl", `{"compilerOptions":{"baseUrl":"./src","paths":{"@/*":["./*"]}}}`, []string{"src"}, false},
		{"target also exists under baseUrl", `{"compilerOptions":{"baseUrl":"./src","paths":{"@/*":["./src/*"]}}}`, []string{"src/src"}, false},
		{"one target not repeating baseUrl", `{"compilerOptions":{"baseUrl":"./src","paths":{"@/*":["./src/*"],"~/*":["./components/*"]}}}`, []string{"src", "src/components"}, false},
		{"target missing on disk", `{"compilerOptions":{"baseUrl":"./src","paths":{"@/*":["./src/*"]}}}`, nil, false},
		{"baseUrl outside root", `{"compilerOptions":{"baseUrl":"../src","paths":{"@/*":["../src/*"]}}}`, []string{"src"}, false},
		{"not valid json", `{"compilerOptions":{"baseUrl":"./src",`, []string{"src"}, false},
		{"baseUrl in a comment", "{\n// \"baseUrl\": \"./src\",\n\"compilerOptions\":{\"paths\":{\"@/*\":[\"./src/*\"]}}}", []string{"src"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := writeProject(t, tc.src, tc.dirs...)
			out, changes, ok := fixTSConfig(root)
			if ok != tc.fixed {
				t.Fatalf("fixed = %v, want %v (changes %+v)", ok, tc.fixed, changes)
			}
			if !ok {
				return
			}
			if !strings.Contains(out, `"baseUrl":"."`) && !strings.Contains(out, `"baseUrl": "."`) {
				t.Fatalf("baseUrl not rewritten: %s", out)
			}
			// Everything else is untouched.
			if strings.Count(out, "paths") != strings.Count(tc.src, "paths") {
				t.Fatalf("output lost content: %s", out)
			}
		})
	}
}

// baseUrl in an unrelated object (a plugin option) must not be edited.
func TestFixTSConfigBaseURLOnlyEditsCompilerOptions(t *testing.T) {
	src := `{
  "plugins": [{ "baseUrl": "./src" }],
  "compilerOptions": {
    "baseUrl": "./src",
    "paths": { "@/*": ["./src/*"] }
  }
}`
	root := writeProject(t, src, "src")
	out, _, ok := fixTSConfig(root)
	if !ok {
		t.Fatal("expected a fix")
	}
	if !strings.Contains(out, `"plugins": [{ "baseUrl": "./src" }]`) || !strings.Contains(out, `"baseUrl": ".",`) {
		t.Fatalf("wrong value edited:\n%s", out)
	}
}

func TestRunAppliesTSConfigFixAndIsIdempotent(t *testing.T) {
	root := writeProject(t, tsconfig040, "src")
	res, err := Run(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 1 || res.Changes[0].Code != diag.CodeTSConfigBaseURL {
		t.Fatalf("dry run = %+v", res)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "tsconfig.json")); string(got) != tsconfig040 {
		t.Fatal("dry run wrote tsconfig.json")
	}
	if _, err := Run(root, true); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "tsconfig.json"))
	if !strings.Contains(string(got), `"baseUrl": "."`) {
		t.Fatalf("apply did not rewrite: %s", got)
	}
	res, err = Run(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 0 {
		t.Fatalf("second apply should be a no-op: %+v", res.Changes)
	}
}

func TestPlanWithCodesIncludesTSConfig(t *testing.T) {
	root := writeProject(t, tsconfig040, "src")
	files, changes, _, err := PlanWithCodes(root, []string{string(diag.CodeTSConfigBaseURL)})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !strings.Contains(files["tsconfig.json"], `"baseUrl": "."`) {
		t.Fatalf("files=%v changes=%+v", files, changes)
	}
	files, changes, _, err = PlanWithCodes(root, []string{"KR5002"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 || len(files) != 0 {
		t.Fatalf("filter ignored: files=%v changes=%+v", files, changes)
	}
}
