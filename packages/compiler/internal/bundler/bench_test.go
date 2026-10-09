package bundler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// BenchmarkBundleSharedGraph bundles N entry modules that all import the same
// large shared module — the shape that dominated builds before the module cache
// (every page re-parsed the shared theme/components/runtime graph).
func BenchmarkBundleSharedGraph(b *testing.B) {
	dir := b.TempDir()
	var sb strings.Builder
	sb.WriteString("import { createSignal } from '@krate/runtime';\n")
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&sb, "export function C%d(props) {\n  const [n, setN] = createSignal(0);\n  return <button onClick={() => setN(n() + 1)}>{props.label}{n()}</button>;\n}\n", i)
	}
	shared := sb.String()
	if err := os.WriteFile(filepath.Join(dir, "shared.tsx"), []byte(shared), 0644); err != nil {
		b.Fatal(err)
	}
	const n = 10
	entries := make([]string, n)
	for i := range entries {
		p := filepath.Join(dir, fmt.Sprintf("entry%d.tsx", i))
		src := "import { C0 } from './shared';\nexport default function Page() {\n  return <C0 label=\"hi\" />;\n}\n"
		if err := os.WriteFile(p, []byte(src), 0644); err != nil {
			b.Fatal(err)
		}
		entries[i] = p
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache := NewModuleCache()
		for _, e := range entries {
			bnd := New(dir)
			bnd.SetModuleCache(cache)
			if _, err := bnd.Bundle(e); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkBundleSharedGraphNoCache models the pre-cache behaviour: a fresh
// bundle (and cache) per entry, so the shared module is re-parsed every time.
func BenchmarkBundleSharedGraphNoCache(b *testing.B) {
	dir := b.TempDir()
	var sb strings.Builder
	sb.WriteString("import { createSignal } from '@krate/runtime';\n")
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&sb, "export function C%d(props) {\n  const [n, setN] = createSignal(0);\n  return <button onClick={() => setN(n() + 1)}>{props.label}{n()}</button>;\n}\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "shared.tsx"), []byte(sb.String()), 0644); err != nil {
		b.Fatal(err)
	}
	const n = 10
	entries := make([]string, n)
	for i := range entries {
		p := filepath.Join(dir, fmt.Sprintf("entry%d.tsx", i))
		src := "import { C0 } from './shared';\nexport default function Page() {\n  return <C0 label=\"hi\" />;\n}\n"
		if err := os.WriteFile(p, []byte(src), 0644); err != nil {
			b.Fatal(err)
		}
		entries[i] = p
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, e := range entries {
			bnd := New(dir) // fresh cache each time
			if _, err := bnd.Bundle(e); err != nil {
				b.Fatal(err)
			}
		}
	}
}
