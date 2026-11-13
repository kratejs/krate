package css

import (
	"os"
	"path/filepath"
	"testing"
)

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
