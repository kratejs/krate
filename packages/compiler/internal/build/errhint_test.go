package build

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// TestBuildFailurePrintsAliasHint checks that the summary of a failed build
// carries the hint naming the alias base directory. A baseUrl that does not
// match the alias targets is otherwise impossible to see from the error.
func TestBuildFailurePrintsAliasHint(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"tsconfig.json":                  `{"compilerOptions":{"baseUrl":"./src","paths":{"@/*":["./src/*"]}}}`,
		"src/components/ui/PageHead.tsx": `export function PageHead() { return <title>t</title>; }`,
		"src/pages/index.tsx": `import { PageHead } from "@/components/ui/PageHead";
export default function Page() { return <main><PageHead /></main>; }`,
	}
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
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.LoadTSConfigPaths(root)
	err := New(root, cfg).BuildAll()
	if err == nil {
		t.Fatal("expected the build to fail: ./src/* is not under baseUrl ./src")
	}
	msg := err.Error()
	if !strings.Contains(msg, "hint: no file exists at the alias target") {
		t.Fatalf("summary lost the alias hint:\n%s", msg)
	}
	if !strings.Contains(msg, filepath.Join(root, "src")+")") {
		t.Errorf("hint does not name the base directory:\n%s", msg)
	}
}

func TestErrWithHint(t *testing.T) {
	d := diag.New("a.tsx", 1, 1, "boom", "try this", "")
	if got := errWithHint(fmt.Errorf("bundling: %w", d)); !strings.HasSuffix(got, "\n      hint: try this") {
		t.Errorf("wrapped diagnostic lost its hint: %q", got)
	}
	// diagList already prints hints.
	list := &diagList{errs: []error{d}}
	if got := errWithHint(fmt.Errorf("x: %w", list)); strings.Count(got, "try this") != 1 {
		t.Errorf("hint repeated: %q", got)
	}
	if got := errWithHint(errors.New("plain")); got != "plain" {
		t.Errorf("plain error changed: %q", got)
	}
}

func TestDynamicRouteTemplateDoesNotReportSlots(t *testing.T) {
	warnings := []diag.Diagnostic{
		{Message: "slot", Code: diag.CodeSlotUnresolved},
		{Message: "other", Code: diag.CodeTierDecorator},
	}
	got := withoutTemplateSlotWarnings(warnings)
	if len(got) != 1 || got[0].Message != "other" {
		t.Errorf("got %+v", got)
	}
}

func TestClientModuleStubKeepsExportNames(t *testing.T) {
	src := `"use client";
export function Counter() { return null; }
export const Badge = () => null;
function Hidden() { return null; }
export { Hidden as Shown };
export default function Page() { return null; }
`
	prog, errs := tsstrip.Parse("a.tsx", src, tsstrip.Options{})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	got := clientModuleStub(prog)
	for _, want := range []string{"export const Counter", "export const Badge", "export const Shown", "export default"} {
		if !strings.Contains(got, want) {
			t.Errorf("stub lacks %q:\n%s", want, got)
		}
	}
}
