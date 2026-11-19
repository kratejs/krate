package bundler

import (
	"os"
	"path/filepath"
	"testing"
)

// A bundle that misses a shared module in the cache, while another bundle
// parses it and ends its claim before this one claims the parse, uses that
// parse instead of parsing the module again.
func TestSharedModuleParsedOnceWhenAnotherBundleFinishesFirst(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"shared.ts": "export const x = 1;\n",
		"a.tsx":     "import { x } from './shared';\nexport default function A() { return <p>{x}</p>; }\n",
		"b.tsx":     "import { x } from './shared';\nexport default function B() { return <p>{x}</p>; }\n",
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cache := NewModuleCache()
	bundle := func(page string) {
		b := New(dir)
		b.SetModuleCache(cache)
		if _, err := b.Bundle(filepath.Join(dir, page)); err != nil {
			t.Fatal(err)
		}
	}
	shared, err := filepath.EvalSymlinks(filepath.Join(dir, "shared.ts"))
	if err != nil {
		t.Fatal(err)
	}
	ran := false
	testAfterProgramMiss = func(abs string) {
		if ran {
			return
		}
		if real, err := filepath.EvalSymlinks(abs); err != nil || real != shared {
			return
		}
		ran = true
		// The other bundle parses shared.ts start to finish right here.
		bundle("b.tsx")
	}
	t.Cleanup(func() { testAfterProgramMiss = nil })
	bundle("a.tsx")
	if !ran {
		t.Fatal("the hook never saw shared.ts")
	}
	for p, n := range cache.ParseCounts() {
		if real, err := filepath.EvalSymlinks(p); err == nil && real == shared && n != 1 {
			t.Errorf("shared.ts parsed %d times, want once", n)
		}
	}
}
