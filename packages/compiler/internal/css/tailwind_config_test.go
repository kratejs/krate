package css

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTailwindConfigStatic(t *testing.T) {
	dir := t.TempDir()
	src := `export default {
  darkMode: 'class',
  theme: {
    extend: {
      colors: { brand: { 500: '#123456' } },
      screens: { '3xl': '1920px' },
    },
    maxWidth: { xxl: '100rem' },
  },
}`
	path := filepath.Join(dir, "tailwind.config.ts")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, ok := ParseTailwindConfigStatic(path)
	if !ok {
		t.Fatal("static parse failed")
	}
	if cfg.Theme.DarkMode != "class" {
		t.Errorf("darkMode = %q, want class", cfg.Theme.DarkMode)
	}
	// extend merges: default screens preserved + 3xl added.
	if cfg.Theme.Screens["sm"] == "" || cfg.Theme.Screens["3xl"] != "1920px" {
		t.Errorf("screens extend failed: %v", cfg.Theme.Screens)
	}
	// extend colors merge.
	if cfg.Theme.Colors["brand"]["500"] != "#123456" {
		t.Errorf("colors extend failed: %v", cfg.Theme.Colors["brand"])
	}
	// top-level maxWidth replaces.
	if cfg.Theme.MaxWidth["xxl"] != "100rem" {
		t.Errorf("maxWidth override failed: %v", cfg.Theme.MaxWidth)
	}
	// Replaced key should not retain old values.
	if _, ok := cfg.Theme.MaxWidth["lg"]; ok {
		t.Errorf("maxWidth should have been replaced, still has lg")
	}
}

func TestExecuteTailwindConfigInProcess(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "helper.ts"), []byte("export const brand = { 500: '#123456' };\n"), 0644); err != nil {
		t.Fatal(err)
	}
	src := `import { brand } from './helper';
export default { darkMode: 'class', theme: { colors: { brand } } };
`
	if err := os.WriteFile(filepath.Join(dir, "tailwind.config.ts"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	// ExecuteConfig forces the in-process (esbuild + QuickJS) path over the static parser.
	cfg := LoadTailwindConfigWithOptions(dir, TailwindOptions{ExecuteConfig: true})
	if cfg.Theme.DarkMode != "class" {
		t.Errorf("darkMode = %q, want class", cfg.Theme.DarkMode)
	}
	if got := cfg.Theme.Colors["brand"]["500"]; got != "#123456" {
		t.Errorf("imported color not resolved: brand.500 = %q (colors=%v)", got, cfg.Theme.Colors)
	}
}

func TestScannerCandidateExtraction(t *testing.T) {
	dir := t.TempDir()
	src := `import { cva } from 'x';
const btn = cva('inline-flex items-center', { variants: { size: { lg: 'text-lg px-8' } } });
export const A = () => <div class={on ? "bg-blue-500" : "bg-gray-200"} className={cn("p-4", x && "mt-2")} />;
`
	path := filepath.Join(dir, "a.tsx")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	classes := NewTailwindScanner(dir).ScanClasses([]string{dir})
	for _, want := range []string{"inline-flex", "items-center", "text-lg", "px-8", "bg-blue-500", "bg-gray-200", "p-4", "mt-2"} {
		if !classes[want] {
			t.Errorf("scanner missed %q (got %v)", want, classes)
		}
	}
	// Prose must not be picked up.
	if classes["import"] || classes["const"] || classes["variants"] {
		t.Errorf("scanner picked up identifiers: %v", classes)
	}
}

func TestScannerSkipsOutputDirs(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "dist")
	if err := os.MkdirAll(out, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "index.html"), []byte(`<div class="bg-red-500"></div>`), 0644); err != nil {
		t.Fatal(err)
	}
	classes := NewTailwindScanner(dir).ScanClasses([]string{dir})
	if classes["bg-red-500"] {
		t.Error("scanner must not scan dist/ output")
	}
}
