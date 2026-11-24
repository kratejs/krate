package build

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

func TestBoostDevGCRestoresTheCollectorSettings(t *testing.T) {
	t.Setenv("GOGC", "")
	os.Unsetenv("GOGC")
	before := debug.SetGCPercent(100)
	defer debug.SetGCPercent(before)
	limitBefore := debug.SetMemoryLimit(-1)
	defer debug.SetMemoryLimit(limitBefore)

	restore := boostDevGC()
	if got := debug.SetGCPercent(devRebuildGCPercent); got != devRebuildGCPercent {
		t.Fatalf("GC percent during a rebuild is %d, want %d", got, devRebuildGCPercent)
	}
	restore()
	if got := debug.SetGCPercent(100); got != 100 {
		t.Fatalf("GC percent after the rebuild is %d, want the 100 it was", got)
	}
	if got := debug.SetMemoryLimit(-1); got != limitBefore {
		t.Fatalf("memory limit after the rebuild is %d, want %d", got, limitBefore)
	}
}

func TestBoostDevGCLeavesAUsersGOGCAlone(t *testing.T) {
	t.Setenv("GOGC", "50")
	before := debug.SetGCPercent(50)
	defer debug.SetGCPercent(before)
	restore := boostDevGC()
	defer restore()
	if got := debug.SetGCPercent(50); got != 50 {
		t.Fatalf("a GOGC the user set was changed to %d", got)
	}
}

// The bundles are scanned side by side; the languages of all of them are
// found, and a bundle that names none adds none.
func TestWriteHighlightTablesFindsTheLanguagesOfEveryBundle(t *testing.T) {
	b, _ := scProject(t, nil)
	bundles := map[string]string{}
	for name, src := range map[string]string{
		"a.mjs": `h(Code, { lang: "go", code: x })`,
		"b.mjs": `h(Code, { lang: 'ts', code: y })`,
		"c.mjs": `export const nothing = 1;`,
	} {
		rel := filepath.Join(".krate", "server-bundles", name)
		p := filepath.Join(b.Cfg.OutDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		bundles[name] = rel
	}
	if err := b.writeHighlightTables(bundles); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(b.Cfg.OutDir, ".krate", "highlight.json"))
	if err != nil {
		t.Fatalf("no highlight tables written: %v", err)
	}
	for _, want := range []string{"go", "typescript"} {
		if !strings.Contains(strings.ToLower(string(data)), want) {
			t.Errorf("highlight tables hold no %q lexer", want)
		}
	}
}
