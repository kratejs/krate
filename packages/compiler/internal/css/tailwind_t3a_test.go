package css

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateLayeredAndThemeVars(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src/p.tsx"), `<div class="bg-brand-500 p-4 flex">Don't</div>`)
	writeFile(t, filepath.Join(root, "src/g.css"), `@import "tailwindcss";
@theme { --color-brand-500: #f00; }
.x { background: var(--color-red-500); }`)
	opts := TailwindOptions{Preflight: true}
	d := ScanTailwindDirectives(root, opts)
	res, err := GenerateTailwindDirectives(root, LoadTailwindConfigWithOptions(root, opts), opts, d)
	if err != nil {
		t.Fatal(err)
	}
	out := res.CSS
	order := []string{"@layer theme, base, components, utilities;", "@layer theme{:root,:host{", "--color-brand-500:#f00", "--color-red-500:", "@layer base{", "@layer utilities{"}
	last := -1
	for _, o := range order {
		i := strings.Index(out, o)
		if i < 0 || i < last {
			t.Fatalf("%q missing or out of order in:\n%s", o, out)
		}
		last = i
	}
	if !strings.Contains(out, "background-color: var(--color-brand-500)") {
		t.Errorf("bg-brand-500 not generated from @theme:\n%s", out)
	}
	if strings.Index(out, "box-sizing") > strings.Index(out, "@layer utilities{") {
		t.Errorf("preflight must sit in base layer before utilities")
	}
	// Determinism.
	res2, _ := GenerateTailwindDirectives(root, LoadTailwindConfigWithOptions(root, opts), opts, ScanTailwindDirectives(root, opts))
	if res2.CSS != out {
		t.Error("output not deterministic")
	}
}

func TestDirectivesImportPrefixSourceUtilityVariant(t *testing.T) {
	root := t.TempDir()
	css := `@import "tailwindcss" prefix(tw) important;
@import "tailwindcss/preflight";
@source "../other";
@source not "../skip";
@source inline("p-{1,2} underline");
@config "./x.js";
@plugin "@tailwindcss/typography";
@utility tab-4 { tab-size: 4; }
@utility tab-* { tab-size: --value(integer); }
@custom-variant theme-mid (&:where([data-theme=mid] *));
@custom-variant print2 (@media print);
@custom-variant hov { &:hover { @slot; } }
.keep{color:red}`
	d := NewTailwindDirectives(root)
	d.Add(css, filepath.Join(root, "src"))
	if d.Prefix != "tw" || !d.Important || !d.Imported {
		t.Errorf("import modifiers: %+v", d)
	}
	if len(d.Sources) != 1 || d.Sources[0] != filepath.Join(root, "other") || len(d.Excludes) != 1 {
		t.Errorf("sources=%v excludes=%v", d.Sources, d.Excludes)
	}
	if strings.Join(d.Inline, " ") != "p-1 p-2 underline" {
		t.Errorf("inline = %v", d.Inline)
	}
	if d.Utilities["tab-4"] != "tab-size: 4;" || len(d.Utilities) != 1 {
		t.Errorf("utilities = %v", d.Utilities)
	}
	if d.Variants["theme-mid"] != "&:where([data-theme=mid] *)" || d.Variants["print2"] != "@media print" || d.Variants["hov"] != "&:hover" {
		t.Errorf("variants = %v", d.Variants)
	}
	w := strings.Join(d.Warnings(), "\n")
	if d.Functional["tab"] != "tab-size: --value(integer);" {
		t.Errorf("functional utilities = %v", d.Functional)
	}
	for _, want := range []string{"@config", "@plugin"} {
		if !strings.Contains(w, want) {
			t.Errorf("no warning mentioning %q:\n%s", want, w)
		}
	}
	out := StripTailwindDirectives(css)
	if strings.TrimSpace(out) != ".keep{color:red}" {
		t.Errorf("strip left: %q", out)
	}
	// Source outside the root is refused with a warning.
	d2 := NewTailwindDirectives(root)
	d2.Add(`@source "../../etc";`, root)
	if len(d2.Sources) != 0 || len(d2.Warnings()) == 0 {
		t.Errorf("outside-root source accepted: %v", d2.Sources)
	}
}
